package secretsintake

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Grafana's local admin password (2026-10-17): moved off the hand-made
// grafana-credentials Secret onto intake. Pinned end to end because every link
// fails silently: a wrong Vault path is a write nothing reads, a wrong property
// name is an ExternalSecret that syncs zero keys, and a wrong ExternalSecret
// name means intake never force-syncs the Secret the init container reads. The
// values must match infra/k8s/production/monitoring/grafana-admin.externalsecret.yaml,
// which tests/scripts/test_grafana_admin_secret.py checks from the manifest side.
func TestMonitoringGrafanaAdminTarget(t *testing.T) {
	tgt, err := GetTarget("monitoring/grafana-admin")
	require.NoError(t, err)
	assert.Equal(t, "secret/monitoring", tgt.VaultPath,
		"the path monitoring-secrets names and alertmanager-smtp writes: one copy of every monitoring credential")
	assert.Equal(t, "monitoring", tgt.Namespace)
	assert.Equal(t, "grafana-admin", tgt.ExternalSecret,
		"the dedicated ExternalSecret Grafana's init container reads, never the git-only monitoring-secrets")
	assert.NotEqual(t, "grafana-credentials", tgt.ExternalSecret,
		"grafana-credentials is the hand-made Secret; ESO must not adopt it")
	assert.Equal(t, []string{"grafana_admin_password"}, tgt.Keys,
		"one key: --generate mints every declared key, and the prompt asks for exactly one")
	for _, k := range tgt.Keys {
		assert.Equal(t, strings.ToLower(k), k,
			"Vault stores lowercase; an upper-case key would never match the ExternalSecret property")
	}
	assert.NotEmpty(t, tgt.Label)
	assert.NotEmpty(t, tgt.Description)
	assert.Contains(t, tgt.Description, "--generate grafana_admin_password")
	// Generated at the default strength: 32 bytes encode to 43 characters.
	assert.Nil(t, tgt.Generate)
	assert.GreaterOrEqual(t, base64.RawURLEncoding.EncodedLen(tgt.GenerateBytes()), 32)
}
