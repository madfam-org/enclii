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
	assert.Len(t, reg, 57)
	assert.Contains(t, reg, "ceq/vast-api-key")
	assert.Contains(t, reg, "karafiel/web-oidc-janua")
	adminSession := reg["karafiel/admin-session"]
	assert.Equal(t, "secret/karafiel", adminSession.VaultPath)
	assert.Equal(t, "karafiel-admin-session", adminSession.ExternalSecret)
	assert.Equal(t, []string{"admin_session_secret"}, adminSession.Keys)
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
	require.Len(t, list, 57)
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
		"angelia/courier-database-url",
		"angelia/courier-producer-keys",
		"angelia/courier-webhook-signing-keys",
		"ceq/janua-client-secret",
		"ceq/vast-api-key",
		"coupler/janua-service-token",
		"crea-map/internal-api-key",
		"crea-map/janua-mail-client",
		"crea-map/kalya-feeds",
		"crea-map/rls-por-caso",
		"crea-map/selva-api-key",
		"crea/porkbun-registrar",
		"creator-census/web-oidc",
		"creator-census/web-session",
		"creator-census/youtube-key",
		"dhanam/app-infra",
		"dhanam/oidc-janua",
		"dhanam/session-auth",
		"dhanam/stripe-mx-live",
		"digifab-quoting/pravara-intake",
		"enclii/internal-api-key",
		"family-history/api-access",
		"family-history/web-oidc",
		"family-history/web-session",
		"fashion-cabinet/asset-shells-publisher",
		"forj/pravara-intake",
		"janua/internal-api-key",
		"kalya/internal-api-key",
		"karafiel/admin-session",
		"karafiel/web-oidc-janua",
		"lexidrop/oidc-janua",
		"lexidrop/selva-inference",
		"monitoring/alertmanager-smtp",
		"nauta/internal-probe-key",
		"nauta/kalya-feed-tokens",
		"nauta/oidc-janua",
		"nauta/oidc-janua-portal",
		"nauta/symbiosis-hcm-oauth",
		"nauta/symbiosis-hcm-token",
		"phynd-crm/oidc-janua",
		"platform/comms-resend-api-key",
		"pravara-mes/asset-shells-publisher-madfam-ecosystem",
		"pravara-mes/fabrication-prep-client",
		"pravara-mes/yantra4d-step-reader",
		"routecraft/billing-relay",
		"symbiosis-hcm/map-absence-feed",
		"telesia/oidc-janua",
		"telesia/runtime",
		"voxa-staging/api-runtime",
		"voxa-staging/web-session",
		"voxa/api-runtime",
		"voxa/selva-client",
		"voxa/web-session",
		"yantra4d/asset-shells-publisher",
		"zavlo/cfdi-emitter",
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
		{"crea-map/janua-mail-client", "secret/crea-map", []string{"janua_mail_client_id", "janua_mail_client_secret"}},
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

// The 2026-09-05 Courier batch (+ the 2026-09-23 ledger URL). Angelia OWNS these targets: it verifies
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
		// Courier Part A (2026-09-23): the durable ledger's connection string.
		// angelia-courier-secrets projects it as DATABASE_URL; a wrong property
		// name here is an api that refuses to start once COURIER_LEDGER=postgres.
		{"angelia/courier-database-url", []string{
			"courier_database_url",
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

// creator-census web: the Janua client secret and the session secret share one
// Vault path and one ExternalSecret but are separate targets, so the session
// key can be minted with --generate without prompting for the client secret.
func TestCreatorCensusWebTargets(t *testing.T) {
	cases := []struct {
		id  string
		key string
	}{
		{"creator-census/web-oidc", "janua_client_secret"},
		{"creator-census/web-session", "session_secret"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, "secret/creator-census", tgt.VaultPath)
			assert.Equal(t, "creator-census", tgt.Namespace)
			assert.Equal(t, "creator-census-web", tgt.ExternalSecret)
			assert.Equal(t, []string{tc.key}, tgt.Keys,
				"one key per target: a second key would be prompted for during --generate")
			assert.NotEmpty(t, tgt.Label)
			assert.Equal(t, DefaultGenerateBytes, tgt.GenerateBytes())
		})
	}
}

// creator-census YouTube key: its own target (typed, never generated) on the census path,
// projected by the ExternalSecret the collection CronJobs mount. The key is lowercase because
// Vault stores lowercase and the census ExternalSecret maps `property: youtube_api_key`.
func TestCreatorCensusYouTubeKeyTarget(t *testing.T) {
	tgt, err := GetTarget("creator-census/youtube-key")
	require.NoError(t, err)
	assert.Equal(t, "secret/creator-census", tgt.VaultPath)
	assert.Equal(t, "creator-census", tgt.Namespace)
	assert.Equal(t, "creator-census-config", tgt.ExternalSecret)
	assert.Equal(t, []string{"youtube_api_key"}, tgt.Keys)
	assert.NotEmpty(t, tgt.Label)
}

// family-history: three targets on one Vault path, each written by exactly one
// route — the provisioner (web-oidc, client id AND secret), --generate
// (web-session) and the masked prompt (api-access). The keys are lowercase and
// must match the family-history ExternalSecrets byte for byte: ESO syncs all of
// an ExternalSecret's keys or none.
func TestFamilyHistoryTargets(t *testing.T) {
	cases := []struct {
		id             string
		externalSecret string
		keys           []string
	}{
		{"family-history/api-access", "family-history-api", []string{"fh_early_access_allowlist"}},
		{"family-history/web-oidc", "family-history-web", []string{"auth_janua_client_id", "auth_janua_client_secret"}},
		{"family-history/web-session", "family-history-web", []string{"fh_session_secret"}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, "secret/family-history", tgt.VaultPath)
			assert.Equal(t, "family-history", tgt.Namespace)
			assert.Equal(t, tc.externalSecret, tgt.ExternalSecret)
			assert.Equal(t, tc.keys, tgt.Keys)
			for _, k := range tgt.Keys {
				assert.Equal(t, strings.ToLower(k), k, "Vault stores lowercase; an upper-case key would never match the ExternalSecret property")
			}
			assert.NotEmpty(t, tgt.Label)
			// FH_SESSION_SECRET must carry at least 32 bytes.
			assert.GreaterOrEqual(t, tgt.GenerateBytes(), 32)
		})
	}
}

// Digital-twins machine edges (2026-10-03): nine client_credentials pairs, each
// written only by `enclii secrets provision oidc`. Every consumer reads them
// from a DEDICATED `<app>-service-clients` ExternalSecret, never from a
// hand-maintained Secret: an Owner ExternalSecret over an existing Secret
// deletes the keys it does not produce. Keys are lowercase, as Vault stores
// them, and come in id/secret pairs.
func TestDigitalTwinsMachineEdgeTargets(t *testing.T) {
	cases := []struct {
		id, vaultPath, namespace, externalSecret string
		keys                                     []string
	}{
		{"pravara-mes/yantra4d-step-reader", "secret/pravara-mes", "pravara-mes", "pravara-service-clients",
			[]string{"yantra4d_step_reader_client_id", "yantra4d_step_reader_client_secret"}},
		{"pravara-mes/asset-shells-publisher-madfam-ecosystem", "secret/pravara-mes", "pravara-mes", "pravara-service-clients",
			[]string{"asset_shells_publisher_madfam_ecosystem_client_id", "asset_shells_publisher_madfam_ecosystem_client_secret"}},
		{"pravara-mes/fabrication-prep-client", "secret/pravara-mes", "pravara-mes", "pravara-service-clients",
			[]string{"fabrication_prep_client_id", "fabrication_prep_client_secret"}},
		{"yantra4d/asset-shells-publisher", "secret/yantra4d", "yantra4d", "yantra4d-service-clients",
			[]string{"asset_shells_publisher_client_id", "asset_shells_publisher_client_secret"}},
		{"fashion-cabinet/asset-shells-publisher", "secret/fashion-cabinet", "fashion-cabinet", "fashion-cabinet-service-clients",
			[]string{"asset_shells_publisher_client_id", "asset_shells_publisher_client_secret"}},
		{"forj/pravara-intake", "secret/forj", "forj", "forj-service-clients",
			[]string{"pravara_intake_client_id", "pravara_intake_client_secret"}},
		{"digifab-quoting/pravara-intake", "secret/digifab-quoting", "digifab-quoting", "digifab-quoting-service-clients",
			[]string{"pravara_intake_client_id", "pravara_intake_client_secret"}},
		{"zavlo/cfdi-emitter", "secret/zavlo", "zavlo", "zavlo-service-clients",
			[]string{"zavlo_cfdi_emitter_client_id", "zavlo_cfdi_emitter_client_secret"}},
		{"routecraft/billing-relay", "secret/routecraft", "routecraft", "routecraft-service-clients",
			[]string{"billing_relay_client_id", "billing_relay_client_secret"}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, tc.vaultPath, tgt.VaultPath)
			assert.Equal(t, tc.namespace, tgt.Namespace)
			assert.Equal(t, tc.externalSecret, tgt.ExternalSecret)
			assert.True(t, strings.HasSuffix(tgt.ExternalSecret, "-service-clients"),
				"a dedicated ExternalSecret, never a consumer's existing Secret")
			assert.Equal(t, tc.keys, tgt.Keys)
			for _, k := range tgt.Keys {
				assert.Equal(t, strings.ToLower(k), k, "Vault stores lowercase; an upper-case key would never match the ExternalSecret property")
			}
			assert.NotEmpty(t, tgt.Label)
			assert.NotEmpty(t, tgt.Description)
		})
	}
}

// Voxa's API → Selva inference gateway edge (ecosystem-oidc platform
// voxa-selva). Same shape as the digital-twins edges: written only by the OIDC
// provisioner, delivered by a dedicated `voxa-service-clients` ExternalSecret
// (never the hand-made voxa-secrets Secret), lowercase id/secret pair.
func TestVoxaSelvaClientTarget(t *testing.T) {
	tgt, err := GetTarget("voxa/selva-client")
	require.NoError(t, err)
	assert.Equal(t, "secret/voxa", tgt.VaultPath)
	assert.Equal(t, "voxa", tgt.Namespace)
	assert.Equal(t, "voxa-service-clients", tgt.ExternalSecret)
	assert.Equal(t, []string{"selva_client_id", "selva_client_secret"}, tgt.Keys)
	assert.NotEmpty(t, tgt.Label)
	assert.NotEmpty(t, tgt.Description)
}

// Voxa web session and API runtime (2026-10-04), production and staging.
// voxa-secrets is the onboarding Secret, not an ESO target, so each value gets
// a DEDICATED planned ExternalSecret and exactly one write route: web-session
// only through --generate auth_secret (Auth.js AUTH_SECRET, ruling R42: never
// the Janua client secret), api-runtime only at the masked prompt (a REDIS_URL
// composed on the host with Voxa's own DB index). Staging lands at its own
// Vault path and namespace, never production's.
func TestVoxaWebSessionAndAPIRuntimeTargets(t *testing.T) {
	cases := []struct {
		id             string
		vaultPath      string
		namespace      string
		externalSecret string
		keys           []string
	}{
		{"voxa/web-session", "secret/voxa", "voxa", "voxa-web-session", []string{"auth_secret"}},
		{"voxa-staging/web-session", "secret/voxa-staging", "voxa-staging", "voxa-web-session", []string{"auth_secret"}},
		{"voxa/api-runtime", "secret/voxa", "voxa", "voxa-api-runtime", []string{"redis_url"}},
		{"voxa-staging/api-runtime", "secret/voxa-staging", "voxa-staging", "voxa-api-runtime", []string{"redis_url"}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tgt, err := GetTarget(tc.id)
			require.NoError(t, err)
			assert.Equal(t, tc.vaultPath, tgt.VaultPath)
			assert.Equal(t, tc.namespace, tgt.Namespace)
			assert.Equal(t, tc.externalSecret, tgt.ExternalSecret)
			assert.Equal(t, tc.keys, tgt.Keys)
			for _, k := range tgt.Keys {
				assert.Equal(t, strings.ToLower(k), k, "Vault stores lowercase; an upper-case key would never match the ExternalSecret property")
			}
			assert.NotEqual(t, "voxa-secrets", tgt.ExternalSecret, "voxa-secrets is the onboarding Secret; intake cannot reach it")
			assert.NotEmpty(t, tgt.Label)
			assert.NotEmpty(t, tgt.Description)
			// AUTH_SECRET must carry at least 32 bytes when minted with --generate.
			assert.GreaterOrEqual(t, tgt.GenerateBytes(), 32)
		})
	}
}

// Alertmanager's SMTP password (2026-10-05, owner decision: route it through
// intake). Pinned end to end because every link fails silently: a wrong Vault
// path is a write nothing reads, a wrong property name is an ExternalSecret
// that syncs zero keys, and a wrong ExternalSecret name means intake never
// force-syncs the Secret Alertmanager mounts. The values here must match
// infra/k8s/production/monitoring/alertmanager-smtp.externalsecret.yaml, which
// tests/scripts/test_alertmanager_secret_mounts.py checks from the manifest
// side. The key is the property monitoring-secrets already reads, so the
// estate keeps one copy of this credential.
func TestMonitoringAlertmanagerSMTPTarget(t *testing.T) {
	tgt, err := GetTarget("monitoring/alertmanager-smtp")
	require.NoError(t, err)
	assert.Equal(t, "secret/monitoring", tgt.VaultPath)
	assert.Equal(t, "monitoring", tgt.Namespace)
	assert.Equal(t, "alertmanager-smtp", tgt.ExternalSecret,
		"the dedicated ExternalSecret Alertmanager mounts, never the git-only monitoring-secrets")
	assert.NotEqual(t, "alertmanager-smtp-secret", tgt.ExternalSecret,
		"alertmanager-smtp-secret is the retired hand-made Secret; ESO must not adopt it")
	assert.Equal(t, []string{"alertmanager_smtp_password"}, tgt.Keys,
		"one key: the prompt must ask for the app password and nothing else")
	for _, k := range tgt.Keys {
		assert.Equal(t, strings.ToLower(k), k, "Vault stores lowercase; an upper-case key would never match the ExternalSecret property")
	}
	assert.NotEmpty(t, tgt.Label)
	assert.NotEmpty(t, tgt.Description)
	// Value-intake, not generatable: a Gmail app password is minted by Google.
	// No generate policy is declared; the description and the runbook say
	// never to pass --generate (the API does not refuse it, see SECRET_INTAKE.md).
	assert.Nil(t, tgt.Generate)
	assert.Contains(t, tgt.Description, "never generated")
}
