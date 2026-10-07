package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate runs all pending database migrations.
//
// A dirty schema requires operator inspection. Startup never forces a version,
// runs down migrations, drops data, or retries a partially applied migration.
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

	return runPendingMigrations(m)
}

// Keep the startup runner incapable of invoking Force, Steps, or Drop.
type pendingMigrationRunner interface {
	Up() error
	Version() (uint, bool, error)
}

func runPendingMigrations(m pendingMigrationRunner) error {
	err := m.Up()
	if err == nil || errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	version, dirty, versionErr := m.Version()
	if versionErr != nil {
		return fmt.Errorf("migration failed and could not read version; operator inspection required: up=%w, version=%w", err, versionErr)
	}
	if dirty {
		return fmt.Errorf("migration v%d is dirty; startup stopped without automatic rollback or version reset. An operator must inspect the partial schema and migration, preserve existing data and operational holds, and perform a reviewed repair before restarting: %w", version, err)
	}
	return fmt.Errorf("migration failed: %w", err)
}
