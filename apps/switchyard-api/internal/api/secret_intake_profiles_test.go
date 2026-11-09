package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
	"github.com/stretchr/testify/require"
)

const cryptoIntakeBody = `{"target":"janua-staging/runtime-crypto","generate":["jwt_private_key","jwt_public_key","jwt_secret_key","secret_key","field_encryption_key"],"reason":"isolated stage initialization"}`
const redisIntakeBody = `{"target":"janua-staging/redis","generate":["redis_password","redis_url"],"reason":"isolated stage initialization"}`

func TestTypedStagingCryptoIsUsableAndNeverEchoed(t *testing.T) {
	var written map[string]interface{}
	h := newIntakeVaultRecorder(t, &written)
	w := postIntake(t, h, cryptoIntakeBody)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Len(t, written, 5)

	privatePEM, rest := pem.Decode([]byte(written["jwt_private_key"].(string)))
	require.NotNil(t, privatePEM)
	require.Empty(t, rest)
	privateAny, err := x509.ParsePKCS8PrivateKey(privatePEM.Bytes)
	require.NoError(t, err)
	private := privateAny.(*rsa.PrivateKey)
	require.Equal(t, 2048, private.N.BitLen())
	publicPEM, rest := pem.Decode([]byte(written["jwt_public_key"].(string)))
	require.NotNil(t, publicPEM)
	require.Empty(t, rest)
	publicAny, err := x509.ParsePKIXPublicKey(publicPEM.Bytes)
	require.NoError(t, err)
	public := publicAny.(*rsa.PublicKey)
	digest := sha256.Sum256([]byte("isolated-stage-compatibility"))
	signature, err := rsa.SignPKCS1v15(rand.Reader, private, crypto.SHA256, digest[:])
	require.NoError(t, err)
	require.NoError(t, rsa.VerifyPKCS1v15(public, crypto.SHA256, digest[:], signature))
	fernet := written["field_encryption_key"].(string)
	require.Len(t, fernet, 44)
	require.Equal(t, byte('='), fernet[43])
	decoded, err := base64.URLEncoding.DecodeString(fernet)
	require.NoError(t, err)
	require.Len(t, decoded, 32)
	for _, key := range []string{"secret_key", "jwt_secret_key"} {
		decoded, err := base64.RawURLEncoding.DecodeString(written[key].(string))
		require.NoError(t, err)
		require.Len(t, decoded, 32)
	}
	require.NotEqual(t, written["secret_key"], written["jwt_secret_key"])
	assertIntakeValuesNotDisclosed(t, h, w.Body.Bytes(), written)
}

func TestTypedStagingRedisMatchesPrivateServiceAndNeverEchoed(t *testing.T) {
	var written map[string]interface{}
	h := newIntakeVaultRecorder(t, &written)
	w := postIntake(t, h, redisIntakeBody)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Len(t, written, 2)
	endpoint, err := url.Parse(written["redis_url"].(string))
	require.NoError(t, err)
	require.Equal(t, "redis", endpoint.Scheme)
	require.Equal(t, "janua-staging-redis:6379", endpoint.Host)
	require.Equal(t, "/0", endpoint.Path)
	password, ok := endpoint.User.Password()
	require.True(t, ok)
	require.Equal(t, written["redis_password"], password)
	require.Len(t, password, 43)
	assertIntakeValuesNotDisclosed(t, h, w.Body.Bytes(), written)
}

func assertIntakeValuesNotDisclosed(t *testing.T, h *Handler, response []byte, written map[string]interface{}) {
	t.Helper()
	var status secretIntakeStatus
	require.NoError(t, json.Unmarshal(response, &status))
	stored, err := h.loadIntakeStatus(t.Context(), status.IntakeID)
	require.NoError(t, err)
	metadata, err := json.Marshal(stored)
	require.NoError(t, err)
	for _, value := range written {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		// Compare the JSON-escaped representation too, so a PEM with newlines
		// cannot evade the response/cache assertion through string escaping.
		require.NotContains(t, string(response), string(encoded))
		require.NotContains(t, string(metadata), string(encoded))
	}
	require.Len(t, status.KeysGenerated, len(written))
	require.Equal(t, status.KeysWritten, status.KeysGenerated)
}

func TestTypedStagingRefusesPartialSuppliedAndGenericGeneration(t *testing.T) {
	cases := []string{
		`{"target":"janua-staging/runtime-crypto","generate":["jwt_private_key"],"reason":"r"}`,
		`{"target":"janua-staging/runtime-crypto","values":{"jwt_private_key":"imported"},"reason":"r"}`,
		`{"target":"janua-staging/redis","generate":["redis_password","redis_password"],"reason":"r"}`,
		`{"target":"janua-staging/redis","generate":["redis_password","wrong"],"reason":"r"}`,
		`{"target":"janua-staging/redis","generate":["redis_password"],"values":{"redis_url":"redis://shared"},"reason":"r"}`,
		`{"target":"janua-staging/database","generate":["database_url"],"reason":"r"}`,
		`{"target":"janua-staging/database-ca","generate":["database_ca"],"reason":"r"}`,
	}
	for _, body := range cases {
		var written map[string]interface{}
		h := newIntakeVaultRecorder(t, &written)
		w := postIntake(t, h, body)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Nil(t, written)
	}
}

type mergeOnlyWriter struct{ merged bool }

func (*mergeOnlyWriter) IsEnabled() bool { return true }
func (w *mergeOnlyWriter) MergeSecretData(context.Context, string, map[string]interface{}) (int, error) {
	w.merged = true
	return 1, nil
}
func (*mergeOnlyWriter) GetSecretData(context.Context, string) (map[string]interface{}, error) {
	return nil, nil
}

func TestTypedStagingNeverFallsBackToMergeOnlyWriter(t *testing.T) {
	writer := &mergeOnlyWriter{}
	w := postIntake(t, &Handler{vaultClient: writer}, redisIntakeBody)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.False(t, writer.merged)
}

type initializerWriter struct {
	mergeOnlyWriter
	err         error
	initialized bool
}

func (w *initializerWriter) InitializeSecretData(context.Context, string, map[string]interface{}) (int, error) {
	w.initialized = true
	return 0, w.err
}

func TestTypedStagingConflictAndUnknownOutcomeNeverMergeOrRetry(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{lockbox.ErrSecretInitializationConflict, http.StatusConflict, "vault_initialization_conflict"},
		{lockbox.ErrSecretInitializationOutcomeUnknown, http.StatusServiceUnavailable, "vault_initialization_outcome_unknown"},
	} {
		writer := &initializerWriter{err: tc.err}
		w := postIntake(t, &Handler{vaultClient: writer}, redisIntakeBody)
		require.Equal(t, tc.status, w.Code)
		require.Contains(t, w.Body.String(), tc.code)
		require.True(t, writer.initialized)
		require.False(t, writer.merged)
	}
}
