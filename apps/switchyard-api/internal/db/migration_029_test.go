package db

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed migrations/029_cleanup_stale_building_releases.up.sql
var migration029Up string

//go:embed migrations/029_cleanup_stale_building_releases.down.sql
var migration029Down string

func TestMigration029FailsStaleBuildingReleases(t *testing.T) {
	required := []string{
		"UPDATE public.releases",
		"SET status = 'failed'",
		"Build timed out (no callback received within 30 minutes)",
		"WHERE status = 'building'",
		"AND created_at < NOW() - INTERVAL '30 minutes'",
	}

	for _, want := range required {
		if !strings.Contains(migration029Up, want) {
			t.Fatalf("migration 029 up missing %q", want)
		}
	}
}

func TestMigration029ClearsReadyReleaseErrors(t *testing.T) {
	required := []string{
		"SET error_message = NULL",
		"WHERE status = 'ready'",
		"AND error_message IS NOT NULL",
	}

	for _, want := range required {
		if !strings.Contains(migration029Up, want) {
			t.Fatalf("migration 029 up missing %q", want)
		}
	}
}

func TestMigration029DownIsIntentionalNoop(t *testing.T) {
	if !strings.Contains(migration029Down, "No-op rollback") {
		t.Fatalf("migration 029 down must document intentional no-op rollback")
	}
}
