package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

func TestArgocdBackupNativeIntakeCreatesOnlyOpaqueHeldCustody(t *testing.T) {
	writer := &backupIntakeWriter{}
	h := &Handler{vaultClient: writer}
	w := postIntake(t, h, backupIntakeBody)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Equal(t, 1, writer.writes)
	require.Zero(t, writer.legacy)
	require.False(t, writer.merged)
	require.Equal(t, "secret/enclii", writer.path)
	require.Len(t, writer.updates, 2)
	encoded := writer.updates[secretsintake.ArgocdBackupKey].(string)
	key, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	require.NoError(t, err)
	require.Len(t, key, 32)
	clear(key)
	var status secretIntakeStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	require.Equal(t, "initialized_held", status.Status)
	require.NotNil(t, status.NativeReady)
	require.False(t, *status.NativeReady)
	require.Equal(t, 7, status.VaultVersion)
	require.Empty(t, status.ExternalSecret)
	require.False(t, status.ExternalSecretRefreshed)
	require.Equal(t, []string{secretsintake.ArgocdBackupKey}, status.KeysWritten)
	require.Equal(t, status.KeysWritten, status.KeysGenerated)
	origin := writer.updates[secretsintake.ArgocdBackupOriginKey].(string)
	require.NoError(t, secretsintake.ValidateArgocdBackupOrigin(origin, status.OriginID, time.Now().UTC()))
	require.NotContains(t, w.Body.String(), encoded)
	require.NotContains(t, w.Body.String(), origin)
	require.NotContains(t, w.Body.String(), "sha256")
	stored, err := h.loadIntakeStatus(t.Context(), status.IntakeID)
	require.NoError(t, err)
	metadata, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NotContains(t, string(metadata), encoded)
	require.NotContains(t, string(metadata), origin)
	require.Equal(t, status.OriginID, stored.OriginID)
	require.False(t, *stored.NativeReady)
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
		require.Equal(t, tc.status, w.Code)
		require.Equal(t, 1, writer.writes)
		require.Zero(t, writer.legacy)
		require.False(t, writer.merged)
		require.NotContains(t, w.Body.String(), "private-provider-canary")
	}
	mergeOnly := &mergeOnlyWriter{}
	w := postIntake(t, &Handler{vaultClient: mergeOnly}, backupIntakeBody)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.False(t, mergeOnly.merged)
	legacyOnly := &initializerWriter{}
	w = postIntake(t, &Handler{vaultClient: legacyOnly}, backupIntakeBody)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
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
