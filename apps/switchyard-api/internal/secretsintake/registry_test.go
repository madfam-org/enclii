package secretsintake

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRegistry(t *testing.T) {
	reg, err := LoadRegistry()
	require.NoError(t, err)
	assert.Len(t, reg, 21)
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
	require.Len(t, list, 21)
	for i := 1; i < len(list); i++ {
		assert.Less(t, list[i-1].ID, list[i].ID, "targets should be sorted by id")
	}
	ids := make([]string, len(list))
	for i, t := range list {
		ids[i] = t.ID
	}
	assert.Equal(t, []string{
		"ceq/janua-client-secret",
		"ceq/vast-api-key",
		"coupler/janua-service-token",
		"crea-map/internal-api-key",
		"crea-map/kalya-feeds",
		"dhanam/app-infra",
		"dhanam/oidc-janua",
		"dhanam/session-auth",
		"dhanam/stripe-mx-live",
		"enclii/internal-api-key",
		"janua/internal-api-key",
		"karafiel/web-oidc-janua",
		"lexidrop/oidc-janua",
		"lexidrop/selva-inference",
		"nauta/kalya-feed-tokens",
		"nauta/oidc-janua",
		"nauta/oidc-janua-portal",
		"nauta/symbiosis-hcm-token",
		"phynd-crm/oidc-janua",
		"platform/comms-resend-api-key",
		"symbiosis-hcm/map-absence-feed",
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
