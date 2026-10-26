package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
)

const backupIntakeBody = `{"target":"enclii/argocd-backup-envelope","generate":["argocd_backup_encryption_key"],"reason":"protected backup preparation"}`

type backupIntakeWriter struct {
	mergeOnlyWriter
	updates map[string]interface{}
	path    string
	writes  int
	legacy  int
	err     error
}

func (w *backupIntakeWriter) InitializeSecretData(context.Context, string, map[string]interface{}) (int, error) {
	w.legacy++
	return 0, errors.New("legacy initialization forbidden")
}

func (w *backupIntakeWriter) InitializeArgocdBackupEnvelope(_ context.Context, origin string, updates map[string]interface{}) (int, error) {
	w.writes++
	w.path, w.updates = "secret/enclii", updates
	if w.err != nil {
		return 0, w.err
	}
	return 7, nil
}

type availabilityBackupIntakeWriter struct {
	backupIntakeWriter
	enabled bool
}

func (w *availabilityBackupIntakeWriter) IsEnabled() bool { return w.enabled }

func TestArgocdBackupGenericIntakeRequiresDedicatedRouteBeforeAnySideEffect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, broadWriter := range []bool{false, true} {
		for _, backupFlag := range []bool{false, true} {
			for _, token := range []bool{false, true} {
				writer := &availabilityBackupIntakeWriter{enabled: broadWriter}
				h := &Handler{vaultClient: writer, config: &config.Config{ArgocdProtectedBackupEnabled: backupFlag}}
				r := gin.New()
				r.POST("/intake", func(c *gin.Context) {
					c.Set("user_id", protectedActor)
					c.Set("user_role", "admin")
					if token {
						c.Set("auth_type", "api_token")
						c.Set("api_token_name", "fixture")
					}
					h.SubmitSecretIntake(c)
				})
				w := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/intake", strings.NewReader(backupIntakeBody))
				request.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, request)
				if broadWriter {
					require.Equal(t, http.StatusBadRequest, w.Code)
					require.Contains(t, w.Body.String(), "fixed_backup_intake_endpoint_required")
				} else {
					require.Equal(t, http.StatusServiceUnavailable, w.Code)
					require.Contains(t, w.Body.String(), "vault_writer_disabled")
				}
				require.Zero(t, writer.writes)
				require.Zero(t, writer.legacy)
				require.False(t, writer.merged)
				require.Nil(t, writer.updates)
				require.Empty(t, writer.path)
			}
		}
	}
}

func TestArgocdBackupIntakeRefusesImportPartialAliasesAndRotation(t *testing.T) {
	for _, body := range []string{
		strings.Replace(backupIntakeBody, `enclii/argocd-backup-envelope`, ` enclii/argocd-backup-envelope`, 1),
		strings.Replace(backupIntakeBody, `argocd_backup_encryption_key`, `ARGOCD_BACKUP_ENCRYPTION_KEY`, 1),
		strings.Replace(backupIntakeBody, `argocd_backup_encryption_key`, `argocd-backup-encryption-key`, 1),
		strings.Replace(backupIntakeBody, `argocd_backup_encryption_key`, ` argocd_backup_encryption_key `, 1),
		strings.Replace(backupIntakeBody, `["argocd_backup_encryption_key"]`, `["argocd_backup_encryption_key","argocd_backup_encryption_key"]`, 1),
		strings.Replace(backupIntakeBody, `"generate":`, `"values":{"argocd_backup_encryption_key":"imported-canary"},"generate":`, 1),
		strings.Replace(backupIntakeBody, `"generate":["argocd_backup_encryption_key"]`, `"values":{"_enclii_argocd_backup_origin_v1":"supplied-origin-canary"}`, 1),
	} {
		writer := &backupIntakeWriter{}
		w := postIntake(t, &Handler{vaultClient: writer}, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Zero(t, writer.writes)
		require.Zero(t, writer.legacy)
		require.False(t, writer.merged)
		require.NotContains(t, w.Body.String(), "imported-canary")
		require.NotContains(t, w.Body.String(), "supplied-origin-canary")
	}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{lockbox.ErrSecretInitializationConflict, http.StatusConflict},
		{lockbox.ErrSecretInitializationOutcomeUnknown, http.StatusServiceUnavailable},
		{errors.New("private-provider-canary"), http.StatusInternalServerError},
	} {
		writer := &backupIntakeWriter{err: tc.err}
		// An extra rotate field never changes native whole-group CAS semantics.
		w := postIntake(t, &Handler{vaultClient: writer}, strings.Replace(backupIntakeBody, `"reason":`, `"rotate":true,"reason":`, 1))
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "fixed_backup_intake_endpoint_required")
		require.Zero(t, writer.writes)
		require.Zero(t, writer.legacy)
		require.False(t, writer.merged)
		require.NotContains(t, w.Body.String(), "private-provider-canary")
	}
	mergeOnly := &mergeOnlyWriter{}
	w := postIntake(t, &Handler{vaultClient: mergeOnly}, backupIntakeBody)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "fixed_backup_intake_endpoint_required")
	require.False(t, mergeOnly.merged)
	legacyOnly := &initializerWriter{}
	w = postIntake(t, &Handler{vaultClient: legacyOnly}, backupIntakeBody)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "fixed_backup_intake_endpoint_required")
	require.False(t, legacyOnly.initialized)
	require.False(t, legacyOnly.merged)
}

type backupKeyReader struct {
	mergeOnlyWriter
	calls   int
	origin  string
	version int
	key     []byte
	err     error
}

func (r *backupKeyReader) ReadArgocdBackupEnvelopeKey(_ context.Context, origin string, version int) ([]byte, error) {
	r.calls++
	r.origin, r.version = origin, version
	return r.key, r.err
}

func TestArgocdBackupInternalConsumerHasNoLatestOrMapFallback(t *testing.T) {
	const origin = "123e4567-e89b-42d3-a456-426614174000"
	for _, writer := range []VaultSecretWriter{nil, &mergeOnlyWriter{}} {
		key, err := (&Handler{vaultClient: writer}).argocdBackupEnvelopeKey(t.Context(), origin, 7)
		require.Error(t, err)
		require.Nil(t, key)
	}
	reader := &backupKeyReader{key: make([]byte, 32)}
	for _, ref := range []struct {
		origin string
		v      int
	}{{origin, 0}, {origin, -1}, {"alias", 7}} {
		_, err := (&Handler{vaultClient: reader}).argocdBackupEnvelopeKey(t.Context(), ref.origin, ref.v)
		require.Error(t, err)
		require.Zero(t, reader.calls)
	}
	key, err := (&Handler{vaultClient: reader}).argocdBackupEnvelopeKey(t.Context(), origin, 7)
	require.NoError(t, err)
	require.Len(t, key, 32)
	require.Equal(t, origin, reader.origin)
	require.Equal(t, 7, reader.version)
	require.Equal(t, 1, reader.calls)
	reader.key, reader.err = []byte("private-read-canary"), errors.New("private-upstream-canary")
	key, err = (&Handler{vaultClient: reader}).argocdBackupEnvelopeKey(t.Context(), origin, 7)
	require.Nil(t, key)
	require.NotContains(t, err.Error(), "canary")
	require.Equal(t, make([]byte, len(reader.key)), reader.key)
}

func TestNativeNormalizerAPIWrapperParity(t *testing.T) {
	for _, raw := range []string{"API_KEY", "a/B--c", "á_KＥＹ\x00ß", "\xffA\xfeB", "😀", "__a...B_"} {
		require.Equal(t, secretsintake.NormalizeVaultSecretKey(raw), normalizeVaultSecretKey(raw))
	}
}
