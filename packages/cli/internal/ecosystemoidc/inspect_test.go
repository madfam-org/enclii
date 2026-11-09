package ecosystemoidc

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInspectOnlyReadsActualMetadataWithoutReconciliation(t *testing.T) {
	actual := loginRemote()
	actual.IsActive = false
	actual.AllowedScopes = []string{"openid"}
	reader := inventoryClient(t, []remoteOAuthClient{actual})
	reader.InternalAPIKey = "must-not-be-used"
	metadata, err := reader.InspectClient(context.Background(), loginSpec())
	require.NoError(t, err)
	require.Equal(t, actual.ClientID, metadata.ClientID)
	require.False(t, metadata.IsActive)
	require.Equal(t, []string{"openid"}, metadata.AllowedScopes, "inspection reports drift without fixing it")
	raw, err := json.Marshal(metadata)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
	require.NotContains(t, string(raw), `"access_token":`)
	require.NotContains(t, string(raw), `"refresh_token":`)
}
func TestInspectRefusesAbsentAmbiguousAndMissingManagedAuth(t *testing.T) {
	second := loginRemote()
	second.ClientID = "jnc_second"
	for _, clients := range [][]remoteOAuthClient{nil, {loginRemote(), second}} {
		_, err := inventoryClient(t, clients).InspectClient(context.Background(), loginSpec())
		require.Error(t, err)
	}
	reader := inventoryClient(t, nil)
	reader.AdminToken = ""
	reader.InternalAPIKey = "not-a-managed-session"
	_, err := reader.InspectClient(context.Background(), loginSpec())
	require.ErrorContains(t, err, "managed")
}
func TestInspectRejectsCredentialBearingResponsesAndRedactsErrors(t *testing.T) {
	for _, location := range []string{"inventory", "detail", "error", "invalid", "duplicate"} {
		t.Run(location, func(t *testing.T) {
			secret := "sensitive-fixture-do-not-emit"
			actual := loginRemote()
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("X-Internal-API-Key"))
				if location == "error" {
					w.WriteHeader(403)
					_, _ = w.Write([]byte(secret))
					return
				}
				if location == "invalid" {
					_, _ = w.Write([]byte(secret))
					return
				}
				if location == "duplicate" {
					_, _ = w.Write([]byte(`{"clients":[],"clients":[],"credential":"` + secret + `"}`))
					return
				}
				wire := map[string]interface{}{}
				raw, _ := json.Marshal(actual)
				_ = json.Unmarshal(raw, &wire)
				if (location == "inventory" && strings.HasSuffix(r.URL.Path, "/all")) || (location == "detail" && !strings.HasSuffix(r.URL.Path, "/all")) {
					wire["refresh_token"] = secret
				}
				if strings.HasSuffix(r.URL.Path, "/all") {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": []interface{}{wire}, "total": 1})
				} else {
					_ = json.NewEncoder(w).Encode(wire)
				}
			}))
			defer server.Close()
			reader := &JanuaClient{BaseURL: server.URL, AdminToken: "fixture", HTTP: server.Client()}
			_, err := reader.InspectClient(context.Background(), loginSpec())
			require.Error(t, err)
			require.NotContains(t, err.Error(), secret)
			require.LessOrEqual(t, count, 2)
		})
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("sensitive-fixture-network-error")
}
func TestInspectRedactsTransportError(t *testing.T) {
	reader := &JanuaClient{BaseURL: "https://example.test", AdminToken: "fixture", HTTP: &http.Client{Transport: failingTransport{}}}
	_, err := reader.InspectClient(context.Background(), loginSpec())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sensitive")
}

func TestMetadataModeCannotSendMutationBodyOrInternalCredential(t *testing.T) {
	reader := inventoryClient(t, nil)
	reader.metadataOnly = true
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut} {
		_, err := reader.doJSON(context.Background(), method, "/forbidden", nil, nil, false)
		require.ErrorContains(t, err, "GET only")
	}
	_, err := reader.doJSON(context.Background(), http.MethodGet, "/forbidden", map[string]string{"fixture": "body"}, nil, false)
	require.ErrorContains(t, err, "GET only")
	_, err = reader.doJSON(context.Background(), http.MethodGet, "/forbidden", nil, nil, true)
	require.ErrorContains(t, err, "GET only")
}
