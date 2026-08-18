package realtime

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/lib/pq" // register the "postgres" driver for addon connections
)

// pqConnector is the production Connector. It opens a short-lived *sql.DB
// against an addon database over the lib/pq "postgres" driver. Connections are
// used for one trigger operation and closed by the Manager, so the pool is kept
// tiny and reaped quickly.
type pqConnector struct{}

// NewPQConnector builds the production Connector.
func NewPQConnector() Connector { return &pqConnector{} }

// Open dials the addon database. It verifies reachability with a bounded ping so
// a mis-provisioned addon surfaces as an error the API can map to 5xx rather
// than hanging the request.
func (pqConnector) Open(ctx context.Context, connInfo string) (*sql.DB, error) {
	dbConn, err := sql.Open("postgres", connInfo)
	if err != nil {
		return nil, err
	}
	// Keep the pool minimal: these connections are per-operation and short-lived.
	dbConn.SetMaxOpenConns(2)
	dbConn.SetMaxIdleConns(1)
	dbConn.SetConnMaxLifetime(2 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := dbConn.PingContext(pingCtx); err != nil {
		_ = dbConn.Close()
		return nil, err
	}
	return dbConn, nil
}
