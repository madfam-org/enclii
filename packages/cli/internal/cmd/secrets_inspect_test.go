package cmd

import (
	"bytes"
	"encoding/json"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOIDCInspectUsesManagedCredentialsAndOnlyGETNoIntake(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "Bearer managed-fixture", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("X-Internal-API-Key"))
		actual := map[string]interface{}{"id": "cccccccc-cccc-4ccc-8ccc-cccccccccccc", "client_id": "jnc_sim_fixture", "name": "crea-sim", "audience": "crea-sim", "organization_id": nil, "redirect_uris": []string{"https://sim.creatumundo.mx/auth/callback"}, "allowed_scopes": []string{"openid", "profile", "email", "offline_access"}, "grant_types": []string{"authorization_code", "refresh_token"}, "is_active": true, "is_confidential": true}
		switch r.URL.Path {
		case "/api/v1/oauth/clients/admin/all":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": []interface{}{actual}, "total": 1})
		case "/api/v1/oauth/clients/cccccccc-cccc-4ccc-8ccc-cccccccccccc":
			_ = json.NewEncoder(w).Encode(actual)
		default:
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()
	t.Setenv("ENCLII_JANUA_API_URL", server.URL)
	t.Setenv("JANUA_INTERNAL_API_KEY", "must-not-be-used")
	root := newSecretsProvisionCommand(&config.Config{APIEndpoint: server.URL, APIToken: "api-fixture", Credentials: &config.Credentials{AccessToken: "managed-fixture"}})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"oidc", "inspect", "--platform", "crea-sim", "--json"})
	require.NoError(t, root.Execute())
	require.Equal(t, 2, calls)
	var actual map[string]interface{}
	require.NoError(t, json.Unmarshal(output.Bytes(), &actual))
	require.Len(t, actual, 10)
	require.Equal(t, "jnc_sim_fixture", actual["client_id"])
	require.NotContains(t, output.String(), "secret")
	require.NotContains(t, output.String(), "managed-fixture")
}
func TestOIDCInspectRefusesMissingAuthUnknownProfileAndMutationFlagsLocally(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected network call %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	defer server.Close()
	t.Setenv("ENCLII_JANUA_API_URL", server.URL)
	for _, tc := range []struct {
		token string
		args  []string
	}{
		{"", []string{"--platform", "crea-sim"}},
		{"fixture", []string{"--platform", "missing"}},
		{"fixture", []string{}},
		{"fixture", []string{"--platform", "crea-sim", "--rotate-secret"}},
		{"fixture", []string{"--all"}},
		{"fixture", []string{"--registry", "anything"}},
	} {
		root := newSecretsProvisionCommand(&config.Config{APIToken: tc.token})
		root.SilenceErrors = true
		root.SilenceUsage = true
		root.SetArgs(append([]string{"oidc", "inspect"}, tc.args...))
		require.Error(t, root.Execute())
	}
}
