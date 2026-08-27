package db

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed migrations/037_one_off_jobs_failure_reason.up.sql
var migration037Up string

//go:embed migrations/037_one_off_jobs_failure_reason.down.sql
var migration037Down string

func TestMigration037AddsFailureReasonColumn(t *testing.T) {
	required := []string{
		"ALTER TABLE one_off_jobs",
		"ADD COLUMN IF NOT EXISTS failure_reason TEXT NOT NULL DEFAULT ''",
	}

	for _, want := range required {
		if !strings.Contains(migration037Up, want) {
			t.Fatalf("migration 037 up missing %q", want)
		}
	}
}

func TestMigration037IsAdditiveOnly(t *testing.T) {
	// Existing one-off job history must survive untouched: the column is
	// additive with a default, so already-recorded runs keep their status,
	// exit code and timestamps.
	forbidden := []string{"DROP COLUMN", "DROP TABLE", "DELETE FROM", "TRUNCATE", "UPDATE ONE_OFF_JOBS"}

	upper := strings.ToUpper(migration037Up)
	for _, bad := range forbidden {
		if strings.Contains(upper, bad) {
			t.Fatalf("migration 037 up contains destructive statement %q", bad)
		}
	}
}

func TestMigration037DownIsIdempotent(t *testing.T) {
	if !strings.Contains(migration037Down, "DROP COLUMN IF EXISTS failure_reason") {
		t.Fatalf("migration 037 down missing idempotent column drop")
	}
}
