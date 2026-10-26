package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

type fixedIntakeFixture struct {
	version                int
	origin                 string
	updates                map[string]interface{}
	initializations, reads int
	err, readErr           error
}

func (f *fixedIntakeFixture) InitializeArgocdBackupEnvelope(_ context.Context, origin string, updates map[string]interface{}) (int, error) {
	f.initializations++
	f.origin = origin
	f.updates = updates
	return f.version, f.err
}
func (f *fixedIntakeFixture) ReadArgocdBackupEnvelopeKey(_ context.Context, origin string, version int) ([]byte, error) {
	f.reads++
	if f.readErr != nil {
		return nil, f.readErr
	}
	if origin != f.origin || version != f.version || f.updates == nil {
		return nil, errors.New("private-custody-canary")
	}
	value, _ := f.updates[secretsintake.ArgocdBackupKey].(string)
	return base64.RawURLEncoding.DecodeString(value)
}
func fixedIntakeRequest(t *testing.T, h *Handler, mock sqlmock.Sqlmock, method, path, body string, allowed bool) *httptest.ResponseRecorder {
	t.Helper()
	mock.ExpectQuery(`SELECT is_platform_admin AND active FROM users WHERE id=\$1`).WithArgs(uuid.MustParse(protectedActor)).WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(allowed))
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", protectedActor)
		c.Set("user_role", "admin")
		c.Set(ctxKeyPlatformAdmin, true)
		c.Next()
	})
	r.POST("/init", h.initializeFixedBackupEnvelope)
	r.GET("/verify/:origin_id/:version", h.readFixedBackupEnvelopeCustody)
	w := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, request)
	require.NoError(t, mock.ExpectationsWereMet())
	return w
}
func TestFixedBackupIntakeUsesNativeProfileWithoutGenericWriterOrStorage(t *testing.T) {
	h, mock, _, _, _, _ := protectedTestHandler(t)
	h.vaultClient = nil
	h.argocdBackup.storage = nil
	custody := &fixedIntakeFixture{version: 7}
	h.SetArgocdBackupKeyReader(custody)
	h.SetArgocdBackupKeyInitializer(custody)
	w := fixedIntakeRequest(t, h, mock, "POST", "/init", `{"reason":"fixed human initialization"}`, true)
	require.Equal(t, 202, w.Code)
	require.Equal(t, 1, custody.initializations)
	require.Len(t, custody.updates, 2)
	require.Zero(t, custody.reads)
	require.Nil(t, h.vaultClient)
	var metadata secretIntakeStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &metadata))
	require.Equal(t, "initialized_held", metadata.Status)
	require.False(t, *metadata.NativeReady)
	require.Equal(t, custody.origin, metadata.OriginID)
	require.Equal(t, 7, metadata.VaultVersion)
	encoded := custody.updates[secretsintake.ArgocdBackupKey].(string)
	key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	require.NoError(t, err)
	require.Len(t, key, 32)
	clear(key)
	require.NotContains(t, w.Body.String(), encoded)
	require.NotContains(t, w.Body.String(), custody.updates[secretsintake.ArgocdBackupOriginKey].(string))
	cached, err := h.loadIntakeStatus(t.Context(), metadata.IntakeID)
	require.NoError(t, err)
	require.Equal(t, custody.origin, cached.OriginID)
	require.Equal(t, 7, cached.VaultVersion)
	generic := postIntake(t, h, backupIntakeBody)
	require.Equal(t, 503, generic.Code)
	require.Contains(t, generic.Body.String(), "vault_writer_disabled")
	require.Equal(t, 1, custody.initializations)
}
func TestFixedBackupIntakeUnknownACKOnlyRecoversExactOriginAndExpectedVersion(t *testing.T) {
	h, mock, _, _, _, _ := protectedTestHandler(t)
	h.vaultClient = nil
	custody := &fixedIntakeFixture{version: 7, err: lockbox.ErrSecretInitializationOutcomeUnknown}
	h.SetArgocdBackupKeyInitializer(custody)
	h.SetArgocdBackupKeyReader(custody)
	w := fixedIntakeRequest(t, h, mock, "POST", "/init", `{"reason":"fixed initialization"}`, true)
	require.Equal(t, 503, w.Code)
	require.Contains(t, w.Body.String(), `"status":"UNKNOWN"`)
	require.Contains(t, w.Body.String(), `"expected_version":7`)
	require.NotContains(t, w.Body.String(), "vault_version")
	require.NotContains(t, w.Body.String(), "initialized_held")
	require.Zero(t, custody.reads)
	verified := fixedIntakeRequest(t, h, mock, "GET", "/verify/"+custody.origin+"/7", "", true)
	require.Equal(t, 200, verified.Code)
	require.Contains(t, verified.Body.String(), `"status":"initialized_held"`)
	require.Contains(t, verified.Body.String(), `"native_ready":false`)
	require.Equal(t, 1, custody.initializations)
	require.Equal(t, 1, custody.reads)
	failed := fixedIntakeRequest(t, h, mock, "GET", "/verify/"+custody.origin+"/8", "", true)
	require.Equal(t, 503, failed.Code)
	require.NotContains(t, failed.Body.String(), "canary")
	require.Equal(t, 1, custody.initializations)
	aliases := fixedIntakeRequest(t, h, mock, "GET", "/verify/"+custody.origin+"/07", "", true)
	require.Equal(t, 400, aliases.Code)
	require.Equal(t, 2, custody.reads)
}
func TestFixedBackupIntakeFlagsAuthoritySchemaConflictAndErrorPrivacy(t *testing.T) {
	for _, body := range []string{`{"reason":"r","values":{"argocd_backup_encryption_key":"supplied-canary"}}`, `{"reason":"r","generate":["argocd_backup_encryption_key"]}`, `{"reason":"r","target":"enclii/argocd-backup-envelope"}`, `{"reason":"r","reason":"duplicate"}`, strings.Repeat("x", 8193)} {
		h, mock, _, _, _, _ := protectedTestHandler(t)
		custody := &fixedIntakeFixture{version: 7}
		h.SetArgocdBackupKeyInitializer(custody)
		w := fixedIntakeRequest(t, h, mock, "POST", "/init", body, true)
		require.Equal(t, 400, w.Code)
		require.Zero(t, custody.initializations)
		require.NotContains(t, w.Body.String(), "canary")
	}
	for _, err := range []error{lockbox.ErrSecretInitializationConflict, errors.New("private-provider-canary")} {
		h, mock, _, _, _, _ := protectedTestHandler(t)
		custody := &fixedIntakeFixture{version: 0, err: err}
		h.SetArgocdBackupKeyInitializer(custody)
		w := fixedIntakeRequest(t, h, mock, "POST", "/init", `{"reason":"r"}`, true)
		if errors.Is(err, lockbox.ErrSecretInitializationConflict) {
			require.Equal(t, 409, w.Code)
		} else {
			require.Equal(t, 503, w.Code)
		}
		require.Equal(t, 1, custody.initializations)
		require.NotContains(t, w.Body.String(), "canary")
		require.NotContains(t, w.Body.String(), "initialized_held")
	}
	h, mock, _, _, _, _ := protectedTestHandler(t)
	custody := &fixedIntakeFixture{version: 7}
	h.SetArgocdBackupKeyInitializer(custody)
	denied := fixedIntakeRequest(t, h, mock, "POST", "/init", `{"reason":"r"}`, false)
	require.Equal(t, 403, denied.Code)
	require.Zero(t, custody.initializations)
	h.config.ArgocdProtectedBackupEnabled = false
	disabled := fixedIntakeRequest(t, h, mock, "POST", "/init", `{"reason":"r"}`, true)
	require.Equal(t, 503, disabled.Code)
	require.Zero(t, custody.initializations)
}
