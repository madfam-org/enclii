package ecosystemoidc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two Dhanam machine edges declared 2026-11-11, each the
// karafiel-dhanam-cost-events shape (client_credentials only, confidential, no
// redirect URIs, org-bound to madfam-ecosystem, exactly one one-colon scope on
// audience dhanam-api, id not pinned until the first provision prints it):
//   - karafiel-dhanam-bank-ledger: Karafiel's bank mirror READS one business
//     space's bank movements (karafiel#383, dhanam#969);
//   - selva-dhanam-cost-events: Selva's inference cost events, source selva
//     (selva-office#363, RFC 0050 C13).
func TestLoadRegistry_bankLedgerAndSelvaCostEventsMachineEdges(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	cases := []struct {
		platform, intakeTarget, scope, idKey, secretKey string
	}{
		{
			platform: "karafiel-dhanam-bank-ledger", intakeTarget: "karafiel/dhanam-bank-ledger-client",
			scope: "dhanam-bank-ledger:read",
			idKey: "dhanam_bank_ledger_client_id", secretKey: "dhanam_bank_ledger_client_secret",
		},
		{
			platform: "selva-dhanam-cost-events", intakeTarget: "selva/dhanam-cost-events-client",
			scope: "dhanam-cost-events:write",
			idKey: "dhanam_cost_events_client_id", secretKey: "dhanam_cost_events_client_secret",
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
		assert.Equal(t, "dhanam-api", jc.Audience, c.platform)
		assert.Equal(t, "1a6233ef-185f-43ee-9181-e2591fbb2643", jc.OrganizationID,
			"%s: Dhanam's machine strategies require org_id; Janua sets it only for org-bound clients", c.platform)
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

	// dhanam-bank-ledger:read reads a business space's bank movements: exactly
	// one holder, Karafiel's bank mirror. A second reader must change this test
	// (and gets its own BankLedgerReaderGrant in Dhanam).
	var holders []string
	for name, p := range reg.Platforms {
		for _, scope := range p.JanuaClient.AllowedScopes {
			if scope == "dhanam-bank-ledger:read" {
				holders = append(holders, name)
			}
		}
	}
	assert.Equal(t, []string{"karafiel-dhanam-bank-ledger"}, holders)
}
