package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MigrationStatus is the live golang-migrate state in Postgres.
type MigrationStatus struct {
	Version uint `json:"version"`
	Dirty   bool `json:"dirty"`
}

// EmbeddedMigrationSummary describes the highest migration shipped in this build.
type EmbeddedMigrationSummary struct {
	Version     uint   `json:"version"`
	Description string `json:"description,omitempty"`
}

// ColumnCheck verifies a GA-critical column exists (migration apply proof).
type ColumnCheck struct {
	Table      string `json:"table"`
	Column     string `json:"column"`
	Migration  uint   `json:"migration,omitempty"`
	Present    bool   `json:"present"`
	RequiredGA bool   `json:"required_ga,omitempty"`
}

// SchemaReport aggregates migration and column verification for operators.
type SchemaReport struct {
	Status          MigrationStatus          `json:"status"`
	EmbeddedLatest  EmbeddedMigrationSummary `json:"embedded_latest"`
	Pending         int                      `json:"pending"`
	Healthy         bool                     `json:"healthy"`
	ColumnChecks    []ColumnCheck            `json:"column_checks,omitempty"`
	SchemaTableSeen bool                     `json:"schema_migrations_seen"`
}

// gaColumnChecks lists columns ops must verify before Stability GA sign-off.
var gaColumnChecks = []ColumnCheck{
	{Table: "services", Column: "rollout_blocked_reason", Migration: 30, RequiredGA: true},
}

// ReadMigrationStatus reads schema_migrations. When the table is missing
// (pre-bootstrap), version 0 and dirty false are returned with seen=false.
func ReadMigrationStatus(ctx context.Context, db *sql.DB) (MigrationStatus, bool, error) {
	if db == nil {
		return MigrationStatus{}, false, fmt.Errorf("database unavailable")
	}
	var version sql.NullInt64
	var dirty sql.NullBool
	err := db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
	if err == sql.ErrNoRows {
		return MigrationStatus{}, true, nil
	}
	if err != nil {
		if isUndefinedTable(err) {
			return MigrationStatus{}, false, nil
		}
		return MigrationStatus{}, false, err
	}
	status := MigrationStatus{}
	if version.Valid && version.Int64 >= 0 {
		status.Version = uint(version.Int64)
	}
	if dirty.Valid {
		status.Dirty = dirty.Bool
	}
	return status, true, nil
}

// LatestEmbeddedMigration returns the highest N from migrations/NNN_*.up.sql.
func LatestEmbeddedMigration() (EmbeddedMigrationSummary, error) {
	type entry struct {
		version uint
		desc    string
	}
	var entries []entry
	err := fs.WalkDir(migrationFS, "migrations", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".up.sql") {
			return nil
		}
		base := filepath.Base(path)
		prefix := strings.SplitN(base, "_", 2)[0]
		n, err := strconv.ParseUint(prefix, 10, 32)
		if err != nil {
			return nil
		}
		desc := strings.TrimSuffix(strings.TrimPrefix(base, prefix+"_"), ".up.sql")
		entries = append(entries, entry{version: uint(n), desc: desc})
		return nil
	})
	if err != nil {
		return EmbeddedMigrationSummary{}, err
	}
	if len(entries) == 0 {
		return EmbeddedMigrationSummary{}, fmt.Errorf("no embedded migrations found")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].version < entries[j].version })
	last := entries[len(entries)-1]
	return EmbeddedMigrationSummary{Version: last.version, Description: last.desc}, nil
}

// BuildSchemaReport composes migration + GA column verification for admin API.
func BuildSchemaReport(ctx context.Context, db *sql.DB) (SchemaReport, error) {
	status, seen, err := ReadMigrationStatus(ctx, db)
	if err != nil {
		return SchemaReport{}, err
	}
	embedded, err := LatestEmbeddedMigration()
	if err != nil {
		return SchemaReport{}, err
	}
	pending := 0
	if embedded.Version > status.Version {
		pending = int(embedded.Version - status.Version)
	}
	checks := make([]ColumnCheck, len(gaColumnChecks))
	for i, want := range gaColumnChecks {
		checks[i] = want
		if db != nil {
			present, err := columnExists(ctx, db, want.Table, want.Column)
			if err != nil {
				return SchemaReport{}, err
			}
			checks[i].Present = present
		}
	}
	healthy := seen && !status.Dirty && pending == 0
	for _, c := range checks {
		if c.RequiredGA && !c.Present {
			healthy = false
		}
	}
	return SchemaReport{
		Status:          status,
		EmbeddedLatest:  embedded,
		Pending:         pending,
		Healthy:         healthy,
		ColumnChecks:    checks,
		SchemaTableSeen: seen,
	}, nil
}

func columnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	return exists, err
}

func isUndefinedTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not exist") && strings.Contains(msg, "schema_migrations")
}
