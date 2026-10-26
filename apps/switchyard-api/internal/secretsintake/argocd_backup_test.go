package secretsintake

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestArgocdBackupFixedVaultOnlyPolicy(t *testing.T) {
	target, err := GetTarget(ArgocdBackupTargetID)
	require.NoError(t, err)
	require.NoError(t, ValidateArgocdBackupTarget(target))
	require.Empty(t, target.ExternalSecret)
	require.Equal(t, 32, target.GenerateBytes())
	for _, mutate := range []func(*Target){
		func(v *Target) { v.ID += "-copy" },
		func(v *Target) { v.VaultPath += "/other" },
		func(v *Target) { v.Namespace = "other" },
		func(v *Target) { v.ExternalSecret = "projection" },
		func(v *Target) { v.Keys = []string{strings.ToUpper(ArgocdBackupKey)} },
		func(v *Target) { v.Keys = []string{ArgocdBackupKey, "extra"} },
		func(v *Target) { v.Generate = nil },
		func(v *Target) { v.Generate.Disabled = true },
		func(v *Target) { v.Generate.Bytes = 32 },
		func(v *Target) { v.Generate.Profile = "" },
	} {
		changed := target
		policy := *target.Generate
		changed.Generate = &policy
		mutate(&changed)
		require.Error(t, validateGenerationPolicy(changed))
	}
}

func TestArgocdBackupReservedKeysRefuseNormalizedAliasesEverywhere(t *testing.T) {
	for _, key := range []string{
		ArgocdBackupKey, strings.ToUpper(ArgocdBackupKey), " ArgoCD-Backup.Encryption / Key ",
		ArgocdBackupOriginKey, "ENCLII.ARGOCD-BACKUP.ORIGIN-V1", "enclii/argocd/backup/origin/v1",
	} {
		target := Target{ID: "other/target", VaultPath: "secret/other", Keys: []string{key}}
		require.Error(t, validateGenerationPolicy(target), key)
	}
	// Existing shared-path groups remain admissible and cannot collide.
	for _, id := range []string{"enclii/internal-api-key", "data/pgbouncer-auth"} {
		target, err := GetTarget(id)
		require.NoError(t, err)
		require.NoError(t, validateGenerationPolicy(target))
	}
}

func TestNativeIntakeNormalizerExactExistingSemantics(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{" JWT__Private-Key. ", "jwt_private_key"},
		{"A/B\\C\x00D", "a_b_c_d"},
		{"ÉAPI_ＫＥＹ_ß", "api"},
		{"APIe\u0301Key", "apie_key"},
		{"ΑPIKey", "pikey"},
		{"_a___B9__", "a_b9"},
		{"☃😄", ""},
		{"\xffA\xfeB", "a_b"},
	} {
		require.Equal(t, tc.want, NormalizeVaultSecretKey(tc.raw))
	}
}

func TestArgocdBackupOriginIsOpaqueMetadataOnlyAndStrict(t *testing.T) {
	const id = "123e4567-e89b-42d3-a456-426614174000"
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	encoded, err := NewArgocdBackupOrigin(id, now)
	require.NoError(t, err)
	require.NoError(t, ValidateArgocdBackupOrigin(encoded, id, now))
	require.NotContains(t, encoded, "hash")
	require.NotContains(t, encoded, "digest")
	require.NotContains(t, encoded, "material")
	for _, invalid := range []string{
		encoded + `{}`, encoded + ` null`,
		strings.Replace(encoded, `"schema":`, `"schema":"duplicate","schema":`, 1),
		strings.Replace(encoded, `"schema":`, `"private_value_hash":"forbidden","schema":`, 1),
		strings.Replace(encoded, id, "123e4567-e89b-42d3-a456-426614174001", 1),
		strings.Replace(encoded, ArgocdBackupTargetID, "other/target", 1),
		strings.Replace(encoded, ArgocdBackupProfile, "other-profile", 1),
		strings.Replace(encoded, "2026-10-26T12:00:00Z", "2026-10-26T12:00:00+00:00", 1),
		strings.Replace(encoded, "2026-10-26T12:00:00Z", "2026-10-27T12:00:00Z", 1),
		strings.Repeat(" ", 1025), `{}`, `[]`, `null`,
	} {
		require.Error(t, ValidateArgocdBackupOrigin(invalid, id, now))
	}
	for _, invalidID := range []string{"", strings.ToUpper(id), " " + id, "123e4567-e89b-12d3-a456-426614174000", "urn:uuid:" + id} {
		require.False(t, ValidArgocdBackupOriginID(invalidID))
		_, err := NewArgocdBackupOrigin(invalidID, now)
		require.Error(t, err)
	}
}
