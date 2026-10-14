package db

import (
	"embed"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Migration 044 stops retired bindings (status 'deleted') from occupying their
// addon/service pair forever. Live bindings stay unique; history is kept.

//go:embed migrations/044_addon_binding_retired_history.up.sql
//go:embed migrations/044_addon_binding_retired_history.down.sql
var migration044FS embed.FS

func readMigration044(t *testing.T, name string) string {
	t.Helper()
	data, err := migration044FS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return stripSQLLineComments(string(data))
}

func TestMigration044_LiveBindingsStayUnique(t *testing.T) {
	up := readMigration044(t, "044_addon_binding_retired_history.up.sql")
	assert.Contains(t, up, "DROP CONSTRAINT IF EXISTS unique_binding_per_service")
	assert.Regexp(t, regexp.MustCompile(`CREATE UNIQUE INDEX IF NOT EXISTS unique_live_binding_per_service\s+ON database_addon_bindings \(addon_id, service_id\)\s+WHERE status <> 'deleted'`), up,
		"only retired rows may be exempt; active and suspended bindings must still occupy the pair")
	assert.NotRegexp(t, regexp.MustCompile(`(?i)\bDELETE\b|\bUPDATE\b`), up, "the up migration must not rewrite binding history")
}

func TestMigration044_DownRefusesToDropHistory(t *testing.T) {
	down := readMigration044(t, "044_addon_binding_retired_history.down.sql")
	assert.Contains(t, down, "HAVING count(*) > 1")
	assert.Contains(t, down, "RAISE EXCEPTION")
	assert.NotRegexp(t, regexp.MustCompile(`(?i)\bDELETE\b`), down, "down must refuse rather than delete retired history")
	assert.Contains(t, down, "ADD CONSTRAINT unique_binding_per_service UNIQUE (addon_id, service_id)")
}
