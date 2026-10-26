package db

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration046PreservesEncryptedPlansAndAppendOnlyDispatchEvidence(t *testing.T) {
	up, err := migrationFS.ReadFile("migrations/046_argocd_protected_backup_journal.up.sql")
	require.NoError(t, err)
	down, err := migrationFS.ReadFile("migrations/046_argocd_protected_backup_journal.down.sql")
	require.NoError(t, err)
	for _, invariant := range []string{"CREATE TABLE argocd_backup_operations", "CREATE TABLE argocd_backup_events", "33554460", "encode(sha256(ciphertext)", "UNIQUE (operation_id, kind)", "argocd_backup_one_provider_outcome", "BEFORE UPDATE OR DELETE", "BEFORE TRUNCATE", "DISPATCH_RESERVED", "DISPATCH_CLAIMED", "READBACK_AUTHENTICATED", "ciphertext_sha256 IS NOT NULL", "IS DISTINCT FROM op.ciphertext_sha256", "enclii:argocd:protected-backup:v1"} {
		require.Contains(t, string(up), invariant)
	}
	require.Contains(t, string(down), "RAISE EXCEPTION")
	for _, forbidden := range []string{"DROP TABLE", "DELETE FROM", "TRUNCATE TABLE", "DROP FUNCTION"} {
		require.NotContains(t, strings.ToUpper(string(down)), forbidden)
	}
	require.NotContains(t, string(up), "encryption_key")
	require.NotContains(t, string(up), "plaintext")
}
