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
		return &r2ProvisionResult{
			Bucket:        opts.Bucket,
			Namespace:     namespace,
			SecretName:    secretName,
			Endpoint:      h.r2Provisioner.EndpointURL(),
			Action:        r2ActionAdopted,
			BucketAdopted: true,
			SecretKeys:    r2SecretKeyNames(),
		}, nil
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

	// Mirror into Vault when it is wired, matching the secret-intake
	// convention of secret/<project>. Failure here is a warning, not a
	// failure: the K8s Secret is already the source of truth for the running
	// service.
	if path, err := h.mirrorR2CredentialsToVault(ctx, opts.Project, creds); err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("credentials written to %s/%s but the Vault mirror failed: %v", namespace, secretName, err))
	} else if path != "" {
		result.VaultPath = path
	}

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

// mirrorR2CredentialsToVault writes the credential set to Vault when Vault is
// configured. Returns the path written, or "" when Vault is not wired.
func (h *Handler) mirrorR2CredentialsToVault(
	ctx context.Context, project string, creds *provisioning.R2Credentials,
) (string, error) {
	if h.vaultClient == nil || !h.vaultClient.IsEnabled() {
		return "", nil
	}
	path := "secret/" + project
	updates := make(map[string]interface{}, 5)
	for _, entry := range creds.Entries() {
		updates[entry.Key] = entry.Value
	}
	if _, err := h.vaultClient.MergeSecretData(ctx, path, updates); err != nil {
		return "", err
	}
	return path, nil
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
