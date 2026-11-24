package secretsintake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phynd-crm lead-intake HMAC key (2026-11-15): one value held by four services,
// phynd-crm verifying and Kalya, madfam-site and Cotiza signing with it. Pinned
// end to end because each link fails silently: a wrong Vault path is a write
// nothing reads, a wrong property is an ExternalSecret that syncs zero keys, and
// a wrong ExternalSecret name means intake never force-syncs the Secret the pod
// reads. The value is minted once by the shared group and no member can be
// generated or supplied alone, so the four copies cannot drift.
const leadIntakeGroup = "phynd-crm/lead-intake-hmac"

var leadIntakeTargets = []struct {
	id, vaultPath, namespace, externalSecret, key, envVar string
}{
	{"phynd-crm/lead-intake", "secret/phynd-crm", "phynd-crm", "phynd-crm-lead-intake", "lead_intake_secret", "PHYND_CRM_LEAD_INTAKE_SECRET"},
	{"kalya/phynd-crm-lead-intake", "secret/kalya", "kalya", "kalya-phynd-crm-lead-intake", "phynd_crm_lead_intake_secret", "PHYND_CRM_LEAD_INTAKE_SECRET"},
	{"madfam-site/phynd-crm-lead-intake", "secret/madfam-site", "madfam-site", "madfam-site-phynd-crm-lead-intake", "phynd_crm_lead_intake_secret", "PHYND_CRM_LEAD_INTAKE_SECRET"},
	{"digifab-quoting/phynd-crm-lead-intake", "secret/digifab-quoting", "digifab-quoting", "digifab-quoting-phynd-crm-lead-intake", "phynd_crm_lead_intake_secret", "PHYNDCRM_LEAD_INTAKE_SECRET"},
}

func TestPhyndCRMLeadIntakeTargets(t *testing.T) {
	for _, tc := range leadIntakeTargets {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, tc.vaultPath, tgt.VaultPath)
			assert.Equal(t, tc.namespace, tgt.Namespace)
			assert.Equal(t, tc.externalSecret, tgt.ExternalSecret)
			assert.Equal(t, []string{tc.key}, tgt.Keys, "a group member holds exactly one key")
			assert.Equal(t, strings.ToLower(tc.key), tc.key,
				"Vault stores lowercase; an upper-case key would never match the ExternalSecret property")
			require.NotNil(t, tgt.Generate)
			assert.Equal(t, leadIntakeGroup, tgt.Generate.Group, "only the shared group writes this target")
			assert.False(t, tgt.Generate.Disabled)
			assert.Zero(t, tgt.Generate.Bytes)
			assert.Empty(t, tgt.Generate.Profile)
			assert.NotEmpty(t, tgt.Label)
			assert.Contains(t, tgt.Description, tc.envVar)
			assert.Contains(t, tgt.Description, "enclii secrets intake group "+leadIntakeGroup)
			// Each description names the other three targets that hold the same
			// value, so whoever reads `intake targets` sees the whole set.
			for _, other := range leadIntakeTargets {
				if other.id != tc.id {
					assert.Contains(t, tgt.Description, other.id)
				}
			}
		})
	}
}

func TestPhyndCRMLeadIntakeGroup(t *testing.T) {
	g, err := GetGroup(leadIntakeGroup)
	require.NoError(t, err)
	want := make([]string, 0, len(leadIntakeTargets))
	for _, tc := range leadIntakeTargets {
		want = append(want, tc.id)
	}
	assert.Equal(t, want, g.Members, "verifier first: the write order is the member order")
	assert.Equal(t, 32, g.GenerateBytes())
	assert.NotEmpty(t, g.Label)
	for _, deployment := range []string{"phynd-crm-web", "kalya-web", "madfam-web", "digifab-quoting-api"} {
		assert.Contains(t, g.Description, deployment, "the group names what must restart")
	}

	groups, err := ListGroups()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, leadIntakeGroup, groups[0].ID)
}

// Each target has its own ExternalSecret, never one a running service already
// reads: ESO parks the whole target Secret while a mapped property is missing,
// so reusing e.g. madfam-site-secrets would take the site's DATABASE_URL down
// with it until this key exists.
func TestPhyndCRMLeadIntakeDedicatedExternalSecrets(t *testing.T) {
	reg, err := LoadRegistry()
	require.NoError(t, err)
	mine := map[string]bool{}
	for _, tc := range leadIntakeTargets {
		mine[tc.id] = true
	}
	seen := map[string]string{}
	for _, tc := range leadIntakeTargets {
		prev, dup := seen[tc.externalSecret]
		assert.False(t, dup, "%s and %s share an ExternalSecret", prev, tc.id)
		seen[tc.externalSecret] = tc.id
	}
	for name, other := range reg {
		if mine[name] {
			continue
		}
		for _, tc := range leadIntakeTargets {
			if other.Namespace == tc.namespace {
				assert.NotEqual(t, tc.externalSecret, other.ExternalSecret,
					"%s reuses %s's dedicated ExternalSecret", name, tc.id)
			}
			// Intake merges into the Vault path, so a second writer of the
			// same property would silently overwrite the shared value.
			if other.VaultPath == tc.vaultPath {
				assert.NotContains(t, other.Keys, tc.key, "%s also writes %s", name, tc.key)
			}
		}
	}
}

// The group contract fails the registry load, not a live run.
func TestSharedGroupValidation(t *testing.T) {
	member := func(id, path, group string, keys ...string) string {
		return "  " + id + ":\n    label: l\n    description: d\n    vault_path: " + path +
			"\n    namespace: n\n    keys: [" + strings.Join(keys, ", ") + "]\n    generate:\n      group: " + group + "\n"
	}
	group := "groups:\n  g/one:\n    label: l\n    description: d\n    members: [a/x, b/y]\n"
	valid := "targets:\n" + member("a/x", "secret/a", "g/one", "k") + member("b/y", "secret/b", "g/one", "k") + group
	targets, groups, err := loadRegistryFile([]byte(valid))
	require.NoError(t, err)
	require.Len(t, targets, 2)
	require.Equal(t, []string{"a/x", "b/y"}, groups["g/one"].Members)

	for name, raw := range map[string]string{
		"member not listed":   "targets:\n" + member("a/x", "secret/a", "g/one", "k") + member("b/y", "secret/b", "g/one", "k") + member("c/z", "secret/c", "g/one", "k") + group,
		"unknown group":       "targets:\n" + member("a/x", "secret/a", "g/one", "k") + member("b/y", "secret/b", "g/other", "k") + group,
		"two keys":            "targets:\n" + member("a/x", "secret/a", "g/one", "k", "j") + member("b/y", "secret/b", "g/one", "k") + group,
		"same property":       "targets:\n" + member("a/x", "secret/a", "g/one", "k") + member("b/y", "secret/a", "g/one", "k") + group,
		"unknown member":      "targets:\n" + member("a/x", "secret/a", "g/one", "k") + "groups:\n  g/one:\n    label: l\n    description: d\n    members: [a/x, q/q]\n",
		"one member":          "targets:\n" + member("a/x", "secret/a", "g/one", "k") + "groups:\n  g/one:\n    label: l\n    description: d\n    members: [a/x]\n",
		"weak entropy":        "targets:\n" + member("a/x", "secret/a", "g/one", "k") + member("b/y", "secret/b", "g/one", "k") + "groups:\n  g/one:\n    label: l\n    description: d\n    bytes: 8\n    members: [a/x, b/y]\n",
		"listed, not marked":  "targets:\n" + member("a/x", "secret/a", "g/one", "k") + "  b/y:\n    label: l\n    description: d\n    vault_path: secret/b\n    namespace: n\n    keys: [k]\n" + group,
		"group id is target":  "targets:\n" + member("a/x", "secret/a", "a/x", "k") + member("b/y", "secret/b", "a/x", "k") + "groups:\n  a/x:\n    label: l\n    description: d\n    members: [a/x, b/y]\n",
		"group plus disabled": "targets:\n" + member("a/x", "secret/a", "g/one", "k") + member("b/y", "secret/b", "g/one", "k") + group + "",
	} {
		if name == "group plus disabled" {
			raw = strings.Replace(raw, "      group: g/one\n", "      group: g/one\n      disabled: true\n", 1)
		}
		t.Run(name, func(t *testing.T) {
			_, _, err := loadRegistryFile([]byte(raw))
			require.Error(t, err)
		})
	}
}
