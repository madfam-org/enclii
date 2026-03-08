package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// RouteRepository handles database operations for routes
type RouteRepository struct {
	db DBTX
}

func NewRouteRepository(db DBTX) *RouteRepository {
	return &RouteRepository{db: db}
}

// NewRouteRepositoryWithTx creates a repository using a transaction
func NewRouteRepositoryWithTx(tx DBTX) *RouteRepository {
	return &RouteRepository{db: tx}
}

// Create adds a new route
func (r *RouteRepository) Create(ctx context.Context, route *types.Route) error {
	query := `
		INSERT INTO routes (
			id, service_id, environment_id, path, path_type, port,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`

	route.ID = uuid.New()

	err := r.db.QueryRowContext(
		ctx,
		query,
		route.ID,
		route.ServiceID,
		route.EnvironmentID,
		route.Path,
		route.PathType,
		route.Port,
	).Scan(&route.ID, &route.CreatedAt, &route.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create route: %w", err)
	}

	return nil
}

// GetByID retrieves a route by ID
func (r *RouteRepository) GetByID(ctx context.Context, id string) (*types.Route, error) {
	query := `
		SELECT id, service_id, environment_id, path, path_type, port,
		       created_at, updated_at
		FROM routes
		WHERE id = $1
	`

	route := &types.Route{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&route.ID,
		&route.ServiceID,
		&route.EnvironmentID,
		&route.Path,
		&route.PathType,
		&route.Port,
		&route.CreatedAt,
		&route.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("route not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get route: %w", err)
	}

	return route, nil
}

// GetByServiceAndEnvironment retrieves all routes for a service in a specific environment
func (r *RouteRepository) GetByServiceAndEnvironment(ctx context.Context, serviceID, environmentID string) ([]types.Route, error) {
	query := `
		SELECT id, service_id, environment_id, path, path_type, port,
		       created_at, updated_at
		FROM routes
		WHERE service_id = $1 AND environment_id = $2
		ORDER BY path ASC
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID, environmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query routes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var routes []types.Route
	for rows.Next() {
		var route types.Route
		err := rows.Scan(
			&route.ID,
			&route.ServiceID,
			&route.EnvironmentID,
			&route.Path,
			&route.PathType,
			&route.Port,
			&route.CreatedAt,
			&route.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan route: %w", err)
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// Update updates a route
func (r *RouteRepository) Update(ctx context.Context, route *types.Route) error {
	query := `
		UPDATE routes
		SET path = $1, path_type = $2, port = $3, updated_at = NOW()
		WHERE id = $4
		RETURNING updated_at
	`

	err := r.db.QueryRowContext(
		ctx,
		query,
		route.Path,
		route.PathType,
		route.Port,
		route.ID,
	).Scan(&route.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to update route: %w", err)
	}

	return nil
}

// Delete removes a route
func (r *RouteRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM routes WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete route: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("route not found: %s", id)
	}

	return nil
}

// DeleteByServiceID deletes all routes for a service
func (r *RouteRepository) DeleteByServiceID(ctx context.Context, serviceID string) error {
	query := `DELETE FROM routes WHERE service_id = $1`
	_, err := r.db.ExecContext(ctx, query, serviceID)
	if err != nil {
		return fmt.Errorf("failed to delete routes for service: %w", err)
	}
	return nil
}
