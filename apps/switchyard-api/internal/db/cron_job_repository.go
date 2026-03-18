package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CronJobRepository handles database operations for cron jobs
type CronJobRepository struct {
	db DBTX
}

// NewCronJobRepository creates a new cron job repository
func NewCronJobRepository(db DBTX) *CronJobRepository {
	return &CronJobRepository{db: db}
}

// NewCronJobRepositoryWithTx creates a repository using a transaction
func NewCronJobRepositoryWithTx(tx DBTX) *CronJobRepository {
	return &CronJobRepository{db: tx}
}

// Create creates a new cron job
func (r *CronJobRepository) Create(ctx context.Context, job *types.CronJob) error {
	job.ID = uuid.New()
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()

	query := `
		INSERT INTO cron_jobs (
			id, project_id, service_id, name, schedule, command, image,
			timeout, retries, suspended, concurrency,
			created_at, updated_at, next_run_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := r.db.ExecContext(ctx, query,
		job.ID, job.ProjectID, job.ServiceID, job.Name, job.Schedule, job.Command, job.Image,
		job.Timeout, job.Retries, job.Suspended, job.Concurrency,
		job.CreatedAt, job.UpdatedAt, job.NextRunAt,
	)
	return err
}

// GetByID retrieves a cron job by ID
func (r *CronJobRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.CronJob, error) {
	job := &types.CronJob{}
	var image sql.NullString
	var lastRunAt, nextRunAt sql.NullTime

	query := `
		SELECT id, project_id, service_id, name, schedule, command, image,
		       timeout, retries, suspended, concurrency,
		       created_at, updated_at, last_run_at, next_run_at
		FROM cron_jobs WHERE id = $1
	`
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&job.ID, &job.ProjectID, &job.ServiceID, &job.Name, &job.Schedule, &job.Command, &image,
		&job.Timeout, &job.Retries, &job.Suspended, &job.Concurrency,
		&job.CreatedAt, &job.UpdatedAt, &lastRunAt, &nextRunAt,
	)
	if err != nil {
		return nil, err
	}

	if image.Valid {
		job.Image = image.String
	}
	if lastRunAt.Valid {
		job.LastRunAt = &lastRunAt.Time
	}
	if nextRunAt.Valid {
		job.NextRunAt = &nextRunAt.Time
	}

	return job, nil
}

// ListByProject retrieves all cron jobs for a project
func (r *CronJobRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.CronJob, error) {
	query := `
		SELECT id, project_id, service_id, name, schedule, command, image,
		       timeout, retries, suspended, concurrency,
		       created_at, updated_at, last_run_at, next_run_at
		FROM cron_jobs
		WHERE project_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanCronJobs(rows)
}

// ListActive retrieves all non-suspended cron jobs (for reconciler)
func (r *CronJobRepository) ListActive(ctx context.Context) ([]*types.CronJob, error) {
	query := `
		SELECT id, project_id, service_id, name, schedule, command, image,
		       timeout, retries, suspended, concurrency,
		       created_at, updated_at, last_run_at, next_run_at
		FROM cron_jobs
		WHERE suspended = FALSE
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return r.scanCronJobs(rows)
}

// Update updates a cron job
func (r *CronJobRepository) Update(ctx context.Context, job *types.CronJob) error {
	job.UpdatedAt = time.Now()

	query := `
		UPDATE cron_jobs
		SET name = $1, schedule = $2, command = $3, image = $4,
		    timeout = $5, retries = $6, suspended = $7, concurrency = $8,
		    updated_at = $9, next_run_at = $10
		WHERE id = $11
	`
	result, err := r.db.ExecContext(ctx, query,
		job.Name, job.Schedule, job.Command, job.Image,
		job.Timeout, job.Retries, job.Suspended, job.Concurrency,
		job.UpdatedAt, job.NextRunAt,
		job.ID,
	)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateLastRun updates the last_run_at and next_run_at timestamps
func (r *CronJobRepository) UpdateLastRun(ctx context.Context, id uuid.UUID, lastRun, nextRun time.Time) error {
	query := `
		UPDATE cron_jobs
		SET last_run_at = $1, next_run_at = $2, updated_at = $3
		WHERE id = $4
	`
	result, err := r.db.ExecContext(ctx, query, lastRun, nextRun, time.Now(), id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Delete permanently removes a cron job
func (r *CronJobRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM cron_jobs WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// scanCronJobs scans multiple cron job rows
func (r *CronJobRepository) scanCronJobs(rows *sql.Rows) ([]*types.CronJob, error) {
	var jobs []*types.CronJob

	for rows.Next() {
		job := &types.CronJob{}
		var image sql.NullString
		var lastRunAt, nextRunAt sql.NullTime

		err := rows.Scan(
			&job.ID, &job.ProjectID, &job.ServiceID, &job.Name, &job.Schedule, &job.Command, &image,
			&job.Timeout, &job.Retries, &job.Suspended, &job.Concurrency,
			&job.CreatedAt, &job.UpdatedAt, &lastRunAt, &nextRunAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cron job: %w", err)
		}

		if image.Valid {
			job.Image = image.String
		}
		if lastRunAt.Valid {
			job.LastRunAt = &lastRunAt.Time
		}
		if nextRunAt.Valid {
			job.NextRunAt = &nextRunAt.Time
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}
