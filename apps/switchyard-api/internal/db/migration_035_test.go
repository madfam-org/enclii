package db

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed migrations/035_addon_retention_hold.up.sql
var migration035Up string

//go:embed migrations/035_addon_retention_hold.down.sql
var migration035Down string

// Migration 035 backs the retention hold (2026-08-17 audit #10): a
// deletion_scheduled_at column and the pending_deletion status value the CHECK
// constraint must now admit. The repository writes both; without them the
// retention path fails at the DB layer.
func TestMigration035AddsRetentionStorage(t *testing.T) {
	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS deletion_scheduled_at",
		"'pending_deletion'::character varying",
		"DROP CONSTRAINT IF EXISTS valid_addon_status",
		"ADD CONSTRAINT valid_addon_status",
		"idx_database_addons_deletion_scheduled_at",
	} {
		if !strings.Contains(migration035Up, want) {
			t.Errorf("migration 035 up missing %q", want)
		}
	}
}

// The rebuilt CHECK constraint must keep every pre-existing status value — the
// point is to ADD pending_deletion, not silently narrow the allowed set and
// orphan live rows.
func TestMigration035PreservesExistingStatuses(t *testing.T) {
	for _, status := range []string{"pending", "provisioning", "ready", "failed", "deleting", "deleted"} {
		needle := "'" + status + "'::character varying"
		if !strings.Contains(migration035Up, needle) {
			t.Errorf("migration 035 up dropped existing status %q from the constraint", status)
		}
	}
}

// The up migration must be non-destructive to data: it only adds a column,
// rebuilds a CHECK, and adds an index. No row deletes or table/column drops.
func TestMigration035UpIsNonDestructive(t *testing.T) {
	upper := strings.ToUpper(migration035Up)
	for _, forbidden := range []string{"DROP TABLE", "DROP COLUMN", "DELETE FROM", "TRUNCATE"} {
		if strings.Contains(upper, forbidden) {
			t.Errorf("migration 035 up contains destructive statement %q", forbidden)
		}
	}
}

// The down migration must move any surviving pending_deletion rows to a value
// the restored (narrower) constraint accepts before re-adding it, or the
// ADD CONSTRAINT fails on a dirty rollback.
func TestMigration035DownReconcilesBeforeRestoringConstraint(t *testing.T) {
	if !strings.Contains(migration035Down, "SET status = 'deleting'") {
		t.Error("migration 035 down does not remap pending_deletion rows before restoring the constraint")
	}
	if !strings.Contains(migration035Down, "DROP COLUMN IF EXISTS deletion_scheduled_at") {
		t.Error("migration 035 down does not drop the deletion_scheduled_at column")
	}
}
