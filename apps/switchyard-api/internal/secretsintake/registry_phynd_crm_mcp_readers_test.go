package secretsintake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PhyndCRM native MCP reader (2026-11-14, RFC 0051 M3; owner ruling V2: the
// ops agents only, no Selva client). Pinned end to end because each link
// fails silently: a wrong Vault path is a write nothing reads, a wrong key is
// an ExternalSecret that syncs nothing, and a shared ExternalSecret would hold
// another edge's Secret until this pair exists.
func TestPhyndCRMMCPReaderClientTargets(t *testing.T) {
	keys := []string{"phynd_crm_pipeline_client_id", "phynd_crm_pipeline_client_secret"}
	cases := []struct {
		id, platform, vaultPath, namespace, externalSecret string
	}{
		{
			// No ops-agent runtime reads Vault today: no namespace, no
			// ExternalSecret; the pair is held until the owner decides.
			id: "ops-agents/phynd-crm-pipeline-client", platform: "ops-agents-phynd-crm-pipeline",
			vaultPath: "secret/ops-agents",
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
		assert.Equal(t, keys, tgt.Keys, c.id)
		for _, k := range tgt.Keys {
			assert.Equal(t, strings.ToLower(k), k, "%s: Vault stores lowercase; an ExternalSecret maps these properties verbatim", c.id)
		}
		require.NotNil(t, tgt.Generate, c.id)
		assert.True(t, tgt.Generate.Disabled, "%s: a Janua credential is minted by Janua, never generated", c.id)
		assert.NotEmpty(t, tgt.Label, c.id)
		assert.Contains(t, tgt.Description, "--platform "+c.platform+"`", c.id)
		assert.Contains(t, tgt.Description, "phynd-crm-pipeline:read", c.id)
		assert.NotContains(t, tgt.Description, "phynd-crm-engagements:read",
			"%s: the MCP scope and nauta's tRPC scope are separate edges", c.id)

		// No other target on the same Vault path shares a key or (when there
		// is one) this ExternalSecret.
		for name, other := range reg {
			if name == c.id || other.VaultPath != c.vaultPath {
				continue
			}
			if c.externalSecret != "" {
				assert.NotEqual(t, c.externalSecret, other.ExternalSecret, "%s shares the ExternalSecret of %s", c.id, name)
			}
			for _, k := range other.Keys {
				assert.NotContains(t, keys, k, "%s shares key %s with %s", c.id, k, name)
			}
		}
	}
}
