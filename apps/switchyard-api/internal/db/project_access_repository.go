package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ProjectAccessRepository handles project access control
type ProjectAccessRepository struct {
	db DBTX
}

func NewProjectAccessRepository(db DBTX) *ProjectAccessRepository {
	return &ProjectAccessRepository{db: db}
}

func (r *ProjectAccessRepository) Grant(ctx context.Context, access *types.ProjectAccess) error {
	access.ID = uuid.New()
	access.GrantedAt = time.Now()

	query := `
		INSERT INTO project_access (id, user_id, project_id, environment_id, role, granted_by, granted_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, project_id, environment_id) DO UPDATE
		SET role = EXCLUDED.role, granted_by = EXCLUDED.granted_by, granted_at = EXCLUDED.granted_at, expires_at = EXCLUDED.expires_at
	`
	_, err := r.db.ExecContext(ctx, query,
		access.ID, access.UserID, access.ProjectID, access.EnvironmentID,
		access.Role, access.GrantedBy, access.GrantedAt, access.ExpiresAt,
	)
	return err
}

func (r *ProjectAccessRepository) Revoke(ctx context.Context, userID, projectID uuid.UUID, environmentID *uuid.UUID) error {
	query := `
		DELETE FROM project_access
		WHERE user_id = $1 AND project_id = $2 AND (environment_id = $3 OR ($3 IS NULL AND environment_id IS NULL))
	`
	_, err := r.db.ExecContext(ctx, query, userID, projectID, environmentID)
	return err
}

func (r *ProjectAccessRepository) UserHasAccess(ctx context.Context, userID uuid.UUID, projectID uuid.UUID) (bool, error) {
	var count int
	query := `
		SELECT COUNT(*) FROM project_access
		WHERE user_id = $1 AND project_id = $2
		AND (expires_at IS NULL OR expires_at > NOW())
	`
	err := r.db.QueryRowContext(ctx, query, userID, projectID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// HasAccess checks if a user has access to a project/environment with the required role
func (r *ProjectAccessRepository) HasAccess(ctx context.Context, userID, projectID uuid.UUID, environmentID *uuid.UUID, requiredRole types.Role) (bool, error) {
	userRole, err := r.GetUserRole(ctx, userID, projectID, environmentID)
	if err != nil {
		// If no access record found, return false
		return false, nil
	}

	// Role hierarchy: admin > developer > viewer
	roleLevel := map[types.Role]int{
		types.RoleAdmin:     3,
		types.RoleDeveloper: 2,
		types.RoleViewer:    1,
	}

	return roleLevel[userRole] >= roleLevel[requiredRole], nil
}

func (r *ProjectAccessRepository) GetUserRole(ctx context.Context, userID, projectID uuid.UUID, environmentID *uuid.UUID) (types.Role, error) {
	var role types.Role
	query := `
		SELECT role FROM project_access
		WHERE user_id = $1 AND project_id = $2
		AND (environment_id = $3 OR environment_id IS NULL)
		AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY environment_id NULLS LAST
		LIMIT 1
	`
	err := r.db.QueryRowContext(ctx, query, userID, projectID, environmentID).Scan(&role)
	if err != nil {
		return "", err
	}
	return role, nil
}

func (r *ProjectAccessRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]*types.ProjectAccess, error) {
	query := `
		SELECT id, user_id, project_id, environment_id, role, granted_by, granted_at, expires_at
		FROM project_access
		WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY granted_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var accesses []*types.ProjectAccess
	for rows.Next() {
		access := &types.ProjectAccess{}
		err := rows.Scan(
			&access.ID, &access.UserID, &access.ProjectID, &access.EnvironmentID,
			&access.Role, &access.GrantedBy, &access.GrantedAt, &access.ExpiresAt,
		)
		if err != nil {
			return nil, err
		}
		accesses = append(accesses, access)
	}

	return accesses, nil
}

func (r *ProjectAccessRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.ProjectAccess, error) {
	query := `
		SELECT id, user_id, project_id, environment_id, role, granted_by, granted_at, expires_at
		FROM project_access
		WHERE project_id = $1 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY granted_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var accesses []*types.ProjectAccess
	for rows.Next() {
		access := &types.ProjectAccess{}
		err := rows.Scan(
			&access.ID, &access.UserID, &access.ProjectID, &access.EnvironmentID,
			&access.Role, &access.GrantedBy, &access.GrantedAt, &access.ExpiresAt,
		)
		if err != nil {
			return nil, err
		}
		accesses = append(accesses, access)
	}

	return accesses, nil
}
