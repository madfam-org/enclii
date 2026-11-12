package secretsintake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsolatedTrainingCustodyRoutes(t *testing.T) {
	cases := []struct {
		id, path, namespace, external string
		keys                          []string
	}{
		{"crea-sim/ui-oidc", "secret/crea-sim-gateway", "crea-sim", "crea-sim-gateway", []string{"crea_sim_janua_ui_id", "crea_sim_janua_ui_secret"}},
		{"crea-sim/mcp-oidc", "secret/crea-sim-gateway", "crea-sim", "crea-sim-gateway", []string{"crea_sim_janua_mcp_id", "crea_sim_janua_mcp_secret"}},
		{"crea-sim/selva-oidc", "secret/crea-sim-selva", "crea-sim", "crea-sim-selva", []string{"crea_sim_selva_id", "crea_sim_selva_secret"}},
		{"crea-sim/gateway-runtime", "secret/crea-sim-gateway", "crea-sim", "crea-sim-gateway", []string{"crea_sim_ctm_org_id", "crea_sim_database_namespace", "crea_sim_portal_database_url", "crea_sim_session_key", "sim01_context_key", "sim01_control_key", "sim02_context_key", "sim02_control_key", "sim03_context_key", "sim03_control_key"}},
		{"crea-sim/capture-runtime", "secret/crea-sim-captura", "crea-sim", "crea-sim-captura", []string{"crea_sim_database_namespace", "crea_sim_portal_database_url", "sim01_mail_key", "sim02_mail_key", "sim03_mail_key"}},
		{"crea-sim/kernel01-runtime", "secret/crea-sim-map01", "crea-sim", "crea-sim-map01", []string{"database_url", "map_simulation_context_key", "map_simulation_control_key", "map_simulation_mail_key", "map_simulation_mcp_client_id", "map_mcp_pseudonym_key"}},
		{"crea-sim/kernel02-runtime", "secret/crea-sim-map01", "crea-sim", "crea-sim-map02", []string{"map02_database_url", "map02_context_key", "map02_control_key", "map02_mail_key", "map02_mcp_pseudonym_key"}},
		{"crea-sim/kernel03-runtime", "secret/crea-sim-map01", "crea-sim", "crea-sim-map03", []string{"map03_database_url", "map03_context_key", "map03_control_key", "map03_mail_key", "map03_mcp_pseudonym_key"}},
		{"crea-sim/gateway-ca", "secret/crea-sim-gateway", "crea-sim", "crea-sim-gateway-ca", []string{"database_ca_pem"}},
		{"crea-sim/capture-ca", "secret/crea-sim-captura", "crea-sim", "crea-sim-captura-ca", []string{"database_ca_pem"}},
		{"crea-sim/kernel01-ca", "secret/crea-sim-map01", "crea-sim", "crea-sim-map01-ca", []string{"database_ca_pem"}},
		{"crea-sim/kernel02-ca", "secret/crea-sim-map01", "crea-sim", "crea-sim-map02-ca", []string{"map02_database_ca_pem"}},
		{"crea-sim/kernel03-ca", "secret/crea-sim-map01", "crea-sim", "crea-sim-map03-ca", []string{"map03_database_ca_pem"}},
		{"janua-staging/runtime-crypto", "secret/janua-staging", "janua-staging", "janua-staging-secrets", []string{"jwt_private_key", "jwt_public_key", "jwt_secret_key", "secret_key", "field_encryption_key"}},
		{"janua-staging/database", "secret/janua-staging", "janua-staging", "janua-staging-secrets", []string{"database_url"}},
		{"janua-staging/database-ca", "secret/janua-staging", "janua-staging", "janua-staging-ca", []string{"database_ca"}},
		{"janua-staging/redis", "secret/janua-staging", "janua-staging", "janua-staging-redis", []string{"redis_password", "redis_url"}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			target, err := GetTarget(tc.id)
			require.NoError(t, err)
			require.Equal(t, tc.path, target.VaultPath)
			require.Equal(t, tc.namespace, target.Namespace)
			require.Equal(t, tc.external, target.ExternalSecret)
			require.Equal(t, tc.keys, target.Keys)
			for _, key := range target.Keys {
				require.Equal(t, strings.ToLower(key), key)
				require.NotContains(t, key, "owner")
				require.NotContains(t, key, "migrator")
				require.NotContains(t, key, "smtp")
				require.NotContains(t, key, "resend")
			}
		})
	}
}

// Kernels 02 and 03 share kernel 01's Vault path, so isolation between kernels
// rests on their keys: no property may be filed by two targets on that path,
// and each kernel's targets carry only its own prefix.
func TestTrainingKernelsSharePathWithDisjointKeys(t *testing.T) {
	registry, err := LoadRegistry()
	require.NoError(t, err)
	owner := map[string]string{}
	kernels := 0
	for id, target := range registry {
		if target.VaultPath != "secret/crea-sim-map01" {
			continue
		}
		kernels++
		require.True(t, strings.HasPrefix(id, "crea-sim/kernel0"), "only kernel targets file into the kernel path: %s", id)
		prefix := ""
		switch {
		case strings.HasPrefix(id, "crea-sim/kernel02-"):
			prefix = "map02_"
		case strings.HasPrefix(id, "crea-sim/kernel03-"):
			prefix = "map03_"
		}
		for _, key := range target.Keys {
			previous, taken := owner[key]
			require.False(t, taken, "%s files %s, already filed by %s", id, key, previous)
			owner[key] = id
			if prefix != "" {
				require.True(t, strings.HasPrefix(key, prefix), "%s files %s outside its own prefix", id, key)
			} else {
				require.False(t, strings.HasPrefix(key, "map02_") || strings.HasPrefix(key, "map03_"), "kernel 01 files %s", key)
			}
		}
	}
	require.Equal(t, 6, kernels, "three runtime and three CA targets")
	require.Equal(t, "crea-sim/kernel01-runtime", owner["map_simulation_mcp_client_id"], "the public MCP client id is filed once, by kernel 01")
}
