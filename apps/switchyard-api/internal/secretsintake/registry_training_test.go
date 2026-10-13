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
		{"crea-sim/gateway-runtime", "secret/crea-sim-gateway", "crea-sim", "crea-sim-gateway", []string{"crea_sim_ctm_org_id", "crea_sim_database_namespace", "crea_sim_portal_database_url", "crea_sim_session_key", "sim01_context_key", "sim01_control_key"}},
		{"crea-sim/capture-runtime", "secret/crea-sim-captura", "crea-sim", "crea-sim-captura", []string{"crea_sim_database_namespace", "crea_sim_portal_database_url", "sim01_mail_key"}},
		{"crea-sim/kernel01-runtime", "secret/crea-sim-map01", "crea-sim", "crea-sim-map01", []string{"database_url", "map_simulation_context_key", "map_simulation_control_key", "map_simulation_mail_key", "map_simulation_mcp_client_id", "map_mcp_pseudonym_key"}},
		{"crea-sim/gateway-ca", "secret/crea-sim-gateway", "crea-sim", "crea-sim-gateway-ca", []string{"database_ca_pem"}},
		{"crea-sim/capture-ca", "secret/crea-sim-captura", "crea-sim", "crea-sim-captura-ca", []string{"database_ca_pem"}},
		{"crea-sim/kernel01-ca", "secret/crea-sim-map01", "crea-sim", "crea-sim-map01-ca", []string{"database_ca_pem"}},
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
