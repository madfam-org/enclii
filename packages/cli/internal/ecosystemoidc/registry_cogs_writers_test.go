package ecosystemoidc

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RFC 0050 COGS edges declared 2026-11-08. Symbiosis-HCM's labor close reads
// Nauta's monthly hours (C10) and writes the labor cost events to Dhanam;
// Pravara writes quantity-only consumption events to Dhanam (C12). Each is the
// karafiel-dhanam-cost-events shape: client_credentials only, confidential, no
// redirect URIs, org-bound to madfam-ecosystem, exactly one scope with one
// colon, and the consumer's existing API audience. Client ids are not pinned
// until the first provision prints them.
func TestLoadRegistry_cogsWriterAndReaderMachineEdges(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	cases := []struct {
		platform, intakeTarget, audience, scope, idKey, secretKey string
	}{
		{
			platform: "symbiosis-hcm-nauta-labor-hours", intakeTarget: "symbiosis-hcm/nauta-labor-hours-client",
			audience: "nauta-api", scope: "nauta-labor-hours:read",
			idKey: "nauta_labor_hours_client_id", secretKey: "nauta_labor_hours_client_secret",
		},
		{
			platform: "symbiosis-hcm-dhanam-cost-events", intakeTarget: "symbiosis-hcm/dhanam-cost-events-client",
			audience: "dhanam-api", scope: "dhanam-cost-events:write",
			idKey: "dhanam_cost_events_client_id", secretKey: "dhanam_cost_events_client_secret",
		},
		{
			platform: "pravara-dhanam-cost-events", intakeTarget: "pravara-mes/dhanam-cost-events-client",
			audience: "dhanam-api", scope: "dhanam-cost-events:write",
			idKey: "pravara_cost_events_client_id", secretKey: "pravara_cost_events_client_secret",
		},
	}
	for _, c := range cases {
		p, ok := reg.Platforms[c.platform]
		require.True(t, ok, "platform %s missing", c.platform)
		assert.Equal(t, c.intakeTarget, p.IntakeTarget, c.platform)
		assert.Empty(t, p.SessionIntakeTarget, "%s: a machine edge has no session secret", c.platform)
		require.Equal(t, map[string]string{
			c.idKey:     "client_id",
			c.secretKey: "client_secret",
		}, p.IntakeKeyMap, c.platform)

		jc := p.JanuaClient
		assert.Equal(t, c.platform, jc.Name)
		assert.Equal(t, c.platform, jc.ClientKey)
		assert.Empty(t, jc.ClientID, "%s: not pinned until the first provision prints it", c.platform)
		assert.Equal(t, c.audience, jc.Audience, c.platform)
		assert.Equal(t, "1a6233ef-185f-43ee-9181-e2591fbb2643", jc.OrganizationID,
			"%s: org-bound to madfam-ecosystem so the token carries org_id", c.platform)
		assert.Equal(t, []string{c.scope}, jc.AllowedScopes, c.platform)
		assert.Regexp(t, `^[a-z][a-z0-9._-]*:[a-z][a-z0-9_-]*$`, jc.AllowedScopes[0],
			"Janua registers only namespace:action scopes")
		assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes, c.platform)
		assert.Empty(t, jc.RedirectURIs, c.platform)
		require.NotNil(t, jc.IsConfidential, c.platform)
		assert.True(t, jc.confidential(), c.platform)
		assert.Contains(t, jc.Description, "scope "+c.scope+" only", c.platform)
		kind, err := jc.kind()
		require.NoError(t, err, c.platform)
		assert.Equal(t, machineClient, kind, c.platform)
		require.NoError(t, validatePlatform(p), c.platform)

		assert.Equal(t, map[string]string{
			c.idKey:     "jnc_fixture",
			c.secretKey: "fixture-secret",
		}, buildIntakeValues(reg.Issuer, "jnc_fixture", "fixture-secret", p), c.platform)
	}

	// nauta-labor-hours:read names the Janua subject who logged each entry, so
	// Nauta allows exactly one holder: HCM's labor close.
	var holders []string
	for name, p := range reg.Platforms {
		for _, scope := range p.JanuaClient.AllowedScopes {
			if scope == "nauta-labor-hours:read" {
				holders = append(holders, name)
			}
		}
	}
	assert.Equal(t, []string{"symbiosis-hcm-nauta-labor-hours"}, holders)

	// Every writer of Dhanam's cost ledger, sorted: a new writer must change
	// this test (and gets its own CostEventWriterGrant in Dhanam).
	var writers []string
	for name, p := range reg.Platforms {
		for _, scope := range p.JanuaClient.AllowedScopes {
			if scope == "dhanam-cost-events:write" {
				writers = append(writers, name)
			}
		}
	}
	sort.Strings(writers)
	assert.Equal(t, []string{
		"karafiel-dhanam-cost-events",
		"pravara-dhanam-cost-events",
		"symbiosis-hcm-dhanam-cost-events",
	}, writers)
}
