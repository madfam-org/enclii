package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Annotations recorded on a Secret whose R2 credentials enclii provisioned.
// Their absence is itself a signal: R2 keys with no provenance were placed by
// hand, which is how one service ended up holding another service's token.
const (
	AnnotationR2Bucket        = "enclii.dev/r2-bucket"
	AnnotationR2Project       = "enclii.dev/r2-project"
	AnnotationR2TokenName     = "enclii.dev/r2-token-name" // #nosec G101 -- annotation key, not a token
	AnnotationR2ProvisionedAt = "enclii.dev/r2-provisioned-at"
)

// Finding severities.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
)

// Finding kinds.
const (
	FindingMissingCredentials = "missing_credentials"
	FindingBucketMismatch     = "bucket_mismatch"
	FindingSharedCredentials  = "shared_credentials" // #nosec G101 -- finding label, not a credential
	FindingBucketShared       = "bucket_shared_across_namespaces"
	FindingUnmanaged          = "unmanaged_credentials"
	FindingOrphanCredentials  = "orphan_credentials" // #nosec G101 -- finding label, not a credential
)

// R2SecretBinding is the non-secret summary of the R2 configuration found in
// one Kubernetes Secret.
//
// No field carries secret material. The access key ID is reduced to a
// fingerprint, which is enough to prove two namespaces hold the same
// credential without ever moving the credential.
type R2SecretBinding struct {
	Namespace            string `json:"namespace"`
	SecretName           string `json:"secret_name"`
	Bucket               string `json:"bucket,omitempty"`
	StorageBackend       string `json:"storage_backend,omitempty"`
	HasAccessKeyID       bool   `json:"has_access_key_id"`
	HasSecretAccessKey   bool   `json:"has_secret_access_key"`
	AccessKeyFingerprint string `json:"access_key_fingerprint,omitempty"`
	ProvisionedBucket    string `json:"provisioned_bucket,omitempty"`
	ProvisionedProject   string `json:"provisioned_project,omitempty"`
	Managed              bool   `json:"managed"`
}

// DeclaresR2 reports whether this Secret has anything to do with R2 at all.
func (b R2SecretBinding) DeclaresR2() bool {
	return b.StorageBackend == StorageBackendR2 ||
		b.Bucket != "" ||
		b.HasAccessKeyID ||
		b.HasSecretAccessKey ||
		b.ProvisionedBucket != ""
}

// Complete reports whether the Secret carries a usable, self-contained R2
// credential set.
func (b R2SecretBinding) Complete() bool {
	return b.Bucket != "" && b.HasAccessKeyID && b.HasSecretAccessKey
}

// Ref renders the binding as namespace/secret.
func (b R2SecretBinding) Ref() string {
	return b.Namespace + "/" + b.SecretName
}

// R2Finding is one drift problem discovered by the audit.
type R2Finding struct {
	Severity    string `json:"severity"`
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Secret      string `json:"secret"`
	Bucket      string `json:"bucket,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

// FingerprintAccessKey reduces an access key ID to a short, stable,
// non-reversible tag used to detect credential sharing between namespaces.
func FingerprintAccessKey(accessKeyID string) string {
	if accessKeyID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(accessKeyID))
	return hex.EncodeToString(sum[:])[:12]
}

// R2Auditor scans Kubernetes Secrets for R2 configuration.
type R2Auditor struct {
	clientset kubernetes.Interface
}

// NewR2Auditor creates an auditor over the given cluster client.
func NewR2Auditor(clientset kubernetes.Interface) *R2Auditor {
	return &R2Auditor{clientset: clientset}
}

// Scan collects the R2 binding of every Secret in the given namespaces. An
// empty namespace list scans all namespaces.
func (a *R2Auditor) Scan(ctx context.Context, namespaces []string) ([]R2SecretBinding, error) {
	if a == nil || a.clientset == nil {
		return nil, fmt.Errorf("kubernetes client is not configured")
	}

	targets := namespaces
	if len(targets) == 0 {
		targets = []string{k8smetav1.NamespaceAll}
	}

	bindings := make([]R2SecretBinding, 0)
	for _, ns := range targets {
		list, err := a.clientset.CoreV1().Secrets(ns).List(ctx, k8smetav1.ListOptions{})
		if err != nil {
			scope := ns
			if scope == k8smetav1.NamespaceAll {
				scope = "all namespaces"
			}
			return nil, fmt.Errorf("list secrets in %s: %w", scope, err)
		}
		for i := range list.Items {
			secret := &list.Items[i]
			binding := bindingFromSecret(secret.Namespace, secret.Name, secret.Data, secret.Annotations)
			if !binding.DeclaresR2() {
				continue
			}
			bindings = append(bindings, binding)
		}
	}

	sortBindings(bindings)
	return bindings, nil
}

// GetBinding reads the R2 binding recorded in a single Secret.
//
// A missing Secret returns the Kubernetes NotFound error alongside a binding
// stamped with the namespace and name, so callers can distinguish "no Secret
// yet" (k8serrors.IsNotFound) from "Secret exists but declares no R2 keys"
// (zero-valued binding, nil error).
func (a *R2Auditor) GetBinding(ctx context.Context, namespace, secretName string) (R2SecretBinding, error) {
	if a == nil || a.clientset == nil {
		return R2SecretBinding{}, fmt.Errorf("kubernetes client is not configured")
	}
	secret, err := a.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, k8smetav1.GetOptions{})
	if err != nil {
		return R2SecretBinding{Namespace: namespace, SecretName: secretName}, err
	}
	return bindingFromSecret(namespace, secretName, secret.Data, secret.Annotations), nil
}

// AccessKeyID reads the R2 access key ID from a Secret.
//
// The access key ID is the Cloudflare token ID — the non-secret half of the
// pair, and the handle required to revoke the credential. The secret half is
// never read back out.
func (a *R2Auditor) AccessKeyID(ctx context.Context, namespace, secretName string) (string, error) {
	if a == nil || a.clientset == nil {
		return "", fmt.Errorf("kubernetes client is not configured")
	}
	secret, err := a.clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, k8smetav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(secret.Data[SecretKeyR2AccessKeyID])), nil
}

func bindingFromSecret(namespace, name string, data map[string][]byte, annotations map[string]string) R2SecretBinding {
	get := func(key string) string { return strings.TrimSpace(string(data[key])) }

	binding := R2SecretBinding{
		Namespace:          namespace,
		SecretName:         name,
		Bucket:             get(SecretKeyR2Bucket),
		StorageBackend:     get(SecretKeyStorageBackend),
		HasAccessKeyID:     get(SecretKeyR2AccessKeyID) != "",
		HasSecretAccessKey: get(SecretKeyR2SecretAccessKey) != "",
	}
	binding.AccessKeyFingerprint = FingerprintAccessKey(get(SecretKeyR2AccessKeyID))

	if annotations != nil {
		binding.ProvisionedBucket = strings.TrimSpace(annotations[AnnotationR2Bucket])
		binding.ProvisionedProject = strings.TrimSpace(annotations[AnnotationR2Project])
		binding.Managed = binding.ProvisionedBucket != ""
	}
	return binding
}

func sortBindings(bindings []R2SecretBinding) {
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Namespace == bindings[j].Namespace {
			return bindings[i].SecretName < bindings[j].SecretName
		}
		return bindings[i].Namespace < bindings[j].Namespace
	})
}

// AuditR2Bindings turns a set of bindings into drift findings.
//
// This is the guard for the incident that motivated it: a service was carrying
// another service's R2 token, having been hand-patched because provisioning
// never minted it one. Each rule below catches a distinct part of that
// failure, so no single missing annotation can hide it.
func AuditR2Bindings(bindings []R2SecretBinding) []R2Finding {
	findings := make([]R2Finding, 0)

	byFingerprint := map[string][]R2SecretBinding{}
	byBucket := map[string][]R2SecretBinding{}

	for _, b := range bindings {
		if !b.DeclaresR2() {
			continue
		}

		// 1. The original defect: told to use R2, given no keys.
		if b.StorageBackend == StorageBackendR2 && !b.Complete() {
			missing := make([]string, 0, 3)
			if b.Bucket == "" {
				missing = append(missing, SecretKeyR2Bucket)
			}
			if !b.HasAccessKeyID {
				missing = append(missing, SecretKeyR2AccessKeyID)
			}
			if !b.HasSecretAccessKey {
				missing = append(missing, SecretKeyR2SecretAccessKey)
			}
			findings = append(findings, R2Finding{
				Severity:  SeverityCritical,
				Kind:      FindingMissingCredentials,
				Namespace: b.Namespace,
				Secret:    b.SecretName,
				Bucket:    b.Bucket,
				Message: fmt.Sprintf("declares %s=%s but is missing %s — the service is configured for R2 with no way to authenticate",
					SecretKeyStorageBackend, StorageBackendR2, strings.Join(missing, ", ")),
				Remediation: fmt.Sprintf("enclii buckets create <bucket> --project %s", projectHint(b)),
			})
		}

		// 2. The bucket the service is pointed at is not the bucket enclii
		//    provisioned for it.
		if b.ProvisionedBucket != "" && b.Bucket != "" && b.Bucket != b.ProvisionedBucket {
			findings = append(findings, R2Finding{
				Severity:  SeverityCritical,
				Kind:      FindingBucketMismatch,
				Namespace: b.Namespace,
				Secret:    b.SecretName,
				Bucket:    b.Bucket,
				Message: fmt.Sprintf("%s=%q but enclii provisioned %q for this service — writes are going to the wrong bucket",
					SecretKeyR2Bucket, b.Bucket, b.ProvisionedBucket),
				Remediation: fmt.Sprintf("enclii buckets create %s --project %s", b.ProvisionedBucket, projectHint(b)),
			})
		}

		// 3. Credentials present with no provisioning provenance: placed by
		//    hand, origin unknown.
		if !b.Managed && (b.HasAccessKeyID || b.HasSecretAccessKey) {
			findings = append(findings, R2Finding{
				Severity:  SeverityWarning,
				Kind:      FindingUnmanaged,
				Namespace: b.Namespace,
				Secret:    b.SecretName,
				Bucket:    b.Bucket,
				Message: fmt.Sprintf("R2 credentials carry no %s annotation — enclii did not provision them, so their scope and owner are unverified",
					AnnotationR2Bucket),
				Remediation: fmt.Sprintf("enclii buckets create %s --project %s --rotate", displayBucket(b), projectHint(b)),
			})
		}

		// 4. A key with no bucket to use it on.
		if b.HasSecretAccessKey && b.Bucket == "" && b.StorageBackend != StorageBackendR2 {
			findings = append(findings, R2Finding{
				Severity:  SeverityWarning,
				Kind:      FindingOrphanCredentials,
				Namespace: b.Namespace,
				Secret:    b.SecretName,
				Message:   fmt.Sprintf("holds %s but no %s", SecretKeyR2SecretAccessKey, SecretKeyR2Bucket),
			})
		}

		if b.AccessKeyFingerprint != "" {
			byFingerprint[b.AccessKeyFingerprint] = append(byFingerprint[b.AccessKeyFingerprint], b)
		}
		if b.Bucket != "" {
			byBucket[b.Bucket] = append(byBucket[b.Bucket], b)
		}
	}

	// 5. The same credential in more than one namespace. This is the sharpest
	//    detector for the incident — it needs no annotation and no knowledge
	//    of which bucket belongs to whom.
	for _, group := range sortedGroups(byFingerprint) {
		others := distinctNamespaces(byFingerprint[group])
		if len(others) < 2 {
			continue
		}
		for _, b := range byFingerprint[group] {
			findings = append(findings, R2Finding{
				Severity:  SeverityCritical,
				Kind:      FindingSharedCredentials,
				Namespace: b.Namespace,
				Secret:    b.SecretName,
				Bucket:    b.Bucket,
				Message: fmt.Sprintf("the same R2 access key is installed in %d namespaces (%s) — these services are not isolated from each other's object storage",
					len(others), strings.Join(others, ", ")),
				Remediation: fmt.Sprintf("enclii buckets create %s --project %s --rotate", displayBucket(b), projectHint(b)),
			})
		}
	}

	// 6. One bucket referenced from several namespaces.
	for _, bucket := range sortedGroups(byBucket) {
		others := distinctNamespaces(byBucket[bucket])
		if len(others) < 2 {
			continue
		}
		for _, b := range byBucket[bucket] {
			findings = append(findings, R2Finding{
				Severity:  SeverityCritical,
				Kind:      FindingBucketShared,
				Namespace: b.Namespace,
				Secret:    b.SecretName,
				Bucket:    b.Bucket,
				Message: fmt.Sprintf("bucket %q is referenced from %d namespaces (%s) — at most one project should own it",
					bucket, len(others), strings.Join(others, ", ")),
			})
		}
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			// critical sorts before warning
			return findings[i].Severity == SeverityCritical
		}
		if findings[i].Namespace != findings[j].Namespace {
			return findings[i].Namespace < findings[j].Namespace
		}
		if findings[i].Secret != findings[j].Secret {
			return findings[i].Secret < findings[j].Secret
		}
		return findings[i].Kind < findings[j].Kind
	})
	return findings
}

// CountCritical returns how many findings are blocking.
func CountCritical(findings []R2Finding) int {
	n := 0
	for _, f := range findings {
		if f.Severity == SeverityCritical {
			n++
		}
	}
	return n
}

func projectHint(b R2SecretBinding) string {
	if b.ProvisionedProject != "" {
		return b.ProvisionedProject
	}
	return b.Namespace
}

func displayBucket(b R2SecretBinding) string {
	if b.ProvisionedBucket != "" {
		return b.ProvisionedBucket
	}
	if b.Bucket != "" {
		return b.Bucket
	}
	return "<bucket>"
}

func distinctNamespaces(bindings []R2SecretBinding) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if seen[b.Namespace] {
			continue
		}
		seen[b.Namespace] = true
		out = append(out, b.Namespace)
	}
	sort.Strings(out)
	return out
}

func sortedGroups(groups map[string][]R2SecretBinding) []string {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
