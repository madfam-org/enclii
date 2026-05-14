package db

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed migrations/028_clear_stale_deployment_errors.up.sql
var migration028Up string

//go:embed migrations/028_clear_stale_deployment_errors.down.sql
var migration028Down string

func TestMigration028ClearsOnlyHealthyRunningDeploymentErrors(t *testing.T) {
	required := []string{
		"UPDATE public.deployments",
		"SET error_message = NULL",
		"WHERE status = 'running'",
		"AND health = 'healthy'",
		"AND error_message IS NOT NULL",
	}

	for _, want := range required {
		if !strings.Contains(migration028Up, want) {
			t.Fatalf("migration 028 up missing %q", want)
		}
	}
}

func TestMigration028DownIsIntentionalNoop(t *testing.T) {
	if !strings.Contains(migration028Down, "No-op rollback") {
		t.Fatalf("migration 028 down must document intentional no-op rollback")
	}
}
