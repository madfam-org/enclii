package secretsintake

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStagingGenerationPoliciesAreFixedAndSuppliedOnlyWhereRequired(t *testing.T) {
	for id, profile := range map[string]string{"janua-staging/runtime-crypto": JanuaStagingCryptoProfile, "janua-staging/redis": JanuaStagingRedisProfile} {
		target, err := GetTarget(id)
		require.NoError(t, err)
		require.Equal(t, profile, target.Generate.Profile)
		for _, mutate := range []func(*Target){
			func(t *Target) { t.Generate = nil },
			func(t *Target) { t.ID = "janua/runtime-crypto" },
			func(t *Target) { t.VaultPath = "secret/janua" },
			func(t *Target) { t.Namespace = "janua" },
			func(t *Target) { t.ExternalSecret = "janua-secrets" },
			func(t *Target) { t.Keys = t.Keys[:1] },
		} {
			changed := target
			mutate(&changed)
			require.Error(t, validateGenerationPolicy(changed))
		}
	}
	for _, id := range []string{"janua-staging/database", "janua-staging/database-ca"} {
		target, err := GetTarget(id)
		require.NoError(t, err)
		require.True(t, target.Generate.Disabled)
		require.Empty(t, target.Generate.Profile)
	}
}

func TestGenerationPolicyRefusesAmbiguousOrUnknownProfiles(t *testing.T) {
	for _, policy := range []*GeneratePolicy{
		{Disabled: true, Bytes: 32},
		{Disabled: true, Profile: JanuaStagingCryptoProfile},
		{Profile: "unknown"},
		{Profile: JanuaStagingCryptoProfile, Bytes: 32},
	} {
		require.Error(t, validateGenerationPolicy(Target{ID: "fixture", Generate: policy}))
	}
}
