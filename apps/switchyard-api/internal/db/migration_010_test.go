package db

import (
	"embed"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Migration 011 adds Heroku-style semantic version numbers to deployments
// (P2.6). These tests exercise the invariants that matter at the migration
// layer: idempotent backfill and a symmetric down migration.
//
// Note: filename historically "migration_010_test.go" from a pre-rename draft;
// the actual on-disk migration number is 011. Kept under the legacy test file
// name to preserve git blame continuity.

//go:embed migrations/011_deployment_version_numbers.up.sql
//go:embed migrations/011_deployment_version_numbers.down.sql
var migration010FS embed.FS

func readMigration010(t *testing.T, name string) string {
	t.Helper()
	data, err := migration010FS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// TestMigration010_BackfillIsIdempotent verifies the up migration only
// assigns version_number to rows where it's NULL. Running the migration
// twice must be a no-op for the second run — otherwise a redeploy that
// reruns migrations would renumber historical deployments, breaking the
// P2.6 contract that version numbers are immutable post-allocation.
func TestMigration010_BackfillIsIdempotent(t *testing.T) {
	up := readMigration010(t, "011_deployment_version_numbers.up.sql")

	// Both UPDATE passes (service_id backfill and version_number backfill)
	// must gate on IS NULL so re-running is a no-op.
	assert.Contains(t, up, "d.service_id IS NULL",
		"service_id backfill must skip rows that already have service_id set")
	assert.Contains(t, up, "d.version_number IS NULL",
		"version_number backfill must skip rows that already have version_number set")

	// The UNIQUE index should be partial (excluding the NULL transitional
	// state) so the backfill doesn't fight the constraint.
	assert.Contains(t, up, "WHERE service_id IS NOT NULL",
		"unique index must be partial to allow the backfill to run")
	assert.Contains(t, up, "AND version_number IS NOT NULL",
		"unique index must exclude rows where version_number hasn't been allocated yet")

	// Schema changes must be idempotent via IF NOT EXISTS so a mid-deploy
	// rerun (triggered by the dirty-recovery path in migrations.go) can
	// replay cleanly.
	assert.Contains(t, up, "ADD COLUMN IF NOT EXISTS version_number")
	assert.Contains(t, up, "ADD COLUMN IF NOT EXISTS service_id")
	assert.Contains(t, up, "CREATE UNIQUE INDEX IF NOT EXISTS")
	assert.Contains(t, up, "CREATE INDEX IF NOT EXISTS")
}

// TestMigration010_DownIsIdempotent confirms the down migration uses
// IF EXISTS so the automatic dirty-recovery in migrations.go can safely
// invoke it even when the up migration partially applied.
func TestMigration010_DownIsIdempotent(t *testing.T) {
	down := readMigration010(t, "011_deployment_version_numbers.down.sql")
	assert.Contains(t, down, "DROP INDEX IF EXISTS idx_deployments_service_id")
	assert.Contains(t, down, "DROP INDEX IF EXISTS idx_deployments_service_version")
	assert.Contains(t, down, "DROP COLUMN IF EXISTS version_number")
	assert.Contains(t, down, "DROP COLUMN IF EXISTS service_id")
}

// TestMigration010_BackfillUsesDeterministicOrder documents that the
// backfill assigns v-numbers by (created_at ASC, id ASC). The id tie-break
// matters when two deploys share a timestamp (possible at microsecond
// granularity on fast test fixtures or during import) — without it the
// assignment is non-deterministic and a re-run on a fresh database could
// produce different v-numbers.
func TestMigration010_BackfillUsesDeterministicOrder(t *testing.T) {
	up := readMigration010(t, "011_deployment_version_numbers.up.sql")
	// The ROW_NUMBER() window should partition by service and order by
	// (created_at, id) so the result is stable.
	assert.Contains(t, strings.ToLower(up), "partition by d.service_id")
	assert.Contains(t, strings.ToLower(up), "order by d.created_at asc, d.id asc")
}
