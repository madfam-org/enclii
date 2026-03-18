package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CronJobRunRepository handles database operations for cron job runs
type CronJobRunRepository struct {
	db DBTX
}

// NewCronJobRunRepository creates a new cron job run repository
func NewCronJobRunRepository(db DBTX) *CronJobRunRepository {
	return &CronJobRunRepository{db: db}
}

// NewCronJobRunRepositoryWithTx creates a repository using a transaction
func NewCronJobRunRepositoryWithTx(tx DBTX) *CronJobRunRepository {
	return &CronJobRunRepository{db: tx}
}

// Create creates a new cron job run record
func (r *CronJobRunRepository) Create(ctx context.Context, run *types.CronJobRun) error {
	run.ID = uuid.New()
	run.StartedAt = time.Now()
	run.Status = "running"

	query := `
		INSERT INTO cron_job_runs (id, cron_job_id, status, started_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.ExecContext(ctx, query, run.ID, run.CronJobID, run.Status, run.StartedAt)
	return err
}

// ListByCronJob retrieves all runs for a cron job
func (r *CronJobRunRepository) ListByCronJob(ctx context.Context, cronJobID uuid.UUID, limit int) ([]*types.CronJobRun, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	query := `
		SELECT id, cron_job_id, status, exit_code, started_at, ended_at, log_output
		FROM cron_job_runs
		WHERE cron_job_id = $1
		ORDER BY started_at DESC
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, cronJobID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var runs []*types.CronJobRun
	for rows.Next() {
		run := &types.CronJobRun{}
		var exitCode sql.NullInt64
		var endedAt sql.NullTime
		var logOutput sql.NullString

		err := rows.Scan(
			&run.ID, &run.CronJobID, &run.Status, &exitCode, &run.StartedAt, &endedAt, &logOutput,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cron job run: %w", err)
		}

		if exitCode.Valid {
			code := int(exitCode.Int64)
			run.ExitCode = &code
		}
		if endedAt.Valid {
			run.EndedAt = &endedAt.Time
		}
		if logOutput.Valid {
			run.LogOutput = logOutput.String
		}

		runs = append(runs, run)
	}

	return runs, nil
}

// UpdateStatus updates a run's status, exit code, and end time
func (r *CronJobRunRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, exitCode *int, logOutput string) error {
	now := time.Now()
	query := `
		UPDATE cron_job_runs
		SET status = $1, exit_code = $2, ended_at = $3, log_output = $4
		WHERE id = $5
	`
	result, err := r.db.ExecContext(ctx, query, status, exitCode, now, logOutput, id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}
