package secretsintake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRegistry(t *testing.T) {
	reg, err := LoadRegistry()
	require.NoError(t, err)
	assert.Len(t, reg, 33)
	assert.Contains(t, reg, "ceq/vast-api-key")
	assert.Contains(t, reg, "karafiel/web-oidc-janua")
	tgt := reg["ceq/vast-api-key"]
	assert.Equal(t, "secret/ceq", tgt.VaultPath)
	assert.Equal(t, "ceq-orchestrator-secrets", tgt.ExternalSecret)
}

func TestGetTarget(t *testing.T) {
	tgt, err := GetTarget("enclii/internal-api-key")
	require.NoError(t, err)
	assert.Equal(t, "secret/enclii", tgt.VaultPath)
	assert.Equal(t, "enclii-internal-api-key", tgt.ExternalSecret)

	_, err = GetTarget("unknown/target")
	require.Error(t, err)
}

func TestListTargetsSorted(t *testing.T) {
	list, err := ListTargets()
	require.NoError(t, err)
	require.Len(t, list, 33)
	for i := 1; i < len(list); i++ {
		assert.Less(t, list[i-1].ID, list[i].ID, "targets should be sorted by id")
	}
	ids := make([]string, len(list))
	for i, t := range list {
		ids[i] = t.ID
	}
	assert.Equal(t, []string{
		"angelia/courier-alertmanager",
		"angelia/courier-channel-tokens",
		"angelia/courier-producer-keys",
		"angelia/courier-webhook-signing-keys",
		"ceq/janua-client-secret",
		"ceq/vast-api-key",
		"coupler/janua-service-token",
		"crea-map/internal-api-key",
		"crea-map/kalya-feeds",
		"crea-map/rls-por-caso",
		"crea-map/selva-api-key",
		"crea/porkbun-registrar",
		"dhanam/app-infra",
		"dhanam/oidc-janua",
		"dhanam/session-auth",
		"dhanam/stripe-mx-live",
		"enclii/internal-api-key",
		"janua/internal-api-key",
		"kalya/internal-api-key",
		"karafiel/web-oidc-janua",
		"lexidrop/oidc-janua",
		"lexidrop/selva-inference",
		"nauta/internal-probe-key",
		"nauta/kalya-feed-tokens",
		"nauta/oidc-janua",
		"nauta/oidc-janua-portal",
		"nauta/symbiosis-hcm-oauth",
		"nauta/symbiosis-hcm-token",
		"phynd-crm/oidc-janua",
		"platform/comms-resend-api-key",
		"symbiosis-hcm/map-absence-feed",
		"telesia/oidc-janua",
		"telesia/runtime",
	}, ids)
}

// The 2026-09-03 batch: the operator had to break-glass `vault kv patch` for
// these because no target existed. Pin their routing so a rename is a test
// failure and not a silent write to the wrong Vault path.
func TestSeptember2026Targets(t *testing.T) {
	cases := []struct {
		id        string
		vaultPath string
		keys      []string
	}{
		{"crea-map/internal-api-key", "secret/crea-map", []string{"internal_api_key"}},
		{"crea-map/kalya-feeds", "secret/crea-map", []string{"kalya_occupancy_feed_url", "kalya_capacity_feed_url"}},
		{"symbiosis-hcm/map-absence-feed", "secret/symbiosis-hcm", []string{"map_absence_feed_url", "map_absence_feed_key"}},
		{"nauta/kalya-feed-tokens", "secret/nauta", []string{"kalya_feed_tokens"}},
		{"nauta/symbiosis-hcm-token", "secret/nauta", []string{"symbiosis_hcm_token"}},
		{"janua/internal-api-key", "secret/janua", []string{"internal_api_key"}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, tc.vaultPath, tgt.VaultPath)
			assert.Equal(t, tc.keys, tgt.Keys)
			assert.NotEmpty(t, tgt.Label)
			assert.NotEmpty(t, tgt.Namespace)
		})
	}
}

// The nauta→Symbiosis-HCM OAuth machine edge (2026-09-08). Its id+secret pair
// is filed by `enclii secrets provision oidc --platform nauta-symbiosis-hcm`.
// Distinct from nauta/symbiosis-hcm-token (a static bearer) by BOTH its keys
// and its ExternalSecret: it projects through nauta-hcm-oauth — nauta #264's
// DEDICATED, isolated ExternalSecret — not nauta-web-secrets, so a not-yet-
// provisioned client degrades only the RH slice. It shares secret/nauta with
// every other nauta target; the intake write is a merge, so it lands alongside
// them without clobbering. Lowercase properties to match the ExternalSecret's
// `property:` fields (ESO is all-or-nothing per ExternalSecret).
func TestNautaSymbiosisHCMOAuthTarget(t *testing.T) {
	tgt, err := GetTarget("nauta/symbiosis-hcm-oauth")
	require.NoError(t, err)
	assert.Equal(t, "secret/nauta", tgt.VaultPath)
	assert.Equal(t, "nauta", tgt.Namespace)
	assert.Equal(t, "nauta-hcm-oauth", tgt.ExternalSecret,
		"must be the dedicated isolated ExternalSecret, not nauta-web-secrets")
	assert.Equal(t, []string{
		"symbiosis_hcm_oauth_client_id",
		"symbiosis_hcm_oauth_client_secret",
	}, tgt.Keys)
	for _, k := range tgt.Keys {
		assert.Equal(t, strings.ToLower(k), k,
			"key %q must be lowercase to match nauta's ExternalSecret property", k)
	}
}

// The entitlement fail-open probe key (nauta #297). Shares secret/nauta with
// every other nauta target — the intake write is a merge, so it lands alongside
// them without clobbering. Deliberately NOT janua_internal_api_key: the probe
// must not carry a credential to the identity provider to ask whether that
// credential is missing. Pinned here so a rename is a test failure and not a
// silent write to a path nothing reads. The key is lowercase to match the
// `property:` field nauta's ExternalSecret will reference (ESO is all-or-nothing
// per ExternalSecret). No generate policy is declared, so it inherits the
// default entropy — which is what makes `--generate nauta_internal_probe_key`
// mint a value the operator never handles.
func TestNautaInternalProbeKeyTarget(t *testing.T) {
	tgt, err := GetTarget("nauta/internal-probe-key")
	require.NoError(t, err)
	assert.Equal(t, "secret/nauta", tgt.VaultPath)
	assert.Equal(t, "nauta", tgt.Namespace)
	assert.Equal(t, "nauta-web-secrets", tgt.ExternalSecret)
	assert.Equal(t, []string{"nauta_internal_probe_key"}, tgt.Keys)
	assert.NotEmpty(t, tgt.Label)
	for _, k := range tgt.Keys {
		assert.Equal(t, strings.ToLower(k), k,
			"key %q must be lowercase to match nauta's ExternalSecret property", k)
	}
	// No custom generate policy: --generate must still work at the default.
	assert.Nil(t, tgt.Generate)
	assert.Equal(t, DefaultGenerateBytes, tgt.GenerateBytes())
}

// The crea-map #303 batch: the Selva AI-gateway bearer and the RLS-por-caso
// switch (K23). BOTH are operator-supplied value-intake — never generated: the
// Selva bearer is minted on the Selva side and the RLS switch is a plain on/off
// string. They share secret/crea-map and crea-map-secrets with the other
// crea-map targets; the intake write is a merge, so they land alongside them
// without clobbering. Lowercase properties to match crea-map's ExternalSecret
// `property:` fields (ESO is all-or-nothing per ExternalSecret). Pinned so a
// rename is a test failure, not a silent write to a path nothing reads.
func TestCreaMap303Targets(t *testing.T) {
	cases := []struct {
		id   string
		keys []string
	}{
		{"crea-map/selva-api-key", []string{"selva_api_key"}},
		{"crea-map/rls-por-caso", []string{"rls_por_caso"}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, "secret/crea-map", tgt.VaultPath)
			assert.Equal(t, "crea-map", tgt.Namespace)
			assert.Equal(t, "crea-map-secrets", tgt.ExternalSecret)
			assert.Equal(t, tc.keys, tgt.Keys)
			assert.NotEmpty(t, tgt.Label)
			for _, k := range tgt.Keys {
				assert.Equal(t, strings.ToLower(k), k,
					"key %q must be lowercase to match crea-map's ExternalSecret property", k)
			}
			// Value-intake, not generatable: no generate policy declared.
			assert.Nil(t, tgt.Generate)
		})
	}
}

// The 2026-09-05 Courier batch. Angelia OWNS these four targets: it verifies
// every one of the credentials, so secret/angelia is the single writable home
// and every consumer reads that copy cross-path. Pinning the routing here makes
// a rename a test failure rather than a silent write to the wrong Vault path —
// and a wrong path is invisible until a page fails to reach a person.
func TestCourierTargets(t *testing.T) {
	cases := []struct {
		id   string
		keys []string
	}{
		{"angelia/courier-producer-keys", []string{
			"courier_producer_key_alarms",
			"courier_producer_key_enclii_ops",
			"courier_producer_key_tulana",
			"courier_producer_key_madfam_site",
		}},
		{"angelia/courier-channel-tokens", []string{
			"courier_telegram_bot_token",
			"courier_slack_bot_token",
		}},
		{"angelia/courier-alertmanager", []string{
			"courier_alertmanager_secret",
		}},
		// R23 (2026-09-05): the webhook channel's signing keys. One per producer,
		// suffix-paired with the producer key above — pinned side by side so a
		// renamed producer breaks here, not as a 503 on a customer's receiver.
		{"angelia/courier-webhook-signing-keys", []string{
			"courier_webhook_signing_key_alarms",
			"courier_webhook_signing_key_enclii_ops",
			"courier_webhook_signing_key_tulana",
			"courier_webhook_signing_key_madfam_site",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, "secret/angelia", tgt.VaultPath)
			assert.Equal(t, "angelia", tgt.Namespace)
			assert.Equal(t, "angelia-courier-secrets", tgt.ExternalSecret)
			assert.Equal(t, tc.keys, tgt.Keys)
			assert.NotEmpty(t, tgt.Label)
		})
	}
}

// Telesia (2026-09-12). Both targets write to ONE Vault path — the intake write
// is a merge — and every property is lowercase because telesia's ExternalSecrets
// reference lowercase `property:` fields and ESO is all-or-nothing per
// ExternalSecret. Pinning the routing here makes a rename a test failure instead
// of a silent write to a path nothing reads.
func TestTelesiaTargets(t *testing.T) {
	cases := []struct {
		id             string
		externalSecret string
		keys           []string
	}{
		{"telesia/oidc-janua", "telesia-web-secrets", []string{
			"janua_client_id",
			"janua_client_secret",
		}},
		{"telesia/runtime", "telesia-api-secrets", []string{
			"database_url",
			"direct_database_url",
			"redis_url",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, "secret/telesia", tgt.VaultPath)
			assert.Equal(t, "telesia", tgt.Namespace)
			assert.Equal(t, tc.externalSecret, tgt.ExternalSecret)
			assert.Equal(t, tc.keys, tgt.Keys)
			assert.NotEmpty(t, tgt.Label)
			for _, k := range tgt.Keys {
				assert.Equal(t, strings.ToLower(k), k,
					"key %q must be lowercase to match telesia's ExternalSecret property", k)
			}
		})
	}
}

// Optional scheduling credentials must never share the database/login projection.
func TestKalyaProvisioningCustody(t *testing.T) {
	key, err := GetTarget("kalya/internal-api-key")
	require.NoError(t, err)
	assert.Equal(t, "secret/kalya", key.VaultPath)
	assert.Equal(t, "kalya", key.Namespace)
	assert.Equal(t, "kalya-internal-api-key", key.ExternalSecret)
	assert.Equal(t, []string{"internal_api_key"}, key.Keys)
	assert.Equal(t, 32, key.GenerateBytes())

	feed, err := GetTarget("nauta/kalya-feed-tokens")
	require.NoError(t, err)
	assert.Equal(t, "nauta-kalya-feeds", feed.ExternalSecret)
	assert.Equal(t, "secret/nauta", feed.VaultPath)
	assert.Equal(t, []string{"kalya_feed_tokens"}, feed.Keys)
}
