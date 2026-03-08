package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CIRunRepository handles CI run CRUD operations for GitHub Actions tracking
type CIRunRepository struct {
	db DBTX
}

func NewCIRunRepository(db *sql.DB) *CIRunRepository {
	return &CIRunRepository{db: db}
}

func NewCIRunRepositoryWithTx(tx *sql.Tx) *CIRunRepository {
	return &CIRunRepository{db: tx}
}

// Create creates a new CI run record
func (r *CIRunRepository) Create(ctx context.Context, run *types.CIRun) error {
	run.ID = uuid.New()
	run.CreatedAt = time.Now()
	run.UpdatedAt = time.Now()

	query := `
		INSERT INTO ci_runs (id, service_id, commit_sha, workflow_name, workflow_id, run_id, run_number,
			status, conclusion, html_url, branch, event_type, actor, started_at, completed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`

	_, err := r.db.ExecContext(ctx, query,
		run.ID, run.ServiceID, run.CommitSHA, run.WorkflowName, run.WorkflowID, run.RunID, run.RunNumber,
		run.Status, run.Conclusion, run.HTMLURL, run.Branch, run.EventType, run.Actor,
		run.StartedAt, run.CompletedAt, run.CreatedAt, run.UpdatedAt,
	)
	return err
}

// GetByRunID retrieves a CI run by its GitHub run ID
func (r *CIRunRepository) GetByRunID(ctx context.Context, runID int64) (*types.CIRun, error) {
	run := &types.CIRun{}
	query := `
		SELECT id, service_id, commit_sha, workflow_name, workflow_id, run_id, run_number,
			status, conclusion, html_url, branch, event_type, actor, started_at, completed_at, created_at, updated_at
		FROM ci_runs WHERE run_id = $1
	`

	var conclusion sql.NullString
	var htmlURL, branch, eventType, actor sql.NullString
	var startedAt, completedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, query, runID).Scan(
		&run.ID, &run.ServiceID, &run.CommitSHA, &run.WorkflowName, &run.WorkflowID, &run.RunID, &run.RunNumber,
		&run.Status, &conclusion, &htmlURL, &branch, &eventType, &actor,
		&startedAt, &completedAt, &run.CreatedAt, &run.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if conclusion.Valid {
		c := types.CIRunConclusion(conclusion.String)
		run.Conclusion = &c
	}
	if htmlURL.Valid {
		run.HTMLURL = htmlURL.String
	}
	if branch.Valid {
		run.Branch = branch.String
	}
	if eventType.Valid {
		run.EventType = eventType.String
	}
	if actor.Valid {
		run.Actor = actor.String
	}
	if startedAt.Valid {
		run.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}

	return run, nil
}

// UpdateStatus updates the status and conclusion of a CI run
func (r *CIRunRepository) UpdateStatus(ctx context.Context, runID int64, status types.CIRunStatus, conclusion *types.CIRunConclusion, completedAt *time.Time) error {
	query := `
		UPDATE ci_runs
		SET status = $1, conclusion = $2, completed_at = $3, updated_at = NOW()
		WHERE run_id = $4
	`
	_, err := r.db.ExecContext(ctx, query, status, conclusion, completedAt, runID)
	return err
}

// Upsert creates or updates a CI run based on run_id
func (r *CIRunRepository) Upsert(ctx context.Context, run *types.CIRun) error {
	query := `
		INSERT INTO ci_runs (id, service_id, commit_sha, workflow_name, workflow_id, run_id, run_number,
			status, conclusion, html_url, branch, event_type, actor, started_at, completed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (run_id) DO UPDATE SET
			status = EXCLUDED.status,
			conclusion = EXCLUDED.conclusion,
			completed_at = EXCLUDED.completed_at,
			updated_at = NOW()
	`

	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now()
	}
	run.UpdatedAt = time.Now()

	_, err := r.db.ExecContext(ctx, query,
		run.ID, run.ServiceID, run.CommitSHA, run.WorkflowName, run.WorkflowID, run.RunID, run.RunNumber,
		run.Status, run.Conclusion, run.HTMLURL, run.Branch, run.EventType, run.Actor,
		run.StartedAt, run.CompletedAt, run.CreatedAt, run.UpdatedAt,
	)
	return err
}

// ListByCommitSHA retrieves all CI runs for a commit SHA
func (r *CIRunRepository) ListByCommitSHA(ctx context.Context, commitSHA string) ([]*types.CIRun, error) {
	query := `
		SELECT id, service_id, commit_sha, workflow_name, workflow_id, run_id, run_number,
			status, conclusion, html_url, branch, event_type, actor, started_at, completed_at, created_at, updated_at
		FROM ci_runs WHERE commit_sha = $1 ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, commitSHA)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var runs []*types.CIRun
	for rows.Next() {
		run := &types.CIRun{}
		var conclusion sql.NullString
		var htmlURL, branch, eventType, actor sql.NullString
		var startedAt, completedAt sql.NullTime

		err := rows.Scan(
			&run.ID, &run.ServiceID, &run.CommitSHA, &run.WorkflowName, &run.WorkflowID, &run.RunID, &run.RunNumber,
			&run.Status, &conclusion, &htmlURL, &branch, &eventType, &actor,
			&startedAt, &completedAt, &run.CreatedAt, &run.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if conclusion.Valid {
			c := types.CIRunConclusion(conclusion.String)
			run.Conclusion = &c
		}
		if htmlURL.Valid {
			run.HTMLURL = htmlURL.String
		}
		if branch.Valid {
			run.Branch = branch.String
		}
		if eventType.Valid {
			run.EventType = eventType.String
		}
		if actor.Valid {
			run.Actor = actor.String
		}
		if startedAt.Valid {
			run.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}

		runs = append(runs, run)
	}

	return runs, nil
}

// ListByServiceAndCommit retrieves CI runs for a specific service and commit
func (r *CIRunRepository) ListByServiceAndCommit(ctx context.Context, serviceID uuid.UUID, commitSHA string) ([]*types.CIRun, error) {
	query := `
		SELECT id, service_id, commit_sha, workflow_name, workflow_id, run_id, run_number,
			status, conclusion, html_url, branch, event_type, actor, started_at, completed_at, created_at, updated_at
		FROM ci_runs WHERE service_id = $1 AND commit_sha = $2 ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, serviceID, commitSHA)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var runs []*types.CIRun
	for rows.Next() {
		run := &types.CIRun{}
		var conclusion sql.NullString
		var htmlURL, branch, eventType, actor sql.NullString
		var startedAt, completedAt sql.NullTime

		err := rows.Scan(
			&run.ID, &run.ServiceID, &run.CommitSHA, &run.WorkflowName, &run.WorkflowID, &run.RunID, &run.RunNumber,
			&run.Status, &conclusion, &htmlURL, &branch, &eventType, &actor,
			&startedAt, &completedAt, &run.CreatedAt, &run.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if conclusion.Valid {
			c := types.CIRunConclusion(conclusion.String)
			run.Conclusion = &c
		}
		if htmlURL.Valid {
			run.HTMLURL = htmlURL.String
		}
		if branch.Valid {
			run.Branch = branch.String
		}
		if eventType.Valid {
			run.EventType = eventType.String
		}
		if actor.Valid {
			run.Actor = actor.String
		}
		if startedAt.Valid {
			run.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}

		runs = append(runs, run)
	}

	return runs, nil
}
