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

// LifecycleEventRepository handles deployment lifecycle event CRUD operations
type LifecycleEventRepository struct {
	db DBTX
}

func NewLifecycleEventRepository(db *sql.DB) *LifecycleEventRepository {
	return &LifecycleEventRepository{db: db}
}

func NewLifecycleEventRepositoryWithTx(tx *sql.Tx) *LifecycleEventRepository {
	return &LifecycleEventRepository{db: tx}
}

// Create inserts a new lifecycle event
func (r *LifecycleEventRepository) Create(ctx context.Context, event *types.DeploymentLifecycleEvent) error {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}

	query := `
		INSERT INTO deployment_lifecycle_events (
			id, deployment_id, release_id, ci_run_id, project_id, service_id,
			repo_full_name, commit_sha, branch, ref, target_env,
			event_type, source, message, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err = r.db.ExecContext(ctx, query,
		event.ID, event.DeploymentID, event.ReleaseID, event.CIRunID,
		event.ProjectID, event.ServiceID,
		event.RepoFullName, event.CommitSHA, event.Branch, event.Ref, event.TargetEnv,
		event.EventType, event.Source, event.Message, metadataJSON, event.CreatedAt,
	)
	return err
}

// GetByCommit retrieves all lifecycle events for a commit SHA
func (r *LifecycleEventRepository) GetByCommit(ctx context.Context, sha string) ([]types.DeploymentLifecycleEvent, error) {
	query := `
		SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id,
			repo_full_name, commit_sha, branch, ref, target_env,
			event_type, source, message, metadata, created_at
		FROM deployment_lifecycle_events
		WHERE commit_sha = $1
		ORDER BY created_at DESC
	`
	return r.scanEvents(ctx, query, sha)
}

// GetByBranch retrieves events for a specific repo + branch
func (r *LifecycleEventRepository) GetByBranch(ctx context.Context, repoFullName, branch string, limit int) ([]types.DeploymentLifecycleEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `
		SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id,
			repo_full_name, commit_sha, branch, ref, target_env,
			event_type, source, message, metadata, created_at
		FROM deployment_lifecycle_events
		WHERE repo_full_name = $1 AND branch = $2
		ORDER BY created_at DESC
		LIMIT $3
	`
	return r.scanEvents(ctx, query, repoFullName, branch, limit)
}

// GetTimeline retrieves events matching a flexible query
func (r *LifecycleEventRepository) GetTimeline(ctx context.Context, q types.LifecycleTimelineQuery) ([]types.DeploymentLifecycleEvent, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}

	var conditions []string
	var args []interface{}
	argN := 1

	if q.RepoFullName != nil {
		conditions = append(conditions, fmt.Sprintf("repo_full_name = $%d", argN))
		args = append(args, *q.RepoFullName)
		argN++
	}
	if q.Branch != nil {
		conditions = append(conditions, fmt.Sprintf("branch = $%d", argN))
		args = append(args, *q.Branch)
		argN++
	}
	if q.CommitSHA != nil {
		conditions = append(conditions, fmt.Sprintf("commit_sha = $%d", argN))
		args = append(args, *q.CommitSHA)
		argN++
	}
	if q.ProjectID != nil {
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argN))
		args = append(args, *q.ProjectID)
		argN++
	}
	if q.TargetEnv != nil {
		conditions = append(conditions, fmt.Sprintf("target_env = $%d", argN))
		args = append(args, *q.TargetEnv)
		argN++
	}
	if len(q.EventTypes) > 0 {
		placeholders := make([]string, len(q.EventTypes))
		for i, et := range q.EventTypes {
			placeholders[i] = fmt.Sprintf("$%d", argN)
			args = append(args, et)
			argN++
		}
		conditions = append(conditions, fmt.Sprintf("event_type IN (%s)", strings.Join(placeholders, ",")))
	}
	if q.Since != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argN))
		args = append(args, *q.Since)
		argN++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id,
			repo_full_name, commit_sha, branch, ref, target_env,
			event_type, source, message, metadata, created_at
		FROM deployment_lifecycle_events
		%s
		ORDER BY created_at DESC
		LIMIT $%d
	`, whereClause, argN)
	args = append(args, q.Limit)

	return r.scanEvents(ctx, query, args...)
}

// GetLatestByRepo retrieves the most recent event for a repo
func (r *LifecycleEventRepository) GetLatestByRepo(ctx context.Context, repoFullName string) (*types.DeploymentLifecycleEvent, error) {
	query := `
		SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id,
			repo_full_name, commit_sha, branch, ref, target_env,
			event_type, source, message, metadata, created_at
		FROM deployment_lifecycle_events
		WHERE repo_full_name = $1
		ORDER BY created_at DESC
		LIMIT 1
	`
	events, err := r.scanEvents(ctx, query, repoFullName)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, sql.ErrNoRows
	}
	return &events[0], nil
}

// GetLatestByBranch retrieves the most recent event for a specific branch
func (r *LifecycleEventRepository) GetLatestByBranch(ctx context.Context, repoFullName, branch string) (*types.DeploymentLifecycleEvent, error) {
	query := `
		SELECT id, deployment_id, release_id, ci_run_id, project_id, service_id,
			repo_full_name, commit_sha, branch, ref, target_env,
			event_type, source, message, metadata, created_at
		FROM deployment_lifecycle_events
		WHERE repo_full_name = $1 AND branch = $2
		ORDER BY created_at DESC
		LIMIT 1
	`
	events, err := r.scanEvents(ctx, query, repoFullName, branch)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, sql.ErrNoRows
	}
	return &events[0], nil
}

// UpdateMetadata updates the metadata JSON field for an existing lifecycle event
func (r *LifecycleEventRepository) UpdateMetadata(ctx context.Context, id uuid.UUID, metadata map[string]interface{}) error {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `UPDATE deployment_lifecycle_events SET metadata = $1 WHERE id = $2`, metadataJSON, id)
	return err
}

// scanEvents is a shared helper to scan event rows
func (r *LifecycleEventRepository) scanEvents(ctx context.Context, query string, args ...interface{}) ([]types.DeploymentLifecycleEvent, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []types.DeploymentLifecycleEvent
	for rows.Next() {
		var e types.DeploymentLifecycleEvent
		var deploymentID, releaseID, ciRunID, projectID, serviceID sql.NullString
		var targetEnv, message sql.NullString
		var metadataJSON []byte

		err := rows.Scan(
			&e.ID, &deploymentID, &releaseID, &ciRunID, &projectID, &serviceID,
			&e.RepoFullName, &e.CommitSHA, &e.Branch, &e.Ref, &targetEnv,
			&e.EventType, &e.Source, &message, &metadataJSON, &e.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if deploymentID.Valid {
			id, _ := uuid.Parse(deploymentID.String)
			e.DeploymentID = &id
		}
		if releaseID.Valid {
			id, _ := uuid.Parse(releaseID.String)
			e.ReleaseID = &id
		}
		if ciRunID.Valid {
			id, _ := uuid.Parse(ciRunID.String)
			e.CIRunID = &id
		}
		if projectID.Valid {
			id, _ := uuid.Parse(projectID.String)
			e.ProjectID = &id
		}
		if serviceID.Valid {
			id, _ := uuid.Parse(serviceID.String)
			e.ServiceID = &id
		}
		if targetEnv.Valid {
			e.TargetEnv = &targetEnv.String
		}
		if message.Valid {
			e.Message = &message.String
		}
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &e.Metadata)
		}

		events = append(events, e)
	}

	return events, rows.Err()
}
