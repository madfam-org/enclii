package secretsintake

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RFC 0050 COGS edges declared 2026-11-08: Symbiosis-HCM's labor close reads
// Nauta's hours (C10) and writes labor cost events to Dhanam; Pravara writes
// quantity-only consumption events to Dhanam (C12). Pinned end to end because
// each link fails silently: a wrong Vault path is a write nothing reads, a
// wrong key is an ExternalSecret that syncs nothing, and a shared
// ExternalSecret would hold another edge's Secret until this pair exists.
func TestCogsWriterClientTargets(t *testing.T) {
	cases := []struct {
		id, platform, vaultPath, namespace, externalSecret, scope string
		keys                                                      []string
	}{
		{
			id: "symbiosis-hcm/nauta-labor-hours-client", platform: "symbiosis-hcm-nauta-labor-hours",
			vaultPath: "secret/symbiosis-hcm", namespace: "symbiosis-hcm", externalSecret: "hcm-nauta-labor-hours",
			scope: "nauta-labor-hours:read",
			keys:  []string{"nauta_labor_hours_client_id", "nauta_labor_hours_client_secret"},
		},
		{
			id: "symbiosis-hcm/dhanam-cost-events-client", platform: "symbiosis-hcm-dhanam-cost-events",
			vaultPath: "secret/symbiosis-hcm", namespace: "symbiosis-hcm", externalSecret: "hcm-dhanam-cost-events",
			scope: "dhanam-cost-events:write",
			keys:  []string{"dhanam_cost_events_client_id", "dhanam_cost_events_client_secret"},
		},
		{
			id: "pravara-mes/dhanam-cost-events-client", platform: "pravara-dhanam-cost-events",
			vaultPath: "secret/pravara-mes", namespace: "pravara-mes", externalSecret: "pravara-dhanam-cost-events",
			scope: "dhanam-cost-events:write",
			keys:  []string{"pravara_cost_events_client_id", "pravara_cost_events_client_secret"},
		},
	}
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
	}

	// Every target on secret/symbiosis-hcm and secret/pravara-mes, sorted: a new
	// one must be added here. No two on one path share a key, and no
	// machine-client pair shares an ExternalSecret with another target (HCM's
	// hcm-secrets carries DATABASE_URL and is mounted optional: false).
	reg, err := LoadRegistry()
	require.NoError(t, err)
	onPath := map[string][]string{}
	seenKeys := map[string]string{}
	for name, tgt := range reg {
		if tgt.VaultPath != "secret/symbiosis-hcm" && tgt.VaultPath != "secret/pravara-mes" {
			continue
		}
		onPath[tgt.VaultPath] = append(onPath[tgt.VaultPath], name)
		for _, k := range tgt.Keys {
			prev, dup := seenKeys[tgt.VaultPath+":"+k]
			assert.False(t, dup, "key %s on both %s and %s", k, prev, name)
			seenKeys[tgt.VaultPath+":"+k] = name
		}
	}
	for path := range onPath {
		sort.Strings(onPath[path])
	}
	assert.Equal(t, []string{
		"symbiosis-hcm/dhanam-cost-events-client",
		"symbiosis-hcm/dhanam-writer-client",
		"symbiosis-hcm/karafiel-client",
		"symbiosis-hcm/map-absence-feed",
		"symbiosis-hcm/nauta-labor-hours-client",
		"symbiosis-hcm/signing-bindings",
	}, onPath["secret/symbiosis-hcm"])
	assert.Equal(t, []string{
		"pravara-mes/asset-shells-publisher-madfam-ecosystem",
		"pravara-mes/dhanam-cost-events-client",
		"pravara-mes/fabrication-prep-client",
		"pravara-mes/yantra4d-step-reader",
	}, onPath["secret/pravara-mes"])
	for _, c := range cases {
		for _, name := range onPath[c.vaultPath] {
			if name == c.id {
				continue
			}
			assert.NotEqual(t, c.externalSecret, reg[name].ExternalSecret,
				"%s shares the ExternalSecret of %s", c.id, name)
		}
	}
}
