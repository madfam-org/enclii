package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// GetLatestByServiceAndEnvironment returns the service's most recent
// deployment in one environment (sql.ErrNoRows when it has none there).
// GetLatestByService answers across environments, which is the wrong record
// when a domain edit targets production and staging deployed last.
func (r *DeploymentRepository) GetLatestByServiceAndEnvironment(ctx context.Context, serviceID, environmentID uuid.UUID) (*types.Deployment, error) {
	deployment := &types.Deployment{}
	query := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health, d.error_message,
		       d.service_id, d.version_number, d.created_at, d.updated_at
		FROM deployments d
		JOIN releases r ON d.release_id = r.id
		WHERE r.service_id = $1 AND d.environment_id = $2
		ORDER BY d.created_at DESC
		LIMIT 1
	`
	err := r.db.QueryRowContext(ctx, query, serviceID, environmentID).Scan(
		&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
		&deployment.Replicas, &deployment.Status, &deployment.Health,
		&deployment.ErrorMessage,
		&deployment.ServiceID, &deployment.VersionNumber,
		&deployment.CreatedAt, &deployment.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return deployment, nil
}
