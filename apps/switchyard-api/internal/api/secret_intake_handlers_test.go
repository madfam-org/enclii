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

func TestSubmitSecretIntake_ValidationErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	vaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	r.POST("/v1/secrets/intake", h.SubmitSecretIntake)

	cases := []struct {
		name string
		body string
		code int
	}{
		{"missing reason", `{"target":"ceq/vast-api-key","values":{"VAST_API_KEY":"x"}}`, http.StatusBadRequest},
		{"unknown target", `{"target":"nope/key","values":{"X":"y"},"reason":"test"}`, http.StatusNotFound},
		{"disallowed key", `{"target":"ceq/vast-api-key","values":{"NOT_ALLOWED":"x"},"reason":"test"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/secrets/intake", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tc.code, w.Code)
		})
	}
}

func TestGetSecretIntakeStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	status := secretIntakeStatus{
		IntakeID: "int_test123",
		TargetID: "ceq/vast-api-key",
		Status:   "ready",
	}
	require.NoError(t, h.saveIntakeStatus(t.Context(), status))

	r := gin.New()
	r.GET("/v1/secrets/intake/:id", h.GetSecretIntakeStatus)

	req := httptest.NewRequest(http.MethodGet, "/v1/secrets/intake/int_test123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "int_test123")
	assert.NotContains(t, w.Body.String(), "super-secret")

	req2 := httptest.NewRequest(http.MethodGet, "/v1/secrets/intake/int_missing", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusNotFound, w2.Code)
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
	assert.Contains(t, w.Body.String(), "dhanam/stripe-mx-live")
	assert.Contains(t, w.Body.String(), "platform/comms-resend-api-key")
	assert.Contains(t, w.Body.String(), "dhanam/oidc-janua")
}

// newIntakeVaultRecorder returns a Handler whose Vault writes are captured, so
// tests can assert on what would land in Vault without ever asserting through
// the HTTP response (which must never carry values).
func newIntakeVaultRecorder(t *testing.T, captured *map[string]interface{}) *Handler {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Data map[string]interface{} `json:"data"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			*captured = body.Data
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"version":7}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return &Handler{
		vaultClient: lockbox.NewVaultClient(&lockbox.VaultConfig{
			Address: server.URL,
			Token:   "test-token",
			Enabled: true,
		}),
	}
}

func postIntake(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.POST("/v1/secrets/intake", func(c *gin.Context) {
		c.Set("user_id", "janua:test-user")
		h.SubmitSecretIntake(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/secrets/intake", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSubmitSecretIntake_GeneratesValueNeverEchoed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var written map[string]interface{}
	h := newIntakeVaultRecorder(t, &written)

	w := postIntake(t, h, `{"target":"crea-map/internal-api-key","generate":["internal_api_key"],"reason":"smoke gate bootstrap"}`)
	require.Equal(t, http.StatusAccepted, w.Code)

	// The value reached Vault...
	require.Contains(t, written, "internal_api_key")
	generated, ok := written["internal_api_key"].(string)
	require.True(t, ok)
	require.NotEmpty(t, generated)
	// 32 random bytes as unpadded base64url is 43 characters.
	assert.Len(t, generated, 43)
	assert.NotContains(t, generated, "=", "value should be unpadded base64url")

	// ...and reached nobody else.
	assert.NotContains(t, w.Body.String(), generated)

	var resp secretIntakeStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ready", resp.Status)
	assert.Equal(t, []string{"INTERNAL_API_KEY"}, resp.KeysGenerated)
	assert.Equal(t, []string{"INTERNAL_API_KEY"}, resp.KeysWritten)

	// The persisted audit record names the key and its provenance, not the value.
	stored, err := h.loadIntakeStatus(t.Context(), resp.IntakeID)
	require.NoError(t, err)
	assert.Equal(t, []string{"INTERNAL_API_KEY"}, stored.KeysGenerated)
	assert.Equal(t, "janua:test-user", stored.ActorSub)
	assert.Equal(t, "smoke gate bootstrap", stored.Reason)
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), generated)
}

func TestSubmitSecretIntake_GenerateIsUniquePerCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var first, second map[string]interface{}

	h1 := newIntakeVaultRecorder(t, &first)
	require.Equal(t, http.StatusAccepted,
		postIntake(t, h1, `{"target":"nauta/symbiosis-hcm-token","generate":["symbiosis_hcm_token"],"reason":"r"}`).Code)

	h2 := newIntakeVaultRecorder(t, &second)
	require.Equal(t, http.StatusAccepted,
		postIntake(t, h2, `{"target":"nauta/symbiosis-hcm-token","generate":["symbiosis_hcm_token"],"reason":"r"}`).Code)

	assert.NotEqual(t, first["symbiosis_hcm_token"], second["symbiosis_hcm_token"],
		"each generation must draw fresh entropy")
}

func TestSubmitSecretIntake_GenerateMixedWithSuppliedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var written map[string]interface{}
	h := newIntakeVaultRecorder(t, &written)

	body := `{"target":"symbiosis-hcm/map-absence-feed",` +
		`"values":{"map_absence_feed_url":"https://hcm.example.mx/feed"},` +
		`"generate":["map_absence_feed_key"],"reason":"hcm feed bootstrap"}`
	w := postIntake(t, h, body)
	require.Equal(t, http.StatusAccepted, w.Code)

	assert.Equal(t, "https://hcm.example.mx/feed", written["map_absence_feed_url"])
	assert.NotEmpty(t, written["map_absence_feed_key"])

	var resp secretIntakeStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, []string{"MAP_ABSENCE_FEED_KEY"}, resp.KeysGenerated)
	assert.Equal(t, []string{"MAP_ABSENCE_FEED_KEY", "MAP_ABSENCE_FEED_URL"}, resp.KeysWritten)
	assert.NotContains(t, w.Body.String(), written["map_absence_feed_key"].(string))
}

func TestSubmitSecretIntake_GenerateRejections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var written map[string]interface{}

	cases := []struct {
		name string
		body string
		code int
	}{
		{
			"key not declared by target",
			`{"target":"crea-map/internal-api-key","generate":["not_a_key"],"reason":"r"}`,
			http.StatusBadRequest,
		},
		{
			"same key supplied and generated",
			`{"target":"crea-map/internal-api-key","values":{"internal_api_key":"x"},"generate":["internal_api_key"],"reason":"r"}`,
			http.StatusBadRequest,
		},
		{
			"key listed twice",
			`{"target":"crea-map/kalya-feeds","generate":["kalya_capacity_feed_url","KALYA_CAPACITY_FEED_URL"],"reason":"r"}`,
			http.StatusBadRequest,
		},
		{
			"empty key",
			`{"target":"crea-map/internal-api-key","generate":["  "],"reason":"r"}`,
			http.StatusBadRequest,
		},
		{
			"generate without reason",
			`{"target":"crea-map/internal-api-key","generate":["internal_api_key"]}`,
			http.StatusBadRequest,
		},
		{
			"neither values nor generate",
			`{"target":"crea-map/internal-api-key","reason":"r"}`,
			http.StatusBadRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			written = nil
			h := newIntakeVaultRecorder(t, &written)
			w := postIntake(t, h, tc.body)
			assert.Equal(t, tc.code, w.Code)
			assert.Nil(t, written, "a rejected intake must not write to Vault")
		})
	}
}

func TestGenerateSecretValueEntropy(t *testing.T) {
	v, err := generateSecretValue(32)
	require.NoError(t, err)
	assert.Len(t, v, 43)

	// A zero/negative policy falls back to the registry default rather than
	// producing an empty (and therefore worthless) secret.
	v0, err := generateSecretValue(0)
	require.NoError(t, err)
	assert.Len(t, v0, 43)
	assert.NotEqual(t, v, v0)
}

func TestIntakeSourceLabel(t *testing.T) {
	assert.Equal(t, "supplied", intakeSourceLabel([]string{"A"}, nil))
	assert.Equal(t, "generated", intakeSourceLabel([]string{"A"}, []string{"A"}))
	assert.Equal(t, "mixed", intakeSourceLabel([]string{"A", "B"}, []string{"B"}))
}
