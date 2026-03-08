package metering

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// newTestDB creates a new sqlmock database connection for metering tests.
// It returns both the *sql.DB and the sqlmock.Sqlmock for setting expectations.
// The connection is automatically closed when the test completes.
func newTestDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db, mock
}

// assertExpectations verifies that all sqlmock expectations were met.
func assertExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}
