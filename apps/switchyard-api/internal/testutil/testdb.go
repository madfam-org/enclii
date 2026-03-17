//go:build integration

package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	_ "github.com/lib/pq"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

var (
	testDB   *sql.DB
	testOnce sync.Once
	testErr  error
)

// RequireTestDB connects to the integration test database using TEST_DATABASE_URL.
// If the env var is not set, the test is skipped (not failed).
// The connection is shared across all tests in the same process.
func RequireTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test (set it to run: export TEST_DATABASE_URL=postgres://...)")
	}

	testOnce.Do(func() {
		testDB, testErr = sql.Open("postgres", dbURL)
		if testErr != nil {
			return
		}
		testErr = testDB.Ping()
	})

	if testErr != nil {
		t.Fatalf("failed to connect to test database: %v", testErr)
	}

	return testDB
}

// RequireTestRepos creates a Repositories instance backed by the test database.
func RequireTestRepos(t *testing.T) *db.Repositories {
	t.Helper()
	testDB := RequireTestDB(t)
	return db.NewRepositories(testDB)
}

// CleanTable truncates a table within the test database (for test isolation).
func CleanTable(t *testing.T, d *sql.DB, table string) {
	t.Helper()
	_, err := d.Exec(fmt.Sprintf("DELETE FROM %s", table))
	if err != nil {
		t.Fatalf("failed to clean table %s: %v", table, err)
	}
}
