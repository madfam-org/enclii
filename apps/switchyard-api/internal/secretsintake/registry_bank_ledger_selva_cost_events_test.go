package secretsintake

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two Dhanam machine-client pairs declared 2026-11-11: Karafiel's bank-ledger
// reader (karafiel#383) and Selva's cost-events writer (selva-office#363).
// Pinned end to end because each link fails silently: a wrong Vault path is a
// write nothing reads, a wrong key is an ExternalSecret that syncs nothing, and
// a shared ExternalSecret would hold another edge's Secret until this pair
// exists.
func TestBankLedgerAndSelvaCostEventsClientTargets(t *testing.T) {
	cases := []struct {
		id, platform, vaultPath, namespace, externalSecret, scope string
		keys                                                      []string
	}{
		{
			id: "karafiel/dhanam-bank-ledger-client", platform: "karafiel-dhanam-bank-ledger",
			vaultPath: "secret/karafiel", namespace: "karafiel", externalSecret: "karafiel-dhanam-bank-ledger",
			scope: "dhanam-bank-ledger:read",
			keys:  []string{"dhanam_bank_ledger_client_id", "dhanam_bank_ledger_client_secret"},
		},
		{
			id: "selva/dhanam-cost-events-client", platform: "selva-dhanam-cost-events",
			vaultPath: "secret/selva", namespace: "selva", externalSecret: "selva-dhanam-cost-events",
			scope: "dhanam-cost-events:write",
			keys:  []string{"dhanam_cost_events_client_id", "dhanam_cost_events_client_secret"},
		},
	}
	reg, err := LoadRegistry()
	require.NoError(t, err)
	for _, c := range cases {
		tgt, err := GetTarget(c.id)
		require.NoError(t, err, c.id)
		assert.Equal(t, c.vaultPath, tgt.VaultPath, c.id)
		assert.Equal(t, c.namespace, tgt.Namespace, c.id)
		assert.Equal(t, c.externalSecret, tgt.ExternalSecret, c.id)
		assert.Equal(t, c.keys, tgt.Keys, c.id)
		for _, k := range tgt.Keys {
			assert.Equal(t, strings.ToLower(k), k, "%s: Vault stores lowercase; the ExternalSecret maps these properties verbatim", c.id)
		}
		require.NotNil(t, tgt.Generate, c.id)
		assert.True(t, tgt.Generate.Disabled, "%s: a Janua credential is minted by Janua, never generated", c.id)
		assert.NotEmpty(t, tgt.Label, c.id)
		assert.Contains(t, tgt.Description, "--platform "+c.platform+"`", c.id)
		assert.Contains(t, tgt.Description, c.scope, c.id)

		// No other target on the same Vault path shares a key or this
		// ExternalSecret.
		for name, other := range reg {
			if name == c.id || other.VaultPath != c.vaultPath {
				continue
			}
			assert.NotEqual(t, c.externalSecret, other.ExternalSecret, "%s shares the ExternalSecret of %s", c.id, name)
			for _, k := range other.Keys {
				assert.NotContains(t, c.keys, k, "%s shares key %s with %s", c.id, k, name)
			}
		}
	}

	// Every target on secret/selva, sorted: a new one must be added here.
	var onSelva []string
	for name, tgt := range reg {
		if tgt.VaultPath == "secret/selva" {
			onSelva = append(onSelva, name)
		}
	}
	sort.Strings(onSelva)
	assert.Equal(t, []string{
		"selva/dhanam-cost-events-client",
		"selva/pravara-mes-client",
		"selva/yantra4d-client",
	}, onSelva)
}
