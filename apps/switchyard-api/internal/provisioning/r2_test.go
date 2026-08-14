package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

const testAccountID = "acct123"

func testLogger(t *testing.T) logging.Logger {
	t.Helper()
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{
		Level:  "panic",
		Format: "text",
		Output: "stderr",
	})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	return logger
}

// cfStub is a stand-in Cloudflare API. It records what it was asked to do so
// tests can assert on the exact wire shape, which is the part that cannot be
// validated against the live account.
type cfStub struct {
	server *httptest.Server
	logger logging.Logger

	bucketExists bool

	// captured
	createdBucket    string
	tokenBody        cfTokenRequest
	permGroupQuery   string
	tokenRequestSeen bool

	// injected failures
	permGroupStatus int
	tokenStatus     int
	tokenErrCode    int
	tokenErrMessage string

	tokenID    string
	tokenValue string
}

func newCFStub(t *testing.T) *cfStub {
	t.Helper()
	s := &cfStub{tokenID: "tok-id-abc", tokenValue: "tok-value-xyz", logger: testLogger(t)}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("missing/incorrect bearer auth: %q", got)
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/"+testAccountID+"/r2/buckets":
			var body r2BucketRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.createdBucket = body.Name
			if s.bucketExists {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10004,"message":"The bucket you tried to create already exists"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{}}`))

		case r.Method == http.MethodGet && r.URL.Path == "/accounts/"+testAccountID+"/tokens/permission_groups":
			s.permGroupQuery = r.URL.RawQuery
			if s.permGroupStatus != 0 {
				w.WriteHeader(s.permGroupStatus)
				_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Unauthorized to access requested resource"}]}`))
				return
			}
			_, _ = fmt.Fprintf(w, `{"success":true,"errors":[],"result":[
				{"id":"pg-read","name":"Workers R2 Storage Bucket Item Read","scopes":["%s"]},
				{"id":"pg-write","name":"%s","scopes":["%s"]}]}`,
				r2BucketScope, r2ObjectRWPermissionGroup, r2BucketScope)

		case r.Method == http.MethodPost && r.URL.Path == "/accounts/"+testAccountID+"/tokens":
			s.tokenRequestSeen = true
			_ = json.NewDecoder(r.Body).Decode(&s.tokenBody)
			if s.tokenStatus != 0 {
				w.WriteHeader(s.tokenStatus)
				_, _ = fmt.Fprintf(w, `{"success":false,"errors":[{"code":%d,"message":%q}]}`,
					s.tokenErrCode, s.tokenErrMessage)
				return
			}
			_, _ = fmt.Fprintf(w, `{"success":true,"errors":[],"result":{"id":%q,"value":%q}}`,
				s.tokenID, s.tokenValue)

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *cfStub) provisioner() *R2Provisioner {
	return NewR2ProvisionerWithBaseURL("test-token", testAccountID, s.server.URL, s.logger)
}

// TestProvisionBucket_ReturnsCompleteCredentials is the regression test for the
// production incident: a provisioned service used to be handed
// STORAGE_BACKEND=r2 with no access keys.
func TestProvisionBucket_ReturnsCompleteCredentials(t *testing.T) {
	stub := newCFStub(t)

	entries, err := stub.provisioner().CreateBucket(context.Background(), "karafiel-documents")
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}

	got := map[string]string{}
	for _, e := range entries {
		got[e.Key] = e.Value
	}

	for _, key := range []string{
		SecretKeyR2Bucket, SecretKeyR2Endpoint,
		SecretKeyR2AccessKeyID, SecretKeyR2SecretAccessKey, SecretKeyStorageBackend,
	} {
		if got[key] == "" {
			t.Errorf("missing or empty secret entry %s", key)
		}
	}

	if got[SecretKeyStorageBackend] != StorageBackendR2 {
		t.Errorf("STORAGE_BACKEND = %q, want %q", got[SecretKeyStorageBackend], StorageBackendR2)
	}
	if got[SecretKeyR2Bucket] != "karafiel-documents" {
		t.Errorf("bucket = %q", got[SecretKeyR2Bucket])
	}
	if got[SecretKeyR2Endpoint] != "https://"+testAccountID+".r2.cloudflarestorage.com" {
		t.Errorf("endpoint = %q", got[SecretKeyR2Endpoint])
	}
}

// TestDeriveSecretAccessKey pins the Cloudflare-documented derivation:
// access key id = token id, secret access key = SHA-256 of the token value.
func TestDeriveSecretAccessKey(t *testing.T) {
	stub := newCFStub(t)
	stub.tokenID = "abc123"
	stub.tokenValue = "super-secret-token-value"

	creds, err := stub.provisioner().MintBucketToken(context.Background(), "some-bucket")
	if err != nil {
		t.Fatalf("MintBucketToken: %v", err)
	}

	if creds.AccessKeyID != "abc123" {
		t.Errorf("AccessKeyID = %q, want the token id %q", creds.AccessKeyID, "abc123")
	}

	sum := sha256.Sum256([]byte("super-secret-token-value"))
	want := hex.EncodeToString(sum[:])
	if creds.SecretAccessKey != want {
		t.Errorf("SecretAccessKey = %q, want SHA-256 hex of the token value %q", creds.SecretAccessKey, want)
	}
	if len(creds.SecretAccessKey) != 64 {
		t.Errorf("SecretAccessKey should be 64 hex chars, got %d", len(creds.SecretAccessKey))
	}
	// The raw token value must never be handed onward.
	if strings.Contains(creds.SecretAccessKey, stub.tokenValue) {
		t.Error("raw token value leaked into the secret access key")
	}
}

// TestMintBucketToken_ScopedToSingleBucket is the least-privilege assertion:
// the minted token must name exactly one bucket resource, never the account.
func TestMintBucketToken_ScopedToSingleBucket(t *testing.T) {
	stub := newCFStub(t)

	if _, err := stub.provisioner().MintBucketToken(context.Background(), "karafiel-documents"); err != nil {
		t.Fatalf("MintBucketToken: %v", err)
	}

	if len(stub.tokenBody.Policies) != 1 {
		t.Fatalf("want exactly 1 policy, got %d", len(stub.tokenBody.Policies))
	}
	policy := stub.tokenBody.Policies[0]

	if policy.Effect != "allow" {
		t.Errorf("effect = %q, want allow", policy.Effect)
	}
	if len(policy.Resources) != 1 {
		t.Fatalf("want exactly 1 resource, got %d: %v", len(policy.Resources), policy.Resources)
	}

	wantKey := fmt.Sprintf("com.cloudflare.edge.r2.bucket.%s_default_karafiel-documents", testAccountID)
	if v, ok := policy.Resources[wantKey]; !ok || v != "*" {
		t.Errorf("resources = %v, want %s => *", policy.Resources, wantKey)
	}
	for key := range policy.Resources {
		if strings.HasPrefix(key, "com.cloudflare.api.account") {
			t.Errorf("token scoped to the whole account (%s) instead of one bucket", key)
		}
	}

	if len(policy.PermissionGroups) != 1 || policy.PermissionGroups[0].ID != "pg-write" {
		t.Errorf("permission groups = %v, want the resolved bucket-item-write group", policy.PermissionGroups)
	}
	if !strings.Contains(stub.permGroupQuery, "scope=com.cloudflare.edge.r2.bucket") {
		t.Errorf("permission-group lookup query = %q, want the bucket scope filter", stub.permGroupQuery)
	}
	if !strings.HasPrefix(stub.tokenBody.Name, "enclii-r2-karafiel-documents") {
		t.Errorf("token name = %q, want an enclii-r2-<bucket> prefix", stub.tokenBody.Name)
	}
}

// TestMintBucketToken_ForbiddenNamesPermission checks that a switchyard token
// which cannot mint tokens produces an actionable error rather than a
// half-provisioned service.
func TestMintBucketToken_ForbiddenNamesPermission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   int
	}{
		{"http 403", http.StatusForbidden, 0},
		{"http 401", http.StatusUnauthorized, 0},
		{"cloudflare code 9109", http.StatusBadRequest, 9109},
		{"cloudflare code 10000", http.StatusBadRequest, 10000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := newCFStub(t)
			stub.tokenStatus = tc.status
			stub.tokenErrCode = tc.code
			stub.tokenErrMessage = "Unauthorized to access requested resource"

			_, err := stub.provisioner().ProvisionBucket(context.Background(), "some-bucket")
			if err == nil {
				t.Fatal("want an error, got nil")
			}

			var forbidden *TokenMintForbiddenError
			if !errors.As(err, &forbidden) {
				t.Fatalf("want TokenMintForbiddenError, got %T: %v", err, err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "API Tokens Write") {
				t.Errorf("error should name the missing permission; got: %s", msg)
			}
			if !strings.Contains(msg, r2ObjectRWPermissionGroup) {
				t.Errorf("error should name the R2 permission group; got: %s", msg)
			}
		})
	}
}

// TestProvisionBucket_NeverPartialOnMintFailure: if the token cannot be minted
// the caller must get an error, never a partial entry list.
func TestProvisionBucket_NeverPartialOnMintFailure(t *testing.T) {
	stub := newCFStub(t)
	stub.permGroupStatus = http.StatusForbidden

	entries, err := stub.provisioner().CreateBucket(context.Background(), "some-bucket")
	if err == nil {
		t.Fatal("want an error when credentials cannot be minted")
	}
	if entries != nil {
		t.Fatalf("want nil entries on failure, got %v", entries)
	}
	if stub.tokenRequestSeen {
		t.Error("should not attempt a token mint after the permission lookup failed")
	}
}

func TestEnsureBucket_AdoptsExisting(t *testing.T) {
	stub := newCFStub(t)
	stub.bucketExists = true

	adopted, err := stub.provisioner().EnsureBucket(context.Background(), "already-there")
	if err != nil {
		t.Fatalf("EnsureBucket on an existing bucket must adopt, got: %v", err)
	}
	if !adopted {
		t.Error("want adopted=true for a pre-existing bucket")
	}

	creds, err := stub.provisioner().ProvisionBucket(context.Background(), "already-there")
	if err != nil {
		t.Fatalf("ProvisionBucket: %v", err)
	}
	if !creds.BucketAdopted {
		t.Error("ProvisionBucket should report the bucket as adopted")
	}
}

func TestProvisionBucket_RejectsBadBucketName(t *testing.T) {
	stub := newCFStub(t)
	for _, name := range []string{
		"",
		"ab",
		"UPPER",
		"has_underscore",
		"-leading",
		"trailing-",
		"double--hyphen",
		"has/slash",
		strings.Repeat("a", 64),
	} {
		if _, err := stub.provisioner().ProvisionBucket(context.Background(), name); err == nil {
			t.Errorf("bucket name %q should be rejected", name)
		}
	}
	if stub.createdBucket != "" {
		t.Errorf("no bucket should have been created, got %q", stub.createdBucket)
	}
}

func TestR2Provisioner_RequiresCloudflareConfig(t *testing.T) {
	p := NewR2Provisioner("", "", testLogger(t))
	if _, err := p.CreateBucket(context.Background(), "some-bucket"); err == nil {
		t.Fatal("want an error when Cloudflare credentials are unset")
	}
}

func TestTokenName_IsStablePrefixedAndBounded(t *testing.T) {
	at := time.Date(2026, 8, 13, 4, 5, 6, 0, time.UTC)
	name := tokenName("karafiel-documents", at)
	if name != "enclii-r2-karafiel-documents-20260813040506" {
		t.Errorf("tokenName = %q", name)
	}
	long := tokenName(strings.Repeat("a", 120), at)
	if len(long) > 96 {
		t.Errorf("token name should stay bounded, got %d chars", len(long))
	}
}
