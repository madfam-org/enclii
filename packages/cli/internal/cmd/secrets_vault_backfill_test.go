package cmd

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestSecretsVaultBackfill_AllowOverwriteSendsOverwriteArg(t *testing.T) {
	for _, path := range [][]string{
		{"secrets", "vault-backfill", "app-secrets", "--vault-path", "secret/app"},
		{"ops", "secrets", "vault-backfill", "app-secrets", "--vault-path", "secret/app"},
	} {
		t.Run(path[0], func(t *testing.T) {
			srv, seen, body := captureOperation(t, `{"operation":"ops.secrets.vault-backfill","status":"ready_to_apply","dry_run":true}`)
			root := NewRootCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "tok"})
			root.SetArgs(append(append([]string{}, path...), "--namespace", "app", "--allow-overwrite"))
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)

			require.NoError(t, root.Execute())
			assert.Equal(t, "POST /v1/ops/secrets/vault-backfill", *seen)
			assert.True(t, body.DryRun)
			assert.Equal(t, "true", body.Args["overwrite"])
			assert.Equal(t, "secret/app", body.Args["vault_path"])
		})
	}
}

func TestSecretsVaultBackfill_NoOverwriteByDefault(t *testing.T) {
	srv, _, body := captureOperation(t, `{"operation":"ops.secrets.vault-backfill","status":"ready_to_apply","dry_run":true}`)
	root := NewRootCommand(&config.Config{APIEndpoint: srv.URL, APIToken: "tok"})
	root.SetArgs([]string{"secrets", "vault-backfill", "app-secrets", "--vault-path", "secret/app", "--namespace", "app"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)

	require.NoError(t, root.Execute())
	_, has := body.Args["overwrite"]
	assert.False(t, has, "overwriting Vault values is never implicit")
}
