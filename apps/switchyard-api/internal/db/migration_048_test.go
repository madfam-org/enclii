package db

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed migrations/048_one_off_jobs_network_identity.up.sql
var migration048Up string

//go:embed migrations/048_one_off_jobs_network_identity.down.sql
var migration048Down string

func TestMigration048AddsNetworkIdentityColumn(t *testing.T) {
	required := []string{
		"ADD COLUMN IF NOT EXISTS network_identity TEXT NOT NULL DEFAULT ''",
		"CHECK (network_identity IN ('', 'service'))",
	}

	for _, want := range required {
		if !strings.Contains(migration048Up, want) {
			t.Fatalf("migration 048 up missing %q", want)
		}
	}
}

func TestMigration048IsAdditiveOnly(t *testing.T) {
	// Existing rows keep their history and default to '' (no identity), which
	// is exactly the behaviour every job had before this column existed.
	forbidden := []string{"DROP COLUMN", "DROP TABLE", "DELETE FROM", "TRUNCATE", "UPDATE ONE_OFF_JOBS"}

	upper := strings.ToUpper(migration048Up)
	for _, bad := range forbidden {
		if strings.Contains(upper, bad) {
			t.Fatalf("migration 048 up contains destructive statement %q", bad)
		}
	}
}

func TestMigration048DownIsIdempotent(t *testing.T) {
	for _, want := range []string{
		"DROP CONSTRAINT IF EXISTS one_off_jobs_network_identity_check",
		"DROP COLUMN IF EXISTS network_identity",
	} {
		if !strings.Contains(migration048Down, want) {
			t.Fatalf("migration 048 down missing %q", want)
		}
	}
}
