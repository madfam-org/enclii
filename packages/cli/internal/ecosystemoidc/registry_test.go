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

// nauta-symbiosis-hcm is the ecosystem's first client_credentials machine edge.
// This pins the shape nauta #264 depends on: org-bound, one scope, no browser
// leg, and the two lowercase Vault properties its ExternalSecret reads. If any
// of these drift, `enclii secrets provision oidc --platform nauta-symbiosis-hcm`
// mints the wrong thing and the RH slice stays NOT_CONNECTED (or worse, 403s).
func TestLoadRegistry_nautaSymbiosisHCMMachineClient(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["nauta-symbiosis-hcm"]
	require.True(t, ok, "nauta-symbiosis-hcm platform missing")

	// Intake lands the pair at nauta's Vault path (a merge — see switchyard's
	// MergeSecretData) as the two properties nauta #264's ExternalSecret reads.
	assert.Equal(t, "nauta/symbiosis-hcm-oauth", p.IntakeTarget)
	require.Equal(t, map[string]string{
		"symbiosis_hcm_oauth_client_id":     "client_id",
		"symbiosis_hcm_oauth_client_secret": "client_secret",
	}, p.IntakeKeyMap)
	for k := range p.IntakeKeyMap {
		assert.Equal(t, strings.ToLower(k), k,
			"intake key %q must be lowercase to match nauta's ExternalSecret property", k)
	}

	jc := p.JanuaClient
	assert.Equal(t, "symbiosis-hcm", jc.Audience)
	assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes,
		"machine edge must be client_credentials, not authorization_code")
	assert.Equal(t, []string{"hcm:hr"}, jc.AllowedScopes)
	assert.Empty(t, jc.RedirectURIs, "a client_credentials client has no browser leg")
	assert.True(t, jc.confidential(), "the machine client must be confidential")
	// Org binding is what makes Janua #595 emit the app:role scope verbatim.
	assert.Equal(t, "e6cbd51d-8329-4c4e-8c74-aba643ab4575", jc.OrganizationID,
		"machine client must be org-bound to CTM/crea or Janua will not emit hcm:hr into the roles claim")

	// The minted pair maps to exactly the two lowercase properties, nothing else.
	values := buildIntakeValues(reg.Issuer, "jnc_hcm", "s3cr3t", p)
	assert.Equal(t, map[string]string{
		"symbiosis_hcm_oauth_client_id":     "jnc_hcm",
		"symbiosis_hcm_oauth_client_secret": "s3cr3t",
	}, values)
}

// TestRegistry_humanCopyMatchesEmbedded fails the build when
// config/ecosystem-oidc-provision.yaml (what scripts/provision-ecosystem-oidc.sh
// passes with --registry, and what humans edit) drifts from the copy this package
// embeds (what a bare `enclii secrets provision oidc` uses). The two diverged for
// ten weeks — the human copy lacked nauta and nauta-portal (#379, #473) — so a
// `--all` run from the script would have skipped the cockpit. go:embed cannot
// reach outside the package, hence two files and one test.
func TestRegistry_humanCopyMatchesEmbedded(t *testing.T) {
	human, err := os.ReadFile("../../../../config/ecosystem-oidc-provision.yaml")
	require.NoError(t, err, "config/ecosystem-oidc-provision.yaml must exist at the repo root")
	if string(human) != string(embeddedRegistry) {
		t.Fatalf("config/ecosystem-oidc-provision.yaml differs from packages/cli/internal/ecosystemoidc/data/ecosystem-oidc-provision.yaml — copy one over the other in the same commit (human %d bytes, embedded %d bytes)", len(human), len(embeddedRegistry))
	}
	reg, err := LoadRegistry("../../../../config/ecosystem-oidc-provision.yaml")
	require.NoError(t, err)
	emb, err := LoadRegistry("")
	require.NoError(t, err)
	assert.Equal(t, emb.PlatformIDs(), reg.PlatformIDs())
	assert.Contains(t, reg.Platforms, "nauta", "the human copy must carry the cockpit client")
	assert.Contains(t, reg.Platforms, "nauta-portal")
	assert.Contains(t, reg.Platforms, "nauta-symbiosis-hcm")
// Telesia (2026-09-12). A confidential authorization_code login client whose
// minted pair is filed at telesia/oidc-janua as two LOWERCASE properties —
// telesia's ExternalSecret reads `property: janua_client_id` and ESO is
// all-or-nothing per ExternalSecret, so a case drift here syncs zero keys and
// telesia-web starts with no environment at all.
//
// client_id is deliberately NOT asserted: the client is not registered on Janua
// yet, and pinning its jnc_… id here later must not break this test.
func TestLoadRegistry_telesiaLoginClient(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["telesia"]
	require.True(t, ok, "telesia platform missing")

	assert.Equal(t, "telesia/oidc-janua", p.IntakeTarget)
	require.Equal(t, map[string]string{
		"janua_client_id":     "client_id",
		"janua_client_secret": "client_secret",
	}, p.IntakeKeyMap)
	for k := range p.IntakeKeyMap {
		assert.Equal(t, strings.ToLower(k), k,
			"intake key %q must be lowercase to match telesia's ExternalSecret property", k)
	}

	jc := p.JanuaClient
	assert.Equal(t, "Telesia", jc.Name)
	assert.Equal(t, "telesia-api", jc.ClientKey)
	assert.Equal(t, "telesia-api", jc.Audience)
	assert.Equal(t, "https://app.telesia.quest", jc.WebsiteURL)
	assert.True(t, jc.confidential(), "the dashboard client must be confidential")
	assert.Equal(t, []string{
		"https://app.telesia.quest/api/auth/callback/janua",
		"http://localhost:3000/api/auth/callback/janua",
	}, jc.RedirectURIs)
	assert.Equal(t, []string{"openid", "profile", "email", "offline_access"}, jc.AllowedScopes)
	assert.Equal(t, []string{"authorization_code", "refresh_token"}, jc.GrantTypes)

	// The minted pair maps to exactly those two lowercase properties.
	assert.Equal(t, map[string]string{
		"janua_client_id":     "jnc_telesia",
		"janua_client_secret": "s3cr3t",
	}, buildIntakeValues(reg.Issuer, "jnc_telesia", "s3cr3t", p))
}
