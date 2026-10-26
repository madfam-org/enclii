package secretsintake

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKarafielStagingTargetsCannotGenerateOrWriteOutsideFixedGroups(t *testing.T) {
	for _, id := range []string{"karafiel-staging/core", "karafiel-staging/admin-session", "karafiel-staging/web-oidc", "karafiel-staging/admin-oidc"} {
		target, err := GetTarget(id)
		require.NoError(t, err)
		require.Equal(t, "secret/karafiel-staging", target.VaultPath)
		require.Equal(t, "enclii", target.Namespace)
		require.Equal(t, "karafiel-staging-core-source", target.ExternalSecret)
		for _, mutate := range []func(*Target){func(t *Target) { t.VaultPath = "secret/karafiel" }, func(t *Target) { t.Namespace = "karafiel" }, func(t *Target) { t.ExternalSecret = "karafiel-secrets" }, func(t *Target) { t.Keys = append(append([]string{}, t.Keys...), "provider_key") }, func(t *Target) { t.Generate = nil }} {
			changed := target
			mutate(&changed)
			require.Error(t, validateGenerationPolicy(changed), id)
		}
	}
	for _, id := range []string{"karafiel-staging/web-oidc", "karafiel-staging/admin-oidc"} {
		target, err := GetTarget(id)
		require.NoError(t, err)
		require.True(t, target.Generate.Disabled)
		require.Empty(t, target.Generate.Profile)
	}
}
