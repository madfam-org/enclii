package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DeploymentRepository handles deployment CRUD operations
type DeploymentRepository struct {
	db DBTX
}

func NewDeploymentRepository(db DBTX) *DeploymentRepository {
	return &DeploymentRepository{db: db}
}

func (r *DeploymentRepository) Create(deployment *types.Deployment) error {
	deployment.ID = uuid.New()
	deployment.CreatedAt = time.Now()
	deployment.UpdatedAt = time.Now()

	// Note: group_id and deploy_order columns don't exist in the database yet
	// They're part of the deployment group feature that hasn't been migrated
	query := `
		INSERT INTO deployments (id, release_id, environment_id, replicas, status, health, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.Exec(query, deployment.ID, deployment.ReleaseID, deployment.EnvironmentID, deployment.Replicas, deployment.Status, deployment.Health, deployment.CreatedAt, deployment.UpdatedAt)
	return err
}

func (r *DeploymentRepository) UpdateStatus(id uuid.UUID, status types.DeploymentStatus, health types.HealthStatus) error {
	query := `UPDATE deployments SET status = $1, health = $2, updated_at = NOW() WHERE id = $3`
	_, err := r.db.Exec(query, status, health, id)
	return err
}

// UpdateStatusWithError updates deployment status and stores error message for failed deployments
func (r *DeploymentRepository) UpdateStatusWithError(id uuid.UUID, status types.DeploymentStatus, health types.HealthStatus, errorMsg *string) error {
	query := `UPDATE deployments SET status = $1, health = $2, error_message = $3, updated_at = NOW() WHERE id = $4`
	_, err := r.db.Exec(query, status, health, errorMsg, id)
	return err
}

func (r *DeploymentRepository) GetByID(ctx context.Context, id string) (*types.Deployment, error) {
	deployment := &types.Deployment{}
	query := `SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at
	          FROM deployments WHERE id = $1`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
		&deployment.Replicas, &deployment.Status, &deployment.Health,
		&deployment.ErrorMessage, &deployment.CreatedAt, &deployment.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return deployment, nil
}

func (r *DeploymentRepository) ListByRelease(ctx context.Context, releaseID string) ([]*types.Deployment, error) {
	query := `SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at
	          FROM deployments WHERE release_id = $1 ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []*types.Deployment
	for rows.Next() {
		deployment := &types.Deployment{}
		err := rows.Scan(
			&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
			&deployment.Replicas, &deployment.Status, &deployment.Health,
			&deployment.ErrorMessage, &deployment.CreatedAt, &deployment.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, deployment)
	}

	return deployments, nil
}

func (r *DeploymentRepository) GetLatestByService(ctx context.Context, serviceID string) (*types.Deployment, error) {
	deployment := &types.Deployment{}
	query := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health, d.error_message, d.created_at, d.updated_at
		FROM deployments d
		JOIN releases r ON d.release_id = r.id
		WHERE r.service_id = $1
		ORDER BY d.created_at DESC
		LIMIT 1
	`

	err := r.db.QueryRowContext(ctx, query, serviceID).Scan(
		&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
		&deployment.Replicas, &deployment.Status, &deployment.Health,
		&deployment.ErrorMessage, &deployment.CreatedAt, &deployment.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return deployment, nil
}

func (r *DeploymentRepository) GetByStatus(ctx context.Context, status types.DeploymentStatus) ([]*types.Deployment, error) {
	// Note: group_id and deploy_order columns don't exist in the database yet
	// They're part of the deployment group feature that hasn't been migrated
	query := `SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at
	          FROM deployments WHERE status = $1 ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []*types.Deployment
	for rows.Next() {
		deployment := &types.Deployment{}
		err := rows.Scan(
			&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
			&deployment.Replicas, &deployment.Status, &deployment.Health,
			&deployment.ErrorMessage, &deployment.CreatedAt, &deployment.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		// GroupID and DeployOrder default to nil/0 until feature is migrated
		deployments = append(deployments, deployment)
	}

	return deployments, nil
}

// ListAll retrieves all deployments across services, optionally filtered by a since time
func (r *DeploymentRepository) ListAll(ctx context.Context, since *time.Time, limit int) ([]*types.Deployment, error) {
	var query string
	var args []interface{}

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	if since != nil {
		query = `SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at
		         FROM deployments WHERE created_at >= $1 ORDER BY created_at DESC LIMIT $2`
		args = []interface{}{*since, limit}
	} else {
		query = `SELECT id, release_id, environment_id, replicas, status, health, error_message, created_at, updated_at
		         FROM deployments ORDER BY created_at DESC LIMIT $1`
		args = []interface{}{limit}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []*types.Deployment
	for rows.Next() {
		deployment := &types.Deployment{}
		err := rows.Scan(
			&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
			&deployment.Replicas, &deployment.Status, &deployment.Health,
			&deployment.ErrorMessage, &deployment.CreatedAt, &deployment.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, deployment)
	}

	return deployments, nil
}

// ListByGroup retrieves all deployments for a deployment group
// Note: This feature is not yet implemented - group_id and deploy_order columns
// don't exist in the database. Returns empty slice until feature is migrated.
func (r *DeploymentRepository) ListByGroup(ctx context.Context, groupID uuid.UUID) ([]*types.Deployment, error) {
	// Deployment groups feature not yet migrated - return empty slice
	return []*types.Deployment{}, nil
}
