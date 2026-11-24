package ecosystemoidc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nauta-phynd-crm (2026-11-09): nauta's cockpit CRM reads from phynd-crm.
// phynd-crm (apps/web/src/lib/trpc/machine-auth.ts) accepts only aud
// `phynd-crm`, scope `phynd-crm-engagements:read` and an org_id equal to its
// PHYND_CRM_MACHINE_JANUA_ORG_ID, so any drift here leaves a provisioned
// client that phynd refuses.
func TestLoadRegistry_nautaPhyndCRMMachineEdge(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["nauta-phynd-crm"]
	require.True(t, ok, "platform nauta-phynd-crm missing")
	assert.Equal(t, "nauta/phynd-crm-oauth", p.IntakeTarget)
	assert.Empty(t, p.SessionIntakeTarget, "a machine edge has no session secret")
	require.Equal(t, map[string]string{
		"phynd_crm_oauth_client_id":     "client_id",
		"phynd_crm_oauth_client_secret": "client_secret",
	}, p.IntakeKeyMap)
	for k := range p.IntakeKeyMap {
		assert.Equal(t, strings.ToLower(k), k,
			"intake key %q must be lowercase to match nauta's ExternalSecret property", k)
	}

	jc := p.JanuaClient
	assert.Equal(t, "nauta-phynd-crm", jc.ClientKey)
	assert.Equal(t, "phynd-crm", jc.Audience,
		"phynd's PHYND_CRM_MACHINE_AUDIENCE default; not the staff SSO audience phynd-crm-api")
	assert.Equal(t, "1a6233ef-185f-43ee-9181-e2591fbb2643", jc.OrganizationID,
		"phynd pins the token's org_id to PHYND_CRM_MACHINE_JANUA_ORG_ID (madfam-ecosystem)")
	assert.Equal(t, []string{"phynd-crm-engagements:read"}, jc.AllowedScopes)
	assert.Regexp(t, `^[a-z][a-z0-9._-]*:[a-z][a-z0-9_-]*$`, jc.AllowedScopes[0],
		"Janua registers only namespace:action scopes")
	assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes)
	assert.Empty(t, jc.RedirectURIs, "a client_credentials client has no browser leg")
	require.NotNil(t, jc.IsConfidential)
	assert.True(t, jc.confidential())
	assert.Empty(t, jc.ClientID, "not pinned until the first provision run creates it")
	require.NoError(t, validatePlatform(p))

	kind, err := jc.kind()
	require.NoError(t, err)
	assert.Equal(t, machineClient, kind)

	assert.Equal(t, map[string]string{
		"phynd_crm_oauth_client_id":     "jnc_fixture",
		"phynd_crm_oauth_client_secret": "fixture-secret",
	}, buildIntakeValues(reg.Issuer, "jnc_fixture", "fixture-secret", p))
}
