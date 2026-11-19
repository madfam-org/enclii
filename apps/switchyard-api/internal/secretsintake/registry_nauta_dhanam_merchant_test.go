package secretsintake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nauta → Dhanam merchant payment projection (2026-11-18). The five properties
// nauta's nauta-merchant-oauth ExternalSecret reads, in its order. Pinned end to
// end because each link fails silently: a wrong key is an ExternalSecret that
// syncs nothing (all-or-nothing), and a shared key on secret/nauta would be
// clobbered by the other target's merge.
func TestNautaDhanamMerchantOAuthTarget(t *testing.T) {
	tgt, err := GetTarget("nauta/dhanam-merchant-oauth")
	require.NoError(t, err)
	assert.Equal(t, "secret/nauta", tgt.VaultPath)
	assert.Equal(t, "nauta", tgt.Namespace)
	assert.Equal(t, "nauta-merchant-oauth", tgt.ExternalSecret,
		"the dedicated ExternalSecret, so missing custody never blocks nauta-web-secrets")
	assert.Equal(t, []string{
		"dhanam_merchant_client_id",
		"dhanam_merchant_client_secret",
		"dhanam_merchant_workspace_id",
		"dhanam_merchant_organization_id",
		"dhanam_merchant_space_id",
	}, tgt.Keys)
	for _, k := range tgt.Keys {
		assert.Equal(t, strings.ToLower(k), k, "key %q must match nauta's lowercase ExternalSecret property", k)
	}
	require.NotNil(t, tgt.Generate)
	assert.True(t, tgt.Generate.Disabled, "a Janua credential and tenant identifiers are never generated")
	assert.Contains(t, tgt.Description, "--platform nauta-dhanam-merchant`")
	assert.Contains(t, tgt.Description, "dhanam:merchant-read")
	assert.NotEmpty(t, tgt.Label)

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
