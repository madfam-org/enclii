package secretsintake

import (
	"fmt"
	"slices"
)

const (
	JanuaStagingCryptoProfile     = "janua-staging-crypto-v1"
	JanuaStagingRedisProfile      = "janua-staging-redis-v1"
	KarafielStagingCoreProfile    = "karafiel-staging-core-v1"
	KarafielStagingSessionProfile = "karafiel-staging-session-v1"
)

// Typed profiles are deliberately scoped to the isolated staging destination.
// A copied policy must not silently turn into a production key generator.
func validateGenerationPolicy(t Target) error {
	if keys, fixed := map[string][]string{
		"karafiel-staging/web-oidc":   {"web_oidc_client_id", "web_janua_secret_key", "web_oidc_issuer"},
		"karafiel-staging/admin-oidc": {"admin_oidc_client_id", "admin_janua_secret_key", "admin_oidc_issuer"},
	}[t.ID]; fixed {
		if t.VaultPath != "secret/karafiel-staging" || t.Namespace != "enclii" || t.ExternalSecret != "karafiel-staging-core-source" || !slices.Equal(t.Keys, keys) || t.Generate == nil || !t.Generate.Disabled || t.Generate.Profile != "" || t.Generate.Bytes != 0 {
			return fmt.Errorf("target %q must remain the fixed supplied-only staging registration group", t.ID)
		}
		return nil
	}
	requiredProfile := map[string]string{
		"janua-staging/runtime-crypto":   JanuaStagingCryptoProfile,
		"janua-staging/redis":            JanuaStagingRedisProfile,
		"karafiel-staging/core":          KarafielStagingCoreProfile,
		"karafiel-staging/admin-session": KarafielStagingSessionProfile,
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
	path, namespace := "secret/janua-staging", "janua-staging"
	var keys []string
	switch p.Profile {
	case JanuaStagingCryptoProfile:
		id, external = "janua-staging/runtime-crypto", "janua-staging-secrets"
		keys = []string{"jwt_private_key", "jwt_public_key", "jwt_secret_key", "secret_key", "field_encryption_key"}
	case JanuaStagingRedisProfile:
		id, external = "janua-staging/redis", "janua-staging-redis"
		keys = []string{"redis_password", "redis_url"}
	case KarafielStagingCoreProfile:
		id, external = "karafiel-staging/core", "karafiel-staging-core-source"
		path, namespace = "secret/karafiel-staging", "enclii"
		keys = []string{"django_secret_key", "encryption_master_key"}
	case KarafielStagingSessionProfile:
		id, external = "karafiel-staging/admin-session", "karafiel-staging-core-source"
		path, namespace = "secret/karafiel-staging", "enclii"
		keys = []string{"admin_session_secret"}
	default:
		return fmt.Errorf("target %q has unknown generation profile", t.ID)
	}
	if t.ID != id || t.VaultPath != path || t.Namespace != namespace || t.ExternalSecret != external || !slices.Equal(t.Keys, keys) {
		return fmt.Errorf("target %q typed generation destination or keys differ from the fixed staging profile", t.ID)
	}
	return nil
}
