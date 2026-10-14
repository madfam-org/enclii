package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
)

// Object-storage (Cloudflare R2) provisioning shared by three callers:
//
//	day-2  POST /v1/projects/:slug/storage/buckets   (enclii storage create)
//	day-1  POST /v1/onboard/repo  --r2-bucket        (enclii onboard)
//	admin  POST /v1/admin/provision/r2
//
// Routing all three through ensureProjectR2Bucket is the point: the day-1 path
// used to emit STORAGE_BACKEND=r2 with no access keys, which left a service
// configured for object storage that it could not authenticate to. There is
// now one implementation, and it either produces a complete bucket-scoped
// credential set or fails.

// Actions reported by ensureProjectR2Bucket.
const (
	r2ActionProvisioned = "provisioned"
	r2ActionAdopted     = "adopted"
	r2ActionRotated     = "rotated"
)

var (
	errR2NotConfigured = errors.New(
		"R2 provisioning is not configured on switchyard-api (CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID)")
	errR2SecretsNotConfigured = errors.New(
		"R2 credentials cannot be persisted: the Kubernetes secrets provisioner is not configured")
)

// r2ConflictError reports that a bucket is already bound to a different
// service. Binding it anyway is precisely the failure this package exists to
// prevent, so it is refused rather than warned about.
type r2ConflictError struct {
	Bucket string
	Holder string
	Reason string
}

func (e *r2ConflictError) Error() string {
	return fmt.Sprintf("bucket %q is already bound to %s (%s); refusing to rebind it. "+
		"Choose a bucket name owned by this project, or unbind the existing holder first",
		e.Bucket, e.Holder, e.Reason)
}

// r2RebindError reports that the target Secret already points at a different
// bucket.
type r2RebindError struct {
	Namespace  string
	SecretName string
	Existing   string
	Requested  string
}

func (e *r2RebindError) Error() string {
	return fmt.Sprintf("%s/%s is already bound to bucket %q; refusing to silently repoint it at %q. "+
		"Destroy the existing binding first if the move is intended",
		e.Namespace, e.SecretName, e.Existing, e.Requested)
}

// r2ProvisionOptions describes a day-2 bucket provisioning request.
type r2ProvisionOptions struct {
	// Project is the project slug; it also seeds the namespace and Secret name.
	Project string
	// Namespace defaults to the project slug.
	Namespace string
	// SecretName defaults to <project>-credentials.
	SecretName string
	Bucket     string
	// Rotate forces a fresh token even when a complete credential set exists.
	Rotate bool
}

// r2ProvisionResult is the non-secret outcome of provisioning. Credentials are
// never returned over the wire — only a reference to where they were written,
// mirroring how addon create returns a Secret reference rather than a password.
type r2ProvisionResult struct {
	Bucket        string   `json:"bucket"`
	Namespace     string   `json:"namespace"`
	SecretName    string   `json:"secret_name"`
	Endpoint      string   `json:"endpoint"`
	Action        string   `json:"action"`
	BucketAdopted bool     `json:"bucket_adopted"`
	SecretKeys    []string `json:"secret_keys"`
	VaultPath     string   `json:"vault_path,omitempty"`
	// PreviousAccessKeyID is set on rotation. It is the superseded Cloudflare
	// token ID — the non-secret half of the pair, and the handle needed to
	// revoke the old credential after the service redeploys. The secret half
	// is never returned.
	PreviousAccessKeyID string   `json:"previous_access_key_id,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
}

func (o r2ProvisionOptions) namespace() string {
	if ns := strings.TrimSpace(o.Namespace); ns != "" {
		return ns
	}
	return strings.TrimSpace(o.Project)
}

func (o r2ProvisionOptions) secretName() string {
	if name := strings.TrimSpace(o.SecretName); name != "" {
		return name
	}
	return strings.TrimSpace(o.Project) + "-credentials"
}

// ensureProjectR2Bucket creates or adopts a bucket, mints a bucket-scoped
// credential set for it, and persists that set where the platform expects
// service secrets.
//
// It is idempotent: a project that already holds a complete credential set for
// the same bucket is adopted without minting a new token, so re-running the
// command does not churn live credentials. It never rebinds a bucket that
// belongs to a different namespace.
func (h *Handler) ensureProjectR2Bucket(ctx context.Context, opts r2ProvisionOptions) (*r2ProvisionResult, error) {
	if h.r2Provisioner == nil {
		return nil, errR2NotConfigured
	}
	if h.secretsProvisioner == nil {
		return nil, errR2SecretsNotConfigured
	}
	if strings.TrimSpace(opts.Project) == "" {
		return nil, errors.New("project is required")
	}
	if err := provisioning.ValidateR2BucketName(opts.Bucket); err != nil {
		return nil, err
	}

	namespace := opts.namespace()
	secretName := opts.secretName()

	auditor := h.secretsProvisioner.R2Auditor()
	if auditor == nil {
		return nil, errR2SecretsNotConfigured
	}

	// Ownership guard. Fails closed: if cluster state cannot be read we do not
	// know whether the bucket is free, and binding on a guess is how a service
	// ends up writing into another platform's bucket.
	existing, err := h.checkR2BucketOwnership(ctx, auditor, namespace, secretName, opts.Bucket)
	if err != nil {
		return nil, err
	}

	// Idempotent adoption: complete credentials for this exact bucket already
	// exist, so leave the working credential alone.
	if !opts.Rotate && existing.Complete() && existing.Bucket == opts.Bucket {
		h.logger.Info(ctx, "Adopted existing R2 binding",
			logging.String("project", opts.Project),
			logging.String("namespace", namespace),
			logging.String("bucket", opts.Bucket))
		result := &r2ProvisionResult{
			Bucket:        opts.Bucket,
			Namespace:     namespace,
			SecretName:    secretName,
			Endpoint:      h.r2Provisioner.EndpointURL(),
			Action:        r2ActionAdopted,
			BucketAdopted: true,
			SecretKeys:    r2SecretKeyNames(),
		}
		// A mirror that failed when the credentials were minted (the writer
		// policy had no path for this project, say) is repaired here, so
		// re-running the command once the cause is fixed is the whole remedy
		// and needs no rotation.
		h.repairR2VaultMirror(ctx, opts, result)
		return result, nil
	}

	// Capture the outgoing access key ID before overwriting it. Rotation
	// deliberately does NOT revoke the old token — the running pod still holds
	// it and would start failing immediately — but an unrevoked credential
	// nobody is tracking is the same class of problem this whole path exists
	// to fix, so the caller is told exactly what to revoke after cutover.
	previousAccessKeyID := ""
	if existing.HasAccessKeyID {
		if id, keyErr := auditor.AccessKeyID(ctx, namespace, secretName); keyErr == nil {
			previousAccessKeyID = id
		}
	}

	creds, err := h.r2Provisioner.ProvisionBucket(ctx, opts.Bucket)
	if err != nil {
		return nil, err
	}

	annotations := map[string]string{
		provisioning.AnnotationR2Bucket:        creds.BucketName,
		provisioning.AnnotationR2Project:       opts.Project,
		provisioning.AnnotationR2TokenName:     creds.TokenName,
		provisioning.AnnotationR2ProvisionedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := h.secretsProvisioner.AppendEntriesWithAnnotations(
		ctx, namespace, opts.Project, secretName, creds.Entries(), annotations,
	); err != nil {
		return nil, fmt.Errorf("persist R2 credentials to %s/%s: %w", namespace, secretName, err)
	}

	action := r2ActionProvisioned
	if opts.Rotate && existing.Complete() {
		action = r2ActionRotated
	}

	result := &r2ProvisionResult{
		Bucket:        creds.BucketName,
		Namespace:     namespace,
		SecretName:    secretName,
		Endpoint:      creds.EndpointURL,
		Action:        action,
		BucketAdopted: creds.BucketAdopted,
		SecretKeys:    r2SecretKeyNames(),
	}

	if previousAccessKeyID != "" && previousAccessKeyID != creds.AccessKeyID {
		result.PreviousAccessKeyID = previousAccessKeyID
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"the previous Cloudflare token %s is still valid so the running service keeps working; "+
				"revoke it once the service has redeployed onto the new credentials",
			previousAccessKeyID))
	}

	// Mirror into Vault when it is wired (see r2VaultMirrorPath). Failure here
	// is a warning, not a failure: the K8s Secret is already the source of
	// truth for the running service.
	updates := make(map[string]interface{}, 5)
	for _, entry := range creds.Entries() {
		updates[entry.Key] = entry.Value
	}
	h.mirrorR2CredentialsToVault(ctx, opts, updates, result)

	// Deliberately logs the bucket and destination only. The access key ID is
	// audit-relevant but is emitted by the provisioner itself; the secret half
	// is never logged anywhere.
	h.logger.Info(ctx, "Provisioned R2 bucket credentials",
		logging.String("project", opts.Project),
		logging.String("namespace", namespace),
		logging.String("secret", secretName),
		logging.String("bucket", creds.BucketName),
		logging.String("action", action))

	return result, nil
}

// checkR2BucketOwnership returns the binding currently recorded in the target
// Secret, after verifying that no other namespace already claims the bucket.
func (h *Handler) checkR2BucketOwnership(
	ctx context.Context,
	auditor *provisioning.R2Auditor,
	namespace, secretName, bucket string,
) (provisioning.R2SecretBinding, error) {
	bindings, err := auditor.Scan(ctx, nil)
	if err != nil {
		return provisioning.R2SecretBinding{}, fmt.Errorf(
			"cannot verify that bucket %q is unclaimed before binding it: %w", bucket, err)
	}

	for _, b := range bindings {
		if b.Namespace == namespace {
			continue
		}
		switch {
		case b.ProvisionedBucket == bucket:
			return provisioning.R2SecretBinding{}, &r2ConflictError{
				Bucket: bucket, Holder: b.Ref(), Reason: "provisioned by enclii for that service",
			}
		case b.Bucket == bucket:
			return provisioning.R2SecretBinding{}, &r2ConflictError{
				Bucket: bucket, Holder: b.Ref(), Reason: "referenced by that service's credentials",
			}
		}
	}

	existing, err := auditor.GetBinding(ctx, namespace, secretName)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return provisioning.R2SecretBinding{}, fmt.Errorf(
				"read existing credentials at %s/%s: %w", namespace, secretName, err)
		}
		return provisioning.R2SecretBinding{Namespace: namespace, SecretName: secretName}, nil
	}

	if existing.Bucket != "" && existing.Bucket != bucket {
		return provisioning.R2SecretBinding{}, &r2RebindError{
			Namespace: namespace, SecretName: secretName,
			Existing: existing.Bucket, Requested: bucket,
		}
	}
	return existing, nil
}

// r2VaultMirrorPath is the Vault path a binding's credentials are mirrored to.
//
// A project's canonical binding (its own namespace and the default
// <project>-credentials Secret) keeps secret/<project>, the secret-intake
// convention: ExternalSecrets read R2 keys from there (symbiosis-hcm's backup
// job does). Every other binding gets a sub-path of its own. The five key
// names are the same for every bucket, so a second binding mirrored into
// secret/<project> would overwrite the first one's credentials, and an
// ExternalSecret reading them would hand a service a token scoped to the
// wrong bucket.
func r2VaultMirrorPath(opts r2ProvisionOptions) string {
	project := strings.TrimSpace(opts.Project)
	path := "secret/" + project
	if opts.namespace() == project && opts.secretName() == project+"-credentials" {
		return path
	}
	return path + "/" + opts.namespace() + "/" + opts.secretName()
}

// mirrorR2CredentialsToVault merges the credential set into Vault when Vault
// is configured, recording the path on success and a warning on failure.
func (h *Handler) mirrorR2CredentialsToVault(
	ctx context.Context, opts r2ProvisionOptions, updates map[string]interface{}, result *r2ProvisionResult,
) {
	if h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		return
	}
	path := r2VaultMirrorPath(opts)
	if _, err := h.vaultClient.MergeSecretData(ctx, path, updates); err != nil {
		result.Warnings = append(result.Warnings, r2VaultMirrorWarning(opts, result, path, err))
		return
	}
	result.VaultPath = path
}

// repairR2VaultMirror brings the Vault copy of an adopted binding up to date
// from its Kubernetes Secret. It writes only when the copy is missing or
// differs, so re-running against a healthy binding leaves Vault untouched.
// The values stay server-side, as with every Lookup caller.
func (h *Handler) repairR2VaultMirror(ctx context.Context, opts r2ProvisionOptions, result *r2ProvisionResult) {
	if h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		return
	}
	path := r2VaultMirrorPath(opts)
	updates := make(map[string]interface{}, 5)
	for _, key := range r2SecretKeyNames() {
		value, ok, err := h.secretsProvisioner.Lookup(ctx, result.Namespace, result.SecretName, key)
		if err != nil {
			result.Warnings = append(result.Warnings, r2VaultMirrorWarning(opts, result, path, err))
			return
		}
		if ok {
			updates[key] = string(value)
		}
	}
	current, err := h.vaultClient.GetSecretData(ctx, path)
	if err != nil {
		result.Warnings = append(result.Warnings, r2VaultMirrorWarning(opts, result, path, err))
		return
	}
	upToDate := true
	for key, value := range updates {
		if got, ok := current[key].(string); !ok || got != value {
			upToDate = false
			break
		}
	}
	if upToDate {
		result.VaultPath = path
		return
	}
	h.mirrorR2CredentialsToVault(ctx, opts, updates, result)
}

// r2VaultMirrorWarning names the usual cause and the remedy: the
// switchyard-secret-writer policy is a per-project allowlist, so a project
// that has never had a Vault path is refused until one is added.
func r2VaultMirrorWarning(opts r2ProvisionOptions, result *r2ProvisionResult, path string, err error) string {
	project := strings.TrimSpace(opts.Project)
	return fmt.Sprintf("credentials are in %s/%s but the Vault mirror to %s failed: %v. "+
		"If Vault denied it, add the secret/data/%s and secret/metadata/%s paths (each with /*) to "+
		"scripts/provision-switchyard-vault-writer.sh, apply the policy, then re-run this command: "+
		"adoption repairs the mirror without rotating the token",
		result.Namespace, result.SecretName, path, err, project, project)
}

// r2SecretKeyNames lists the keys written into the service Secret. Names only.
func r2SecretKeyNames() []string {
	return []string{
		provisioning.SecretKeyR2Bucket,
		provisioning.SecretKeyR2Endpoint,
		provisioning.SecretKeyR2AccessKeyID,
		provisioning.SecretKeyR2SecretAccessKey,
		provisioning.SecretKeyStorageBackend,
	}
}

// r2ProvisionStatusCode maps a provisioning error onto an HTTP status.
func r2ProvisionStatusCode(err error) int {
	var conflict *r2ConflictError
	var rebind *r2RebindError
	var forbidden *provisioning.TokenMintForbiddenError
	switch {
	case errors.As(err, &conflict), errors.As(err, &rebind):
		return 409
	case errors.As(err, &forbidden):
		// The platform's own Cloudflare token is under-permissioned. That is a
		// server configuration problem, not a bad request.
		return 503
	case errors.Is(err, errR2NotConfigured), errors.Is(err, errR2SecretsNotConfigured):
		return 503
	case strings.Contains(err.Error(), "is invalid"), strings.Contains(err.Error(), "is required"):
		return 400
	default:
		return 500
	}
}
