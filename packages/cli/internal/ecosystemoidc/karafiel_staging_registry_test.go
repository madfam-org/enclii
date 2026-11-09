package ecosystemoidc

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKarafielStagingClientsAreSeparateConfidentialFixedAudienceRegistrations(t *testing.T) {
	registry, err := LoadRegistry("")
	require.NoError(t, err)
	for _, role := range []string{"web", "admin"} {
		client := registry.Platforms["karafiel-staging-"+role]
		require.Equal(t, "karafiel-staging/"+role+"-oidc", client.IntakeTarget)
		require.Empty(t, client.SessionIntakeTarget)
		require.Equal(t, "karafiel-staging-api", client.JanuaClient.Audience)
		require.Equal(t, "karafiel-staging-"+role, client.JanuaClient.ClientKey)
		require.Empty(t, client.JanuaClient.ClientID)
		require.True(t, client.JanuaClient.confidential())
		require.Empty(t, client.JanuaClient.OrganizationID)
		host := "staging-app.karafiel.mx"
		if role == "admin" {
			host = "staging-admin.karafiel.mx"
		}
		require.Equal(t, []string{"https://" + host + "/api/auth/callback"}, client.JanuaClient.RedirectURIs)
		require.ElementsMatch(t, []string{"openid", "profile", "email", "offline_access"}, client.JanuaClient.AllowedScopes)
		require.ElementsMatch(t, []string{"authorization_code", "refresh_token"}, client.JanuaClient.GrantTypes)
		require.Equal(t, map[string]string{role + "_oidc_client_id": "client_id", role + "_janua_secret_key": "client_secret", role + "_oidc_issuer": "issuer"}, client.IntakeKeyMap)
	}
	require.Equal(t, "karafiel-app", registry.Platforms["karafiel"].JanuaClient.Audience, "existing production registration stays intact")
}
