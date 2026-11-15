package ecosystemoidc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PhyndCRM native MCP readers (2026-11-14, RFC 0051 M3: Selva's agent fleet
// and the ops agents, one client each). PhyndCRM's POST /mcp (phynd-crm#131)
// accepts only aud `phynd-crm`, scope `phynd-crm-pipeline:read` matched
// exactly and an org_id equal to its PHYND_CRM_MACHINE_JANUA_ORG_ID, so any
// drift here leaves a provisioned client that phynd refuses. The scope is NOT
// nauta's `phynd-crm-engagements:read`: neither opens the other's surface.
func TestLoadRegistry_phyndCRMMCPReaderMachineEdges(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	for _, consumer := range []string{"selva", "ops-agents"} {
		platform := consumer + "-phynd-crm-pipeline"
		p, ok := reg.Platforms[platform]
		require.True(t, ok, "platform %s missing", platform)
		assert.Equal(t, consumer+"/phynd-crm-pipeline-client", p.IntakeTarget, platform)
		assert.Empty(t, p.SessionIntakeTarget, "%s: a machine edge has no session secret", platform)
		require.Equal(t, map[string]string{
			"phynd_crm_pipeline_client_id":     "client_id",
			"phynd_crm_pipeline_client_secret": "client_secret",
		}, p.IntakeKeyMap, platform)
		for k := range p.IntakeKeyMap {
			assert.Equal(t, strings.ToLower(k), k, "%s: intake key %q must be lowercase", platform, k)
		}

		jc := p.JanuaClient
		assert.Equal(t, platform, jc.ClientKey, platform)
		assert.Equal(t, platform, jc.Name, platform)
		assert.Equal(t, "phynd-crm", jc.Audience,
			"%s: phynd's PHYND_CRM_MACHINE_AUDIENCE default; not the staff SSO audience phynd-crm-api", platform)
		assert.Equal(t, "1a6233ef-185f-43ee-9181-e2591fbb2643", jc.OrganizationID,
			"%s: phynd pins the token's org_id to PHYND_CRM_MACHINE_JANUA_ORG_ID (madfam-ecosystem)", platform)
		assert.Equal(t, []string{"phynd-crm-pipeline:read"}, jc.AllowedScopes,
			"%s: the MCP read scope only; never widened with the tRPC scope or a write", platform)
		assert.Regexp(t, `^[a-z][a-z0-9._-]*:[a-z][a-z0-9_-]*$`, jc.AllowedScopes[0],
			"Janua registers only namespace:action scopes")
		assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes, platform)
		assert.Empty(t, jc.RedirectURIs, "%s: a client_credentials client has no browser leg", platform)
		require.NotNil(t, jc.IsConfidential, platform)
		assert.True(t, jc.confidential(), platform)
		assert.Empty(t, jc.ClientID, "%s: not pinned until the first provision run creates it", platform)
		require.NoError(t, validatePlatform(p), platform)

		kind, err := jc.kind()
		require.NoError(t, err, platform)
		assert.Equal(t, machineClient, kind, platform)

		assert.Equal(t, map[string]string{
			"phynd_crm_pipeline_client_id":     "jnc_fixture",
			"phynd_crm_pipeline_client_secret": "fixture-secret",
		}, buildIntakeValues(reg.Issuer, "jnc_fixture", "fixture-secret", p), platform)
	}

	// The tRPC scope stays on nauta's client only, and the MCP scope off it.
	nauta, ok := reg.Platforms["nauta-phynd-crm"]
	require.True(t, ok)
	assert.Equal(t, []string{"phynd-crm-engagements:read"}, nauta.JanuaClient.AllowedScopes)
}
