package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	k8scorev1 "k8s.io/api/core/v1"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
)

const r2TestAccount = "acct123"

func r2TestLogger(t *testing.T) logging.Logger {
	t.Helper()
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{Level: "panic", Format: "text", Output: "stderr"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	return logger
}

// cloudflareStub answers just enough of the Cloudflare API to mint a token.
type cloudflareStub struct {
	server    *httptest.Server
	mintCount int
}

func newCloudflareStub(t *testing.T) *cloudflareStub {
	t.Helper()
	s := &cloudflareStub{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/r2/buckets"):
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{}}`))
		case strings.HasSuffix(r.URL.Path, "/tokens/permission_groups"):
			_, _ = fmt.Fprintf(w, `{"success":true,"errors":[],"result":[{"id":"pg-write","name":%q,"scopes":["com.cloudflare.edge.r2.bucket"]}]}`,
				"Workers R2 Storage Bucket Item Write")
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/tokens"):
			s.mintCount++
			_, _ = fmt.Fprintf(w, `{"success":true,"errors":[],"result":{"id":"tok-%d","value":"val-%d"}}`, s.mintCount, s.mintCount)
		case r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{}}`))
		default:
			t.Errorf("unexpected cloudflare request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func newR2TestHandler(t *testing.T, stub *cloudflareStub, objects ...k8sruntime.Object) (*Handler, *fake.Clientset) {
	t.Helper()
	logger := r2TestLogger(t)
	clientset := fake.NewSimpleClientset(objects...)

	h := &Handler{
		logger:             logger,
		secretsProvisioner: provisioning.NewSecretsProvisioner(clientset, logger),
	}
	if stub != nil {
		h.r2Provisioner = provisioning.NewR2ProvisionerWithBaseURL("tok", r2TestAccount, stub.server.URL, logger)
	}
	return h, clientset
}

func r2Secret(namespace, name string, data map[string]string, annotations map[string]string) *k8scorev1.Secret {
	raw := make(map[string][]byte, len(data))
	for k, v := range data {
		raw[k] = []byte(v)
	}
	return &k8scorev1.Secret{
		ObjectMeta: k8smetav1.ObjectMeta{Name: name, Namespace: namespace, Annotations: annotations},
		Type:       k8scorev1.SecretTypeOpaque,
		Data:       raw,
	}
}

// TestEnsureProjectR2Bucket_WritesCompleteCredentials is the day-1/day-2
// regression: provisioning must never leave STORAGE_BACKEND=r2 without keys.
func TestEnsureProjectR2Bucket_WritesCompleteCredentials(t *testing.T) {
	stub := newCloudflareStub(t)
	h, clientset := newR2TestHandler(t, stub)

	result, err := h.ensureProjectR2Bucket(context.Background(), r2ProvisionOptions{
		Project: "karafiel",
		Bucket:  "karafiel-documents",
	})
	if err != nil {
		t.Fatalf("ensureProjectR2Bucket: %v", err)
	}
	if result.Action != r2ActionProvisioned {
		t.Errorf("action = %q, want %q", result.Action, r2ActionProvisioned)
	}
	if result.Namespace != "karafiel" || result.SecretName != "karafiel-credentials" {
		t.Errorf("wrote to %s/%s, want karafiel/karafiel-credentials", result.Namespace, result.SecretName)
	}

	secret, err := clientset.CoreV1().Secrets("karafiel").Get(context.Background(), "karafiel-credentials", k8smetav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	for _, key := range []string{
		provisioning.SecretKeyR2Bucket,
		provisioning.SecretKeyR2Endpoint,
		provisioning.SecretKeyR2AccessKeyID,
		provisioning.SecretKeyR2SecretAccessKey,
		provisioning.SecretKeyStorageBackend,
	} {
		if len(secret.Data[key]) == 0 {
			t.Errorf("secret is missing %s", key)
		}
	}
	if got := string(secret.Data[provisioning.SecretKeyStorageBackend]); got != provisioning.StorageBackendR2 {
		t.Errorf("STORAGE_BACKEND = %q", got)
	}

	// Provenance annotations must be recorded, or the drift guard cannot tell
	// platform-minted credentials from hand-placed ones.
	if secret.Annotations[provisioning.AnnotationR2Bucket] != "karafiel-documents" {
		t.Errorf("missing bucket provenance annotation: %v", secret.Annotations)
	}
	if secret.Annotations[provisioning.AnnotationR2Project] != "karafiel" {
		t.Errorf("missing project provenance annotation: %v", secret.Annotations)
	}

	// The result must never carry credential material.
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "val-1") {
		t.Errorf("token value leaked into the API result: %s", encoded)
	}
}

// TestEnsureProjectR2Bucket_IsIdempotent: re-running must adopt rather than
// churn a live credential.
func TestEnsureProjectR2Bucket_IsIdempotent(t *testing.T) {
	stub := newCloudflareStub(t)
	h, clientset := newR2TestHandler(t, stub)
	ctx := context.Background()

	first, err := h.ensureProjectR2Bucket(ctx, r2ProvisionOptions{Project: "karafiel", Bucket: "karafiel-documents"})
	if err != nil {
		t.Fatalf("first provision: %v", err)
	}
	before, _ := clientset.CoreV1().Secrets("karafiel").Get(ctx, "karafiel-credentials", k8smetav1.GetOptions{})
	firstKey := string(before.Data[provisioning.SecretKeyR2AccessKeyID])

	second, err := h.ensureProjectR2Bucket(ctx, r2ProvisionOptions{Project: "karafiel", Bucket: "karafiel-documents"})
	if err != nil {
		t.Fatalf("second provision must adopt, got: %v", err)
	}
	if second.Action != r2ActionAdopted {
		t.Errorf("second action = %q, want %q", second.Action, r2ActionAdopted)
	}
	if stub.mintCount != 1 {
		t.Errorf("minted %d tokens, want 1 — re-running must not rotate a working credential", stub.mintCount)
	}

	after, _ := clientset.CoreV1().Secrets("karafiel").Get(ctx, "karafiel-credentials", k8smetav1.GetOptions{})
	if got := string(after.Data[provisioning.SecretKeyR2AccessKeyID]); got != firstKey {
		t.Errorf("access key changed on adopt: %q -> %q", firstKey, got)
	}
	if first.Bucket != second.Bucket {
		t.Errorf("bucket changed between runs: %q -> %q", first.Bucket, second.Bucket)
	}
}

// TestEnsureProjectR2Bucket_RotateMintsFresh checks the explicit rotation path.
func TestEnsureProjectR2Bucket_RotateMintsFresh(t *testing.T) {
	stub := newCloudflareStub(t)
	h, clientset := newR2TestHandler(t, stub)
	ctx := context.Background()

	if _, err := h.ensureProjectR2Bucket(ctx, r2ProvisionOptions{Project: "karafiel", Bucket: "karafiel-documents"}); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	before, _ := clientset.CoreV1().Secrets("karafiel").Get(ctx, "karafiel-credentials", k8smetav1.GetOptions{})

	result, err := h.ensureProjectR2Bucket(ctx, r2ProvisionOptions{
		Project: "karafiel", Bucket: "karafiel-documents", Rotate: true,
	})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if result.Action != r2ActionRotated {
		t.Errorf("action = %q, want %q", result.Action, r2ActionRotated)
	}
	if stub.mintCount != 2 {
		t.Errorf("minted %d tokens, want 2", stub.mintCount)
	}
	after, _ := clientset.CoreV1().Secrets("karafiel").Get(ctx, "karafiel-credentials", k8smetav1.GetOptions{})
	oldKey := string(before.Data[provisioning.SecretKeyR2AccessKeyID])
	if oldKey == string(after.Data[provisioning.SecretKeyR2AccessKeyID]) {
		t.Error("rotate must replace the access key")
	}

	// The superseded token stays live so the running pod does not break, but
	// it must be reported so it does not become an untracked credential.
	if result.PreviousAccessKeyID != oldKey {
		t.Errorf("PreviousAccessKeyID = %q, want the superseded token %q", result.PreviousAccessKeyID, oldKey)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("rotation must warn that the previous token is still valid")
	}
	if !strings.Contains(strings.Join(result.Warnings, " "), "revoke") {
		t.Errorf("rotation warning should tell the operator to revoke; got %v", result.Warnings)
	}
}

// TestEnsureProjectR2Bucket_RefusesForeignBucket is the guard that would have
// stopped karafiel being pointed at tezca's bucket.
func TestEnsureProjectR2Bucket_RefusesForeignBucket(t *testing.T) {
	stub := newCloudflareStub(t)
	h, _ := newR2TestHandler(t, stub,
		r2Secret("tezca", "tezca-credentials", map[string]string{
			provisioning.SecretKeyR2Bucket:          "tezca-documents",
			provisioning.SecretKeyR2AccessKeyID:     "key-tezca",
			provisioning.SecretKeyR2SecretAccessKey: "secret-tezca",
			provisioning.SecretKeyStorageBackend:    provisioning.StorageBackendR2,
		}, map[string]string{
			provisioning.AnnotationR2Bucket:  "tezca-documents",
			provisioning.AnnotationR2Project: "tezca",
		}),
	)

	_, err := h.ensureProjectR2Bucket(context.Background(), r2ProvisionOptions{
		Project: "karafiel",
		Bucket:  "tezca-documents",
	})
	if err == nil {
		t.Fatal("binding another project's bucket must be refused")
	}
	var conflict *r2ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("want r2ConflictError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "tezca") {
		t.Errorf("error should name the existing holder; got: %s", err)
	}
	if got := r2ProvisionStatusCode(err); got != http.StatusConflict {
		t.Errorf("status = %d, want 409", got)
	}
	if stub.mintCount != 0 {
		t.Error("no token should be minted for a refused binding")
	}
}

// TestEnsureProjectR2Bucket_RefusesSilentRebind: an existing binding must not
// be repointed at a different bucket behind the operator's back.
func TestEnsureProjectR2Bucket_RefusesSilentRebind(t *testing.T) {
	stub := newCloudflareStub(t)
	h, _ := newR2TestHandler(t, stub,
		r2Secret("karafiel", "karafiel-credentials", map[string]string{
			provisioning.SecretKeyR2Bucket:          "karafiel-documents",
			provisioning.SecretKeyR2AccessKeyID:     "key-k",
			provisioning.SecretKeyR2SecretAccessKey: "secret-k",
		}, map[string]string{
			provisioning.AnnotationR2Bucket:  "karafiel-documents",
			provisioning.AnnotationR2Project: "karafiel",
		}),
	)

	_, err := h.ensureProjectR2Bucket(context.Background(), r2ProvisionOptions{
		Project: "karafiel",
		Bucket:  "karafiel-archive",
	})
	if err == nil {
		t.Fatal("repointing an existing binding must be refused")
	}
	var rebind *r2RebindError
	if !errors.As(err, &rebind) {
		t.Fatalf("want r2RebindError, got %T: %v", err, err)
	}
	if got := r2ProvisionStatusCode(err); got != http.StatusConflict {
		t.Errorf("status = %d, want 409", got)
	}
}

// TestEnsureProjectR2Bucket_FailsWhenTokenCannotBeMinted: the platform's own
// Cloudflare token being under-permissioned must surface as an actionable
// 503, and must not write a partial credential set.
func TestEnsureProjectR2Bucket_FailsWhenTokenCannotBeMinted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/r2/buckets") {
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{}}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Unauthorized to access requested resource"}]}`))
	}))
	defer server.Close()

	logger := r2TestLogger(t)
	clientset := fake.NewSimpleClientset()
	h := &Handler{
		logger:             logger,
		secretsProvisioner: provisioning.NewSecretsProvisioner(clientset, logger),
		r2Provisioner:      provisioning.NewR2ProvisionerWithBaseURL("tok", r2TestAccount, server.URL, logger),
	}

	_, err := h.ensureProjectR2Bucket(context.Background(), r2ProvisionOptions{
		Project: "karafiel", Bucket: "karafiel-documents",
	})
	if err == nil {
		t.Fatal("want an error when the token cannot be minted")
	}
	var forbidden *provisioning.TokenMintForbiddenError
	if !errors.As(err, &forbidden) {
		t.Fatalf("want TokenMintForbiddenError, got %T: %v", err, err)
	}
	if got := r2ProvisionStatusCode(err); got != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (platform misconfiguration, not a bad request)", got)
	}

	// Critically: nothing was written.
	if _, err := clientset.CoreV1().Secrets("karafiel").Get(
		context.Background(), "karafiel-credentials", k8smetav1.GetOptions{},
	); err == nil {
		t.Error("no Secret should be written when credentials could not be minted")
	}
}

func TestEnsureProjectR2Bucket_RequiresConfiguration(t *testing.T) {
	h := &Handler{logger: r2TestLogger(t)}
	if _, err := h.ensureProjectR2Bucket(context.Background(), r2ProvisionOptions{
		Project: "p", Bucket: "some-bucket",
	}); !errors.Is(err, errR2NotConfigured) {
		t.Errorf("want errR2NotConfigured, got %v", err)
	}

	stub := newCloudflareStub(t)
	h2 := &Handler{
		logger:        r2TestLogger(t),
		r2Provisioner: provisioning.NewR2ProvisionerWithBaseURL("tok", r2TestAccount, stub.server.URL, r2TestLogger(t)),
	}
	if _, err := h2.ensureProjectR2Bucket(context.Background(), r2ProvisionOptions{
		Project: "p", Bucket: "some-bucket",
	}); !errors.Is(err, errR2SecretsNotConfigured) {
		t.Errorf("want errR2SecretsNotConfigured, got %v", err)
	}
}

func TestEnsureProjectR2Bucket_RejectsInvalidBucketName(t *testing.T) {
	stub := newCloudflareStub(t)
	h, _ := newR2TestHandler(t, stub)
	_, err := h.ensureProjectR2Bucket(context.Background(), r2ProvisionOptions{
		Project: "karafiel", Bucket: "Not A Bucket",
	})
	if err == nil {
		t.Fatal("want a validation error")
	}
	if got := r2ProvisionStatusCode(err); got != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", got)
	}
}
