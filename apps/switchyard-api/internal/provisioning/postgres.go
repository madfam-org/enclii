package provisioning

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// PostgresProvisioner creates databases and roles in a shared Postgres instance.
type PostgresProvisioner struct {
	adminURL string
	logger   logging.Logger
}

// NewPostgresProvisioner creates a provisioner using a superuser connection string.
func NewPostgresProvisioner(adminURL string, logger logging.Logger) *PostgresProvisioner {
	return &PostgresProvisioner{
		adminURL: adminURL,
		logger:   logger,
	}
}

// Provision creates a database, role, grants privileges, and optionally enables extensions.
func (p *PostgresProvisioner) Provision(ctx context.Context, spec *types.PostgresProvisionSpec) error {
	roleName := spec.RoleName
	if roleName == "" {
		roleName = spec.DatabaseName
	}

	// Validate identifiers
	if err := ValidateSQLIdentifier(spec.DatabaseName, "database_name"); err != nil {
		return err
	}
	if err := ValidateSQLIdentifier(roleName, "role_name"); err != nil {
		return err
	}
	for _, ext := range spec.Extensions {
		if err := ValidateExtensionName(ext); err != nil {
			return err
		}
	}

	db, err := sql.Open("postgres", p.adminURL)
	if err != nil {
		return fmt.Errorf("connect to admin postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping admin postgres: %w", err)
	}

	// Create role (idempotent)
	var roleExists bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)", roleName).Scan(&roleExists)
	if err != nil {
		return fmt.Errorf("check role existence: %w", err)
	}
	if !roleExists {
		// Password is passed as a parameter to SET PASSWORD, but CREATE ROLE ... PASSWORD
		// requires string interpolation. Use ALTER ROLE to set password safely.
		// Role name is validated via regex — safe for identifier use.
		_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s WITH LOGIN", roleName))
		if err != nil {
			return fmt.Errorf("create role %s: %w", roleName, err)
		}
		p.logger.Info(ctx, "Created Postgres role", logging.String("role", roleName))
	} else {
		p.logger.Info(ctx, "Postgres role already exists", logging.String("role", roleName))
	}

	// Set password via ALTER ROLE (cannot parameterize passwords in DDL)
	// The role name is regex-validated. The password is quoted by lib/pq's QuoteLiteral.
	quotedPassword := quoteLiteral(spec.RolePassword)
	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER ROLE %s WITH PASSWORD %s", roleName, quotedPassword))
	if err != nil {
		return fmt.Errorf("set role password: %w", err)
	}

	// Create database (idempotent)
	var dbExists bool
	err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", spec.DatabaseName).Scan(&dbExists)
	if err != nil {
		return fmt.Errorf("check database existence: %w", err)
	}
	if !dbExists {
		// Database name is validated via regex — safe for identifier use.
		_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s OWNER %s", spec.DatabaseName, roleName))
		if err != nil {
			return fmt.Errorf("create database %s: %w", spec.DatabaseName, err)
		}
		p.logger.Info(ctx, "Created Postgres database", logging.String("database", spec.DatabaseName))
	} else {
		p.logger.Info(ctx, "Postgres database already exists", logging.String("database", spec.DatabaseName))
	}

	// Grant privileges
	_, err = db.ExecContext(ctx, fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", spec.DatabaseName, roleName))
	if err != nil {
		return fmt.Errorf("grant privileges: %w", err)
	}

	// Enable extensions (must connect to the target database)
	if len(spec.Extensions) > 0 {
		if err := p.enableExtensions(ctx, spec.DatabaseName, spec.Extensions); err != nil {
			return err
		}
	}

	p.logger.Info(ctx, "Postgres provisioning complete",
		logging.String("database", spec.DatabaseName),
		logging.String("role", roleName))

	return nil
}

// enableExtensions connects to the target database and creates extensions.
func (p *PostgresProvisioner) enableExtensions(ctx context.Context, dbName string, extensions []string) error {
	// Build a connection string for the target database by replacing the dbname.
	// Parse the admin URL and swap the database.
	targetURL := replaceDBName(p.adminURL, dbName)

	db, err := sql.Open("postgres", targetURL)
	if err != nil {
		return fmt.Errorf("connect to target database %s: %w", dbName, err)
	}
	defer func() { _ = db.Close() }()

	for _, ext := range extensions {
		// Extension name is validated via regex — safe for identifier use.
		_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS \"%s\"", ext))
		if err != nil {
			return fmt.Errorf("create extension %s: %w", ext, err)
		}
		p.logger.Info(ctx, "Enabled Postgres extension",
			logging.String("database", dbName),
			logging.String("extension", ext))
	}
	return nil
}

// quoteLiteral quotes a string value for use in SQL (prevents injection in password values).
func quoteLiteral(s string) string {
	// Escape single quotes by doubling them, wrap in single quotes.
	escaped := ""
	for _, c := range s {
		if c == '\'' {
			escaped += "''"
		} else {
			escaped += string(c)
		}
	}
	return "'" + escaped + "'"
}

// replaceDBName replaces the database name in a PostgreSQL connection string.
func replaceDBName(connStr, newDB string) string {
	// Handle both URL-style and key=value style connection strings.
	// For URL-style: postgresql://user:pass@host:port/dbname?params
	// For key=value: host=... dbname=...

	// Try URL-style first
	if len(connStr) > 13 && (connStr[:13] == "postgresql://" || connStr[:11] == "postgres://") {
		// Find the path component (after host:port/)
		// Split on ? to preserve query params
		base := connStr
		params := ""
		if idx := indexOf(connStr, "?"); idx >= 0 {
			base = connStr[:idx]
			params = connStr[idx:]
		}
		// Find the last / which separates host from dbname
		lastSlash := lastIndexOf(base, "/")
		if lastSlash >= 0 {
			return base[:lastSlash+1] + newDB + params
		}
	}

	// Key=value style: replace dbname= value
	// This is a simple approach — find dbname= and replace the value
	if idx := indexOf(connStr, "dbname="); idx >= 0 {
		before := connStr[:idx+7] // includes "dbname="
		rest := connStr[idx+7:]
		// Find end of value (next space or end of string)
		endIdx := indexOf(rest, " ")
		if endIdx < 0 {
			return before + newDB
		}
		return before + newDB + rest[endIdx:]
	}

	// Fallback: append dbname
	return connStr + " dbname=" + newDB
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, substr string) int {
	last := -1
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			last = i
		}
	}
	return last
}
