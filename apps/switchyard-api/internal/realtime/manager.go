package realtime

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sirupsen/logrus"
)

// This file owns the trigger-install side of the feature: turning a table's
// realtime on/off by running the generated DDL against the addon's own
// database. It is separate from the hub (which owns the LISTEN/fan-out side) so
// each can be tested in isolation.

// Connector opens a short-lived *sql.DB against an addon database given its
// connection URI. Abstracted so tests inject sqlmock and production uses the
// real driver (openConn). The caller is responsible for Close.
type Connector interface {
	Open(ctx context.Context, connInfo string) (*sql.DB, error)
}

// Manager installs and removes realtime triggers on addon databases. It holds
// no long-lived connection: each operation opens, runs, and closes. Safe for
// concurrent use.
type Manager struct {
	connector Connector
	logger    logrus.FieldLogger
}

// NewManager builds a trigger Manager. logger may be nil.
func NewManager(connector Connector, logger logrus.FieldLogger) *Manager {
	return &Manager{connector: connector, logger: logger}
}

// EnableTable installs the realtime trigger on ref in the addon database named
// by connInfo. Idempotent: re-enabling an already-enabled table succeeds. The
// statements run inside one transaction so a partial install can't leave the
// table with the function but no trigger (or vice versa).
func (m *Manager) EnableTable(ctx context.Context, connInfo string, ref TableRef) error {
	ref = ref.Normalize()
	stmts, err := BuildEnableTableSQL(ref)
	if err != nil {
		return err // validation error — caller maps to 400
	}
	return m.runTx(ctx, connInfo, stmts)
}

// DisableTable removes the realtime trigger from ref. Idempotent: disabling a
// table that was never enabled succeeds (DROP TRIGGER IF EXISTS).
func (m *Manager) DisableTable(ctx context.Context, connInfo string, ref TableRef) error {
	ref = ref.Normalize()
	stmt, err := BuildDisableTableSQL(ref)
	if err != nil {
		return err
	}
	return m.runTx(ctx, connInfo, []string{stmt})
}

// ListTables returns the tables in the addon database that currently have an
// enclii realtime trigger installed.
func (m *Manager) ListTables(ctx context.Context, connInfo string) ([]TableRef, error) {
	dbConn, err := m.connector.Open(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("open addon db: %w", err)
	}
	defer func() { _ = dbConn.Close() }()

	rows, err := dbConn.QueryContext(ctx, BuildListTablesSQL())
	if err != nil {
		return nil, fmt.Errorf("list realtime tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []TableRef
	for rows.Next() {
		var ref TableRef
		if err := rows.Scan(&ref.Schema, &ref.Table); err != nil {
			return nil, fmt.Errorf("scan realtime table row: %w", err)
		}
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime tables: %w", err)
	}
	return out, nil
}

// runTx opens a connection, runs stmts in a single transaction, and closes.
func (m *Manager) runTx(ctx context.Context, connInfo string, stmts []string) error {
	dbConn, err := m.connector.Open(ctx, connInfo)
	if err != nil {
		return fmt.Errorf("open addon db: %w", err)
	}
	defer func() { _ = dbConn.Close() }()

	tx, err := dbConn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("exec trigger statement: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit trigger tx: %w", err)
	}
	return nil
}
