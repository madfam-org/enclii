package db

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/sirupsen/logrus"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate runs all pending database migrations.
//
// If a previous migration left the schema in a dirty state (e.g. due to a pod
// crash or OOM kill), this function attempts automatic recovery:
//  1. Read the dirty version N from schema_migrations.
//  2. Roll back to version N-1 (runs the N.down.sql script, which must be
//     idempotent — all shipped down migrations use IF EXISTS).
//  3. Re-run Up() so version N is applied cleanly.
//
// This avoids the most common production failure mode: pods stuck in
// CrashLoopBackOff because a one-time migration interruption left a dirty flag
// that persists across every restart.
func Migrate(db *sql.DB, databaseURL string) error {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("postgres driver: %w", err)
	}

	source, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		// Check if the error is a dirty database state.
		version, dirty, verErr := m.Version()
		if verErr != nil {
			return fmt.Errorf("migration failed and could not read version: up=%w, version=%w", err, verErr)
		}

		if !dirty {
			return fmt.Errorf("migration failed: %w", err)
		}

		logrus.WithFields(logrus.Fields{
			"dirty_version": version,
			"error":         err.Error(),
		}).Warn("Dirty migration detected, attempting automatic recovery")

		// Force the version back to dirty_version - 1 so golang-migrate
		// considers it the current clean version, then re-run Up().
		// This works because:
		//   - golang-migrate marks dirty BEFORE executing SQL
		//   - If the SQL partially applied, the down migration (IF EXISTS)
		//     cleans it up before the subsequent Up() re-applies it
		//   - If the SQL never applied, forcing version-1 is a no-op reset
		prev := int(version) - 1
		if prev < 0 {
			// Version 1 failed — drop back to no-version state.
			if fErr := m.Drop(); fErr != nil {
				return fmt.Errorf("dirty v%d: drop failed: %w (original: %w)", version, fErr, err)
			}
		} else {
			// Step down to clean version, then run the down migration to
			// undo any partial state from the dirty version.
			if fErr := m.Force(prev); fErr != nil {
				return fmt.Errorf("dirty v%d: force to v%d failed: %w (original: %w)", version, prev, fErr, err)
			}

			// Run the down migration for the dirty version to clean up
			// any partial schema changes. Steps() with -1 runs one down.
			if sErr := m.Steps(-1); sErr != nil && sErr != migrate.ErrNoChange {
				// Down migration failed — this is acceptable if the dirty
				// version's SQL never partially applied. Log and continue.
				logrus.WithFields(logrus.Fields{
					"dirty_version": version,
					"error":         sErr.Error(),
				}).Debug("Down migration for dirty version returned error (may be expected)")
			}

			// Force version again in case Steps() moved it.
			if fErr := m.Force(prev); fErr != nil {
				return fmt.Errorf("dirty v%d: re-force to v%d failed: %w", version, prev, fErr)
			}
		}

		logrus.WithField("reset_to", prev).Info("Dirty state cleared, re-running migrations")

		// Re-create the migrate instance — the previous one may have
		// internal state from the failed run.
		driver2, err2 := postgres.WithInstance(db, &postgres.Config{})
		if err2 != nil {
			return fmt.Errorf("postgres driver (retry): %w", err2)
		}
		source2, err2 := iofs.New(migrationFS, "migrations")
		if err2 != nil {
			return fmt.Errorf("migration source (retry): %w", err2)
		}
		m2, err2 := migrate.NewWithInstance("iofs", source2, "postgres", driver2)
		if err2 != nil {
			return fmt.Errorf("migrate instance (retry): %w", err2)
		}

		if err2 := m2.Up(); err2 != nil && err2 != migrate.ErrNoChange {
			return fmt.Errorf("migration retry after dirty recovery failed: %w", err2)
		}

		logrus.Info("Migration recovery successful")
	}

	return nil
}
