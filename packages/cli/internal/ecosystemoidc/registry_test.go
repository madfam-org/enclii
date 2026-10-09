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
		if p.IntakeTarget == "" && p.publicLogin() {
			// A public login client has no secret to deliver; the provisioner
			// prints its client_id and the consumer pins it. Nothing to join.
			continue
		}
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

// crea-map-payment-mail is the second client_credentials machine edge. janua
// #635 authorizes POST /api/v1/email/payment-notices from THIS client row alone
// (active, confidential, org-bound, audience janua-email, scope
// crea-map:payment-mail, grant client_credentials), so any drift here makes the
// provisioned client unusable (403 mail_service_grant_unavailable).
func TestLoadRegistry_creaMapPaymentMailMachineClient(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["crea-map-payment-mail"]
	require.True(t, ok, "crea-map-payment-mail platform missing")

	assert.Equal(t, "crea-map/janua-mail-client", p.IntakeTarget)
	require.Equal(t, map[string]string{
		"janua_mail_client_id":     "client_id",
		"janua_mail_client_secret": "client_secret",
	}, p.IntakeKeyMap)

	jc := p.JanuaClient
	assert.Equal(t, "jnc_9PHl6rQWZFakbXtmWSB5i-bAWJVWKZ1J", jc.ClientID,
		"pinned after the first provision run; an unpinned re-run registers a duplicate")
	assert.Equal(t, "janua-email", jc.Audience)
	assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes)
	assert.Equal(t, []string{"crea-map:payment-mail"}, jc.AllowedScopes)
	assert.Empty(t, jc.RedirectURIs, "a client_credentials client has no browser leg")
	assert.True(t, jc.confidential(), "the machine client must be confidential")
	assert.Equal(t, "e6cbd51d-8329-4c4e-8c74-aba643ab4575", jc.OrganizationID,
		"janua #635 requires the client row to be bound to the CTM organization")

	values := buildIntakeValues(reg.Issuer, "jnc_mail", "s3cr3t", p)
	assert.Equal(t, map[string]string{
		"janua_mail_client_id":     "jnc_mail",
		"janua_mail_client_secret": "s3cr3t",
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
}

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

func TestLoadRegistry_yantra4dStudioPublicLoginClient(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	p, ok := reg.Platforms["yantra4d-studio"]
	require.True(t, ok, "yantra4d-studio must be in the registry")

	// A browser SPA: PKCE, no secret, no org binding, no Vault target.
	require.NotNil(t, p.JanuaClient.IsConfidential)
	assert.False(t, *p.JanuaClient.IsConfidential, "the Studio holds no secret")
	assert.Empty(t, p.JanuaClient.OrganizationID)
	assert.True(t, p.publicLogin())
	assert.Empty(t, p.IntakeTarget, "nothing about a public client is secret; no intake target")
	assert.Empty(t, p.SessionIntakeTarget)

	// The redirect URI is the Studio ORIGIN, byte for byte — no path, no slash.
	assert.Equal(t, []string{"https://app.yantra4d.com", "http://localhost:5173"}, p.JanuaClient.RedirectURIs)
	assert.Equal(t, "yantra4d-api", p.JanuaClient.Audience)
	assert.Equal(t, "yantra4d-studio", p.JanuaClient.Name)
	assert.Regexp(t, `^jnc_[A-Za-z0-9_-]{8,56}$`, p.JanuaClient.ClientID, "the Studio client must stay pinned so a re-run reconciles instead of duplicating")
	assert.ElementsMatch(t, []string{"authorization_code", "refresh_token"}, p.JanuaClient.GrantTypes)
	assert.ElementsMatch(t, []string{"openid", "profile", "email"}, p.JanuaClient.AllowedScopes)

	// Every confidential platform still needs somewhere for its secret to go.
	for _, id := range reg.PlatformIDs() {
		q := reg.Platforms[id]
		if !q.publicLogin() {
			assert.NotEmpty(t, q.IntakeTarget, "confidential platform %q has no intake_target", id)
		}
	}
}

func TestLoadRegistry_creatorCensusWebLoginClient(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["creator-census-web"]
	require.True(t, ok, "creator-census-web platform missing")

	assert.Equal(t, "creator-census/web-oidc", p.IntakeTarget)
	// The session secret is minted server-side through its own intake target
	// (--generate), never re-minted by every provision run.
	assert.Empty(t, p.SessionIntakeTarget)
	require.Equal(t, map[string]string{"janua_client_secret": "client_secret"}, p.IntakeKeyMap,
		"only the secret goes to Vault; the client_id is plain Deployment config")

	jc := p.JanuaClient
	assert.Equal(t, "MADFAM Creator Census", jc.Name)
	assert.Equal(t, "creator-census-web", jc.ClientKey)
	assert.Equal(t, "creator-census-api", jc.Audience)
	assert.Equal(t, "https://cc.madfam.io", jc.WebsiteURL)
	assert.True(t, jc.confidential(), "the BFF holds the secret; the client must be confidential")
	assert.False(t, p.publicLogin())
	assert.Empty(t, jc.OrganizationID)
	// Janua matches redirect URIs exactly (scheme, host, port, path).
	assert.Equal(t, []string{
		"https://cc-app.madfam.io/auth/callback",
		"http://localhost:3000/auth/callback",
	}, jc.RedirectURIs)
	assert.ElementsMatch(t, []string{"openid", "email", "profile", "offline_access"}, jc.AllowedScopes)
	assert.ElementsMatch(t, []string{"authorization_code", "refresh_token"}, jc.GrantTypes)

	assert.Equal(t, map[string]string{"janua_client_secret": "s3cr3t"},
		buildIntakeValues(reg.Issuer, "jnc_census", "s3cr3t", p))
}

// family-history-web (2026-10-01). A confidential login client like
// creator-census-web, with two deliberate differences: BOTH the id and the
// secret are filed in Vault (the public family-history repo reads
// AUTH_JANUA_CLIENT_ID from its ExternalSecret, nauta-style), and the
// production client carries NO localhost redirect URI (local development uses
// its own client). client_id is deliberately NOT asserted: the client is not
// registered on Janua yet, and pinning its jnc_… id later must not break this.
func TestLoadRegistry_familyHistoryWebLoginClient(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["family-history-web"]
	require.True(t, ok, "family-history-web platform missing")

	assert.Equal(t, "family-history/web-oidc", p.IntakeTarget)
	// The session secret is minted server-side through its own intake target
	// (--generate fh_session_secret), never re-minted by every provision run.
	assert.Empty(t, p.SessionIntakeTarget)
	// Lowercase, byte for byte what the family-history-web ExternalSecret maps;
	// ESO syncs all of an ExternalSecret's keys or none.
	require.Equal(t, map[string]string{
		"auth_janua_client_id":     "client_id",
		"auth_janua_client_secret": "client_secret",
	}, p.IntakeKeyMap)

	jc := p.JanuaClient
	assert.Equal(t, "MADFAM Family History", jc.Name)
	assert.Equal(t, "family-history-web", jc.ClientKey)
	assert.Equal(t, "family-history-api", jc.Audience)
	assert.Equal(t, "https://fh.madfam.io", jc.WebsiteURL)
	assert.True(t, jc.confidential(), "the BFF holds the secret; the client must be confidential")
	assert.False(t, p.publicLogin())
	assert.Empty(t, jc.OrganizationID)
	// Janua matches redirect URIs exactly (scheme, host, port, path). Exactly
	// one: no localhost twin on the production client. Janua's RP-Initiated
	// Logout admits the origin root https://fh-app.madfam.io/ from this entry.
	assert.Equal(t, []string{"https://fh-app.madfam.io/auth/callback"}, jc.RedirectURIs)
	for _, u := range jc.RedirectURIs {
		assert.NotContains(t, u, "localhost", "production client must not accept a localhost callback")
	}
	// Exactly the web's request. The API enforces fh:read / fh:write per
	// method; fh:admin and fh:export must never be client-level grants, since
	// any user of the client could then request them.
	assert.ElementsMatch(t, []string{"openid", "profile", "email", "fh:read", "fh:write"}, jc.AllowedScopes)
	assert.NotContains(t, jc.AllowedScopes, "fh:admin")
	assert.NotContains(t, jc.AllowedScopes, "fh:export")
	// The web rotates refresh tokens, so both grants are required.
	assert.ElementsMatch(t, []string{"authorization_code", "refresh_token"}, jc.GrantTypes)

	assert.Equal(t, map[string]string{
		"auth_janua_client_id":     "jnc_fh",
		"auth_janua_client_secret": "s3cr3t",
	}, buildIntakeValues(reg.Issuer, "jnc_fh", "s3cr3t", p))
}

// Digital-twins machine edges (2026-10-03). Nine client_credentials clients:
// eight copy janua's seed_service_clients.py (SERVICE_CLIENTS and the
// ORG_BOUND_SERVICE_CLIENTS templates, named `<template>.<org slug>`), the
// ninth follows fabrication-prep's operator setup. Every one is confidential,
// has no browser leg, and files exactly an id/secret pair at its dedicated
// intake target. client_id is deliberately NOT asserted: none is pinned yet,
// and pinning the printed jnc_… ids later must not break this test.
func TestLoadRegistry_digitalTwinsMachineEdges(t *testing.T) {
	const madfamEcosystemOrg = "1a6233ef-185f-43ee-9181-e2591fbb2643"
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	cases := []struct {
		platform, name, audience, org, target, idKey, secretKey string
		scopes                                                  []string
	}{
		{"pravara-yantra4d-step-reader", "pravara-yantra4d-step-reader", "yantra4d-api", "",
			"pravara-mes/yantra4d-step-reader", "yantra4d_step_reader_client_id", "yantra4d_step_reader_client_secret",
			[]string{"yantra4d:render"}},
		{"yantra4d-asset-shells-publisher", "yantra4d-asset-shells-publisher", "asset-shells-api", "",
			"yantra4d/asset-shells-publisher", "asset_shells_publisher_client_id", "asset_shells_publisher_client_secret",
			[]string{"asset-shells:publish-types"}},
		{"fashion-cabinet-asset-shells-publisher", "fashion-cabinet-asset-shells-publisher", "asset-shells-api", "",
			"fashion-cabinet/asset-shells-publisher", "asset_shells_publisher_client_id", "asset_shells_publisher_client_secret",
			[]string{"asset-shells:publish-types"}},
		{"zavlo-cfdi-emitter", "zavlo-cfdi-emitter", "karafiel-api", "",
			"zavlo/cfdi-emitter", "zavlo_cfdi_emitter_client_id", "zavlo_cfdi_emitter_client_secret",
			[]string{"cfdi:issue"}},
		{"routecraft-billing-relay", "routecraft-billing-relay", "dhanam-api", "",
			"routecraft/billing-relay", "billing_relay_client_id", "billing_relay_client_secret",
			[]string{"billing:events"}},
		{"pravara-fabrication-prep", "pravara-fabrication-prep", "fabrication-prep-api", "",
			"pravara-mes/fabrication-prep-client", "fabrication_prep_client_id", "fabrication_prep_client_secret",
			[]string{"fabrication-prep:slice"}},
		{"forj-pravara-intake-madfam-ecosystem", "forj-pravara-intake.madfam-ecosystem", "pravara-api", madfamEcosystemOrg,
			"forj/pravara-intake", "pravara_intake_client_id", "pravara_intake_client_secret",
			[]string{"pravara-mes:jobs"}},
		{"cotiza-pravara-intake-madfam-ecosystem", "cotiza-pravara-intake.madfam-ecosystem", "pravara-api", madfamEcosystemOrg,
			"digifab-quoting/pravara-intake", "pravara_intake_client_id", "pravara_intake_client_secret",
			[]string{"pravara-mes:jobs"}},
		{"pravara-asset-shells-publisher-madfam-ecosystem", "pravara-asset-shells-publisher.madfam-ecosystem", "asset-shells-api", madfamEcosystemOrg,
			"pravara-mes/asset-shells-publisher-madfam-ecosystem", "asset_shells_publisher_madfam_ecosystem_client_id", "asset_shells_publisher_madfam_ecosystem_client_secret",
			[]string{"asset-shells:publish-instances", "asset-shells:read"}},
	}
	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			p, ok := reg.Platforms[tc.platform]
			require.True(t, ok, "platform %q missing", tc.platform)
			assert.Equal(t, tc.target, p.IntakeTarget)
			assert.Empty(t, p.SessionIntakeTarget, "a machine edge has no session secret")
			require.Equal(t, map[string]string{tc.idKey: "client_id", tc.secretKey: "client_secret"}, p.IntakeKeyMap)

			jc := p.JanuaClient
			assert.Equal(t, tc.name, jc.Name, "the Janua client name; existing clients are matched by it")
			assert.Equal(t, tc.name, jc.ClientKey)
			assert.Equal(t, tc.audience, jc.Audience)
			assert.Equal(t, tc.org, jc.OrganizationID)
			assert.Equal(t, tc.scopes, jc.AllowedScopes)
			assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes)
			assert.Empty(t, jc.RedirectURIs, "a client_credentials client has no browser leg")
			require.NotNil(t, jc.IsConfidential)
			assert.True(t, jc.confidential())
			assert.False(t, p.publicLogin())
			assert.NotEmpty(t, jc.Description)

			assert.Equal(t, map[string]string{tc.idKey: "jnc_fixture", tc.secretKey: "fixture-secret"},
				buildIntakeValues(reg.Issuer, "jnc_fixture", "fixture-secret", p))
		})
	}
}

// The nine digital-twins machine edges are pinned to the client ids their first
// provision run printed (2026-10-03). A pin is exclusive: the provisioner then
// reconciles exactly that client and refuses to create a second one, so a
// changed or dropped pin here is a different client, not an edit.
func TestLoadRegistry_digitalTwinsEdgesPinned(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	pins := map[string]string{
		"pravara-yantra4d-step-reader":                    "jnc_J3YH8KGGzBI1c7PoDSvKtwVr23fPd1Sw",
		"yantra4d-asset-shells-publisher":                 "jnc_qtf_llhI6wXiRKb_-1pHVScuAHrfGbP_",
		"fashion-cabinet-asset-shells-publisher":          "jnc_jb7_ZSyLKOlntsEesRnSPd5RiUzrKvP3",
		"zavlo-cfdi-emitter":                              "jnc_6H59wA9XIfa_pXTblLPvMYUPPFdWLjgA",
		"routecraft-billing-relay":                        "jnc_3NfmbrkXXFp9sWsPn5E53unmjaWgiD4j",
		"forj-pravara-intake-madfam-ecosystem":            "jnc_70Aza2a0PPhnQ91vbAhyZv_ALitHbEHB",
		"cotiza-pravara-intake-madfam-ecosystem":          "jnc_O_tTNXViDH3UetR3tOgQ0NKyWlWFuiou",
		"pravara-asset-shells-publisher-madfam-ecosystem": "jnc_8hP5pYanYdYjS-QKapjyh4gpdk-7oKK9",
		"pravara-fabrication-prep":                        "jnc_e3gHN2ZdyGONKSZPDo-dP2HTxjtlhj2y",
	}
	for id, want := range pins {
		p, ok := reg.Platforms[id]
		require.True(t, ok, "platform %q missing from the registry", id)
		assert.Equal(t, want, p.JanuaClient.ClientID, "platform %q must stay pinned to its provisioned client", id)
	}
}

// voxa-selva: Voxa's API calling Selva's inference gateway. A confidential
// client_credentials edge with no browser leg, audience selva-office (what
// Selva's verify_jwt checks `aud` against), scope selva:infer only, org-bound
// to madfam-ecosystem so the token carries the org_id Selva keys its tenant
// policy and usage ledger on. client_id is deliberately NOT asserted: it is
// pinned after the first provision run.
func TestLoadRegistry_voxaSelvaMachineEdge(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["voxa-selva"]
	require.True(t, ok, "platform voxa-selva missing")
	assert.Equal(t, "voxa/selva-client", p.IntakeTarget)
	assert.Empty(t, p.SessionIntakeTarget, "a machine edge has no session secret")
	require.Equal(t, map[string]string{"selva_client_id": "client_id", "selva_client_secret": "client_secret"}, p.IntakeKeyMap)

	jc := p.JanuaClient
	assert.Equal(t, "voxa-selva", jc.Name)
	assert.Equal(t, "voxa-selva", jc.ClientKey)
	assert.Equal(t, "selva-office", jc.Audience)
	assert.Equal(t, "1a6233ef-185f-43ee-9181-e2591fbb2643", jc.OrganizationID)
	assert.Equal(t, []string{"selva:infer"}, jc.AllowedScopes)
	assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes)
	assert.Empty(t, jc.RedirectURIs, "a client_credentials client has no browser leg")
	require.NotNil(t, jc.IsConfidential)
	assert.True(t, jc.confidential())
	assert.False(t, p.publicLogin())
	assert.NotEmpty(t, jc.Description)

	assert.Equal(t, map[string]string{"selva_client_id": "jnc_fixture", "selva_client_secret": "fixture-secret"},
		buildIntakeValues(reg.Issuer, "jnc_fixture", "fixture-secret", p))
}

// selva-pravara-mes-madfam-ecosystem: Selva's phygital tools reading Pravara
// MES order status and inventory. Pravara refuses tokens without tenant_id, and
// Janua emits it only for organization-bound clients, so unlike selva-yantra4d
// this edge is org-bound (madfam-ecosystem, where Cotiza's and Forj's Pravara
// intakes are bound). Scope pravara-mes:read only; audience pravara-api.
func TestLoadRegistry_selvaPravaraMesMachineEdge(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["selva-pravara-mes-madfam-ecosystem"]
	require.True(t, ok, "platform selva-pravara-mes-madfam-ecosystem missing")
	assert.Equal(t, "selva/pravara-mes-client", p.IntakeTarget)
	assert.Empty(t, p.SessionIntakeTarget, "a machine edge has no session secret")
	require.Equal(t, map[string]string{
		"pravara_mes_client_id":     "client_id",
		"pravara_mes_client_secret": "client_secret",
	}, p.IntakeKeyMap)

	jc := p.JanuaClient
	assert.Equal(t, "selva-pravara-mes.madfam-ecosystem", jc.Name)
	assert.Equal(t, "selva-pravara-mes.madfam-ecosystem", jc.ClientKey)
	assert.Equal(t, "pravara-api", jc.Audience)
	assert.Equal(t, "1a6233ef-185f-43ee-9181-e2591fbb2643", jc.OrganizationID, "Pravara needs tenant_id; Janua sets it only for org-bound clients")
	assert.Equal(t, []string{"pravara-mes:read"}, jc.AllowedScopes)
	assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes)
	assert.Empty(t, jc.RedirectURIs, "a client_credentials client has no browser leg")
	require.NotNil(t, jc.IsConfidential)
	assert.True(t, jc.confidential())
	assert.False(t, p.publicLogin())
	assert.NotEmpty(t, jc.Description)
}

// selva-yantra4d: Selva's phygital tools calling Yantra4D render, analysis and
// quote requests. A confidential client_credentials edge with no browser leg,
// audience yantra4d-api (what Yantra4D's decode_token checks `aud` against),
// scope yantra4d:render only (what its render routes require of machine
// tokens). Platform-admin like pravara-yantra4d-step-reader: Yantra4D reads no
// tenant from machine tokens. The pair lands at selva/yantra4d-client as the
// two lowercase properties selva-office's selva-service-clients ExternalSecret
// maps. client_id is deliberately NOT asserted: it is pinned after the first
// provision run.
func TestLoadRegistry_selvaYantra4dMachineEdge(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)

	p, ok := reg.Platforms["selva-yantra4d"]
	require.True(t, ok, "platform selva-yantra4d missing")
	assert.Equal(t, "selva/yantra4d-client", p.IntakeTarget)
	assert.Empty(t, p.SessionIntakeTarget, "a machine edge has no session secret")
	require.Equal(t, map[string]string{
		"yantra4d_client_id":     "client_id",
		"yantra4d_client_secret": "client_secret",
	}, p.IntakeKeyMap)
	for k := range p.IntakeKeyMap {
		assert.Equal(t, strings.ToLower(k), k,
			"intake key %q must be lowercase to match selva-office's ExternalSecret property", k)
	}

	jc := p.JanuaClient
	assert.Equal(t, "selva-yantra4d", jc.Name)
	assert.Equal(t, "selva-yantra4d", jc.ClientKey)
	assert.Equal(t, "yantra4d-api", jc.Audience)
	assert.Empty(t, jc.OrganizationID, "Yantra4D reads no tenant from machine tokens; the edge is platform-admin")
	assert.Equal(t, []string{"yantra4d:render"}, jc.AllowedScopes)
	assert.Equal(t, []string{"client_credentials"}, jc.GrantTypes)
	assert.Empty(t, jc.RedirectURIs, "a client_credentials client has no browser leg")
	require.NotNil(t, jc.IsConfidential)
	assert.True(t, jc.confidential())
	assert.False(t, p.publicLogin())
	assert.NotEmpty(t, jc.Description)

	assert.Equal(t, map[string]string{
		"yantra4d_client_id":     "jnc_fixture",
		"yantra4d_client_secret": "fixture-secret",
	}, buildIntakeValues(reg.Issuer, "jnc_fixture", "fixture-secret", p))
}
