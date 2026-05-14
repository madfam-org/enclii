package db

import (
	"embed"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Migration 027 creates the normalized Timetable tables expected by the
// repositories behind `enclii jobs list` and `enclii jobs run-once`.
// Migration 020 only added services.jobs, leaving production without the
// tables queried by CronJobRepository, CronJobRunRepository, and
// OneOffJobRepository.

//go:embed migrations/027_timetable_tables.up.sql
//go:embed migrations/027_timetable_tables.down.sql
var migration027FS embed.FS

func readMigration027(t *testing.T, name string) string {
	t.Helper()
	data, err := migration027FS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func TestMigration027_CreatesTimetableTables(t *testing.T) {
	up := readMigration027(t, "027_timetable_tables.up.sql")

	for _, table := range []string{
		"public.cron_jobs",
		"public.cron_job_runs",
		"public.one_off_jobs",
	} {
		assert.Contains(t, up, "CREATE TABLE IF NOT EXISTS "+table,
			"%s must be created idempotently", table)
	}
}

func TestMigration027_CoversRepositoryColumns(t *testing.T) {
	up := readMigration027(t, "027_timetable_tables.up.sql")

	expected := map[string][]string{
		"cron_jobs": {
			"id", "project_id", "service_id", "name", "schedule", "command", "image",
			"timeout", "retries", "suspended", "concurrency",
			"created_at", "updated_at", "last_run_at", "next_run_at",
		},
		"cron_job_runs": {
			"id", "cron_job_id", "status", "exit_code", "started_at", "ended_at", "log_output",
		},
		"one_off_jobs": {
			"id", "project_id", "service_id", "name", "command", "image",
			"timeout", "run_at", "status", "exit_code",
			"created_at", "started_at", "ended_at",
		},
	}

	for table, columns := range expected {
		for _, column := range columns {
			assert.Contains(t, up, column,
				"%s migration must include repository-scanned column %s", table, column)
		}
	}
}

func TestMigration027_DownDropsDependentsFirst(t *testing.T) {
	down := readMigration027(t, "027_timetable_tables.down.sql")

	runIdx := strings.Index(down, "DROP TABLE IF EXISTS public.cron_job_runs")
	oneOffIdx := strings.Index(down, "DROP TABLE IF EXISTS public.one_off_jobs")
	cronIdx := strings.Index(down, "DROP TABLE IF EXISTS public.cron_jobs")

	assert.NotEqual(t, -1, runIdx, "down migration must drop cron_job_runs")
	assert.NotEqual(t, -1, oneOffIdx, "down migration must drop one_off_jobs")
	assert.NotEqual(t, -1, cronIdx, "down migration must drop cron_jobs")
	assert.Less(t, runIdx, cronIdx, "cron_job_runs must drop before cron_jobs")
	assert.Less(t, oneOffIdx, cronIdx, "one_off_jobs should drop before cron_jobs")
}

func TestMigration027_NoBareTransactionMarkers(t *testing.T) {
	for _, name := range []string{
		"027_timetable_tables.up.sql",
		"027_timetable_tables.down.sql",
	} {
		body := readMigration027(t, name)
		bareBegin := regexp.MustCompile(`(?m)^\s*BEGIN\s*;`)
		bareCommit := regexp.MustCompile(`(?m)^\s*COMMIT\s*;`)
		assert.False(t, bareBegin.MatchString(body), "%s must not contain bare BEGIN", name)
		assert.False(t, bareCommit.MatchString(body), "%s must not contain bare COMMIT", name)
	}
}
