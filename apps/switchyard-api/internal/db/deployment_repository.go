package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// ErrVersionAllocationConflict is returned when the (service_id, version_number)
// UNIQUE constraint is violated during allocation. Callers should treat this as
// a transient error and retry the enclosing transaction. Per the P2.6 contract
// we never silently renumber or reuse: the conflict must bubble up to the app
// layer so the retry is observable.
var ErrVersionAllocationConflict = errors.New("deployment version allocation conflict")

// Create inserts a deployment and allocates a semantic version number.
//
// Allocation contract (P2.6):
//  1. Service ID must be set on the deployment (denormalized from releases).
//  2. version_number is set to MAX(version_number)+1 for that service in the
//     same SQL statement, so the read-compute-write is atomic per row.
//  3. The UNIQUE (service_id, version_number) index catches any concurrent
//     allocations that compute the same MAX value — we return
//     ErrVersionAllocationConflict so the caller can retry the enclosing
//     transaction (we do NOT silently retry-allocate at this layer).
//
// Callers should invoke Create inside r.WithTransaction when they want
// atomic "allocate + record audit" semantics. The serialization guarantee
// comes from the UNIQUE constraint, not from read locks.
func (r *DeploymentRepository) Create(deployment *types.Deployment) error {
	if deployment.ServiceID == nil {
		// Historical call sites that pre-date P2.6 may not set ServiceID.
		// We accept those at the storage layer (version_number stays NULL)
		// so the schema change is non-breaking during the rollout window.
		// New code paths MUST set ServiceID — the handlers enforce this.
		return r.createWithoutVersion(deployment)
	}
	return r.createWithVersion(deployment)
}

// createWithVersion performs the allocate-and-insert in a single SQL
// statement. The subquery reads MAX(version_number) for the target service;
// the UNIQUE index on (service_id, version_number) ensures that if two
// concurrent inserts observe the same MAX, exactly one succeeds and the
// other receives a unique-violation we surface as ErrVersionAllocationConflict.
func (r *DeploymentRepository) createWithVersion(deployment *types.Deployment) error {
	deployment.ID = uuid.New()
	deployment.CreatedAt = time.Now()
	deployment.UpdatedAt = time.Now()

	query := `
		INSERT INTO deployments (
			id, release_id, environment_id, service_id, version_number,
			replicas, status, health, created_at, updated_at
		)
		SELECT $1, $2, $3, $4,
		       COALESCE(MAX(d.version_number), 0) + 1,
		       $5, $6, $7, $8, $9
		  FROM deployments d
		 WHERE d.service_id = $4
		RETURNING version_number
	`
	var version int
	err := r.db.QueryRow(
		query,
		deployment.ID, deployment.ReleaseID, deployment.EnvironmentID,
		*deployment.ServiceID,
		deployment.Replicas, deployment.Status, deployment.Health,
		deployment.CreatedAt, deployment.UpdatedAt,
	).Scan(&version)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: service=%s", ErrVersionAllocationConflict, deployment.ServiceID)
		}
		return err
	}
	deployment.VersionNumber = &version
	return nil
}

// createWithoutVersion is the legacy path for call sites that haven't been
// updated to pass ServiceID. Allocates no version number.
func (r *DeploymentRepository) createWithoutVersion(deployment *types.Deployment) error {
	deployment.ID = uuid.New()
	deployment.CreatedAt = time.Now()
	deployment.UpdatedAt = time.Now()

	query := `
		INSERT INTO deployments (id, release_id, environment_id, replicas, status, health, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.Exec(query, deployment.ID, deployment.ReleaseID, deployment.EnvironmentID, deployment.Replicas, deployment.Status, deployment.Health, deployment.CreatedAt, deployment.UpdatedAt)
	return err
}

// isUniqueViolation inspects the database error for a Postgres unique-violation
// (SQLSTATE 23505) on the deployment allocation index. We match on the error
// message to avoid a hard dependency on the pgx driver's typed errors — the
// codebase uses database/sql + lib/pq and sqlmock in tests.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// lib/pq surfaces this as `pq: duplicate key value violates unique constraint "idx_deployments_service_version"`.
	// Matching the constraint name keeps the check specific to our
	// allocation path and avoids false positives from other UNIQUE indexes.
	if strings.Contains(msg, "idx_deployments_service_version") {
		return true
	}
	return strings.Contains(msg, "23505") &&
		strings.Contains(msg, "service_id") &&
		strings.Contains(msg, "version_number")
}

// sqlErrNoRows re-exported for external consumers that want to branch on
// the "not found" sentinel without importing database/sql directly.
var sqlErrNoRows = sql.ErrNoRows

func (r *DeploymentRepository) UpdateStatus(id uuid.UUID, status types.DeploymentStatus, health types.HealthStatus) error {
	query := `UPDATE deployments SET status = $1, health = $2, error_message = NULL, updated_at = NOW() WHERE id = $3`
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
	query := `SELECT id, release_id, environment_id, replicas, status, health, error_message,
	                 service_id, version_number, created_at, updated_at
	          FROM deployments WHERE id = $1`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
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

// GetByServiceAndVersion resolves a deployment by (service_id, version_number).
// Returns sql.ErrNoRows if no match exists — callers should surface this as a
// 404 at the API boundary.
func (r *DeploymentRepository) GetByServiceAndVersion(ctx context.Context, serviceID uuid.UUID, version int) (*types.Deployment, error) {
	deployment := &types.Deployment{}
	query := `SELECT id, release_id, environment_id, replicas, status, health, error_message,
	                 service_id, version_number, created_at, updated_at
	          FROM deployments
	          WHERE service_id = $1 AND version_number = $2`

	err := r.db.QueryRowContext(ctx, query, serviceID, version).Scan(
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

func (r *DeploymentRepository) ListByRelease(ctx context.Context, releaseID string) ([]*types.Deployment, error) {
	query := `SELECT id, release_id, environment_id, replicas, status, health, error_message,
	                 service_id, version_number, created_at, updated_at
	          FROM deployments WHERE release_id = $1 ORDER BY created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, releaseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deployments []*types.Deployment
	for rows.Next() {
		deployment := &types.Deployment{}
		err := rows.Scan(
			&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
			&deployment.Replicas, &deployment.Status, &deployment.Health,
			&deployment.ErrorMessage,
			&deployment.ServiceID, &deployment.VersionNumber,
			&deployment.CreatedAt, &deployment.UpdatedAt,
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
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health, d.error_message,
		       d.service_id, d.version_number, d.created_at, d.updated_at
		FROM deployments d
		JOIN releases r ON d.release_id = r.id
		WHERE r.service_id = $1
		ORDER BY d.created_at DESC
		LIMIT 1
	`

	err := r.db.QueryRowContext(ctx, query, serviceID).Scan(
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

func (r *DeploymentRepository) GetByStatus(ctx context.Context, status types.DeploymentStatus) ([]*types.Deployment, error) {
	// Note: group_id and deploy_order columns don't exist in the database yet
	// They're part of the deployment group feature that hasn't been migrated
	query := `SELECT id, release_id, environment_id, replicas, status, health, error_message,
	                 service_id, version_number, created_at, updated_at
	          FROM deployments WHERE status = $1 ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deployments []*types.Deployment
	for rows.Next() {
		deployment := &types.Deployment{}
		err := rows.Scan(
			&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
			&deployment.Replicas, &deployment.Status, &deployment.Health,
			&deployment.ErrorMessage,
			&deployment.ServiceID, &deployment.VersionNumber,
			&deployment.CreatedAt, &deployment.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		// GroupID and DeployOrder default to nil/0 until feature is migrated
		deployments = append(deployments, deployment)
	}

	return deployments, nil
}

// GetByServiceSince returns deployments for a service created after a given time, ordered by created_at ASC.
func (r *DeploymentRepository) GetByServiceSince(ctx context.Context, serviceID string, since time.Time) ([]*types.Deployment, error) {
	query := `SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health, d.error_message,
	                 d.service_id, d.version_number, d.created_at, d.updated_at
	          FROM deployments d
	          JOIN releases r ON d.release_id = r.id
	          WHERE r.service_id = $1 AND d.created_at >= $2
	          ORDER BY d.created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, serviceID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deployments []*types.Deployment
	for rows.Next() {
		deployment := &types.Deployment{}
		err := rows.Scan(
			&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
			&deployment.Replicas, &deployment.Status, &deployment.Health,
			&deployment.ErrorMessage,
			&deployment.ServiceID, &deployment.VersionNumber,
			&deployment.CreatedAt, &deployment.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
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
		query = `SELECT id, release_id, environment_id, replicas, status, health, error_message,
		                service_id, version_number, created_at, updated_at
		         FROM deployments WHERE created_at >= $1 ORDER BY created_at DESC LIMIT $2`
		args = []interface{}{*since, limit}
	} else {
		query = `SELECT id, release_id, environment_id, replicas, status, health, error_message,
		                service_id, version_number, created_at, updated_at
		         FROM deployments ORDER BY created_at DESC LIMIT $1`
		args = []interface{}{limit}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deployments []*types.Deployment
	for rows.Next() {
		deployment := &types.Deployment{}
		err := rows.Scan(
			&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
			&deployment.Replicas, &deployment.Status, &deployment.Health,
			&deployment.ErrorMessage,
			&deployment.ServiceID, &deployment.VersionNumber,
			&deployment.CreatedAt, &deployment.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, deployment)
	}

	return deployments, nil
}

// ListAllEnriched retrieves all deployments with joined release and service data
func (r *DeploymentRepository) ListAllEnriched(ctx context.Context, since *time.Time, limit int) ([]*types.DeploymentEnriched, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	baseQuery := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health,
		       d.error_message,
		       d.version_number,
		       d.created_at, d.updated_at,
		       COALESCE(s.id, '00000000-0000-0000-0000-000000000000') as service_id,
		       COALESCE(s.name, '') as service_name,
		       COALESCE(r.git_sha, '') as git_sha,
		       COALESCE(r.git_branch, '') as git_branch,
		       COALESCE(r.commit_message, '') as commit_message,
		       COALESCE(r.commit_author_name, '') as commit_author,
		       COALESCE(r.commit_author_email, '') as commit_author_email,
		       r.pr_number,
		       COALESCE(r.pr_title, '') as pr_title,
		       COALESCE(r.pr_url, '') as pr_url,
		       COALESCE(r.repo_url, '') as repo_url
		FROM deployments d
		LEFT JOIN releases r ON d.release_id = r.id
		LEFT JOIN services s ON r.service_id = s.id`

	var query string
	var args []interface{}

	if since != nil {
		query = baseQuery + ` WHERE d.created_at >= $1 ORDER BY d.created_at DESC LIMIT $2`
		args = []interface{}{*since, limit}
	} else {
		query = baseQuery + ` ORDER BY d.created_at DESC LIMIT $1`
		args = []interface{}{limit}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deployments []*types.DeploymentEnriched
	for rows.Next() {
		d := &types.DeploymentEnriched{}
		var prNumber *int
		err := rows.Scan(
			&d.ID, &d.ReleaseID, &d.EnvironmentID, &d.Replicas, &d.Status, &d.Health,
			&d.ErrorMessage,
			&d.VersionNumber,
			&d.CreatedAt, &d.UpdatedAt,
			&d.ServiceID, &d.ServiceName, &d.GitSHA, &d.GitBranch, &d.CommitMessage,
			&d.CommitAuthor, &d.CommitAuthorEmail,
			&prNumber, &d.PRTitle, &d.PRURL, &d.RepoURL,
		)
		if err != nil {
			return nil, err
		}
		d.PRNumber = prNumber
		// Denormalized ServiceID on the base struct mirrors the joined
		// services.id so downstream code can trust Deployment.ServiceID
		// without a separate lookup.
		sid := d.ServiceID
		d.Deployment.ServiceID = &sid
		deployments = append(deployments, d)
	}

	return deployments, nil
}

// ListAllEnrichedByTeam mirrors ListAllEnriched but adds a project-team filter:
// only deployments whose owning service's project belongs to the given team
// are returned. Used by the master-admin tenant-filter middleware (XC-2
// Round 5) when an acting-as session scopes /v1/deployments to a single
// tenant. Deployments orphaned from a service (LEFT JOIN matching no row) are
// excluded — without a service we can't resolve a team, and surfacing them
// to a master admin acting-as a tenant would defeat the impersonation guard.
func (r *DeploymentRepository) ListAllEnrichedByTeam(ctx context.Context, teamID uuid.UUID, since *time.Time, limit int) ([]*types.DeploymentEnriched, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	// Inner JOINs on releases + services + projects: scoping to a team
	// requires every deployment to resolve to a project, so the LEFT JOINs
	// from the unscoped path don't make sense here.
	baseQuery := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health,
		       d.error_message,
		       d.version_number,
		       d.created_at, d.updated_at,
		       s.id as service_id,
		       COALESCE(s.name, '') as service_name,
		       COALESCE(r.git_sha, '') as git_sha,
		       COALESCE(r.git_branch, '') as git_branch,
		       COALESCE(r.commit_message, '') as commit_message,
		       COALESCE(r.commit_author_name, '') as commit_author,
		       COALESCE(r.commit_author_email, '') as commit_author_email,
		       r.pr_number,
		       COALESCE(r.pr_title, '') as pr_title,
		       COALESCE(r.pr_url, '') as pr_url,
		       COALESCE(r.repo_url, '') as repo_url
		FROM deployments d
		JOIN releases r ON d.release_id = r.id
		JOIN services s ON r.service_id = s.id
		JOIN projects p ON p.id = s.project_id
		WHERE p.team_id = $1`

	var query string
	var args []interface{}

	if since != nil {
		query = baseQuery + ` AND d.created_at >= $2 ORDER BY d.created_at DESC LIMIT $3`
		args = []interface{}{teamID, *since, limit}
	} else {
		query = baseQuery + ` ORDER BY d.created_at DESC LIMIT $2`
		args = []interface{}{teamID, limit}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deployments []*types.DeploymentEnriched
	for rows.Next() {
		d := &types.DeploymentEnriched{}
		var prNumber *int
		err := rows.Scan(
			&d.ID, &d.ReleaseID, &d.EnvironmentID, &d.Replicas, &d.Status, &d.Health,
			&d.ErrorMessage,
			&d.VersionNumber,
			&d.CreatedAt, &d.UpdatedAt,
			&d.ServiceID, &d.ServiceName, &d.GitSHA, &d.GitBranch, &d.CommitMessage,
			&d.CommitAuthor, &d.CommitAuthorEmail,
			&prNumber, &d.PRTitle, &d.PRURL, &d.RepoURL,
		)
		if err != nil {
			return nil, err
		}
		d.PRNumber = prNumber
		sid := d.ServiceID
		d.Deployment.ServiceID = &sid
		deployments = append(deployments, d)
	}

	return deployments, rows.Err()
}

// ListAllEnrichedForUser returns deployments for projects the user can access.
func (r *DeploymentRepository) ListAllEnrichedForUser(ctx context.Context, userID uuid.UUID, since *time.Time, limit int) ([]*types.DeploymentEnriched, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	baseQuery := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health,
		       d.error_message,
		       d.version_number,
		       d.created_at, d.updated_at,
		       s.id as service_id,
		       COALESCE(s.name, '') as service_name,
		       COALESCE(r.git_sha, '') as git_sha,
		       COALESCE(r.git_branch, '') as git_branch,
		       COALESCE(r.commit_message, '') as commit_message,
		       COALESCE(r.commit_author_name, '') as commit_author,
		       COALESCE(r.commit_author_email, '') as commit_author_email,
		       r.pr_number,
		       COALESCE(r.pr_title, '') as pr_title,
		       COALESCE(r.pr_url, '') as pr_url,
		       COALESCE(r.repo_url, '') as repo_url
		FROM deployments d
		JOIN releases r ON d.release_id = r.id
		JOIN services s ON r.service_id = s.id
		JOIN projects p ON p.id = s.project_id
		JOIN project_access pa ON pa.project_id = p.id AND pa.user_id = $1`

	var query string
	var args []interface{}

	if since != nil {
		query = baseQuery + ` AND d.created_at >= $2 ORDER BY d.created_at DESC LIMIT $3`
		args = []interface{}{userID, *since, limit}
	} else {
		query = baseQuery + ` ORDER BY d.created_at DESC LIMIT $2`
		args = []interface{}{userID, limit}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deployments []*types.DeploymentEnriched
	for rows.Next() {
		d := &types.DeploymentEnriched{}
		var prNumber *int
		err := rows.Scan(
			&d.ID, &d.ReleaseID, &d.EnvironmentID, &d.Replicas, &d.Status, &d.Health,
			&d.ErrorMessage,
			&d.VersionNumber,
			&d.CreatedAt, &d.UpdatedAt,
			&d.ServiceID, &d.ServiceName, &d.GitSHA, &d.GitBranch, &d.CommitMessage,
			&d.CommitAuthor, &d.CommitAuthorEmail,
			&prNumber, &d.PRTitle, &d.PRURL, &d.RepoURL,
		)
		if err != nil {
			return nil, err
		}
		d.PRNumber = prNumber
		sid := d.ServiceID
		d.Deployment.ServiceID = &sid
		deployments = append(deployments, d)
	}

	return deployments, rows.Err()
}

// GetByIDEnriched retrieves a single deployment with joined release and service data
func (r *DeploymentRepository) GetByIDEnriched(ctx context.Context, id string) (*types.DeploymentEnriched, error) {
	query := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health,
		       d.error_message,
		       d.version_number,
		       d.created_at, d.updated_at,
		       COALESCE(s.id, '00000000-0000-0000-0000-000000000000') as service_id,
		       COALESCE(s.name, '') as service_name,
		       COALESCE(r.git_sha, '') as git_sha,
		       COALESCE(r.git_branch, '') as git_branch,
		       COALESCE(r.commit_message, '') as commit_message,
		       COALESCE(r.commit_author_name, '') as commit_author,
		       COALESCE(r.commit_author_email, '') as commit_author_email,
		       r.pr_number,
		       COALESCE(r.pr_title, '') as pr_title,
		       COALESCE(r.pr_url, '') as pr_url,
		       COALESCE(r.repo_url, '') as repo_url
		FROM deployments d
		LEFT JOIN releases r ON d.release_id = r.id
		LEFT JOIN services s ON r.service_id = s.id
		WHERE d.id = $1`

	d := &types.DeploymentEnriched{}
	var prNumber *int
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&d.ID, &d.ReleaseID, &d.EnvironmentID, &d.Replicas, &d.Status, &d.Health,
		&d.ErrorMessage,
		&d.VersionNumber,
		&d.CreatedAt, &d.UpdatedAt,
		&d.ServiceID, &d.ServiceName, &d.GitSHA, &d.GitBranch, &d.CommitMessage,
		&d.CommitAuthor, &d.CommitAuthorEmail,
		&prNumber, &d.PRTitle, &d.PRURL, &d.RepoURL,
	)
	if err != nil {
		return nil, err
	}
	d.PRNumber = prNumber
	// Mirror the joined service_id onto the embedded Deployment so the
	// pointer field is populated consistently for downstream consumers.
	sid := d.ServiceID
	d.Deployment.ServiceID = &sid

	return d, nil
}

// FindDeployingByServiceAndSHA finds the most recent deployment with status 'deploying'
// for a given service and git SHA. Returns nil, nil if no match is found.
func (r *DeploymentRepository) FindDeployingByServiceAndSHA(ctx context.Context, serviceID uuid.UUID, gitSHA string) (*types.Deployment, error) {
	deployment := &types.Deployment{}
	query := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health,
		       d.error_message, d.service_id, d.version_number, d.created_at, d.updated_at
		FROM deployments d
		JOIN releases r ON d.release_id = r.id
		WHERE r.service_id = $1 AND r.git_sha = $2 AND d.status = 'deploying'
		ORDER BY d.created_at DESC
		LIMIT 1
	`
	err := r.db.QueryRowContext(ctx, query, serviceID, gitSHA).Scan(
		&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
		&deployment.Replicas, &deployment.Status, &deployment.Health,
		&deployment.ErrorMessage,
		&deployment.ServiceID, &deployment.VersionNumber,
		&deployment.CreatedAt, &deployment.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return deployment, nil
}

// FindDeployingByServiceAndReleaseSHA finds the most recent deployment with status 'deploying'
// for a given service where the associated release has the specified git SHA.
// This bridges the SHA mismatch between CI push (SHA=A) and digest-commit (SHA=B).
// Returns nil, nil if no match is found.
func (r *DeploymentRepository) FindDeployingByServiceAndReleaseSHA(ctx context.Context, serviceID uuid.UUID, gitSHA string) (*types.Deployment, error) {
	deployment := &types.Deployment{}
	query := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health,
		       d.error_message, d.service_id, d.version_number, d.created_at, d.updated_at
		FROM deployments d
		JOIN releases r ON d.release_id = r.id
		WHERE r.service_id = $1 AND r.git_sha = $2 AND d.status = 'deploying'
		ORDER BY d.created_at DESC
		LIMIT 1
	`
	err := r.db.QueryRowContext(ctx, query, serviceID, gitSHA).Scan(
		&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
		&deployment.Replicas, &deployment.Status, &deployment.Health,
		&deployment.ErrorMessage,
		&deployment.ServiceID, &deployment.VersionNumber,
		&deployment.CreatedAt, &deployment.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return deployment, nil
}

// FindRecentDeployingByService finds the most recent deployment with status 'deploying'
// for a given service within a time window. Used as a fallback when the ArgoCD sync SHA
// differs from the CI push SHA (e.g., due to digest-commit creating a new commit).
// Returns nil, nil if no match is found.
func (r *DeploymentRepository) FindRecentDeployingByService(ctx context.Context, serviceID uuid.UUID, window time.Duration) (*types.Deployment, error) {
	deployment := &types.Deployment{}
	query := `
		SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health,
		       d.error_message, d.service_id, d.version_number, d.created_at, d.updated_at
		FROM deployments d
		JOIN releases r ON d.release_id = r.id
		WHERE r.service_id = $1 AND d.status = 'deploying' AND d.created_at >= $2
		ORDER BY d.created_at DESC
		LIMIT 1
	`
	cutoff := time.Now().Add(-window)
	err := r.db.QueryRowContext(ctx, query, serviceID, cutoff).Scan(
		&deployment.ID, &deployment.ReleaseID, &deployment.EnvironmentID,
		&deployment.Replicas, &deployment.Status, &deployment.Health,
		&deployment.ErrorMessage,
		&deployment.ServiceID, &deployment.VersionNumber,
		&deployment.CreatedAt, &deployment.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	return deployment, nil
}

// CleanupStaleDeploying marks orphaned "deploying" records as "failed" for a given service.
// This handles race conditions where the CI creates a deploying record but no ArgoCD sync
// ever arrives (e.g., sync failure, manifest error, or digest never committed).
func (r *DeploymentRepository) CleanupStaleDeploying(ctx context.Context, serviceID uuid.UUID, maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	_, err := r.db.ExecContext(ctx, `
		UPDATE deployments SET status = 'failed',
		       error_message = 'Deployment timed out (no sync received within 30 minutes)',
		       updated_at = NOW()
		WHERE id IN (
			SELECT d.id FROM deployments d
			JOIN releases r ON d.release_id = r.id
			WHERE r.service_id = $1
			AND d.status = 'deploying'
			AND d.created_at < $2
		)`, serviceID, cutoff)
	return err
}

// CleanupAllStaleDeploying marks orphaned "deploying" records as "failed" across ALL services.
// Called periodically by the reconciler to catch deployments that never received an ArgoCD sync.
func (r *DeploymentRepository) CleanupAllStaleDeploying(ctx context.Context, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge)
	result, err := r.db.ExecContext(ctx, `
		UPDATE deployments SET status = 'failed',
		       error_message = 'Deployment timed out (no sync received within 30 minutes)',
		       updated_at = NOW()
		WHERE status = 'deploying'
		AND created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// ListByGroup retrieves all deployments for a deployment group
// Note: This feature is not yet implemented - group_id and deploy_order columns
// don't exist in the database. Returns empty slice until feature is migrated.
func (r *DeploymentRepository) ListByGroup(ctx context.Context, groupID uuid.UUID) ([]*types.Deployment, error) {
	// Deployment groups feature not yet migrated - return empty slice
	return []*types.Deployment{}, nil
}
