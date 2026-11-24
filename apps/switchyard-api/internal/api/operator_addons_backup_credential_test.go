package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
)

const (
	bcBucket  = "addon-backups-fixture"
	bcAccount = "acct-fixture"
	bcVault   = "secret/enclii"
	bcKeyID   = "addon_backup_access_key_id"
	bcKeySec  = "addon_backup_secret_access_key"
)

// bcCloudflare answers the slice of the Cloudflare API the operation uses and
// counts every mutation, so "nothing was minted" is a number, not a hope.
type bcCloudflare struct {
	server       *httptest.Server
	bucketExists bool
	mintForbid   bool
	creates      int
	mints        int
	revokes      []string
}

func newBCCloudflare(t *testing.T) *bcCloudflare {
	t.Helper()
	cf := &bcCloudflare{}
	cf.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/r2/buckets/"+bcBucket):
			if !cf.bucketExists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10006,"message":"bucket not found"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/r2/buckets"):
			cf.creates++
			cf.bucketExists = true
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{}}`))
		case strings.HasSuffix(path, "/tokens/permission_groups"):
			_, _ = fmt.Fprintf(w, `{"success":true,"errors":[],"result":[{"id":"pg-write","name":%q,"scopes":["com.cloudflare.edge.r2.bucket"]}]}`,
				"Workers R2 Storage Bucket Item Write")
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/tokens"):
			if cf.mintForbid {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Unauthorized to access requested resource"}]}`))
				return
			}
			cf.mints++
			var body struct {
				Policies []struct {
					Resources map[string]string `json:"resources"`
				} `json:"policies"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			want := "com.cloudflare.edge.r2.bucket." + bcAccount + "_default_" + bcBucket
			if len(body.Policies) != 1 || len(body.Policies[0].Resources) != 1 || body.Policies[0].Resources[want] != "*" {
				t.Errorf("token is not scoped to the one bucket: %+v", body.Policies)
			}
			_, _ = fmt.Fprintf(w, `{"success":true,"errors":[],"result":{"id":"tok-%d","value":"raw-token-value-%d"}}`, cf.mints, cf.mints)
		case r.Method == http.MethodDelete && strings.Contains(path, "/tokens/"):
			cf.revokes = append(cf.revokes, path[strings.LastIndex(path, "/")+1:])
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{}}`))
		default:
			t.Errorf("unexpected Cloudflare request %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(cf.server.Close)
	return cf
}

type bcFixture struct {
	router  *gin.Engine
	cf      *bcCloudflare
	vault   *fakeVault
	dyn     *dynamicfake.FakeDynamicClient
	mock    sqlmock.Sqlmock
	logFile string
}

func newBCFixture(t *testing.T) *bcFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("ENCLII_ADDON_BACKUP_DESTINATION_BASE", "s3://"+bcBucket)
	t.Setenv("ENCLII_ADDON_BACKUP_ENDPOINT_URL", "https://"+bcAccount+".r2.cloudflarestorage.com")
	logFile := filepath.Join(t.TempDir(), "switchyard.log")
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{Level: "debug", Format: "json", Output: logFile})
	require.NoError(t, err)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	es := &unstructured.Unstructured{}
	es.SetAPIVersion("external-secrets.io/v1beta1")
	es.SetKind("ExternalSecret")
	es.SetNamespace("enclii")
	es.SetName("enclii-db-backup-credentials")
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{externalSecretGVR: "ExternalSecretList"}, es)
	cf := newBCCloudflare(t)
	vault := newFakeVault()
	h := &Handler{
		logger:        logger,
		r2Provisioner: provisioning.NewR2ProvisionerWithBaseURL("cf-api-token-fixture", bcAccount, cf.server.URL, logger),
		vaultClient:   vault,
		k8sClient:     &k8sclient.Client{DynamicClient: dyn},
		repos:         &db.Repositories{AuditLogs: db.NewAuditLogRepository(database)},
	}
	actor := uuid.New()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", actor.String())
		c.Set("user_email", "ops@example.test")
		c.Next()
	})
	router.POST("/v1/ops/:domain/:action", h.HandleOpsOperation)
	return &bcFixture{router: router, cf: cf, vault: vault, dyn: dyn, mock: mock, logFile: logFile}
}

func (f *bcFixture) post(t *testing.T, req operatorOperationRequest) (int, map[string]any, string) {
	t.Helper()
	req.Operation = opsAddonsBackupCredentialOperation
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/ops/addons/backup-credential", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httpReq)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return rec.Code, resp, rec.Body.String()
}

func (f *bcFixture) dryRun(t *testing.T, args map[string]string) (map[string]any, string) {
	t.Helper()
	code, resp, _ := f.post(t, operatorOperationRequest{DryRun: true, Args: args})
	require.Equal(t, http.StatusOK, code, resp)
	data, ok := resp["data"].(map[string]any)
	require.True(t, ok, resp)
	hash, _ := data["plan_hash"].(string)
	require.Len(t, hash, 64)
	return data, hash
}

func (f *bcFixture) apply(t *testing.T, hash string, extra map[string]string) (int, map[string]any, string) {
	t.Helper()
	args := map[string]string{"expected_plan": hash}
	for k, v := range extra {
		args[k] = v
	}
	return f.post(t, operatorOperationRequest{Reason: "addon backups credential", IdempotencyKey: "addon-backups-1", Args: args})
}

func secretFor(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestOpsAddonsBackupCredentialIsAnAdvertisedPlatformOperation(t *testing.T) {
	require.True(t, operationSupported("addons", "backup-credential", opsCapabilities))
}

func TestOpsAddonsBackupCredentialDryRunWritesNothing(t *testing.T) {
	f := newBCFixture(t)
	data, _ := f.dryRun(t, nil)
	assert.Equal(t, bcBucket, data["bucket"])
	assert.Equal(t, "create", data["bucket_action"])
	assert.Equal(t, "mint", data["token_action"])
	assert.Equal(t, bcVault, data["vault_path"])
	assert.Equal(t, []any{bcKeyID, bcKeySec}, data["vault_keys"])
	assert.Equal(t, "enclii/enclii-db-backup-credentials", data["eso_target"])
	assert.Zero(t, f.cf.creates)
	assert.Zero(t, f.cf.mints)
	assert.Zero(t, f.vault.writes)
	assert.NoError(t, f.mock.ExpectationsWereMet(), "a dry run writes no audit row")

	f.cf.bucketExists = true
	data, _ = f.dryRun(t, nil)
	assert.Equal(t, "adopt", data["bucket_action"])
}

func TestOpsAddonsBackupCredentialApplyIsBoundToTheReviewedPlan(t *testing.T) {
	f := newBCFixture(t)
	code, resp, _ := f.post(t, operatorOperationRequest{Reason: "r", IdempotencyKey: "k"})
	assert.Equal(t, http.StatusBadRequest, code, resp)
	code, resp, _ = f.post(t, operatorOperationRequest{Reason: "r", Args: map[string]string{"expected_plan": strings.Repeat("0", 64)}})
	assert.Equal(t, http.StatusBadRequest, code, "an idempotency key is required: %v", resp)

	_, hash := f.dryRun(t, nil)
	f.cf.bucketExists = true // the bucket appeared between the review and the apply
	code, resp, _ = f.apply(t, hash, nil)
	assert.Equal(t, http.StatusConflict, code, resp)
	assert.Zero(t, f.cf.mints, "a stale plan mints nothing")
	assert.Zero(t, f.vault.writes)
	assert.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsAddonsBackupCredentialApplyMintsOnceAndNeverReturnsOrLogsTheSecret(t *testing.T) {
	f := newBCFixture(t)
	_, hash := f.dryRun(t, nil)
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	code, resp, body := f.apply(t, hash, nil)
	require.Equal(t, http.StatusOK, code, resp)
	assert.Equal(t, "succeeded", resp["status"])
	assert.NoError(t, f.mock.ExpectationsWereMet(), "a request row before the mint and a result row after it")

	assert.Equal(t, 1, f.cf.creates)
	assert.Equal(t, 1, f.cf.mints)
	assert.Equal(t, "tok-1", f.vault.str(t, bcVault, bcKeyID))
	assert.Equal(t, secretFor("raw-token-value-1"), f.vault.str(t, bcVault, bcKeySec), "secret access key = sha256(token value)")

	secret := secretFor("raw-token-value-1")
	assert.NotContains(t, body, secret, "the secret is never returned")
	assert.NotContains(t, body, "raw-token-value-1", "the raw token is never returned")
	logs, err := os.ReadFile(f.logFile)
	require.NoError(t, err)
	assert.NotContains(t, string(logs), secret, "the secret is never logged")
	assert.NotContains(t, string(logs), "raw-token-value-1", "the raw token is never logged")

	es, err := f.dyn.Resource(externalSecretGVR).Namespace("enclii").Get(t.Context(), "enclii-db-backup-credentials", metav1.GetOptions{})
	require.NoError(t, err)
	assert.NotEmpty(t, es.GetAnnotations()["force-sync"], "the ExternalSecret is force-synced like an intake target")
	assert.Equal(t, opsAddonsBackupCredentialOperation, es.GetAnnotations()["enclii.dev/last-ops-operation"])
	data := resp["data"].(map[string]any)
	assert.Equal(t, true, data["eso_refreshed"])

	// Idempotent: the same apply again finds the credential and writes nothing.
	code, resp, _ = f.apply(t, hash, nil)
	require.Equal(t, http.StatusOK, code, resp)
	assert.Equal(t, "succeeded", resp["status"])
	assert.Contains(t, resp["summary"], "already provisioned")
	assert.Equal(t, 1, f.cf.mints, "no second token")
	assert.Equal(t, 1, f.vault.writes)
	assert.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsAddonsBackupCredentialRotatesOnlyWhenAsked(t *testing.T) {
	f := newBCFixture(t)
	f.cf.bucketExists = true
	f.vault.data[bcVault] = map[string]interface{}{bcKeyID: "tok-old", bcKeySec: "old-secret", "database_url": "kept"}

	data, _ := f.dryRun(t, nil)
	assert.Equal(t, "keep", data["token_action"])

	data, hash := f.dryRun(t, map[string]string{"rotate": "true"})
	assert.Equal(t, "rotate", data["token_action"])
	assert.Equal(t, "tok-old", data["current_access_key_id"], "the revocation handle of the outgoing token")

	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	code, resp, _ := f.apply(t, hash, map[string]string{"rotate": "true"})
	require.Equal(t, http.StatusOK, code, resp)
	assert.Equal(t, "tok-1", f.vault.str(t, bcVault, bcKeyID))
	assert.Equal(t, "kept", f.vault.str(t, bcVault, "database_url"), "a merge keeps the other enclii properties")
	assert.Empty(t, f.cf.revokes, "the outgoing token is not revoked while clusters still archive with it")
	assert.Equal(t, "tok-old", resp["data"].(map[string]any)["previous_access_key_id"])

	// The rotation's plan is spent: replaying it is refused, never a second mint.
	code, _, _ = f.apply(t, hash, map[string]string{"rotate": "true"})
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, 1, f.cf.mints)
}

func TestOpsAddonsBackupCredentialNamesTheMissingCloudflarePermission(t *testing.T) {
	f := newBCFixture(t)
	f.cf.mintForbid = true
	_, hash := f.dryRun(t, nil)
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	code, resp, _ := f.apply(t, hash, nil)
	assert.Equal(t, http.StatusFailedDependency, code, resp)
	assert.Equal(t, "blocked", resp["status"])
	summary, _ := resp["summary"].(string)
	assert.Contains(t, summary, `"API Tokens Write"`)
	assert.Contains(t, summary, `"Workers R2 Storage Bucket Item Write"`)
	assert.Contains(t, summary, "Nothing was written to Vault")
	assert.Zero(t, f.vault.writes)
	assert.NoError(t, f.mock.ExpectationsWereMet(), "the refusal is recorded as a failure row")
}

func TestOpsAddonsBackupCredentialRevokesTheTokenWhenVaultRefusesTheWrite(t *testing.T) {
	f := newBCFixture(t)
	f.vault.writeErr[bcVault] = errors.New("permission denied")
	_, hash := f.dryRun(t, nil)
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnResult(sqlmock.NewResult(1, 1))
	code, resp, _ := f.apply(t, hash, nil)
	assert.Equal(t, http.StatusFailedDependency, code, resp, "an upstream failure is sent as 424 so the summary survives Cloudflare")
	assert.Contains(t, resp["summary"], "the new token was revoked")
	assert.Equal(t, []string{"tok-1"}, f.cf.revokes, "a token nobody holds is revoked")
}

func TestOpsAddonsBackupCredentialMintsNothingWithoutAnAuditRow(t *testing.T) {
	f := newBCFixture(t)
	_, hash := f.dryRun(t, nil)
	f.mock.ExpectExec(`INSERT INTO audit_logs`).WillReturnError(assert.AnError)
	code, resp, _ := f.apply(t, hash, nil)
	assert.Equal(t, http.StatusServiceUnavailable, code, resp)
	assert.Zero(t, f.cf.creates)
	assert.Zero(t, f.cf.mints)
}

func TestOpsAddonsBackupCredentialRefusesAMismatchedOrMissingStore(t *testing.T) {
	f := newBCFixture(t)
	t.Setenv("ENCLII_ADDON_BACKUP_ENDPOINT_URL", "https://another-account.r2.cloudflarestorage.com")
	code, resp, _ := f.post(t, operatorOperationRequest{DryRun: true})
	assert.Equal(t, http.StatusFailedDependency, code, resp)
	assert.Contains(t, resp["summary"], "does not match")

	t.Setenv("ENCLII_ADDON_BACKUP_DESTINATION_BASE", "")
	code, resp, _ = f.post(t, operatorOperationRequest{DryRun: true})
	assert.Equal(t, http.StatusServiceUnavailable, code, resp)
	assert.Zero(t, f.cf.mints)
}
