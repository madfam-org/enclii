package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

type DatabaseConfig struct {
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	// Connection pool settings
	ConnTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// Performance settings
	StatementTimeout  time.Duration
	LockTimeout       time.Duration
	IdleInTransaction time.Duration
}

type DatabaseManager struct {
	db     *sql.DB
	config *DatabaseConfig
}

func NewDatabaseManager(config *DatabaseConfig) (*DatabaseManager, error) {
	// Build connection string with performance optimizations
	connStr := buildConnectionString(config)

	// Open database connection
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	// Test connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), config.ConnTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	manager := &DatabaseManager{
		db:     db,
		config: config,
	}

	// Log connection pool stats
	go manager.logConnectionStats()

	logrus.WithFields(logrus.Fields{
		"host":           config.Host,
		"database":       config.Database,
		"max_open_conns": config.MaxOpenConns,
		"max_idle_conns": config.MaxIdleConns,
	}).Info("Database connection pool configured")

	return manager, nil
}

func buildConnectionString(config *DatabaseConfig) string {
	params := make(map[string]string)

	// Basic connection parameters
	params["host"] = config.Host
	params["port"] = fmt.Sprintf("%d", config.Port)
	params["dbname"] = config.Database
	params["user"] = config.User
	params["password"] = config.Password
	params["sslmode"] = config.SSLMode

	// Performance parameters
	params["connect_timeout"] = fmt.Sprintf("%.0f", config.ConnTimeout.Seconds())

	if config.StatementTimeout > 0 {
		params["statement_timeout"] = fmt.Sprintf("%dms", config.StatementTimeout.Milliseconds())
	}

	if config.LockTimeout > 0 {
		params["lock_timeout"] = fmt.Sprintf("%dms", config.LockTimeout.Milliseconds())
	}

	if config.IdleInTransaction > 0 {
		params["idle_in_transaction_session_timeout"] = fmt.Sprintf("%dms", config.IdleInTransaction.Milliseconds())
	}

	// Application name for monitoring
	params["application_name"] = "enclii-switchyard"

	// Build connection string
	var connStr string
	for key, value := range params {
		if connStr != "" {
			connStr += " "
		}
		connStr += fmt.Sprintf("%s=%s", key, value)
	}

	return connStr
}

func (dm *DatabaseManager) GetDB() *sql.DB {
	return dm.db
}

func (dm *DatabaseManager) Close() error {
	return dm.db.Close()
}

// Health check with detailed information
func (dm *DatabaseManager) HealthCheck(ctx context.Context) error {
	// Test basic connectivity
	if err := dm.db.PingContext(ctx); err != nil {
		return fmt.Errorf("database ping failed: %w", err)
	}

	// Test with a simple query
	var version string
	query := "SELECT version()"
	if err := dm.db.QueryRowContext(ctx, query).Scan(&version); err != nil {
		return fmt.Errorf("database query failed: %w", err)
	}

	// Check connection pool stats
	stats := dm.db.Stats()
	if stats.OpenConnections == 0 {
		return fmt.Errorf("no open database connections")
	}

	logrus.WithFields(logrus.Fields{
		"open_connections": stats.OpenConnections,
		"in_use":           stats.InUse,
		"idle":             stats.Idle,
		"version":          version[:50], // Truncate for logging
	}).Debug("Database health check passed")

	return nil
}

// Connection pool statistics logging
func (dm *DatabaseManager) logConnectionStats() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := dm.db.Stats()
		logrus.WithFields(logrus.Fields{
			"max_open":            stats.MaxOpenConnections,
			"open":                stats.OpenConnections,
			"in_use":              stats.InUse,
			"idle":                stats.Idle,
			"wait_count":          stats.WaitCount,
			"wait_duration":       stats.WaitDuration.String(),
			"max_idle_closed":     stats.MaxIdleClosed,
			"max_lifetime_closed": stats.MaxLifetimeClosed,
		}).Debug("Database connection pool stats")

		// Alert on potential issues
		if stats.WaitCount > 0 {
			logrus.Warnf("Database connections are waiting: %d waits, duration: %v", stats.WaitCount, stats.WaitDuration)
		}

		if stats.OpenConnections == stats.MaxOpenConnections {
			logrus.Warn("Database connection pool is at maximum capacity")
		}
	}
}

// Transaction helper with proper context handling
func (dm *DatabaseManager) WithTransaction(ctx context.Context, fn func(tx *sql.Tx) error) (txErr error) {
	tx, err := dm.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			// Rollback on panic but don't re-panic - convert to error instead
			rollbackErr := tx.Rollback()
			if rollbackErr != nil {
				logrus.WithFields(logrus.Fields{
					"panic":        p,
					"rollback_err": rollbackErr,
				}).Error("Transaction panic with rollback failure")
			} else {
				logrus.WithField("panic", p).Error("Transaction panic recovered, rolled back successfully")
			}
			// Convert panic to error instead of re-panicking
			txErr = fmt.Errorf("transaction panic recovered: %v", p)
		} else if txErr != nil {
			// Rollback on error
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logrus.WithFields(logrus.Fields{
					"error":        txErr,
					"rollback_err": rollbackErr,
				}).Error("Transaction rollback failed")
			}
		} else {
			// Commit on success
			if commitErr := tx.Commit(); commitErr != nil {
				logrus.WithError(commitErr).Error("Failed to commit transaction")
				txErr = fmt.Errorf("failed to commit transaction: %w", commitErr)
			}
		}
	}()

	txErr = fn(tx)
	return txErr
}

// Prepared statement manager for frequently used queries
type PreparedStatements struct {
	GetProject             *sql.Stmt
	ListProjects           *sql.Stmt
	GetService             *sql.Stmt
	ListServices           *sql.Stmt
	GetRelease             *sql.Stmt
	ListReleases           *sql.Stmt
	GetDeployment          *sql.Stmt
	UpdateDeploymentStatus *sql.Stmt
}

func (dm *DatabaseManager) PrepareStatements(ctx context.Context) (*PreparedStatements, error) {
	stmts := &PreparedStatements{}

	// Prepare frequently used queries
	queries := map[string]**sql.Stmt{
		"SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug = $1":                                                                        &stmts.GetProject,
		"SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC LIMIT $1 OFFSET $2":                                            &stmts.ListProjects,
		"SELECT id, project_id, name, git_repo, build_config, created_at, updated_at FROM services WHERE id = $1":                                                            &stmts.GetService,
		"SELECT id, project_id, name, git_repo, build_config, created_at, updated_at FROM services WHERE project_id = $1 ORDER BY created_at DESC":                           &stmts.ListServices,
		"SELECT id, service_id, version, image_uri, git_sha, status, created_at, updated_at FROM releases WHERE id = $1":                                                     &stmts.GetRelease,
		"SELECT id, service_id, version, image_uri, git_sha, status, created_at, updated_at FROM releases WHERE service_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3": &stmts.ListReleases,
		"SELECT id, release_id, environment_id, replicas, status, health, created_at, updated_at FROM deployments WHERE id = $1":                                             &stmts.GetDeployment,
		"UPDATE deployments SET status = $1, health = $2, updated_at = NOW() WHERE id = $3":                                                                                  &stmts.UpdateDeploymentStatus,
	}

	for query, stmt := range queries {
		prepared, err := dm.db.PrepareContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare statement '%s': %w", query, err)
		}
		*stmt = prepared
	}

	return stmts, nil
}

func (ps *PreparedStatements) Close() error {
	stmts := []*sql.Stmt{
		ps.GetProject,
		ps.ListProjects,
		ps.GetService,
		ps.ListServices,
		ps.GetRelease,
		ps.ListReleases,
		ps.GetDeployment,
		ps.UpdateDeploymentStatus,
	}

	var lastErr error
	for _, stmt := range stmts {
		if stmt != nil {
			if err := stmt.Close(); err != nil {
				lastErr = err
				logrus.Errorf("Failed to close prepared statement: %v", err)
			}
		}
	}

	return lastErr
}

// Database monitoring and metrics
type DatabaseMetrics struct {
	ConnectionsOpen   int
	ConnectionsInUse  int
	ConnectionsIdle   int
	WaitCount         int64
	WaitDuration      time.Duration
	MaxIdleClosed     int64
	MaxLifetimeClosed int64
	QueryDuration     time.Duration
	SlowQueries       int64
}

func (dm *DatabaseManager) GetMetrics() *DatabaseMetrics {
	stats := dm.db.Stats()
	return &DatabaseMetrics{
		ConnectionsOpen:   stats.OpenConnections,
		ConnectionsInUse:  stats.InUse,
		ConnectionsIdle:   stats.Idle,
		WaitCount:         stats.WaitCount,
		WaitDuration:      stats.WaitDuration,
		MaxIdleClosed:     stats.MaxIdleClosed,
		MaxLifetimeClosed: stats.MaxLifetimeClosed,
	}
}

// Query execution with timing
func (dm *DatabaseManager) QueryWithTiming(ctx context.Context, query string, args ...interface{}) (*sql.Rows, time.Duration, error) {
	start := time.Now()
	rows, err := dm.db.QueryContext(ctx, query, args...)
	duration := time.Since(start)

	if duration > 1*time.Second {
		logrus.WithFields(logrus.Fields{
			"query":    query,
			"duration": duration.String(),
			"args":     args,
		}).Warn("Slow query detected")
	}

	return rows, duration, err
}

// Default configuration for production
func DefaultDatabaseConfig() *DatabaseConfig {
	return &DatabaseConfig{
		Host:              "localhost",
		Port:              5432,
		SSLMode:           "prefer",
		MaxOpenConns:      25, // Reasonable default for most applications
		MaxIdleConns:      5,  // Keep some connections idle
		ConnMaxLifetime:   30 * time.Minute,
		ConnMaxIdleTime:   5 * time.Minute,
		ConnTimeout:       5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		StatementTimeout:  30 * time.Second,
		LockTimeout:       5 * time.Second,
		IdleInTransaction: 10 * time.Minute,
	}
}

// Error handling for database operations
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	// Check for PostgreSQL specific connection errors
	if pqErr, ok := err.(*pq.Error); ok {
		switch pqErr.Code {
		case "08000", "08003", "08006", "08001", "08004":
			return true
		}
	}

	return false
}

func IsDeadlockError(err error) bool {
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == "40P01"
	}
	return false
}

func IsUniqueConstraintError(err error) bool {
	if pqErr, ok := err.(*pq.Error); ok {
		return pqErr.Code == "23505"
	}
	return false
}
