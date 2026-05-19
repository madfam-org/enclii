package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// EnvironmentRepository handles environment CRUD operations
type EnvironmentRepository struct {
	db DBTX
}

func NewEnvironmentRepository(db DBTX) *EnvironmentRepository {
	return &EnvironmentRepository{db: db}
}

func (r *EnvironmentRepository) Create(env *types.Environment) error {
	env.ID = uuid.New()
	env.CreatedAt = time.Now()
	env.UpdatedAt = time.Now()

	query := `
		INSERT INTO environments (id, project_id, name, kube_namespace, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.Exec(query, env.ID, env.ProjectID, env.Name, env.KubeNamespace, env.CreatedAt, env.UpdatedAt)
	return err
}

func (r *EnvironmentRepository) UpdateKubeNamespace(ctx context.Context, id uuid.UUID, namespace string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE environments SET kube_namespace = $1, updated_at = NOW() WHERE id = $2`,
		namespace,
		id,
	)
	return err
}

func (r *EnvironmentRepository) GetByProjectAndName(projectID uuid.UUID, name string) (*types.Environment, error) {
	env := &types.Environment{}
	query := `SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = $1 AND name = $2`

	err := r.db.QueryRow(query, projectID, name).Scan(
		&env.ID, &env.ProjectID, &env.Name, &env.KubeNamespace,
		&env.CreatedAt, &env.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return env, nil
}

func (r *EnvironmentRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Environment, error) {
	env := &types.Environment{}
	query := `SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE id = $1`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&env.ID, &env.ProjectID, &env.Name, &env.KubeNamespace,
		&env.CreatedAt, &env.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return env, nil
}

func (r *EnvironmentRepository) ListByProject(projectID uuid.UUID) ([]*types.Environment, error) {
	query := `SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = $1 ORDER BY name`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var environments []*types.Environment
	for rows.Next() {
		env := &types.Environment{}
		if err := rows.Scan(&env.ID, &env.ProjectID, &env.Name, &env.KubeNamespace, &env.CreatedAt, &env.UpdatedAt); err != nil {
			return nil, err
		}
		environments = append(environments, env)
	}

	return environments, nil
}

// ListAll retrieves all environments across all projects
// Used by the reconciler to build dynamic namespace list for K8s sync
func (r *EnvironmentRepository) ListAll() ([]*types.Environment, error) {
	query := `SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var environments []*types.Environment
	for rows.Next() {
		env := &types.Environment{}
		if err := rows.Scan(&env.ID, &env.ProjectID, &env.Name, &env.KubeNamespace, &env.CreatedAt, &env.UpdatedAt); err != nil {
			return nil, err
		}
		environments = append(environments, env)
	}

	return environments, nil
}

// GetByKubeNamespace retrieves an environment by its Kubernetes namespace (used for K8s→DB reconciliation)
func (r *EnvironmentRepository) GetByKubeNamespace(namespace string) (*types.Environment, error) {
	env := &types.Environment{}
	query := `SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE kube_namespace = $1`

	err := r.db.QueryRow(query, namespace).Scan(
		&env.ID, &env.ProjectID, &env.Name, &env.KubeNamespace,
		&env.CreatedAt, &env.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return env, nil
}
