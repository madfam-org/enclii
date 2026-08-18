package realtime

import (
	"context"
	"database/sql"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

// mockConnector returns a pre-built *sql.DB (backed by sqlmock) for any
// connInfo, so the Manager's open/exec/close flow can be tested without a real
// Postgres.
type mockConnector struct {
	db *sql.DB
}

func (m mockConnector) Open(_ context.Context, _ string) (*sql.DB, error) {
	return m.db, nil
}

func TestManager_EnableTable_RunsTriggerTxInOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mgr := NewManager(mockConnector{db: db}, nil)

	// Enable runs three statements (function, drop, create) in one tx, in order.
	mock.ExpectBegin()
	mock.ExpectExec("CREATE OR REPLACE FUNCTION").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP TRIGGER IF EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TRIGGER").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if err := mgr.EnableTable(context.Background(), "dsn", TableRef{Schema: "public", Table: "orders"}); err != nil {
		t.Fatalf("EnableTable: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestManager_EnableTable_RollsBackOnError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mgr := NewManager(mockConnector{db: db}, nil)

	mock.ExpectBegin()
	mock.ExpectExec("CREATE OR REPLACE FUNCTION").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP TRIGGER IF EXISTS").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	if err := mgr.EnableTable(context.Background(), "dsn", TableRef{Table: "orders"}); err == nil {
		t.Fatal("expected EnableTable to error when a statement fails")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestManager_EnableTable_RejectsBadIdentifierBeforeDB(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mgr := NewManager(mockConnector{db: db}, nil)
	// No DB expectations set: validation must fail before any connection use.
	if err := mgr.EnableTable(context.Background(), "dsn", TableRef{Table: "bad; DROP"}); err == nil {
		t.Fatal("expected validation error for an injection table name")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("no DB calls should have happened: %v", err)
	}
}

func TestManager_DisableTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mgr := NewManager(mockConnector{db: db}, nil)

	mock.ExpectBegin()
	mock.ExpectExec("DROP TRIGGER IF EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if err := mgr.DisableTable(context.Background(), "dsn", TableRef{Table: "orders"}); err != nil {
		t.Fatalf("DisableTable: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestManager_ListTables(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	mgr := NewManager(mockConnector{db: db}, nil)

	rows := sqlmock.NewRows([]string{"schema", "table"}).
		AddRow("public", "orders").
		AddRow("billing", "invoices")
	mock.ExpectQuery("FROM pg_trigger").WillReturnRows(rows)

	got, err := mgr.ListTables(context.Background(), "dsn")
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tables, got %d: %+v", len(got), got)
	}
	if got[0] != (TableRef{Schema: "public", Table: "orders"}) {
		t.Errorf("unexpected first row: %+v", got[0])
	}
	if got[1] != (TableRef{Schema: "billing", Table: "invoices"}) {
		t.Errorf("unexpected second row: %+v", got[1])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
