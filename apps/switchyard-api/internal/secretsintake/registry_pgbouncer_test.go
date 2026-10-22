package secretsintake

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pgbouncer_auth's password (2026-10-21): the credential the shared pgbouncer
// uses for auth_query. Pinned end to end because each link fails quietly: a
// wrong Vault path is a write nothing reads, a wrong property is an
// ExternalSecret that never syncs, and a wrong ExternalSecret name means intake
// never force-syncs the Secret the pooler's init container reads. The values
// must match infra/k8s/platform-infra/pgbouncer-auth.externalsecret.yaml, which
// tests/scripts/test_pgbouncer_auth_query.py checks from the manifest side.
func TestDataPgbouncerAuthTarget(t *testing.T) {
	tgt, err := GetTarget("data/pgbouncer-auth")
	require.NoError(t, err)
	assert.Equal(t, "secret/enclii", tgt.VaultPath,
		"a path the switchyard-secret-writer policy already covers")
	assert.Equal(t, "data", tgt.Namespace)
	assert.Equal(t, "pgbouncer-auth", tgt.ExternalSecret)
	assert.NotEqual(t, "pgbouncer-userlist", tgt.ExternalSecret,
		"pgbouncer-userlist is written by switchyard-api; ESO must not adopt it")
	assert.Equal(t, []string{"pgbouncer_auth_password"}, tgt.Keys)
	assert.Contains(t, tgt.Description, "--generate pgbouncer_auth_password")
	// The init container refuses anything shorter than 32 base64url
	// characters; the default 32 bytes encode to 43.
	assert.Nil(t, tgt.Generate)
	assert.GreaterOrEqual(t, base64.RawURLEncoding.EncodedLen(tgt.GenerateBytes()), 32)
}

// pgbouncer_admin's password (2026-10-22): the admin console user's userlist
// line. Same chain as pgbouncer_auth's, through its own ExternalSecret so a
// missing property never blocks pgbouncer-auth's sync. The values must match
// infra/k8s/platform-infra/pgbouncer-admin.externalsecret.yaml, which
// tests/scripts/test_pgbouncer_auth_query.py checks from the manifest side.
func TestDataPgbouncerAdminTarget(t *testing.T) {
	tgt, err := GetTarget("data/pgbouncer-admin")
	require.NoError(t, err)
	assert.Equal(t, "secret/enclii", tgt.VaultPath,
		"a path the switchyard-secret-writer policy already covers")
	assert.Equal(t, "data", tgt.Namespace)
	assert.Equal(t, "pgbouncer-admin", tgt.ExternalSecret)
	assert.NotEqual(t, "pgbouncer-userlist", tgt.ExternalSecret,
		"pgbouncer-userlist is written by switchyard-api; ESO must not adopt it")
	assert.NotEqual(t, "pgbouncer-auth", tgt.ExternalSecret,
		"its own ExternalSecret: a missing admin property must not stop pgbouncer-auth syncing")
	assert.Equal(t, []string{"pgbouncer_admin_password"}, tgt.Keys)
	assert.Contains(t, tgt.Description, "--generate pgbouncer_admin_password")
	assert.Nil(t, tgt.Generate)
	assert.GreaterOrEqual(t, base64.RawURLEncoding.EncodedLen(tgt.GenerateBytes()), 32)
}
