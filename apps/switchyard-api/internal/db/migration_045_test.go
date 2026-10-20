package db

import (
	"os"
	"strings"
	"testing"
)

func TestMigration045RollbackLocksBeforeCheckingRetainedHistory(t *testing.T) {
	b, err := os.ReadFile("migrations/045_janua_staging_preregistration.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := stripSQLLineComments(string(b))
	lock := strings.Index(body, "LOCK TABLE janua_staging_preregistration_intents IN ACCESS EXCLUSIVE MODE")
	check := strings.Index(body, "IF EXISTS (SELECT 1 FROM janua_staging_preregistration_intents)")
	drop := strings.Index(body, "DROP TABLE janua_staging_preregistration_intents")
	end := strings.Index(body, "END; $$;")
	if !strings.HasPrefix(strings.TrimSpace(body), "DO $$ BEGIN") || lock < 0 || check <= lock || drop <= check || end <= drop || strings.TrimSpace(body[end+len("END; $$;"):]) != "" {
		t.Fatal("rollback must lock, inspect and drop in one atomic statement")
	}
}
