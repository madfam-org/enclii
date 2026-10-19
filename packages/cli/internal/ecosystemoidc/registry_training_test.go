package ecosystemoidc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrainingHumanClientsHaveSeparateClosedCallbacks(t *testing.T) {
	registry, err := LoadRegistry("")
	require.NoError(t, err)
	for _, tc := range []struct {
		id, callback, target string
		scopes               []string
	}{
		{"crea-sim", "https://sim.creatumundo.mx/auth/callback", "crea-sim/ui-oidc", []string{"openid", "profile", "email", "offline_access", "madfam:silent_auth"}},
		{"crea-sim-mcp", "https://sim.creatumundo.mx/auth/mcp/callback", "crea-sim/mcp-oidc", []string{"openid", "profile", "email", "offline_access"}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			platform, found := registry.Platforms[tc.id]
			require.True(t, found)
			require.Equal(t, tc.target, platform.IntakeTarget)
			client := platform.JanuaClient
			require.Equal(t, tc.id, client.ClientKey)
			require.Equal(t, tc.id, client.Audience)
			require.Empty(t, client.ClientID, "registration must yield actual ID, never a logical-name guess")
			require.Empty(t, client.OrganizationID, "human membership is checked on the minted UI token")
			require.True(t, client.confidential())
			require.Equal(t, []string{tc.callback}, client.RedirectURIs)
			require.Equal(t, tc.scopes, client.AllowedScopes)
			if tc.id == "crea-sim-mcp" {
				require.NotContains(t, client.AllowedScopes, "madfam:silent_auth")
			}
			require.Equal(t, []string{"authorization_code", "refresh_token"}, client.GrantTypes)
			require.NotContains(t, client.AllowedScopes, "map.ops:read")
			require.NotContains(t, client.AllowedScopes, "map.cobro:read")
			require.NotContains(t, client.AllowedScopes, "map.sim:read")
			require.NotContains(t, client.AllowedScopes, "map.sim:write")
		})
	}
}

func TestTrainingOrganizationOverrideKeepsBothDeclaredCallbacksAndPilotScopes(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	for _, id := range []string{"crea-sim", "crea-sim-mcp"} {
		before := reg.Platforms[id]
		scoped, err := reg.WithOrganization(id, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
		require.NoError(t, err)
		after := scoped.Platforms[id]
		require.Equal(t, before.JanuaClient.RedirectURIs, after.JanuaClient.RedirectURIs)
		require.Equal(t, before.JanuaClient.AllowedScopes, after.JanuaClient.AllowedScopes)
		require.Equal(t, before.IntakeTarget, after.IntakeTarget)
		require.Equal(t, before.IntakeKeyMap, after.IntakeKeyMap)
		kind, err := after.JanuaClient.kind()
		require.NoError(t, err)
		require.Equal(t, humanClient, kind)
		require.Empty(t, reg.Platforms[id].JanuaClient.OrganizationID)
	}
	_, exists := reg.Platforms["madfam-sim"]
	require.False(t, exists, "generic client support must not invent an unreviewed profile")
}
