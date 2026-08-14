package provisioning

import (
	"context"
	"strings"
	"testing"

	k8scorev1 "k8s.io/api/core/v1"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func secretWithR2(namespace, name string, data map[string]string, annotations map[string]string) *k8scorev1.Secret {
	raw := make(map[string][]byte, len(data))
	for k, v := range data {
		raw[k] = []byte(v)
	}
	return &k8scorev1.Secret{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Type: k8scorev1.SecretTypeOpaque,
		Data: raw,
	}
}

func findingKinds(findings []R2Finding) map[string][]R2Finding {
	out := map[string][]R2Finding{}
	for _, f := range findings {
		out[f.Kind] = append(out[f.Kind], f)
	}
	return out
}

// TestAudit_CatchesBorrowedCredentials covers the failure this audit exists
// for: one service's namespace holding a copy of another's R2 credentials,
// pointed at the other's bucket. Nothing in cluster state marks that as wrong,
// so the audit has to detect it from the credentials alone — no annotation and
// no ownership knowledge required.
func TestAudit_CatchesBorrowedCredentials(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		secretWithR2("svc-alpha", "svc-alpha-credentials", map[string]string{
			SecretKeyR2Bucket:          "svc-alpha-documents",
			SecretKeyR2AccessKeyID:     "shared-key-id",
			SecretKeyR2SecretAccessKey: "shared-secret",
			SecretKeyStorageBackend:    StorageBackendR2,
		}, map[string]string{
			AnnotationR2Bucket:  "svc-alpha-documents",
			AnnotationR2Project: "svc-alpha",
		}),
		// The hand-placed copy.
		secretWithR2("svc-beta", "r2-credentials", map[string]string{
			SecretKeyR2Bucket:          "svc-alpha-documents",
			SecretKeyR2AccessKeyID:     "shared-key-id",
			SecretKeyR2SecretAccessKey: "shared-secret",
			SecretKeyStorageBackend:    StorageBackendR2,
		}, nil),
	)

	bindings, err := NewR2Auditor(clientset).Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("want 2 bindings, got %d: %+v", len(bindings), bindings)
	}

	findings := AuditR2Bindings(bindings)
	byKind := findingKinds(findings)

	if len(byKind[FindingSharedCredentials]) == 0 {
		t.Error("the same access key in two namespaces must be flagged as shared_credentials")
	}
	if len(byKind[FindingBucketShared]) == 0 {
		t.Error("one bucket referenced from two namespaces must be flagged")
	}
	if len(byKind[FindingUnmanaged]) == 0 {
		t.Error("hand-placed credentials with no provenance annotation must be flagged")
	}
	if CountCritical(findings) == 0 {
		t.Fatal("this configuration must produce at least one critical finding")
	}

	// The finding must actually point at svc-beta.
	found := false
	for _, f := range byKind[FindingSharedCredentials] {
		if f.Namespace == "svc-beta" && strings.Contains(f.Message, "svc-alpha") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a shared_credentials finding naming svc-beta and svc-alpha, got %+v", byKind[FindingSharedCredentials])
	}

	// No secret material may appear anywhere in the output.
	for _, b := range bindings {
		if strings.Contains(b.AccessKeyFingerprint, "shared-key-id") {
			t.Error("access key id leaked into the fingerprint")
		}
	}
	for _, f := range findings {
		if strings.Contains(f.Message, "shared-secret") || strings.Contains(f.Message, "shared-key-id") {
			t.Errorf("credential material leaked into a finding message: %s", f.Message)
		}
	}
}

// TestAudit_CatchesStorageBackendWithoutKeys is the original defect: a service
// told to use R2 and given no way to authenticate.
func TestAudit_CatchesStorageBackendWithoutKeys(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		secretWithR2("svc-beta", "svc-beta-credentials", map[string]string{
			SecretKeyR2Bucket:       "svc-beta-documents",
			SecretKeyR2Endpoint:     "https://acct.r2.cloudflarestorage.com",
			SecretKeyStorageBackend: StorageBackendR2,
		}, nil),
	)

	bindings, err := NewR2Auditor(clientset).Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	findings := AuditR2Bindings(bindings)
	byKind := findingKinds(findings)

	missing := byKind[FindingMissingCredentials]
	if len(missing) != 1 {
		t.Fatalf("want exactly 1 missing_credentials finding, got %d: %+v", len(missing), findings)
	}
	if missing[0].Severity != SeverityCritical {
		t.Errorf("severity = %q, want critical", missing[0].Severity)
	}
	for _, key := range []string{SecretKeyR2AccessKeyID, SecretKeyR2SecretAccessKey} {
		if !strings.Contains(missing[0].Message, key) {
			t.Errorf("message should name the missing key %s; got: %s", key, missing[0].Message)
		}
	}
	if missing[0].Remediation == "" {
		t.Error("a critical finding must carry a remediation command")
	}
}

// TestAudit_CatchesBucketMismatch covers the case where a service points at a
// bucket other than the one enclii provisioned for it.
func TestAudit_CatchesBucketMismatch(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		secretWithR2("svc-beta", "svc-beta-credentials", map[string]string{
			SecretKeyR2Bucket:          "svc-alpha-documents",
			SecretKeyR2AccessKeyID:     "key-a",
			SecretKeyR2SecretAccessKey: "secret-a",
			SecretKeyStorageBackend:    StorageBackendR2,
		}, map[string]string{
			AnnotationR2Bucket:  "svc-beta-documents",
			AnnotationR2Project: "svc-beta",
		}),
	)

	bindings, err := NewR2Auditor(clientset).Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	byKind := findingKinds(AuditR2Bindings(bindings))

	mismatch := byKind[FindingBucketMismatch]
	if len(mismatch) != 1 {
		t.Fatalf("want 1 bucket_mismatch finding, got %d", len(mismatch))
	}
	if mismatch[0].Severity != SeverityCritical {
		t.Errorf("bucket mismatch must be critical, got %q", mismatch[0].Severity)
	}
	if !strings.Contains(mismatch[0].Message, "svc-beta-documents") ||
		!strings.Contains(mismatch[0].Message, "svc-alpha-documents") {
		t.Errorf("message should name both buckets; got: %s", mismatch[0].Message)
	}
}

// TestAudit_CleanSetupIsQuiet: a properly provisioned fleet must produce no
// findings, otherwise the guard is noise and will be ignored.
func TestAudit_CleanSetupIsQuiet(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		secretWithR2("svc-alpha", "svc-alpha-credentials", map[string]string{
			SecretKeyR2Bucket:          "svc-alpha-documents",
			SecretKeyR2Endpoint:        "https://acct.r2.cloudflarestorage.com",
			SecretKeyR2AccessKeyID:     "key-svc-alpha",
			SecretKeyR2SecretAccessKey: "secret-svc-alpha",
			SecretKeyStorageBackend:    StorageBackendR2,
		}, map[string]string{
			AnnotationR2Bucket:  "svc-alpha-documents",
			AnnotationR2Project: "svc-alpha",
		}),
		secretWithR2("svc-beta", "svc-beta-credentials", map[string]string{
			SecretKeyR2Bucket:          "svc-beta-documents",
			SecretKeyR2Endpoint:        "https://acct.r2.cloudflarestorage.com",
			SecretKeyR2AccessKeyID:     "key-svc-beta",
			SecretKeyR2SecretAccessKey: "secret-svc-beta",
			SecretKeyStorageBackend:    StorageBackendR2,
		}, map[string]string{
			AnnotationR2Bucket:  "svc-beta-documents",
			AnnotationR2Project: "svc-beta",
		}),
		// An unrelated secret must be ignored entirely.
		secretWithR2("other", "db-credentials", map[string]string{
			"DATABASE_URL": "postgres://user:pass@host/db",
		}, nil),
	)

	bindings, err := NewR2Auditor(clientset).Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("want 2 R2 bindings (non-R2 secrets ignored), got %d: %+v", len(bindings), bindings)
	}
	findings := AuditR2Bindings(bindings)
	if len(findings) != 0 {
		t.Fatalf("a correctly provisioned fleet must produce no findings, got: %+v", findings)
	}
}

func TestAudit_ScanIsNamespaceScopable(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		secretWithR2("svc-alpha", "svc-alpha-credentials", map[string]string{
			SecretKeyR2Bucket: "svc-alpha-documents",
		}, nil),
		secretWithR2("svc-beta", "svc-beta-credentials", map[string]string{
			SecretKeyR2Bucket: "svc-beta-documents",
		}, nil),
	)

	bindings, err := NewR2Auditor(clientset).Scan(context.Background(), []string{"svc-beta"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(bindings) != 1 || bindings[0].Namespace != "svc-beta" {
		t.Fatalf("namespace-scoped scan returned %+v", bindings)
	}
}

func TestBinding_CompleteAndDeclares(t *testing.T) {
	complete := R2SecretBinding{Bucket: "b", HasAccessKeyID: true, HasSecretAccessKey: true}
	if !complete.Complete() || !complete.DeclaresR2() {
		t.Error("a bucket with both keys is complete")
	}
	partial := R2SecretBinding{Bucket: "b", StorageBackend: StorageBackendR2}
	if partial.Complete() {
		t.Error("a bucket with no keys is not complete")
	}
	if !partial.DeclaresR2() {
		t.Error("STORAGE_BACKEND=r2 declares R2 intent")
	}
	if (R2SecretBinding{}).DeclaresR2() {
		t.Error("an empty binding declares nothing")
	}
}

func TestFingerprintAccessKey(t *testing.T) {
	if FingerprintAccessKey("") != "" {
		t.Error("empty key has no fingerprint")
	}
	a := FingerprintAccessKey("key-a")
	b := FingerprintAccessKey("key-b")
	if a == b {
		t.Error("different keys must fingerprint differently")
	}
	if a != FingerprintAccessKey("key-a") {
		t.Error("fingerprints must be stable")
	}
	if len(a) != 12 {
		t.Errorf("fingerprint length = %d, want 12", len(a))
	}
	if strings.Contains(a, "key-a") {
		t.Error("fingerprint must not contain the key")
	}
}

func TestAuditor_GetBindingReadsAnnotations(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		secretWithR2("svc-beta", "svc-beta-credentials", map[string]string{
			SecretKeyR2Bucket:          "svc-beta-documents",
			SecretKeyR2AccessKeyID:     "key-k",
			SecretKeyR2SecretAccessKey: "secret-k",
		}, map[string]string{
			AnnotationR2Bucket:  "svc-beta-documents",
			AnnotationR2Project: "svc-beta",
		}),
	)
	auditor := NewR2Auditor(clientset)

	binding, err := auditor.GetBinding(context.Background(), "svc-beta", "svc-beta-credentials")
	if err != nil {
		t.Fatalf("GetBinding: %v", err)
	}
	if !binding.Managed || binding.ProvisionedBucket != "svc-beta-documents" {
		t.Errorf("binding = %+v, want managed with the provisioned bucket recorded", binding)
	}
	if !binding.Complete() {
		t.Error("binding should be complete")
	}

	keyID, err := auditor.AccessKeyID(context.Background(), "svc-beta", "svc-beta-credentials")
	if err != nil {
		t.Fatalf("AccessKeyID: %v", err)
	}
	if keyID != "key-k" {
		t.Errorf("AccessKeyID = %q", keyID)
	}
}
