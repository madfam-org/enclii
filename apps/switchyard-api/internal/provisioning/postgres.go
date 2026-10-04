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
	// openDB opens the admin connection. Tests replace it with sqlmock.
	openDB func(dsn string) (*sql.DB, error)
}

// NewPostgresProvisioner creates a provisioner using a superuser connection string.
func NewPostgresProvisioner(adminURL string, logger logging.Logger) *PostgresProvisioner {
	return &PostgresProvisioner{
		adminURL: adminURL,
		logger:   logger,
		openDB:   func(dsn string) (*sql.DB, error) { return sql.Open("postgres", dsn) },
	}
}

// connect opens and pings the admin connection.
func (p *PostgresProvisioner) connect(ctx context.Context) (*sql.DB, error) {
	open := p.openDB
	if open == nil {
		open = func(dsn string) (*sql.DB, error) { return sql.Open("postgres", dsn) }
	}
	db, err := open(p.adminURL)
	if err != nil {
		return nil, fmt.Errorf("connect to admin postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping admin postgres: %w", err)
	}
	return db, nil
}

// Provision creates a database, role, grants privileges, and optionally enables extensions.
// The role's password is set to spec.RolePassword (caller-chosen).
func (p *PostgresProvisioner) Provision(ctx context.Context, spec *types.PostgresProvisionSpec) error {
	quotedPassword := quoteLiteral(spec.RolePassword)
	return p.provision(ctx, spec, &quotedPassword)
}

// ProvisionGeneratedOwner is Provision for a server-generated owner password.
//
// With a non-empty password the role's password is set from a SCRAM-SHA-256
// verifier, so the statement never carries the plaintext. With an empty
// password the role's existing password is left alone: the database, grants,
// connection limit and extensions are still converged. That is the re-run
// path of `enclii onboard ensure`.
func (p *PostgresProvisioner) ProvisionGeneratedOwner(ctx context.Context, spec *types.PostgresProvisionSpec, password string) error {
	if password == "" {
		return p.provision(ctx, spec, nil)
	}
	verifier, err := newScramVerifier(password)
	if err != nil {
		return err
	}
	quoted := quoteLiteral(verifier)
	return p.provision(ctx, spec, &quoted)
}

// provision is the shared body. passwordLiteral is an already-quoted SQL
// literal (plaintext or verifier); nil keeps the role's current password.
func (p *PostgresProvisioner) provision(ctx context.Context, spec *types.PostgresProvisionSpec, passwordLiteral *string) error {
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
	if spec.ConnectionLimit != 0 {
		if err := ValidateConnectionLimit(spec.ConnectionLimit, "connection_limit"); err != nil {
			return err
		}
	}

	db, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

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
	// The role name is regex-validated. The password is quoted by quoteLiteral.
	if passwordLiteral != nil { // pragma: allowlist secret -- variable name, not a value
		_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER ROLE %s WITH PASSWORD %s", roleName, *passwordLiteral))
		if err != nil {
			// Never wrap the driver error here: it is the only error path
			// that runs a statement carrying a credential.
			return fmt.Errorf("set role password for %s failed", roleName)
		}
	}
	if spec.ConnectionLimit != 0 {
		_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER ROLE %s CONNECTION LIMIT %d", roleName, spec.ConnectionLimit))
		if err != nil {
			return fmt.Errorf("set connection limit on %s: %w", roleName, err)
		}
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

// RoleExists reports whether a role of that name exists on the cluster.
func (p *PostgresProvisioner) RoleExists(ctx context.Context, roleName string) (bool, error) {
	if err := ValidateSQLIdentifier(roleName, "role_name"); err != nil {
		return false, err
	}
	db, err := p.connect(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)", roleName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check role existence: %w", err)
	}
	return exists, nil
}

// AppRole describes a runtime role for EnsureAppRole.
type AppRole struct {
	RoleName        string
	DatabaseName    string
	ConnectionLimit int
}

// AppRoleAction says what EnsureAppRole did.
type AppRoleAction string

const (
	AppRoleCreated AppRoleAction = "created"
	AppRoleKept    AppRoleAction = "kept"
	AppRoleRotated AppRoleAction = "rotated"
)

// appRoleAttributes is the fixed attribute set of a runtime role. NOBYPASSRLS
// and NOSUPERUSER are the point: row-level security applies to this role.
const appRoleAttributes = "LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION"

// EnsureAppRole converges a runtime role and its CONNECT grant.
//
//   - Role absent: CREATE ROLE with the fixed attributes, the connection limit
//     and the password (as a SCRAM verifier). password must be non-empty.
//   - Role present, password empty: the role is left untouched (no ALTER).
//   - Role present, password non-empty: an explicit rotation. The attributes
//     and the connection limit are re-asserted along with the new password.
//
// In every case a role that is a superuser, has BYPASSRLS, or owns the
// database is refused before anything is changed: handing such a role out
// as the runtime connection would defeat row-level security. GRANT CONNECT
// is idempotent and runs on every path; table grants belong to the
// application's own migrations.
func (p *PostgresProvisioner) EnsureAppRole(ctx context.Context, role AppRole, password string) (AppRoleAction, error) {
	if err := ValidateAppRoleName(role.RoleName, role.DatabaseName, ""); err != nil {
		return "", err
	}
	if err := ValidateConnectionLimit(role.ConnectionLimit, "connection_limit"); err != nil {
		return "", err
	}

	db, err := p.connect(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	var dbOwner string
	err = db.QueryRowContext(ctx,
		"SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = $1", role.DatabaseName).Scan(&dbOwner)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("database %s does not exist; provision it first", role.DatabaseName)
	}
	if err != nil {
		return "", fmt.Errorf("look up database owner: %w", err)
	}
	if dbOwner == role.RoleName {
		return "", fmt.Errorf("role %s owns database %s; the runtime role must not be the owner", role.RoleName, role.DatabaseName)
	}

	var super, bypass bool
	err = db.QueryRowContext(ctx,
		"SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = $1", role.RoleName).Scan(&super, &bypass)
	exists := true
	if err == sql.ErrNoRows {
		exists = false
	} else if err != nil {
		return "", fmt.Errorf("look up role %s: %w", role.RoleName, err)
	}
	if exists && (super || bypass) {
		return "", fmt.Errorf("role %s exists with SUPERUSER or BYPASSRLS; refusing to use it as a runtime role (left unchanged)", role.RoleName)
	}

	var action AppRoleAction
	switch {
	case !exists && password == "":
		return "", fmt.Errorf("role %s does not exist and no password was generated", role.RoleName)
	case !exists:
		verifier, verr := newScramVerifier(password)
		if verr != nil {
			return "", verr
		}
		stmt := fmt.Sprintf("CREATE ROLE %s %s CONNECTION LIMIT %d PASSWORD %s",
			role.RoleName, appRoleAttributes, role.ConnectionLimit, quoteLiteral(verifier))
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return "", fmt.Errorf("create role %s failed", role.RoleName)
		}
		action = AppRoleCreated
	case password != "":
		verifier, verr := newScramVerifier(password)
		if verr != nil {
			return "", verr
		}
		stmt := fmt.Sprintf("ALTER ROLE %s WITH %s CONNECTION LIMIT %d PASSWORD %s",
			role.RoleName, appRoleAttributes, role.ConnectionLimit, quoteLiteral(verifier))
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return "", fmt.Errorf("rotate role %s failed", role.RoleName)
		}
		action = AppRoleRotated
	default:
		action = AppRoleKept
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", role.DatabaseName, role.RoleName)); err != nil {
		return "", fmt.Errorf("grant connect on %s to %s: %w", role.DatabaseName, role.RoleName, err)
	}

	p.logger.Info(ctx, "Postgres app role converged",
		logging.String("role", role.RoleName),
		logging.String("database", role.DatabaseName),
		logging.String("action", string(action)))
	return action, nil
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
