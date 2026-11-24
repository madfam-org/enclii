package secretsintake

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tulana → Dhanam COGS MCP reader (2026-11-17, tulana#151, dhanam#1002).
// Pinned end to end because each link fails silently: a wrong Vault path is a
// write nothing reads, a wrong key is an ExternalSecret that syncs nothing, and
// joining tulana-secrets would freeze every other tulana secret until this
// pair exists (ESO is all-or-nothing per ExternalSecret).
func TestTulanaDhanamCogsClientTarget(t *testing.T) {
	keys := []string{"dhanam_cogs_client_id", "dhanam_cogs_client_secret"}

	tgt, err := GetTarget("tulana/dhanam-cogs-client")
	require.NoError(t, err)
	assert.Equal(t, "secret/tulana", tgt.VaultPath)
	assert.Equal(t, "tulana", tgt.Namespace)
	assert.Equal(t, "tulana-dhanam-cogs-client", tgt.ExternalSecret)
	assert.NotEqual(t, "tulana-secrets", tgt.ExternalSecret,
		"a Janua pair must not join tulana's self-generated keys' ExternalSecret")
	assert.Equal(t, keys, tgt.Keys)
	for _, k := range tgt.Keys {
		assert.Equal(t, strings.ToLower(k), k, "Vault stores lowercase; the ExternalSecret maps these properties verbatim")
	}
	require.NotNil(t, tgt.Generate)
	assert.True(t, tgt.Generate.Disabled, "a Janua credential is minted by Janua, never generated")
	assert.NotEmpty(t, tgt.Label)
	assert.Contains(t, tgt.Description, "--platform tulana-dhanam-cogs`")
	assert.Contains(t, tgt.Description, "dhanam-cogs:read")
	assert.NotContains(t, tgt.Description, ":write", "a read-only client")

	// No other target on secret/tulana shares a key or this ExternalSecret.
	reg, err := LoadRegistry()
	require.NoError(t, err)
	var onTulana []string
	for name, other := range reg {
		if other.VaultPath != "secret/tulana" {
			continue
		}
		onTulana = append(onTulana, name)
		if name == "tulana/dhanam-cogs-client" {
			continue
		}
		assert.NotEqual(t, tgt.ExternalSecret, other.ExternalSecret, "shares the ExternalSecret of %s", name)
		for _, k := range other.Keys {
			assert.NotContains(t, keys, k, "shares key %s with %s", k, name)
		}
	}

	// Every target on secret/tulana, sorted: a new one must be added here.
	sort.Strings(onTulana)
	assert.Equal(t, []string{
		"tulana/commercial-ga-evidence-token",
		"tulana/dhanam-cogs-client",
		"tulana/django-secret-key",
		"tulana/product-offer-ga-evidence-token",
		"tulana/selva-webhook-secret",
	}, onTulana)
}
