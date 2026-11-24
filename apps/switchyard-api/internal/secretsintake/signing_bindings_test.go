package secretsintake

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSigningAuthorityTargetsAreIsolatedSuppliedOnly(t *testing.T) {
	registry, err := LoadRegistry()
	require.NoError(t, err)
	for _, tc := range []struct {
		id, path, namespace, external, key string
	}{
		{"karafiel/hcm-signing-bindings", "secret/karafiel", "karafiel", "karafiel-hcm-signing-bindings", "hcm_signing_bindings"},
		{"symbiosis-hcm/signing-bindings", "secret/symbiosis-hcm", "symbiosis-hcm", "hcm-signing-bindings", "company_signers"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			target := registry[tc.id]
			require.Equal(t, tc.path, target.VaultPath)
			require.Equal(t, tc.namespace, target.Namespace)
			require.Equal(t, tc.external, target.ExternalSecret)
			require.Equal(t, []string{tc.key}, target.Keys)
			require.NotNil(t, target.Generate)
			require.True(t, target.Generate.Disabled)
			for id, other := range registry {
				if id != tc.id && other.VaultPath == tc.path {
					require.NotContains(t, other.Keys, tc.key, "identity property must have one intake owner")
				}
			}
			for _, mutate := range []func(*Target){
				func(v *Target) { v.Generate = nil },
				func(v *Target) { v.Generate = &GeneratePolicy{Bytes: 32} },
				func(v *Target) { v.Namespace = "other-namespace" },
				func(v *Target) { v.VaultPath = "secret/other-application" },
				func(v *Target) { v.ExternalSecret = "mandatory-database-secret" },
				func(v *Target) { v.Keys = []string{"database_url"} },
			} {
				changed := target
				mutate(&changed)
				require.Error(t, validateGenerationPolicy(changed))
			}
		})
	}
}
