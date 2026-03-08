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
	query := `SELECT id, name, slug, created_at, updated_at FROM projects WHERE id = $1`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&project.ID, &project.Name, &project.Slug,
		&project.CreatedAt, &project.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return project, nil
}

func (r *ProjectRepository) GetBySlug(slug string) (*types.Project, error) {
	project := &types.Project{}
	query := `SELECT id, name, slug, created_at, updated_at FROM projects WHERE slug = $1`

	err := r.db.QueryRow(query, slug).Scan(
		&project.ID, &project.Name, &project.Slug,
		&project.CreatedAt, &project.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return project, nil
}

func (r *ProjectRepository) List() ([]*types.Project, error) {
	query := `SELECT id, name, slug, created_at, updated_at FROM projects ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []*types.Project
	for rows.Next() {
		project := &types.Project{}
		err := rows.Scan(&project.ID, &project.Name, &project.Slug, &project.CreatedAt, &project.UpdatedAt)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return projects, nil
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
