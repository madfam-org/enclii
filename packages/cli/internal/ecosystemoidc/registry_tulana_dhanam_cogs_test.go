package ecosystemoidc

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tulana-dhanam-cogs (2026-11-17, tulana#151, dhanam#1002): Tulana's API reads
// Dhanam's native COGS MCP (tool cogs_sku_floors) for the internal catalogue's
// measured cost per offer. Read only: org-bound to madfam-ecosystem (Dhanam
// refuses a token without org_id), exactly dhanam-cogs:read on audience
// dhanam-api, client_credentials only, not pinned until the first provision
// prints its id. Never a writer: a write scope here would let the catalogue
// reach cogs_submit_cost_events.
func TestLoadRegistry_tulanaDhanamCogsReaderMachineEdge(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["tulana-dhanam-cogs"]
	require.True(t, ok, "platform tulana-dhanam-cogs missing")
	assert.Equal(t, "tulana/dhanam-cogs-client", p.IntakeTarget)
	assert.Empty(t, p.SessionIntakeTarget, "a machine edge has no session secret")
	require.Equal(t, map[string]string{
		"dhanam_cogs_client_id":     "client_id",
		"dhanam_cogs_client_secret": "client_secret",
	}, p.IntakeKeyMap)
	for k := range p.IntakeKeyMap {
		assert.Equal(t, strings.ToLower(k), k, "intake key %q must be lowercase", k)
	}

	jc := p.JanuaClient
	assert.Equal(t, "tulana-dhanam-cogs", jc.Name)
	assert.Equal(t, "tulana-dhanam-cogs", jc.ClientKey)
	assert.Empty(t, jc.ClientID, "not pinned until the first provision prints it")
	assert.Equal(t, "dhanam-api", jc.Audience, "what Dhanam's MCP machine strategy checks")
	assert.Equal(t, "1a6233ef-185f-43ee-9181-e2591fbb2643", jc.OrganizationID,
		"Dhanam's MCP refuses a token without org_id; Janua sets it only for org-bound clients")
	assert.Equal(t, []string{"dhanam-cogs:read"}, jc.AllowedScopes,
		"the COGS MCP read scope only; never widened into a writer")
	assert.Regexp(t, `^[a-z][a-z0-9._-]*:[a-z][a-z0-9_-]*$`, jc.AllowedScopes[0],
		"Janua registers only namespace:action scopes")
	for _, scope := range jc.AllowedScopes {
		assert.False(t, strings.HasSuffix(scope, ":write"), "write scope %s on a read-only client", scope)
	}
	assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes)
	assert.Empty(t, jc.RedirectURIs, "a client_credentials client has no browser leg")
	require.NotNil(t, jc.IsConfidential)
	assert.True(t, jc.confidential())
	assert.Contains(t, jc.Description, "scope dhanam-cogs:read only")
	kind, err := jc.kind()
	require.NoError(t, err)
	assert.Equal(t, machineClient, kind)
	require.NoError(t, validatePlatform(p))

	assert.Equal(t, map[string]string{
		"dhanam_cogs_client_id":     "jnc_fixture",
		"dhanam_cogs_client_secret": "fixture-secret",
	}, buildIntakeValues(reg.Issuer, "jnc_fixture", "fixture-secret", p))

	// Every holder of dhanam-cogs:read, sorted: a new reader must change this
	// test, and needs its own client@org entry in DHANAM_MCP_READ_CLIENTS.
	var readers []string
	for name, p := range reg.Platforms {
		for _, scope := range p.JanuaClient.AllowedScopes {
			if scope == "dhanam-cogs:read" {
				readers = append(readers, name)
			}
		}
	}
	sort.Strings(readers)
	assert.Equal(t, []string{"tulana-dhanam-cogs"}, readers)
}
