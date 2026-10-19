package ecosystemoidc

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func simRemote() remoteOAuthClient {
	value := loginRemote()
	value.Name = "crea-sim"
	value.ClientID = "jnc_training_ui_fixture"
	value.ClientKey = pointer("crea-sim")
	value.Audience = pointer("crea-sim")
	value.OrganizationID = pointer("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	value.RedirectURIs = []string{"https://sim.creatumundo.mx/auth/callback"}
	value.IsConfidential = true
	value.AllowedScopes = []string{"openid", "profile", "email", "offline_access"}
	return value
}
func simSpec(t *testing.T) JanuaClientSpec {
	t.Helper()
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	return reg.Platforms["crea-sim"].JanuaClient
}

func scopeServer(t *testing.T, actual *remoteOAuthClient, patches *int, mode string) *JanuaClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer fixture", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("X-Internal-API-Key"))
		if r.Method == http.MethodPatch && r.URL.Path == "/api/v1/oauth/clients/"+actual.ID {
			*patches++
			var body map[string]json.RawMessage
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Len(t, body, 1)
			require.Contains(t, body, "allowed_scopes")
			require.NoError(t, json.Unmarshal(body["allowed_scopes"], &actual.AllowedScopes))
			switch mode {
			case "denied":
				w.WriteHeader(403)
				_, _ = w.Write([]byte("sensitive-fixture-provider-error"))
				return
			case "unknown":
				w.WriteHeader(500)
				return
			case "drift":
				actual.OrganizationID = pointer("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
			case "credential":
				_ = json.NewEncoder(w).Encode(map[string]string{"client_secret": "sensitive-fixture-provider-secret"})
				return
			}
			_ = json.NewEncoder(w).Encode(actual)
			return
		}
		require.Equal(t, http.MethodGet, r.Method, "no create/rotate/resolve/intake permitted")
		switch r.URL.Path {
		case "/api/v1/oauth/clients/admin/all":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": []remoteOAuthClient{*actual}, "total": 1})
		case "/api/v1/oauth/clients/" + actual.ID:
			_ = json.NewEncoder(w).Encode(actual)
		default:
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return &JanuaClient{BaseURL: server.URL, AdminToken: "fixture", InternalAPIKey: "must-not-be-used", HTTP: server.Client()}
}

func TestSIMSSODefaultPlanGETOnlyThenExactScopeOnlyApplyAndReversiblePlan(t *testing.T) {
	actual := simRemote()
	patches := 0
	reader := scopeServer(t, &actual, &patches, "")
	spec := simSpec(t)
	plan, err := reader.ConfigureSIMSSO(context.Background(), spec, "", false, false, "")
	require.NoError(t, err)
	require.True(t, plan.Changed)
	require.False(t, plan.Applied)
	require.Zero(t, patches)
	require.Equal(t, append(append([]string(nil), plan.Before.AllowedScopes...), simSilentScope), plan.After.AllowedScopes)
	require.Equal(t, plan.Before.OrganizationID, plan.After.OrganizationID)
	require.Equal(t, plan.Before.ClientID, plan.After.ClientID)
	require.Equal(t, plan.Before.RedirectURIs, plan.After.RedirectURIs)
	applied, err := reader.ConfigureSIMSSO(context.Background(), spec, plan.MetadataSHA256, true, false, "synthetic scope-only fixture")
	require.NoError(t, err)
	require.True(t, applied.Applied)
	require.Equal(t, 1, patches)
	noop, err := reader.ConfigureSIMSSO(context.Background(), spec, "", false, false, "")
	require.NoError(t, err)
	require.False(t, noop.Changed)
	require.Equal(t, 1, patches)
	rollback, err := reader.ConfigureSIMSSO(context.Background(), spec, "", false, true, "")
	require.NoError(t, err)
	require.Equal(t, plan.Before.AllowedScopes, rollback.After.AllowedScopes)
	require.Equal(t, 1, patches)
	reverted, err := reader.ConfigureSIMSSO(context.Background(), spec, rollback.MetadataSHA256, true, true, "synthetic disable after app flag-off")
	require.NoError(t, err)
	require.True(t, reverted.Applied)
	require.Equal(t, 2, patches)
	require.Equal(t, plan.Before.AllowedScopes, actual.AllowedScopes)
}

func TestSIMSSORejectsStaleFingerprintUnknownContractAndUnsafeApplyBeforeMutation(t *testing.T) {
	for _, mutation := range []func(*remoteOAuthClient){func(a *remoteOAuthClient) { a.IsActive = false }, func(a *remoteOAuthClient) { a.IsConfidential = false }, func(a *remoteOAuthClient) { a.OrganizationID = nil }, func(a *remoteOAuthClient) { a.Audience = pointer("another-app") }, func(a *remoteOAuthClient) { a.RedirectURIs = append(a.RedirectURIs, "https://elsewhere.invalid") }, func(a *remoteOAuthClient) { a.AllowedScopes = append(a.AllowedScopes, "other:privilege") }, func(a *remoteOAuthClient) { a.GrantTypes = []string{"client_credentials"} }} {
		actual := simRemote()
		mutation(&actual)
		patches := 0
		reader := scopeServer(t, &actual, &patches, "")
		_, err := reader.ConfigureSIMSSO(context.Background(), simSpec(t), "", false, false, "")
		require.Error(t, err)
		require.Zero(t, patches)
	}
	actual := simRemote()
	patches := 0
	reader := scopeServer(t, &actual, &patches, "")
	spec := simSpec(t)
	plan, err := reader.ConfigureSIMSSO(context.Background(), spec, "", false, false, "")
	require.NoError(t, err)
	actual.OrganizationID = pointer("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	_, err = reader.ConfigureSIMSSO(context.Background(), spec, plan.MetadataSHA256, true, false, "fixture")
	require.ErrorContains(t, err, "metadata changed")
	require.Zero(t, patches)
	for _, input := range []struct{ hash, reason string }{{"", "fixture"}, {"not-sha", "fixture"}, {plan.MetadataSHA256, ""}, {plan.MetadataSHA256, "line\nbreak"}} {
		_, err = reader.ConfigureSIMSSO(context.Background(), spec, input.hash, true, false, input.reason)
		require.Error(t, err)
		require.Zero(t, patches)
	}
	spec.Name = "crea-sim-mcp"
	_, err = reader.ConfigureSIMSSO(context.Background(), spec, "", false, false, "")
	require.Error(t, err)
	require.Zero(t, patches)
}

func TestSIMSSOUncertainOrDriftedPatchHeldWithoutRetryOrCredentialDisclosure(t *testing.T) {
	for _, mode := range []string{"denied", "unknown", "drift", "credential"} {
		t.Run(mode, func(t *testing.T) {
			actual := simRemote()
			patches := 0
			reader := scopeServer(t, &actual, &patches, mode)
			spec := simSpec(t)
			plan, err := reader.ConfigureSIMSSO(context.Background(), spec, "", false, false, "")
			require.NoError(t, err)
			_, err = reader.ConfigureSIMSSO(context.Background(), spec, plan.MetadataSHA256, true, false, "synthetic fixture")
			require.ErrorContains(t, err, "HELD")
			require.NotContains(t, err.Error(), "sensitive-fixture")
			require.Equal(t, 1, patches, "no retry or automatic rollback")
		})
	}
}
