package secretsintake

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phynd-crm staff access (2026-11-16, phynd-crm#141). Pinned because each link
// fails silently: a wrong path or key is a write no ExternalSecret reads, and a
// shared ExternalSecret would park another Secret while a property is missing.
func TestPhyndCRMStaffAccessTargets(t *testing.T) {
	cases := []struct{ id, externalSecret, key string }{
		{"phynd-crm/staff-bootstrap", "phynd-crm-staff-bootstrap", "bootstrap_staff"},
		{"phynd-crm/default-task-owner", "phynd-crm-default-task-owner", "default_task_owner_id"},
	}
	reg, err := LoadRegistry()
	require.NoError(t, err)
	for _, c := range cases {
		tgt, err := GetTarget(c.id)
		require.NoError(t, err, c.id)
		assert.Equal(t, "secret/phynd-crm", tgt.VaultPath, c.id)
		assert.Equal(t, "phynd-crm", tgt.Namespace, c.id)
		assert.Equal(t, c.externalSecret, tgt.ExternalSecret, c.id)
		assert.Equal(t, []string{c.key}, tgt.Keys, c.id)
		require.NotNil(t, tgt.Generate, c.id)
		assert.True(t, tgt.Generate.Disabled, "%s: a random value would name nobody", c.id)
		for name, other := range reg {
			if name == c.id || other.VaultPath != tgt.VaultPath {
				continue
			}
			assert.NotEqual(t, c.externalSecret, other.ExternalSecret, "%s shares the ExternalSecret of %s", c.id, name)
			assert.NotContains(t, other.Keys, c.key, "%s shares key %s with %s", c.id, c.key, name)
		}
	}
}
