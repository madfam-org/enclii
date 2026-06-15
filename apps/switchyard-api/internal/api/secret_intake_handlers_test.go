package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
)

func TestSubmitSecretIntake_VaultWriterDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	r := gin.New()
	r.POST("/v1/secrets/intake", h.SubmitSecretIntake)

	body := `{"target":"ceq/vast-api-key","values":{"VAST_API_KEY":"x"},"reason":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/intake", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestSubmitSecretIntake_SuccessNoValuesInResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var sawVaultWrite bool
	vaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			sawVaultWrite = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"version":3}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer vaultServer.Close()

	h := &Handler{
		vaultClient: lockbox.NewVaultClient(&lockbox.VaultConfig{
			Address: vaultServer.URL,
			Token:   "test-token",
			Enabled: true,
		}),
	}

	r := gin.New()
	r.POST("/v1/secrets/intake", func(c *gin.Context) {
		c.Set("user_id", "janua:test-user")
		h.SubmitSecretIntake(c)
	})

	body := `{"target":"ceq/vast-api-key","values":{"VAST_API_KEY":"super-secret-value"},"reason":"unit test"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/intake", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusAccepted, w.Code)
	assert.True(t, sawVaultWrite)

	var resp secretIntakeStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ready", resp.Status)
	assert.Equal(t, "ceq/vast-api-key", resp.TargetID)
	assert.Contains(t, resp.KeysWritten, "VAST_API_KEY")
	assert.NotContains(t, w.Body.String(), "super-secret-value")

	got, err := h.loadIntakeStatus(req.Context(), resp.IntakeID)
	require.NoError(t, err)
	assert.Equal(t, resp.IntakeID, got.IntakeID)
}

func TestListSecretIntakeTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	r := gin.New()
	r.GET("/v1/secrets/intake/targets", h.ListSecretIntakeTargets)

	req := httptest.NewRequest(http.MethodGet, "/v1/secrets/intake/targets", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ceq/vast-api-key")
}
