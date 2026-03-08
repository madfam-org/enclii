package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DeploymentGroupStatus represents the status of a deployment group
type DeploymentGroupStatus string

const (
	DeploymentGroupStatusPending    DeploymentGroupStatus = "pending"
	DeploymentGroupStatusInProgress DeploymentGroupStatus = "in_progress"
	DeploymentGroupStatusDeploying  DeploymentGroupStatus = "deploying"
	DeploymentGroupStatusSucceeded  DeploymentGroupStatus = "succeeded"
	DeploymentGroupStatusFailed     DeploymentGroupStatus = "failed"
	DeploymentGroupStatusRolledBack DeploymentGroupStatus = "rolled_back"
)

// DeploymentGroupStrategy represents the deployment strategy
type DeploymentGroupStrategy string

const (
	DeploymentGroupStrategyParallel          DeploymentGroupStrategy = "parallel"
	DeploymentGroupStrategyDependencyOrdered DeploymentGroupStrategy = "dependency_ordered"
	DeploymentGroupStrategySequential        DeploymentGroupStrategy = "sequential"
)

// DeploymentGroup represents a coordinated multi-service deployment
type DeploymentGroup struct {
	ID            uuid.UUID               `json:"id"`
	ProjectID     uuid.UUID               `json:"project_id"`
	EnvironmentID uuid.UUID               `json:"environment_id"`
	Name          *string                 `json:"name,omitempty"`
	Status        DeploymentGroupStatus   `json:"status"`
	Strategy      DeploymentGroupStrategy `json:"strategy"`
	TriggeredBy   *string                 `json:"triggered_by,omitempty"`
	GitSHA        *string                 `json:"git_sha,omitempty"`
	PRURL         *string                 `json:"pr_url,omitempty"`
	StartedAt     *time.Time              `json:"started_at,omitempty"`
	CompletedAt   *time.Time              `json:"completed_at,omitempty"`
	ErrorMessage  *string                 `json:"error_message,omitempty"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
}

// DependencyType represents the type of service dependency
type DependencyType string

const (
	DependencyTypeRuntime DependencyType = "runtime"
	DependencyTypeBuild   DependencyType = "build"
	DependencyTypeData    DependencyType = "data"
)

// ServiceDependency represents a dependency between two services
type ServiceDependency struct {
	ID                 uuid.UUID      `json:"id"`
	ServiceID          uuid.UUID      `json:"service_id"`
	DependsOnServiceID uuid.UUID      `json:"depends_on_service_id"`
	DependencyType     DependencyType `json:"dependency_type"`
	CreatedAt          time.Time      `json:"created_at"`
}

// DeploymentGroupRepository handles deployment group CRUD operations
type DeploymentGroupRepository struct {
	db DBTX
}

// NewDeploymentGroupRepository creates a new DeploymentGroupRepository
func NewDeploymentGroupRepository(db DBTX) *DeploymentGroupRepository {
	return &DeploymentGroupRepository{db: db}
}

// NewDeploymentGroupRepositoryWithTx creates a repository using a transaction
func NewDeploymentGroupRepositoryWithTx(tx DBTX) *DeploymentGroupRepository {
	return &DeploymentGroupRepository{db: tx}
}

// Create inserts a new deployment group
func (r *DeploymentGroupRepository) Create(ctx context.Context, group *DeploymentGroup) error {
	group.ID = uuid.New()
	group.CreatedAt = time.Now()
	group.UpdatedAt = time.Now()
	if group.Status == "" {
		group.Status = DeploymentGroupStatusPending
	}
	if group.Strategy == "" {
		group.Strategy = DeploymentGroupStrategyDependencyOrdered
	}

	query := `
		INSERT INTO deployment_groups (
			id, project_id, environment_id, name, status, strategy,
			triggered_by, git_sha, pr_url, started_at, completed_at,
			error_message, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`

	_, err := r.db.ExecContext(ctx, query,
		group.ID, group.ProjectID, group.EnvironmentID, group.Name,
		group.Status, group.Strategy, group.TriggeredBy, group.GitSHA,
		group.PRURL, group.StartedAt, group.CompletedAt, group.ErrorMessage,
		group.CreatedAt, group.UpdatedAt,
	)
	return err
}

// GetByID retrieves a deployment group by ID
func (r *DeploymentGroupRepository) GetByID(ctx context.Context, id uuid.UUID) (*DeploymentGroup, error) {
	group := &DeploymentGroup{}
	var name, triggeredBy, gitSHA, prURL, errorMessage sql.NullString
	var startedAt, completedAt sql.NullTime

	query := `
		SELECT id, project_id, environment_id, name, status, strategy,
		       triggered_by, git_sha, pr_url, started_at, completed_at,
		       error_message, created_at, updated_at
		FROM deployment_groups
		WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&group.ID, &group.ProjectID, &group.EnvironmentID, &name,
		&group.Status, &group.Strategy, &triggeredBy, &gitSHA,
		&prURL, &startedAt, &completedAt, &errorMessage,
		&group.CreatedAt, &group.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if name.Valid {
		group.Name = &name.String
	}
	if triggeredBy.Valid {
		group.TriggeredBy = &triggeredBy.String
	}
	if gitSHA.Valid {
		group.GitSHA = &gitSHA.String
	}
	if prURL.Valid {
		group.PRURL = &prURL.String
	}
	if startedAt.Valid {
		group.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		group.CompletedAt = &completedAt.Time
	}
	if errorMessage.Valid {
		group.ErrorMessage = &errorMessage.String
	}

	return group, nil
}

// ListByProject retrieves deployment groups for a project
func (r *DeploymentGroupRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit, offset int) ([]*DeploymentGroup, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, project_id, environment_id, name, status, strategy,
		       triggered_by, git_sha, pr_url, started_at, completed_at,
		       error_message, created_at, updated_at
		FROM deployment_groups
		WHERE project_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, projectID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanGroups(rows)
}

// ListByProjectAndEnvironment retrieves deployment groups for a specific environment
func (r *DeploymentGroupRepository) ListByProjectAndEnvironment(ctx context.Context, projectID, environmentID uuid.UUID, limit, offset int) ([]*DeploymentGroup, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, project_id, environment_id, name, status, strategy,
		       triggered_by, git_sha, pr_url, started_at, completed_at,
		       error_message, created_at, updated_at
		FROM deployment_groups
		WHERE project_id = $1 AND environment_id = $2
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := r.db.QueryContext(ctx, query, projectID, environmentID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanGroups(rows)
}

// ListByStatus retrieves deployment groups with a specific status
func (r *DeploymentGroupRepository) ListByStatus(ctx context.Context, status DeploymentGroupStatus, limit int) ([]*DeploymentGroup, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, project_id, environment_id, name, status, strategy,
		       triggered_by, git_sha, pr_url, started_at, completed_at,
		       error_message, created_at, updated_at
		FROM deployment_groups
		WHERE status = $1
		ORDER BY created_at ASC
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, status, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanGroups(rows)
}

// UpdateStatus updates the status of a deployment group
func (r *DeploymentGroupRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status DeploymentGroupStatus, errorMsg *string) error {
	query := `
		UPDATE deployment_groups
		SET status = $1, error_message = $2, updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, status, errorMsg, id)
	return err
}

// UpdateStarted marks a deployment group as started
func (r *DeploymentGroupRepository) UpdateStarted(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE deployment_groups
		SET status = $1, started_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, DeploymentGroupStatusInProgress, id)
	return err
}

// UpdateCompleted marks a deployment group as completed
func (r *DeploymentGroupRepository) UpdateCompleted(ctx context.Context, id uuid.UUID, status DeploymentGroupStatus, errorMsg *string) error {
	query := `
		UPDATE deployment_groups
		SET status = $1, completed_at = NOW(), error_message = $2, updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, status, errorMsg, id)
	return err
}

// Delete removes a deployment group
func (r *DeploymentGroupRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM deployment_groups WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *DeploymentGroupRepository) scanGroups(rows *sql.Rows) ([]*DeploymentGroup, error) {
	var groups []*DeploymentGroup

	for rows.Next() {
		group := &DeploymentGroup{}
		var name, triggeredBy, gitSHA, prURL, errorMessage sql.NullString
		var startedAt, completedAt sql.NullTime

		err := rows.Scan(
			&group.ID, &group.ProjectID, &group.EnvironmentID, &name,
			&group.Status, &group.Strategy, &triggeredBy, &gitSHA,
			&prURL, &startedAt, &completedAt, &errorMessage,
			&group.CreatedAt, &group.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if name.Valid {
			group.Name = &name.String
		}
		if triggeredBy.Valid {
			group.TriggeredBy = &triggeredBy.String
		}
		if gitSHA.Valid {
			group.GitSHA = &gitSHA.String
		}
		if prURL.Valid {
			group.PRURL = &prURL.String
		}
		if startedAt.Valid {
			group.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			group.CompletedAt = &completedAt.Time
		}
		if errorMessage.Valid {
			group.ErrorMessage = &errorMessage.String
		}

		groups = append(groups, group)
	}

	return groups, nil
}

// ServiceDependencyRepository handles service dependency CRUD operations
type ServiceDependencyRepository struct {
	db DBTX
}

// NewServiceDependencyRepository creates a new ServiceDependencyRepository
func NewServiceDependencyRepository(db DBTX) *ServiceDependencyRepository {
	return &ServiceDependencyRepository{db: db}
}

// NewServiceDependencyRepositoryWithTx creates a repository using a transaction
func NewServiceDependencyRepositoryWithTx(tx DBTX) *ServiceDependencyRepository {
	return &ServiceDependencyRepository{db: tx}
}

// Create inserts a new service dependency
func (r *ServiceDependencyRepository) Create(ctx context.Context, dep *ServiceDependency) error {
	dep.ID = uuid.New()
	dep.CreatedAt = time.Now()
	if dep.DependencyType == "" {
		dep.DependencyType = DependencyTypeRuntime
	}

	query := `
		INSERT INTO service_dependencies (id, service_id, depends_on_service_id, dependency_type, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := r.db.ExecContext(ctx, query,
		dep.ID, dep.ServiceID, dep.DependsOnServiceID, dep.DependencyType, dep.CreatedAt,
	)
	return err
}

// GetByService retrieves all dependencies for a service
func (r *ServiceDependencyRepository) GetByService(ctx context.Context, serviceID uuid.UUID) ([]*ServiceDependency, error) {
	query := `
		SELECT id, service_id, depends_on_service_id, dependency_type, created_at
		FROM service_dependencies
		WHERE service_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanDependencies(rows)
}

// GetDependents retrieves all services that depend on a given service
func (r *ServiceDependencyRepository) GetDependents(ctx context.Context, serviceID uuid.UUID) ([]*ServiceDependency, error) {
	query := `
		SELECT id, service_id, depends_on_service_id, dependency_type, created_at
		FROM service_dependencies
		WHERE depends_on_service_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanDependencies(rows)
}

// Delete removes a dependency
func (r *ServiceDependencyRepository) Delete(ctx context.Context, serviceID, dependsOnID uuid.UUID) error {
	query := `DELETE FROM service_dependencies WHERE service_id = $1 AND depends_on_service_id = $2`
	_, err := r.db.ExecContext(ctx, query, serviceID, dependsOnID)
	return err
}

// DeleteByServiceID removes all dependencies for a service
func (r *ServiceDependencyRepository) DeleteByServiceID(ctx context.Context, serviceID uuid.UUID) error {
	query := `DELETE FROM service_dependencies WHERE service_id = $1 OR depends_on_service_id = $1`
	_, err := r.db.ExecContext(ctx, query, serviceID)
	return err
}

// GetProjectDependencyGraph returns the full dependency graph for a project
// Returns a map where key is service ID and value is a slice of service IDs it depends on
func (r *ServiceDependencyRepository) GetProjectDependencyGraph(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	query := `
		SELECT sd.service_id, sd.depends_on_service_id
		FROM service_dependencies sd
		INNER JOIN services s ON sd.service_id = s.id
		WHERE s.project_id = $1
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	graph := make(map[uuid.UUID][]uuid.UUID)
	for rows.Next() {
		var serviceID, dependsOnID uuid.UUID
		if err := rows.Scan(&serviceID, &dependsOnID); err != nil {
			return nil, err
		}
		graph[serviceID] = append(graph[serviceID], dependsOnID)
	}

	return graph, nil
}

// HasCycle checks if adding a dependency would create a cycle
func (r *ServiceDependencyRepository) HasCycle(ctx context.Context, serviceID, dependsOnID uuid.UUID) (bool, error) {
	// Get the project for the service
	var projectID uuid.UUID
	err := r.db.QueryRowContext(ctx, "SELECT project_id FROM services WHERE id = $1", serviceID).Scan(&projectID)
	if err != nil {
		return false, fmt.Errorf("failed to get project for service: %w", err)
	}

	// Get the full dependency graph
	graph, err := r.GetProjectDependencyGraph(ctx, projectID)
	if err != nil {
		return false, err
	}

	// Add the proposed edge
	graph[serviceID] = append(graph[serviceID], dependsOnID)

	// DFS to detect cycles
	visited := make(map[uuid.UUID]bool)
	recStack := make(map[uuid.UUID]bool)

	var hasCycleDFS func(node uuid.UUID) bool
	hasCycleDFS = func(node uuid.UUID) bool {
		visited[node] = true
		recStack[node] = true

		for _, neighbor := range graph[node] {
			if !visited[neighbor] {
				if hasCycleDFS(neighbor) {
					return true
				}
			} else if recStack[neighbor] {
				return true
			}
		}

		recStack[node] = false
		return false
	}

	// Check all nodes in graph
	for node := range graph {
		if !visited[node] {
			if hasCycleDFS(node) {
				return true, nil
			}
		}
	}

	return false, nil
}

func (r *ServiceDependencyRepository) scanDependencies(rows *sql.Rows) ([]*ServiceDependency, error) {
	var deps []*ServiceDependency

	for rows.Next() {
		dep := &ServiceDependency{}
		err := rows.Scan(
			&dep.ID, &dep.ServiceID, &dep.DependsOnServiceID,
			&dep.DependencyType, &dep.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}

	return deps, nil
}
