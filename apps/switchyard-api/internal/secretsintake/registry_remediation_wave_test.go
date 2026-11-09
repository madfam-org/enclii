package secretsintake

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Remediation wave (2026-11-09): the intake targets behind three durable
// mechanisms. Each link fails silently when it drifts: a wrong Vault path is a
// write nothing reads, a wrong property is an ExternalSecret that syncs
// nothing, and a wrong ExternalSecret name means intake never force-syncs the
// Secret the pod reads. So every target is pinned end to end.

// nauta → phynd-crm: an OIDC-filled pair, never generated.
func TestNautaPhyndCRMOAuthTarget(t *testing.T) {
	tgt, err := GetTarget("nauta/phynd-crm-oauth")
	require.NoError(t, err)
	assert.Equal(t, "secret/nauta", tgt.VaultPath)
	assert.Equal(t, "nauta", tgt.Namespace)
	assert.Equal(t, "nauta-phynd-crm-oauth", tgt.ExternalSecret,
		"the dedicated ExternalSecret, so the unprovisioned pair never blocks nauta-web-secrets")
	assert.Equal(t, []string{"phynd_crm_oauth_client_id", "phynd_crm_oauth_client_secret"}, tgt.Keys)
	for _, k := range tgt.Keys {
		assert.Equal(t, strings.ToLower(k), k, "key %q must match nauta's lowercase ExternalSecret property", k)
	}
	require.NotNil(t, tgt.Generate)
	assert.True(t, tgt.Generate.Disabled, "a Janua credential is minted by Janua, never generated")
	assert.Contains(t, tgt.Description, "--platform nauta-phynd-crm`")
	assert.NotEmpty(t, tgt.Label)

	// Every other target on secret/nauta keeps its own ExternalSecret and keys:
	// intake merges into the same Vault path, so a shared key would clobber.
	reg, err := LoadRegistry()
	require.NoError(t, err)
	for name, other := range reg {
		if other.VaultPath != "secret/nauta" || name == tgt.ID {
			continue
		}
		assert.NotEqual(t, tgt.ExternalSecret, other.ExternalSecret, "%s shares the ExternalSecret", name)
		for _, k := range tgt.Keys {
			assert.NotContains(t, other.Keys, k, "%s already writes %s", name, k)
		}
	}
}

// madfam-crawler per-caller keys: verifier-owned, one key per target, minted
// server-side.
func TestMadfamCrawlerCallerKeyTargets(t *testing.T) {
	callers := []string{"avala", "forgesight", "fortuna", "rondelio", "tezca", "tulana"}
	for _, caller := range callers {
		t.Run(caller, func(t *testing.T) {
			key := "api_key_" + caller
			tgt, err := GetTarget("madfam-crawler/api-key-" + caller)
			require.NoError(t, err)
			assert.Equal(t, "secret/madfam-crawler", tgt.VaultPath, "minted where it is verified")
			assert.Equal(t, "madfam-crawler", tgt.Namespace)
			assert.Equal(t, "madfam-crawler-caller-key-"+caller, tgt.ExternalSecret,
				"intake force-syncs the verifier's per-caller ExternalSecret")
			assert.Equal(t, []string{key}, tgt.Keys,
				"one key per target: a second key would be prompted for during --generate")
			assert.Contains(t, tgt.Description, "--generate "+key, "server-minted, never typed")
			assert.Contains(t, tgt.Description, "CALLER_KEY_"+strings.ToUpper(caller))
			assert.NotEmpty(t, tgt.Label)
			assert.Nil(t, tgt.Generate, "default entropy: generation must stay allowed")
			assert.GreaterOrEqual(t, tgt.GenerateBytes(), DefaultGenerateBytes,
				"crawler-api ignores a per-caller key shorter than 32 characters")
		})
	}

	// Exactly these targets write secret/madfam-crawler, each to its own
	// ExternalSecret: one object per caller, so one missing key never freezes
	// another caller's sync.
	reg, err := LoadRegistry()
	require.NoError(t, err)
	var onCrawler []string
	externalSecrets := map[string]string{}
	for name, tgt := range reg {
		if tgt.VaultPath != "secret/madfam-crawler" {
			continue
		}
		onCrawler = append(onCrawler, strings.TrimPrefix(name, "madfam-crawler/api-key-"))
		prev, dup := externalSecrets[tgt.ExternalSecret]
		assert.False(t, dup, "%s and %s share an ExternalSecret", prev, name)
		externalSecrets[tgt.ExternalSecret] = name
	}
	sort.Strings(onCrawler)
	assert.Equal(t, callers, onCrawler)
}

// Dhanam credit-metering key (DHC-07): Dhanam verifies it, so it owns the
// copy; Karafiel signs with it through its own cross-path ExternalSecret.
func TestDhanamBillingWebhookSecretTarget(t *testing.T) {
	tgt, err := GetTarget("dhanam/billing-webhook-secret")
	require.NoError(t, err)
	assert.Equal(t, "secret/dhanam", tgt.VaultPath)
	assert.Equal(t, "dhanam", tgt.Namespace)
	assert.Equal(t, "dhanam-billing-webhook", tgt.ExternalSecret)
	assert.NotEqual(t, "dhanam-secrets", tgt.ExternalSecret,
		"never the service's main ExternalSecret: one missing property would freeze it")
	assert.Equal(t, []string{"billing_webhook_secret"}, tgt.Keys)
	assert.Contains(t, tgt.Description, "--generate billing_webhook_secret")
	assert.Contains(t, tgt.Description, "karafiel-dhanam-credit-signing",
		"the description names the signer's read-only copy")
	assert.Contains(t, tgt.Description, "BILLING_WEBHOOK_SECRET_NEXT")
	assert.NotEmpty(t, tgt.Label)
	assert.Nil(t, tgt.Generate, "default entropy: no person handles the value")
	assert.GreaterOrEqual(t, tgt.GenerateBytes(), DefaultGenerateBytes)

	// The CFDI callback key is a different value with a different verifier.
	callback, err := GetTarget("dhanam/karafiel-callback-secret")
	require.NoError(t, err)
	assert.NotEqual(t, callback.ExternalSecret, tgt.ExternalSecret)
	assert.NotContains(t, callback.Keys, tgt.Keys[0])
}
