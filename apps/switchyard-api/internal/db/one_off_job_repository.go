package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// OneOffJobRepository handles database operations for one-off jobs
type OneOffJobRepository struct {
	db DBTX
}

// NewOneOffJobRepository creates a new one-off job repository
func NewOneOffJobRepository(db DBTX) *OneOffJobRepository {
	return &OneOffJobRepository{db: db}
}

// NewOneOffJobRepositoryWithTx creates a repository using a transaction
func NewOneOffJobRepositoryWithTx(tx DBTX) *OneOffJobRepository {
	return &OneOffJobRepository{db: tx}
}

// Create creates a new one-off job
func (r *OneOffJobRepository) Create(ctx context.Context, job *types.OneOffJob) error {
	job.ID = uuid.New()
	job.CreatedAt = time.Now()
	job.Status = "pending"

	query := `
		INSERT INTO one_off_jobs (
			id, project_id, service_id, name, command, image,
			timeout, run_at, status, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, query,
		job.ID, job.ProjectID, job.ServiceID, job.Name, job.Command, job.Image,
		job.Timeout, job.RunAt, job.Status, job.CreatedAt,
	)
	return err
}

// GetByID retrieves a one-off job by ID
func (r *OneOffJobRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.OneOffJob, error) {
	job := &types.OneOffJob{}
	var image sql.NullString
	var runAt, startedAt, endedAt sql.NullTime
	var exitCode sql.NullInt64

	query := `
		SELECT id, project_id, service_id, name, command, image,
		       timeout, run_at, status, exit_code,
		       created_at, started_at, ended_at
		FROM one_off_jobs WHERE id = $1
	`
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&job.ID, &job.ProjectID, &job.ServiceID, &job.Name, &job.Command, &image,
		&job.Timeout, &runAt, &job.Status, &exitCode,
		&job.CreatedAt, &startedAt, &endedAt,
	)
	if err != nil {
		return nil, err
	}

	if image.Valid {
		job.Image = image.String
	}
	if runAt.Valid {
		job.RunAt = &runAt.Time
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		job.ExitCode = &code
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if endedAt.Valid {
		job.EndedAt = &endedAt.Time
	}

	return job, nil
}

// ListByProject retrieves all one-off jobs for a project
func (r *OneOffJobRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.OneOffJob, error) {
	query := `
		SELECT id, project_id, service_id, name, command, image,
		       timeout, run_at, status, exit_code,
		       created_at, started_at, ended_at
		FROM one_off_jobs
		WHERE project_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanOneOffJobs(rows)
}

// ListPending retrieves all pending one-off jobs (for reconciler)
func (r *OneOffJobRepository) ListPending(ctx context.Context) ([]*types.OneOffJob, error) {
	query := `
		SELECT id, project_id, service_id, name, command, image,
		       timeout, run_at, status, exit_code,
		       created_at, started_at, ended_at
		FROM one_off_jobs
		WHERE status = 'pending' AND (run_at IS NULL OR run_at <= NOW())
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanOneOffJobs(rows)
}

// UpdateStatus updates a one-off job's status
func (r *OneOffJobRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, exitCode *int) error {
	now := time.Now()

	var query string
	var args []interface{}

	switch status {
	case "running":
		query = `UPDATE one_off_jobs SET status = $1, started_at = $2 WHERE id = $3`
		args = []interface{}{status, now, id}
	case "completed", "failed":
		query = `UPDATE one_off_jobs SET status = $1, exit_code = $2, ended_at = $3 WHERE id = $4`
		args = []interface{}{status, exitCode, now, id}
	default:
		query = `UPDATE one_off_jobs SET status = $1 WHERE id = $2`
		args = []interface{}{status, id}
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// scanOneOffJobs scans multiple one-off job rows
func (r *OneOffJobRepository) scanOneOffJobs(rows *sql.Rows) ([]*types.OneOffJob, error) {
	var jobs []*types.OneOffJob

	for rows.Next() {
		job := &types.OneOffJob{}
		var image sql.NullString
		var runAt, startedAt, endedAt sql.NullTime
		var exitCode sql.NullInt64

		err := rows.Scan(
			&job.ID, &job.ProjectID, &job.ServiceID, &job.Name, &job.Command, &image,
			&job.Timeout, &runAt, &job.Status, &exitCode,
			&job.CreatedAt, &startedAt, &endedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan one-off job: %w", err)
		}

		if image.Valid {
			job.Image = image.String
		}
		if runAt.Valid {
			job.RunAt = &runAt.Time
		}
		if exitCode.Valid {
			code := int(exitCode.Int64)
			job.ExitCode = &code
		}
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if endedAt.Valid {
			job.EndedAt = &endedAt.Time
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}
