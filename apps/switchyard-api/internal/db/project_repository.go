package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ProjectRepository handles project CRUD operations
type ProjectRepository struct {
	db DBTX
}

func NewProjectRepository(db DBTX) *ProjectRepository {
	return &ProjectRepository{db: db}
}

func (r *ProjectRepository) Create(project *types.Project) error {
	project.ID = uuid.New()
	project.CreatedAt = time.Now()
	project.UpdatedAt = time.Now()

	query := `
		INSERT INTO projects (id, name, slug, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.Exec(query, project.ID, project.Name, project.Slug, project.CreatedAt, project.UpdatedAt)
	return err
}

func (r *ProjectRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Project, error) {
	project := &types.Project{}
	query := `SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE id = $1`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&project.ID, &project.Name, &project.Slug, &project.CIRunnerMode,
		&project.CreatedAt, &project.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return project, nil
}

func (r *ProjectRepository) GetBySlug(slug string) (*types.Project, error) {
	project := &types.Project{}
	query := `SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug = $1`

	err := r.db.QueryRow(query, slug).Scan(
		&project.ID, &project.Name, &project.Slug, &project.CIRunnerMode,
		&project.CreatedAt, &project.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return project, nil
}

func (r *ProjectRepository) List() ([]*types.Project, error) {
	query := `SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []*types.Project
	for rows.Next() {
		project := &types.Project{}
		err := rows.Scan(&project.ID, &project.Name, &project.Slug, &project.CIRunnerMode, &project.CreatedAt, &project.UpdatedAt)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return projects, nil
}

// ListByTeam returns projects whose team_id matches the given team. Used by
// the master-admin tenant-filter middleware to scope a list to the
// currently-acted-on tenant. Projects with team_id IS NULL ("personal" /
// unparented) are NOT included — that's the right behavior for impersonation
// (the operator wants the tenant's view, not the platform-wide view).
func (r *ProjectRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]*types.Project, error) {
	query := `
		SELECT id, name, slug, ci_runner_mode, created_at, updated_at
		  FROM projects
		 WHERE team_id = $1
		 ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []*types.Project
	for rows.Next() {
		p := &types.Project{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.CIRunnerMode, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// GetTeamID returns the team_id (or uuid.Nil if NULL/personal) for the given
// project. Used by the master-admin tenant-filter middleware (XC-2 Round 5)
// to enforce 403 guards on per-resource detail endpoints (services, deploys,
// domains, addons): when the caller is acting-as a tenant, a resource whose
// project belongs to a different team must be invisible.
//
// Returns sql.ErrNoRows when the project does not exist. We deliberately
// return uuid.Nil (no error) for an existing project with a NULL team_id so
// the caller can compare "this project has no team" vs "team mismatch"
// without an extra branch.
func (r *ProjectRepository) GetTeamID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	var teamID uuid.NullUUID
	err := r.db.QueryRowContext(ctx,
		`SELECT team_id FROM projects WHERE id = $1`,
		projectID,
	).Scan(&teamID)
	if err != nil {
		return uuid.Nil, err
	}
	if !teamID.Valid {
		return uuid.Nil, nil
	}
	return teamID.UUID, nil
}

// UpdateCIRunnerMode sets the CI runner mode for a project
func (r *ProjectRepository) UpdateCIRunnerMode(ctx context.Context, id uuid.UUID, mode types.CIRunnerMode) error {
	query := `UPDATE projects SET ci_runner_mode = $1, updated_at = NOW() WHERE id = $2`
	result, err := r.db.ExecContext(ctx, query, mode, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes a project by ID
// Note: All related records (services, environments, etc.) are automatically
// deleted via ON DELETE CASCADE foreign key constraints
func (r *ProjectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM projects WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
