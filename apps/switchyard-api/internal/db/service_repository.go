package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ServiceRepository handles service CRUD operations
type ServiceRepository struct {
	db DBTX
}

func NewServiceRepository(db DBTX) *ServiceRepository {
	return &ServiceRepository{db: db}
}

func (r *ServiceRepository) Create(service *types.Service) error {
	service.ID = uuid.New()
	service.CreatedAt = time.Now()
	service.UpdatedAt = time.Now()

	// Set sensible defaults for auto-deploy if not provided
	if service.AutoDeployBranch == "" {
		service.AutoDeployBranch = "main"
	}
	if service.AutoDeployEnv == "" {
		service.AutoDeployEnv = "production"
	}

	buildConfigJSON, err := json.Marshal(service.BuildConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal build config: %w", err)
	}

	query := `
		INSERT INTO services (id, project_id, name, git_repo, app_path, build_config,
			auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = r.db.Exec(query, service.ID, service.ProjectID, service.Name, service.GitRepo,
		service.AppPath, buildConfigJSON, service.AutoDeploy, service.AutoDeployBranch,
		service.AutoDeployEnv, service.CreatedAt, service.UpdatedAt)
	return err
}

func (r *ServiceRepository) GetByID(id uuid.UUID) (*types.Service, error) {
	service := &types.Service{}
	var buildConfigJSON []byte
	var appPath sql.NullString

	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at
		FROM services WHERE id = $1`

	err := r.db.QueryRow(query, id).Scan(
		&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
		&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
		&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if appPath.Valid {
		service.AppPath = appPath.String
	}

	if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
	}

	return service, nil
}

// GetByName retrieves a service by its name (used for K8s→DB reconciliation)
func (r *ServiceRepository) GetByName(name string) (*types.Service, error) {
	service := &types.Service{}
	var buildConfigJSON []byte
	var appPath sql.NullString

	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at
		FROM services WHERE name = $1`

	err := r.db.QueryRow(query, name).Scan(
		&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
		&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
		&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if appPath.Valid {
		service.AppPath = appPath.String
	}

	if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
	}

	return service, nil
}

func (r *ServiceRepository) ListAll(ctx context.Context) ([]*types.Service, error) {
	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at
		FROM services ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []*types.Service
	for rows.Next() {
		service := &types.Service{}
		var buildConfigJSON []byte
		var appPath sql.NullString

		err := rows.Scan(
			&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
			&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
			&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if appPath.Valid {
			service.AppPath = appPath.String
		}

		if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
		}

		services = append(services, service)
	}

	return services, nil
}

func (r *ServiceRepository) ListByProject(projectID uuid.UUID) ([]*types.Service, error) {
	query := `SELECT s.id, s.project_id, s.name, s.git_repo, COALESCE(s.app_path, '') as app_path, s.build_config,
		s.auto_deploy, s.auto_deploy_branch, s.auto_deploy_env,
		s.k8s_namespace, COALESCE(s.health, 'unknown') as health, COALESCE(s.status, 'unknown') as status,
		COALESCE(s.desired_replicas, 0) as desired_replicas, COALESCE(s.ready_replicas, 0) as ready_replicas,
		s.last_health_check,
		(SELECT MAX(d.created_at) FROM deployments d JOIN releases r ON d.release_id = r.id WHERE r.service_id = s.id) as last_deployment,
		(SELECT r2.commit_message FROM releases r2 JOIN deployments d2 ON d2.release_id = r2.id WHERE r2.service_id = s.id ORDER BY d2.created_at DESC LIMIT 1) as last_commit_message,
		(SELECT r3.git_branch FROM releases r3 JOIN deployments d3 ON d3.release_id = r3.id WHERE r3.service_id = s.id ORDER BY d3.created_at DESC LIMIT 1) as last_commit_branch,
		s.created_at, s.updated_at
		FROM services s WHERE s.project_id = $1 ORDER BY s.created_at DESC`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []*types.Service
	for rows.Next() {
		service := &types.Service{}
		var buildConfigJSON []byte
		var appPath sql.NullString
		var k8sNamespace sql.NullString
		var lastHealthCheck sql.NullTime
		var lastDeployment sql.NullTime
		var lastCommitMsg sql.NullString
		var lastCommitBranch sql.NullString

		err := rows.Scan(&service.ID, &service.ProjectID, &service.Name, &service.GitRepo, &appPath, &buildConfigJSON,
			&service.AutoDeploy, &service.AutoDeployBranch, &service.AutoDeployEnv,
			&k8sNamespace, &service.Health, &service.Status,
			&service.DesiredReplicas, &service.ReadyReplicas, &lastHealthCheck,
			&lastDeployment, &lastCommitMsg, &lastCommitBranch,
			&service.CreatedAt, &service.UpdatedAt)
		if err != nil {
			return nil, err
		}

		if appPath.Valid {
			service.AppPath = appPath.String
		}
		if k8sNamespace.Valid {
			service.K8sNamespace = &k8sNamespace.String
		}
		if lastHealthCheck.Valid {
			service.LastHealthCheck = &lastHealthCheck.Time
		}
		if lastDeployment.Valid {
			service.LastDeployment = &lastDeployment.Time
		}
		if lastCommitMsg.Valid {
			service.LastCommitMsg = lastCommitMsg.String
		}
		if lastCommitBranch.Valid {
			service.LastCommitBranch = lastCommitBranch.String
		}

		if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
		}

		services = append(services, service)
	}

	return services, nil
}

// GetByGitRepo retrieves a service by its git repository URL
// Used by GitHub webhooks to find the service to build when a push event is received
func (r *ServiceRepository) GetByGitRepo(gitRepoURL string) (*types.Service, error) {
	service := &types.Service{}
	var buildConfigJSON []byte
	var appPath sql.NullString

	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at
		FROM services WHERE git_repo = $1`

	err := r.db.QueryRow(query, gitRepoURL).Scan(
		&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
		&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
		&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if appPath.Valid {
		service.AppPath = appPath.String
	}

	if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
	}

	return service, nil
}

// ListByGitRepo retrieves ALL services matching a git repository URL
// Supports monorepos where multiple services share the same repo
// Normalizes URLs to handle variations like .git suffix, trailing slashes
func (r *ServiceRepository) ListByGitRepo(gitRepoURL string) ([]*types.Service, error) {
	// Normalize the input URL for matching
	normalizedURL := normalizeGitURL(gitRepoURL)

	// Query with normalized URL matching (handles .git suffix variations)
	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at
		FROM services
		WHERE REPLACE(REPLACE(git_repo, '.git', ''), 'https://github.com/', '') = $1
		   OR git_repo = $2
		   OR git_repo = $3`

	// Try with and without .git suffix
	urlWithGit := normalizedURL
	if !strings.HasSuffix(normalizedURL, ".git") {
		urlWithGit = normalizedURL + ".git"
	}
	urlWithoutGit := strings.TrimSuffix(normalizedURL, ".git")

	rows, err := r.db.Query(query,
		strings.TrimPrefix(strings.TrimPrefix(urlWithoutGit, "https://github.com/"), "http://github.com/"),
		urlWithGit,
		urlWithoutGit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []*types.Service
	for rows.Next() {
		service := &types.Service{}
		var buildConfigJSON []byte
		var appPath sql.NullString

		if err := rows.Scan(
			&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
			&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
			&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if appPath.Valid {
			service.AppPath = appPath.String
		}

		if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
		}

		services = append(services, service)
	}

	return services, nil
}

// Update updates an existing service
func (r *ServiceRepository) Update(ctx context.Context, service *types.Service) error {
	service.UpdatedAt = time.Now()

	buildConfigJSON, err := json.Marshal(service.BuildConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal build config: %w", err)
	}

	query := `
		UPDATE services
		SET name = $1, git_repo = $2, app_path = $3, build_config = $4,
		    auto_deploy = $5, auto_deploy_branch = $6, auto_deploy_env = $7, updated_at = $8
		WHERE id = $9
	`
	result, err := r.db.ExecContext(ctx, query,
		service.Name, service.GitRepo, service.AppPath, buildConfigJSON,
		service.AutoDeploy, service.AutoDeployBranch, service.AutoDeployEnv, service.UpdatedAt, service.ID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateHealthStatus updates the health and replica status of a service.
// Called by the reconciler to propagate K8s deployment health to the parent service.
func (r *ServiceRepository) UpdateHealthStatus(ctx context.Context, id uuid.UUID, health types.HealthStatus, status string, desiredReplicas, readyReplicas int32) error {
	query := `UPDATE services SET health = $1, status = $2, desired_replicas = $3, ready_replicas = $4, last_health_check = NOW(), updated_at = NOW() WHERE id = $5`
	_, err := r.db.ExecContext(ctx, query, health, status, desiredReplicas, readyReplicas, id)
	return err
}

// Delete removes a service by ID
func (r *ServiceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM services WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
