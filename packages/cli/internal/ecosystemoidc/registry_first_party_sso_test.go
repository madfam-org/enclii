package ecosystemoidc

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// First-party silent SSO (owner decisions D2 and D5, 2026-10-27).
//
// Janua lets a client use prompt=none and skip consent only when it is active,
// CONFIDENTIAL and either carries madfam:silent_auth or has a selva-office* /
// madfam-* name (janua oauth_provider.py _is_silent_auth_allowed). The registry
// is the canonical declaration, so the set of clients that may sign a person in
// silently is decided here, and these tests pin it.

// firstPartyBrowserClients are the confidential first-party login clients that
// must carry the silent-auth scope. Adding a confidential login client to the
// registry without deciding its silent-SSO status fails
// TestEveryConfidentialLoginClientHasADecidedSilentSSOStatus.
var firstPartyBrowserClients = []string{
	"avala",
	"ceq",
	"converge-dash-web",
	"cotiza-web",
	"crea-map",
	"crea-sim",
	"creator-census-web",
	"cv-app",
	"family-history-web",
	"karafiel",
	"karafiel-admin",
	"lexidrop",
	"nauta",
	"nauta-portal",
	"phynd-crm",
	"telesia",
}

// confidentialLoginClientsWithoutSilentSSO are deliberate exclusions: the two
// isolated staging clients (a production Janua session must not sign anyone
// into staging silently) and the crea-sim MCP client, whose consent stays
// independent of the UI.
var confidentialLoginClientsWithoutSilentSSO = []string{
	"crea-sim-mcp",
	"karafiel-staging-admin",
	"karafiel-staging-web",
}

func TestFirstPartyBrowserClientsCarrySilentAuth(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	for _, id := range firstPartyBrowserClients {
		t.Run(id, func(t *testing.T) {
			p, ok := reg.Platforms[id]
			require.True(t, ok, "platform %q missing", id)
			require.NoError(t, validatePlatform(p))
			kind, err := p.JanuaClient.kind()
			require.NoError(t, err)
			require.Equal(t, humanClient, kind)
			require.True(t, p.JanuaClient.confidential(),
				"Janua refuses prompt=none for a public client; silent SSO needs a confidential client")
			assert.Contains(t, p.JanuaClient.AllowedScopes, simSilentScope)
			assert.Empty(t, p.JanuaClient.OrganizationID)
		})
	}
	for _, id := range confidentialLoginClientsWithoutSilentSSO {
		p, ok := reg.Platforms[id]
		require.True(t, ok, "platform %q missing", id)
		assert.NotContains(t, p.JanuaClient.AllowedScopes, simSilentScope, "platform %q is excluded from silent SSO", id)
	}
}

func TestEveryConfidentialLoginClientHasADecidedSilentSSOStatus(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	decided := map[string]bool{}
	for _, id := range firstPartyBrowserClients {
		decided[id] = true
	}
	for _, id := range confidentialLoginClientsWithoutSilentSSO {
		decided[id] = true
	}
	for _, id := range reg.PlatformIDs() {
		p := reg.Platforms[id]
		kind, err := p.JanuaClient.kind()
		require.NoError(t, err)
		if kind == humanClient && p.JanuaClient.confidential() {
			assert.True(t, decided[id],
				"confidential login client %q is in neither firstPartyBrowserClients nor confidentialLoginClientsWithoutSilentSSO", id)
		}
	}
}

// The scope means nothing on a public client (Janua never issues silently to a
// client without a secret) and it is reserved, so listing it on one would only
// make every later edit of that client platform-admin-only. On a machine client
// it has no browser to act on.
func TestSilentAuthOnlyOnConfidentialLoginClients(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	for _, id := range reg.PlatformIDs() {
		p := reg.Platforms[id]
		kind, err := p.JanuaClient.kind()
		require.NoError(t, err)
		has := false
		for _, scope := range p.JanuaClient.AllowedScopes {
			if scope == simSilentScope {
				has = true
			}
		}
		if has {
			assert.Equal(t, humanClient, kind, "platform %q: silent auth on a machine client", id)
			assert.True(t, p.JanuaClient.confidential(), "platform %q: silent auth on a public client", id)
		}
	}
}

// yaml.v3 drops unknown keys without a word, so a field the schema lacks (for
// example post_logout_redirect_uris) would read as declared and change nothing.
// Decode both copies strictly so such a field fails the build instead.
func TestRegistryHasNoUnknownFields(t *testing.T) {
	for _, path := range []string{"", "../../../../config/ecosystem-oidc-provision.yaml"} {
		raw := embeddedRegistry
		if path != "" {
			var err error
			raw, err = os.ReadFile(path)
			require.NoError(t, err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		var reg Registry
		require.NoError(t, dec.Decode(&reg), "registry %q carries a field the schema does not define", path)
	}
}

func TestEveryRegistryPlatformPassesLocalValidation(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	for _, id := range reg.PlatformIDs() {
		assert.NoError(t, validatePlatform(reg.Platforms[id]), "platform %q", id)
	}
}

// Dhanam's web exchanges the code in the browser without a secret, and Janua
// refuses a confidential client that sends none, so the pinned row is public.
func TestDhanamReconciledAsPublicBrowserClient(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	p := reg.Platforms["dhanam"]
	jc := p.JanuaClient
	assert.Equal(t, "dhanam-web", jc.Name)
	assert.Equal(t, "jnc_uE2zp9ume_Fd6jMl1elL6wqjiECM711t", jc.ClientID, "the id dhanam's web ships")
	assert.False(t, jc.confidential())
	assert.True(t, p.publicLogin())
	assert.NotContains(t, jc.AllowedScopes, simSilentScope)
	assert.Equal(t, "dhanam-api", jc.Audience)
	// A public client never has a secret written for it.
	values := buildIntakeValues(reg.Issuer, "jnc_fixture", "never-written", p)
	assert.NotContains(t, values, "OIDC_CLIENT_SECRET")
}

func TestSSOWave1Clients(t *testing.T) {
	reg, err := LoadRegistry("")
	require.NoError(t, err)
	cases := []struct {
		id, name, audience, target string
		confidential               bool
		redirects                  []string
		keyMap                     map[string]string
	}{
		{"forgesight-app", "forgesight-app", "forgesight-api", "", false,
			[]string{"https://app.forgesight.app", "https://app.forgesight.app/auth/callback", "https://app.forgesight.app/api/auth/callback", "http://localhost:3000", "http://localhost:3000/auth/callback", "http://localhost:3000/api/auth/callback"}, nil},
		{"forgesight-admin", "forgesight-admin", "forgesight-api", "", false,
			[]string{"https://admin.forgesight.app", "https://admin.forgesight.app/auth/callback", "http://localhost:3001", "http://localhost:3001/auth/callback"}, nil},
		{"converge-dash-web", "converge-dash-web", "converge-dash", "converge-dash/web-oidc", true,
			[]string{"https://dash.madfam.io/auth/callback"},
			map[string]string{"dash_web_janua_client_id": "client_id", "dash_web_janua_client_secret": "client_secret"}},
		{"avala", "avala", "avala-api", "avala/oidc-janua", true,
			[]string{"https://admin.avala.studio/api/auth/sso/callback", "https://app.avala.studio/api/auth/sso/callback"},
			map[string]string{"janua_client_id": "client_id", "janua_client_secret": "client_secret"}},
		{"selva-office", "selva-office", "selva-office", "", false,
			[]string{"https://app.selva.town/api/auth/callback/janua", "https://selva.town/api/auth/callback/janua", "https://admin.selva.town/api/auth/callback", "http://localhost:4301/api/auth/callback/janua"}, nil},
		{"crea-map", "crea-map", "crea-map", "crea-map/web-oidc", true,
			[]string{"https://crea-map.madfam.io/api/auth/callback", "https://map.creatumundo.mx/api/auth/callback", "https://ensayo-map.madfam.io/api/auth/callback", "http://localhost:3000/api/auth/callback"},
			map[string]string{"janua_client_secret": "client_secret"}},
		{"symbiosis-hcm-browser", "symbiosis-hcm-browser", "symbiosis-hcm", "", false,
			[]string{"https://hcm-admin.madfam.io/auth/callback", "https://hcm-app.madfam.io/auth/callback"}, nil},
		{"karafiel-admin", "Karafiel Admin", "karafiel-api", "karafiel/admin-oidc-janua", true,
			[]string{"https://admin.karafiel.mx/api/auth/callback", "https://kf-admin.madfam.io/api/auth/callback", "http://localhost:3051/api/auth/callback", "http://localhost:3000/api/auth/callback"},
			map[string]string{"oidc_client_id": "client_id", "janua_secret_key": "client_secret"}},
	}
	// Pins: only ids the consumer already ships. Kept apart from the audience
	// strings above so a secret scanner does not read an id as an API key.
	pins := map[string]string{
		"forgesight-app":        "jnc_hMmsXcCeWGFTn95l9I1iLzibSugXCgDC",
		"forgesight-admin":      "jnc_71GgDdnFqsb_ha9_Z45tAMRZCbLX9Rz5",
		"avala":                 "jnc_t6ruGZaATrpZ34ofKRhPi1Rv2WO0S3HW",
		"crea-map":              "jnc_Ed3t6r_RRxBq0aa2IeUnrxKQ0_UFaHLO",
		"symbiosis-hcm-browser": "jnc_bFCbNwR7iYdy19S_BldXuKYvgDii9mlT",
		"karafiel-admin":        "jnc_karafiel_admin_prod",
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			p, ok := reg.Platforms[tc.id]
			require.True(t, ok, "platform %q missing", tc.id)
			require.NoError(t, validatePlatform(p))
			jc := p.JanuaClient
			assert.Equal(t, tc.name, jc.Name, "existing rows are matched by name; a different name creates a twin")
			assert.Equal(t, tc.audience, jc.Audience)
			assert.Equal(t, pins[tc.id], jc.ClientID, "pin only an id the consumer already ships; unpinned entries stay empty")
			require.NotNil(t, jc.IsConfidential)
			assert.Equal(t, tc.confidential, jc.confidential())
			assert.Equal(t, tc.redirects, jc.RedirectURIs, "Janua matches callbacks byte for byte")
			assert.Empty(t, jc.OrganizationID)
			assert.Empty(t, p.SessionIntakeTarget, "a provision re-run must never re-mint a session secret")
			assert.Equal(t, tc.target, p.IntakeTarget)
			assert.Equal(t, tc.keyMap, p.IntakeKeyMap)
			assert.Equal(t, tc.confidential, contains(jc.AllowedScopes, simSilentScope))
			kind, err := jc.kind()
			require.NoError(t, err)
			assert.Equal(t, humanClient, kind)
		})
	}
	// Selva's own declaration grants authorization_code alone.
	assert.Equal(t, []string{"authorization_code"}, reg.Platforms["selva-office"].JanuaClient.GrantTypes)
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
