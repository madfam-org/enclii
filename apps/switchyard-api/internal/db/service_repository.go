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

	var jobsJSON []byte
	if len(service.Jobs) > 0 {
		jobsJSON, err = json.Marshal(service.Jobs)
		if err != nil {
			return fmt.Errorf("failed to marshal jobs: %w", err)
		}
	} else {
		jobsJSON = []byte("[]")
	}

	// k8s_namespace persisted so subsequent reads via ListAll/ListByProject
	// can populate Service.K8sNamespace and the observability handler can
	// probe the right namespace. Optional: NULL for services that haven't
	// been onboarded to a specific namespace yet.
	var k8sNs interface{}
	if service.K8sNamespace != nil && *service.K8sNamespace != "" {
		k8sNs = *service.K8sNamespace
	} else {
		k8sNs = nil
	}

	if service.Type == "" {
		service.Type = types.ServiceTypeWeb
	}
	if service.Region == "" {
		service.Region = "default"
	}

	query := `
		INSERT INTO services (id, project_id, name, git_repo, app_path, build_config,
			auto_deploy, auto_deploy_branch, auto_deploy_env, k8s_namespace, created_at, updated_at, jobs, type, region)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, err = r.db.Exec(query, service.ID, service.ProjectID, service.Name, service.GitRepo,
		service.AppPath, buildConfigJSON, service.AutoDeploy, service.AutoDeployBranch,
		service.AutoDeployEnv, k8sNs, service.CreatedAt, service.UpdatedAt, jobsJSON, service.Type, service.Region)
	return err
}

// UpdateK8sNamespace sets the k8s_namespace column for an existing service.
// Used by the reconcile-services-from-cluster admin endpoint to repair
// services rows whose k8s_namespace column is NULL.
func (r *ServiceRepository) UpdateK8sNamespace(ctx context.Context, serviceID uuid.UUID, namespace string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE services SET k8s_namespace = $1, updated_at = NOW() WHERE id = $2`,
		namespace, serviceID)
	return err
}

func (r *ServiceRepository) GetByID(id uuid.UUID) (*types.Service, error) {
	service := &types.Service{}
	var buildConfigJSON []byte
	var jobsJSON []byte
	var appPath sql.NullString

	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at, COALESCE(jobs, '[]'::jsonb) as jobs, type, region
		FROM services WHERE id = $1`

	err := r.db.QueryRow(query, id).Scan(
		&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
		&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
		&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt, &jobsJSON,
		&service.Type, &service.Region,
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

	if len(jobsJSON) > 0 {
		if err := json.Unmarshal(jobsJSON, &service.Jobs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal jobs: %w", err)
		}
	}

	return service, nil
}

// GetByName retrieves a service by its name (used for K8s→DB reconciliation)
func (r *ServiceRepository) GetByName(name string) (*types.Service, error) {
	service := &types.Service{}
	var buildConfigJSON []byte
	var jobsJSON []byte
	var appPath sql.NullString

	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at, COALESCE(jobs, '[]'::jsonb) as jobs, type, region
		FROM services WHERE name = $1`

	err := r.db.QueryRow(query, name).Scan(
		&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
		&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
		&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt, &jobsJSON,
		&service.Type, &service.Region,
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

	if len(jobsJSON) > 0 {
		if err := json.Unmarshal(jobsJSON, &service.Jobs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal jobs: %w", err)
		}
	}

	return service, nil
}

// ListByTeam returns every service whose parent project belongs to the given
// team. Mirror of ListAll's column shape — no per-service release/health
// subqueries, callers that need those use ListByProject. Used by the master-
// admin tenant-filter middleware (XC-2 Round 5) when an acting-as session
// scopes a global services view to a single tenant.
//
// Services whose projects.team_id IS NULL ("personal" / unparented) are NOT
// returned — same convention as ProjectRepository.ListByTeam: when a master
// admin is acting-as a tenant, they see exactly that tenant's resources.
func (r *ServiceRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]*types.Service, error) {
	query := `SELECT s.id, s.project_id, s.name, s.git_repo, COALESCE(s.app_path, '') as app_path, s.build_config,
		s.auto_deploy, s.auto_deploy_branch, s.auto_deploy_env, s.k8s_namespace, s.created_at, s.updated_at, COALESCE(s.jobs, '[]'::jsonb) as jobs, s.type, s.region
		FROM services s
		JOIN projects p ON p.id = s.project_id
		WHERE p.team_id = $1
		ORDER BY s.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var services []*types.Service
	for rows.Next() {
		service := &types.Service{}
		var buildConfigJSON []byte
		var jobsJSON []byte
		var appPath sql.NullString
		var k8sNamespace sql.NullString

		err := rows.Scan(
			&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
			&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
			&service.AutoDeployEnv, &k8sNamespace, &service.CreatedAt, &service.UpdatedAt, &jobsJSON,
			&service.Type, &service.Region,
		)
		if err != nil {
			return nil, err
		}

		if appPath.Valid {
			service.AppPath = appPath.String
		}
		if k8sNamespace.Valid {
			ns := k8sNamespace.String
			service.K8sNamespace = &ns
		}

		if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
		}

		if len(jobsJSON) > 0 {
			if err := json.Unmarshal(jobsJSON, &service.Jobs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal jobs: %w", err)
			}
		}

		services = append(services, service)
	}

	return services, rows.Err()
}

func (r *ServiceRepository) ListAll(ctx context.Context) ([]*types.Service, error) {
	// k8s_namespace included so callers (notably the health/observability
	// handler) can probe the right namespace for pod counts. Audit
	// 2026-04-29 traced uniformly-zero pod counts back to ListAll
	// dropping this column, forcing the caller to fall back to "default".
	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, k8s_namespace, created_at, updated_at, COALESCE(jobs, '[]'::jsonb) as jobs, type, region
		FROM services ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var services []*types.Service
	for rows.Next() {
		service := &types.Service{}
		var buildConfigJSON []byte
		var jobsJSON []byte
		var appPath sql.NullString
		var k8sNamespace sql.NullString

		err := rows.Scan(
			&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
			&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
			&service.AutoDeployEnv, &k8sNamespace, &service.CreatedAt, &service.UpdatedAt, &jobsJSON,
			&service.Type, &service.Region,
		)
		if err != nil {
			return nil, err
		}

		if appPath.Valid {
			service.AppPath = appPath.String
		}
		if k8sNamespace.Valid {
			ns := k8sNamespace.String
			service.K8sNamespace = &ns
		}

		if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
		}

		if len(jobsJSON) > 0 {
			if err := json.Unmarshal(jobsJSON, &service.Jobs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal jobs: %w", err)
			}
		}

		services = append(services, service)
	}

	return services, nil
}

func (r *ServiceRepository) ListByProject(projectID uuid.UUID) ([]*types.Service, error) {
	// recent_releases: last 5 releases per service as a JSON array. Built from a
	// correlated subquery that pre-orders by created_at DESC + LIMIT 5 so the
	// json_agg output is deterministic and the operator dashboard can render
	// rollback-eligible deploys without a follow-up round trip.
	query := `SELECT s.id, s.project_id, s.name, s.git_repo, COALESCE(s.app_path, '') as app_path, s.build_config,
		s.auto_deploy, s.auto_deploy_branch, s.auto_deploy_env,
		s.k8s_namespace, COALESCE(s.health, 'unknown') as health, COALESCE(s.status, 'unknown') as status,
		COALESCE(s.desired_replicas, 0) as desired_replicas, COALESCE(s.ready_replicas, 0) as ready_replicas,
		s.last_health_check,
		(SELECT MAX(d.created_at) FROM deployments d JOIN releases r ON d.release_id = r.id WHERE r.service_id = s.id) as last_deployment,
		(SELECT r2.commit_message FROM releases r2 JOIN deployments d2 ON d2.release_id = r2.id WHERE r2.service_id = s.id ORDER BY d2.created_at DESC LIMIT 1) as last_commit_message,
		(SELECT r3.git_branch FROM releases r3 JOIN deployments d3 ON d3.release_id = r3.id WHERE r3.service_id = s.id ORDER BY d3.created_at DESC LIMIT 1) as last_commit_branch,
		(SELECT r4.image_uri FROM releases r4 WHERE r4.service_id = s.id AND r4.status = 'succeeded' ORDER BY r4.created_at DESC LIMIT 1) as current_image_uri,
		(SELECT r5.id FROM releases r5 WHERE r5.service_id = s.id AND r5.status = 'succeeded' ORDER BY r5.created_at DESC LIMIT 1) as current_release_id,
		(SELECT r6.created_at FROM releases r6 WHERE r6.service_id = s.id AND r6.status = 'succeeded' ORDER BY r6.created_at DESC LIMIT 1) as current_release_created_at,
		(SELECT COALESCE(json_agg(rr ORDER BY rr.created_at DESC), '[]'::json)
			FROM (
				SELECT id, version, image_uri, git_sha, status, created_at
				FROM releases
				WHERE service_id = s.id
				ORDER BY created_at DESC
				LIMIT 5
			) rr) as recent_releases,
		s.created_at, s.updated_at, COALESCE(s.jobs, '[]'::jsonb) as jobs, s.type, s.region
		FROM services s WHERE s.project_id = $1 ORDER BY s.created_at DESC`

	rows, err := r.db.Query(query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var services []*types.Service
	for rows.Next() {
		service := &types.Service{}
		var buildConfigJSON []byte
		var jobsJSON []byte
		var appPath sql.NullString
		var k8sNamespace sql.NullString
		var lastHealthCheck sql.NullTime
		var lastDeployment sql.NullTime
		var lastCommitMsg sql.NullString
		var lastCommitBranch sql.NullString
		var currentImageURI sql.NullString
		var currentReleaseID sql.NullString
		var currentReleaseCreatedAt sql.NullTime
		var recentReleasesJSON []byte

		err := rows.Scan(&service.ID, &service.ProjectID, &service.Name, &service.GitRepo, &appPath, &buildConfigJSON,
			&service.AutoDeploy, &service.AutoDeployBranch, &service.AutoDeployEnv,
			&k8sNamespace, &service.Health, &service.Status,
			&service.DesiredReplicas, &service.ReadyReplicas, &lastHealthCheck,
			&lastDeployment, &lastCommitMsg, &lastCommitBranch,
			&currentImageURI, &currentReleaseID, &currentReleaseCreatedAt, &recentReleasesJSON,
			&service.CreatedAt, &service.UpdatedAt, &jobsJSON, &service.Type, &service.Region)
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
		if currentImageURI.Valid {
			service.CurrentImageURI = currentImageURI.String
		}
		if currentReleaseID.Valid {
			if id, parseErr := uuid.Parse(currentReleaseID.String); parseErr == nil {
				service.CurrentReleaseID = &id
			}
		}
		if currentReleaseCreatedAt.Valid {
			service.CurrentReleaseCreatedAt = &currentReleaseCreatedAt.Time
		}
		if len(recentReleasesJSON) > 0 {
			if err := json.Unmarshal(recentReleasesJSON, &service.RecentReleases); err != nil {
				return nil, fmt.Errorf("failed to unmarshal recent releases: %w", err)
			}
		}

		if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
		}

		if len(jobsJSON) > 0 {
			if err := json.Unmarshal(jobsJSON, &service.Jobs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal jobs: %w", err)
			}
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
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at, type, region
		FROM services WHERE git_repo = $1`

	err := r.db.QueryRow(query, gitRepoURL).Scan(
		&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
		&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
		&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt,
		&service.Type, &service.Region,
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

// EnrichWithLatestRelease populates CurrentImageURI, CurrentReleaseCreatedAt,
// and RecentReleases on each service by querying the releases table. This is
// the same data ListByProject embeds inline via subqueries; it is broken out
// here so callers that don't need the full dashboard projection (e.g. the
// public ListServicesByGitRepo endpoint consumed by Pillar 4 image-staleness
// detection) can opt in to image-age fields without paying for the rest.
//
// Performance: each service triggers two indexed lookups against
// releases(service_id) — one for the latest succeeded release, one for the
// last 5 releases — using idx_releases_service_id. Total cost is
// O(N services * 2 indexed lookups). For the small N typical of
// ListByGitRepo callers (1-5 services per repo), this is well under a
// millisecond. If this is later called for project-wide listings, consider
// folding the subqueries into the parent query as ListByProject already does.
func (r *ServiceRepository) EnrichWithLatestRelease(services []*types.Service) error {
	currentQuery := `SELECT image_uri, created_at FROM releases
		WHERE service_id = $1 AND status = 'succeeded'
		ORDER BY created_at DESC LIMIT 1`

	recentQuery := `SELECT id, version, image_uri, git_sha, status, created_at FROM releases
		WHERE service_id = $1
		ORDER BY created_at DESC LIMIT 5`

	for _, svc := range services {
		// Latest succeeded release: drives current_image_uri + current_release_created_at.
		var imageURI sql.NullString
		var createdAt sql.NullTime
		err := r.db.QueryRow(currentQuery, svc.ID).Scan(&imageURI, &createdAt)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to query current release for service %s: %w", svc.ID, err)
		}
		if imageURI.Valid {
			svc.CurrentImageURI = imageURI.String
		}
		if createdAt.Valid {
			t := createdAt.Time
			svc.CurrentReleaseCreatedAt = &t
		}

		// Last 5 releases for the recent_releases array.
		rows, err := r.db.Query(recentQuery, svc.ID)
		if err != nil {
			return fmt.Errorf("failed to query recent releases for service %s: %w", svc.ID, err)
		}
		var recent []types.ReleaseSummary
		for rows.Next() {
			var rs types.ReleaseSummary
			if err := rows.Scan(&rs.ID, &rs.Version, &rs.ImageURI, &rs.GitSHA, &rs.Status, &rs.CreatedAt); err != nil {
				_ = rows.Close()
				return fmt.Errorf("failed to scan recent release for service %s: %w", svc.ID, err)
			}
			recent = append(recent, rs)
		}
		_ = rows.Close()
		svc.RecentReleases = recent
	}

	return nil
}

// ListByGitRepo retrieves ALL services matching a git repository URL
// Supports monorepos where multiple services share the same repo
// Normalizes URLs to handle variations like .git suffix, trailing slashes
func (r *ServiceRepository) ListByGitRepo(gitRepoURL string) ([]*types.Service, error) {
	// Normalize the input URL for matching
	normalizedURL := normalizeGitURL(gitRepoURL)

	// Query with normalized URL matching (handles .git suffix variations)
	query := `SELECT id, project_id, name, git_repo, COALESCE(app_path, '') as app_path, build_config,
		auto_deploy, auto_deploy_branch, auto_deploy_env, created_at, updated_at, COALESCE(jobs, '[]'::jsonb) as jobs, type, region
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
	defer func() { _ = rows.Close() }()

	var services []*types.Service
	for rows.Next() {
		service := &types.Service{}
		var buildConfigJSON []byte
		var jobsJSON []byte
		var appPath sql.NullString

		if err := rows.Scan(
			&service.ID, &service.ProjectID, &service.Name, &service.GitRepo,
			&appPath, &buildConfigJSON, &service.AutoDeploy, &service.AutoDeployBranch,
			&service.AutoDeployEnv, &service.CreatedAt, &service.UpdatedAt, &jobsJSON,
			&service.Type, &service.Region,
		); err != nil {
			return nil, err
		}

		if appPath.Valid {
			service.AppPath = appPath.String
		}

		if err := json.Unmarshal(buildConfigJSON, &service.BuildConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal build config: %w", err)
		}

		if len(jobsJSON) > 0 {
			if err := json.Unmarshal(jobsJSON, &service.Jobs); err != nil {
				return nil, fmt.Errorf("failed to unmarshal jobs: %w", err)
			}
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

	var jobsJSON []byte
	if len(service.Jobs) > 0 {
		jobsJSON, err = json.Marshal(service.Jobs)
		if err != nil {
			return fmt.Errorf("failed to marshal jobs: %w", err)
		}
	} else {
		jobsJSON = []byte("[]")
	}

	query := `
		UPDATE services
		SET name = $1, git_repo = $2, app_path = $3, build_config = $4,
		    auto_deploy = $5, auto_deploy_branch = $6, auto_deploy_env = $7, updated_at = $8, jobs = $9, type = $10, region = $11
		WHERE id = $12
	`
	result, err := r.db.ExecContext(ctx, query,
		service.Name, service.GitRepo, service.AppPath, buildConfigJSON,
		service.AutoDeploy, service.AutoDeployBranch, service.AutoDeployEnv, service.UpdatedAt, jobsJSON, service.Type, service.Region, service.ID)
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

// MarkReconciledHealthy updates the namespace-discoverer fields for a service
// that is matched to a live K8s workload: clears zombie_since (if set) and
// stamps last_reconciled_at, replica counts. Idempotent.
func (r *ServiceRepository) MarkReconciledHealthy(ctx context.Context, id uuid.UUID, desiredReplicas, readyReplicas int32) error {
	query := `UPDATE services
		SET zombie_since = NULL,
		    last_reconciled_at = NOW(),
		    desired_replicas = $1,
		    ready_replicas = $2,
		    updated_at = NOW()
		WHERE id = $3`
	_, err := r.db.ExecContext(ctx, query, desiredReplicas, readyReplicas, id)
	return err
}

// MarkReconciledZombie sets zombie_since to NOW() if it is currently NULL.
// Called when the namespace discoverer cannot find a matching workload for a
// service that has k8s_namespace pinned. Idempotent: subsequent calls are
// no-ops while zombie_since is already set.
func (r *ServiceRepository) MarkReconciledZombie(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE services
		SET zombie_since = COALESCE(zombie_since, NOW()),
		    last_reconciled_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
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
