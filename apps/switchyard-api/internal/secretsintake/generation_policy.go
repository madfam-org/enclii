package secretsintake

import (
	"fmt"
	"slices"
)

const (
	JanuaStagingCryptoProfile = "janua-staging-crypto-v1"
	JanuaStagingRedisProfile  = "janua-staging-redis-v1"
)

// Typed profiles are deliberately scoped to the isolated staging destination.
// A copied policy must not silently turn into a production key generator.
func validateGenerationPolicy(t Target) error {
	requiredProfile := map[string]string{
		"janua-staging/runtime-crypto": JanuaStagingCryptoProfile,
		"janua-staging/redis":          JanuaStagingRedisProfile,
	}[t.ID]
	if requiredProfile != "" && (t.Generate == nil || t.Generate.Profile != requiredProfile) {
		return fmt.Errorf("target %q requires its fixed staging generation profile", t.ID)
	}
	if t.ID == "janua-staging/database" || t.ID == "janua-staging/database-ca" {
		if t.Generate == nil || !t.Generate.Disabled {
			return fmt.Errorf("target %q must remain supplied-only", t.ID)
		}
	}
	if t.Generate == nil {
		return nil
	}
	p := t.Generate
	if p.Disabled && (p.Profile != "" || p.Bytes != 0) {
		return fmt.Errorf("target %q has conflicting generation policy", t.ID)
	}
	if p.Profile == "" {
		return nil
	}
	if p.Bytes != 0 {
		return fmt.Errorf("target %q typed profile cannot override entropy", t.ID)
	}
	var id, external string
	var keys []string
	switch p.Profile {
	case JanuaStagingCryptoProfile:
		id, external = "janua-staging/runtime-crypto", "janua-staging-secrets"
		keys = []string{"jwt_private_key", "jwt_public_key", "jwt_secret_key", "secret_key", "field_encryption_key"}
	case JanuaStagingRedisProfile:
		id, external = "janua-staging/redis", "janua-staging-redis"
		keys = []string{"redis_password", "redis_url"}
	default:
		return fmt.Errorf("target %q has unknown generation profile", t.ID)
	}
	if t.ID != id || t.VaultPath != "secret/janua-staging" || t.Namespace != "janua-staging" || t.ExternalSecret != external || !slices.Equal(t.Keys, keys) {
		return fmt.Errorf("target %q typed generation destination or keys differ from the fixed staging profile", t.ID)
	}
	return nil
}
