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

func TestSIMSSOCommandDefaultsGETOnlyAndRefusesApplyWithoutExpectedPlan(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "Bearer managed-fixture", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("X-Internal-API-Key"))
		actual := map[string]interface{}{"id": "cccccccc-cccc-4ccc-8ccc-cccccccccccc", "client_id": "jnc_sim_fixture", "name": "crea-sim", "audience": "crea-sim", "organization_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "redirect_uris": []string{"https://sim.creatumundo.mx/auth/callback"}, "allowed_scopes": []string{"openid", "profile", "email", "offline_access"}, "grant_types": []string{"authorization_code", "refresh_token"}, "is_active": true, "is_confidential": true}
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
	cfg := &config.Config{APIToken: "api-fixture", Credentials: &config.Credentials{AccessToken: "managed-fixture"}}
	root := newSecretsProvisionCommand(cfg)
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"oidc", "sim-sso"})
	require.NoError(t, root.Execute())
	require.Equal(t, 2, calls)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(output.Bytes(), &plan))
	require.Equal(t, false, plan["applied"])
	require.Equal(t, true, plan["change_needed"])
	for _, args := range [][]string{{"--apply"}, {"--platform", "crea-sim-mcp"}, {"--rotate-secret"}, {"--all"}, {"--registry", "other"}} {
		cmd := newSecretsProvisionCommand(cfg)
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs(append([]string{"oidc", "sim-sso"}, args...))
		require.Error(t, cmd.Execute())
		require.Equal(t, 2, calls)
	}
}
