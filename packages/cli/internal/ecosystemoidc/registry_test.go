package ecosystemoidc

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestLoadRegistry_phyndCRM(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	assert.Contains(t, reg.Platforms, "phynd-crm")
	assert.Equal(t, "phynd-crm/oidc-janua", reg.Platforms["phynd-crm"].IntakeTarget)
}

func TestLoadRegistry_embedded(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	assert.Equal(t, "https://auth.madfam.io", reg.Issuer)
	assert.Contains(t, reg.Platforms, "dhanam")
	assert.Equal(t, "dhanam/oidc-janua", reg.Platforms["dhanam"].IntakeTarget)
	assert.Equal(t, "dhanam/session-auth", reg.Platforms["dhanam"].SessionIntakeTarget)
	assert.Contains(t, reg.Platforms, "karafiel")
	assert.Equal(t, "karafiel/web-oidc-janua", reg.Platforms["karafiel"].IntakeTarget)
}

func TestBuildIntakeValues_standard(t *testing.T) {
	values := buildIntakeValues("https://auth.madfam.io", "jnc_test", "sec", Platform{})
	assert.Equal(t, map[string]string{
		"OIDC_CLIENT_ID":     "jnc_test",
		"OIDC_CLIENT_SECRET": "sec",
		"OIDC_ISSUER":        "https://auth.madfam.io",
	}, values)
}

func TestBuildIntakeValues_ceqMap(t *testing.T) {
	platform := Platform{
		IntakeKeyMap: map[string]string{
			"JANUA_CLIENT_SECRET": "client_secret",
		},
	}
	values := buildIntakeValues("https://auth.madfam.io", "jnc_ceq", "sec", platform)
	assert.Equal(t, map[string]string{"JANUA_CLIENT_SECRET": "sec"}, values)
}

func TestGenerateSessionAuthValues(t *testing.T) {
	values, err := generateSessionAuthValues()
	require.NoError(t, err)
	assert.Len(t, values["SESSION_SECRET"], 64)
	assert.Len(t, values["NEXTAUTH_SECRET"], 64)
}

// The two registries in this repo are coupled but live in different packages:
// this one names an `intake_target`, and switchyard's secretsintake registry
// decides whether that target exists. Nothing enforced the join, so a platform
// could be declared here, pass every test, and fail at provision time with
// "unknown target" — after the operator had already run `enclii login`.
//
// Reading switchyard's YAML directly rather than importing its package keeps
// the CLI free of a dependency on the API's internals; the coupling being
// tested is the data, not the code.
func TestEveryPlatformIntakeTargetExistsInSwitchyardRegistry(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	raw, err := os.ReadFile("../../../../apps/switchyard-api/internal/secretsintake/registry.yaml")
	require.NoError(t, err, "switchyard intake registry must be readable from the CLI package")

	var intake struct {
		Targets map[string]struct{} `yaml:"targets"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &intake))
	require.NotEmpty(t, intake.Targets, "read zero intake targets — the join is not being checked")

	for _, id := range reg.PlatformIDs() {
		p := reg.Platforms[id]
		require.NotEmpty(t, p.IntakeTarget, "platform %q has no intake_target; provisioning it errors at run time", id)
		assert.Contains(t, intake.Targets, p.IntakeTarget,
			"platform %q points at intake target %q, which switchyard does not define — "+
				"`enclii secrets provision oidc --platform %s` would fail after login",
			id, p.IntakeTarget, id)
		if p.SessionIntakeTarget != "" {
			assert.Contains(t, intake.Targets, p.SessionIntakeTarget,
				"platform %q session_intake_target %q is not defined in switchyard", id, p.SessionIntakeTarget)
		}
	}
}

func TestLoadRegistry_nautaBothClientsPinned(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	for _, tc := range []struct{ platform, target, audience string }{
		{"nauta", "nauta/oidc-janua", "nauta-api"},
		{"nauta-portal", "nauta/oidc-janua-portal", "nauta-portal"},
	} {
		p, ok := reg.Platforms[tc.platform]
		require.True(t, ok, "platform %q missing", tc.platform)
		assert.Equal(t, tc.target, p.IntakeTarget)
		assert.Equal(t, tc.audience, p.JanuaClient.Audience)

		// The pin is what makes this reconcile instead of create. Janua's client
		// list carries eight duplicate "Voxa" entries, which is what an unpinned
		// re-run produces.
		assert.NotEmpty(t, p.JanuaClient.ClientID,
			"platform %q has no client_id pin — re-running provisioning would register a DUPLICATE client", tc.platform)

		// Lowercase, because nauta's ExternalSecrets reference lower_snake
		// properties and ESO is all-or-nothing.
		require.NotEmpty(t, p.IntakeKeyMap, "platform %q needs an intake_key_map for nauta's lowercase properties", tc.platform)
		for k := range p.IntakeKeyMap {
			assert.Equal(t, strings.ToLower(k), k,
				"intake key %q must be lowercase to match nauta's ExternalSecret properties", k)
		}
	}
}
