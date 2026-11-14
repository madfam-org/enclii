package db

import (
	"os"
	"strings"
	"testing"
)

// A denied rollback must leave the caller connection usable, with retained
// operation history intact; do not use a multi-statement BEGIN that can strand
// the pool connection in an aborted transaction on the expected refusal.
func TestMigration050RollbackIsOneAtomicHistoryGuard(t *testing.T) {
	b, err := os.ReadFile("migrations/050_janua_staging_database_operations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := stripSQLLineComments(string(b))
	lock := strings.Index(body, "LOCK TABLE janua_staging_database_operations,janua_staging_database_objects IN ACCESS EXCLUSIVE MODE")
	check := strings.Index(body, "IF EXISTS(SELECT 1 FROM janua_staging_database_operations)")
	drop := strings.Index(body, "DROP TABLE janua_staging_database_objects")
	end := strings.Index(body, "END; $$;")
	if !strings.HasPrefix(strings.TrimSpace(body), "DO $$ BEGIN") || lock < 0 || check <= lock || drop <= check || end <= drop || strings.TrimSpace(body[end+len("END; $$;"):]) != "" {
		t.Fatal("rollback must lock, inspect and drop inside one atomic statement")
	}
}
