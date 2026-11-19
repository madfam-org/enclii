package ecosystemoidc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nauta-dhanam-merchant (2026-11-18): nauta's /finanzas reads one month of a
// client workspace's stored payment obligations from Dhanam's machine-only
// projection route. Same shape as the other Dhanam readers (client_credentials
// only, confidential, no redirect URIs, one one-colon scope on audience
// dhanam-api, id not pinned until the first provision prints it), but
// org-bound to the CLIENT workspace's organization, because Dhanam's
// PaymentProjectionGrant pairs the client id with the token's org_id.
//
// The provision writes only the pair: the three tenant identifiers the target
// also declares are filed by the operator, so they never sit in this registry.
func TestLoadRegistry_nautaDhanamMerchantMachineEdge(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["nauta-dhanam-merchant"]
	require.True(t, ok, "nauta-dhanam-merchant platform missing")
	assert.Equal(t, "nauta/dhanam-merchant-oauth", p.IntakeTarget)
	assert.Empty(t, p.SessionIntakeTarget, "a machine edge has no session secret")
	require.Equal(t, map[string]string{
		"dhanam_merchant_client_id":     "client_id",
		"dhanam_merchant_client_secret": "client_secret",
	}, p.IntakeKeyMap, "only the pair; workspace, organization and space ids are operator-filed")

	jc := p.JanuaClient
	assert.Equal(t, "nauta-dhanam-merchant", jc.Name)
	assert.Equal(t, "nauta-dhanam-merchant", jc.ClientKey)
	assert.Empty(t, jc.ClientID, "not pinned until the first provision prints it")
	assert.Equal(t, "dhanam-api", jc.Audience, "what Dhanam's payment-projection strategy checks")
	assert.Equal(t, reg.Platforms["nauta-symbiosis-hcm"].JanuaClient.OrganizationID, jc.OrganizationID,
		"bound to the same client organization as nauta's other tenant edge, never to madfam-ecosystem")
	assert.Equal(t, []string{"dhanam:merchant-read"}, jc.AllowedScopes)
	assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes)
	assert.Empty(t, jc.RedirectURIs)
	require.NotNil(t, jc.IsConfidential)
	assert.True(t, jc.confidential())
	assert.Contains(t, jc.Description, "scope dhanam:merchant-read only")
	kind, err := jc.kind()
	require.NoError(t, err)
	assert.Equal(t, machineClient, kind)
	require.NoError(t, validatePlatform(p))

	assert.Equal(t, map[string]string{
		"dhanam_merchant_client_id":     "jnc_fixture",
		"dhanam_merchant_client_secret": "fixture-secret",
	}, buildIntakeValues(reg.Issuer, "jnc_fixture", "fixture-secret", p))

	// dhanam:merchant-read has exactly one holder in this registry. A second
	// consumer (another product reading the same projection) must change this
	// test and gets its own client and its own Dhanam grant.
	var holders []string
	for name, other := range reg.Platforms {
		for _, scope := range other.JanuaClient.AllowedScopes {
			if scope == "dhanam:merchant-read" {
				holders = append(holders, name)
			}
		}
	}
	assert.Equal(t, []string{"nauta-dhanam-merchant"}, holders)
}
