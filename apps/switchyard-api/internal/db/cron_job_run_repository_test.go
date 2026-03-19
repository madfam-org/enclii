package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newCronJobRunMockDB(t *testing.T) (*CronJobRunRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewCronJobRunRepository(db)
	return repo, mock, func() { db.Close() }
}

var cronJobRunColumns = []string{
	"id", "cron_job_id", "status", "exit_code", "started_at", "ended_at", "log_output",
}

// --- Create ---

func TestCronJobRunRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		cronJobID := uuid.New()
		run := &types.CronJobRun{
			CronJobID: cronJobID,
		}

		mock.ExpectExec(`INSERT INTO cron_job_runs`).
			WithArgs(
				sqlmock.AnyArg(), // id
				cronJobID,
				"running",
				sqlmock.AnyArg(), // started_at
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), run)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, run.ID)
		assert.Equal(t, "running", run.Status)
		assert.False(t, run.StartedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		run := &types.CronJobRun{
			CronJobID: uuid.New(),
		}

		mock.ExpectExec(`INSERT INTO cron_job_runs`).
			WillReturnError(sql.ErrConnDone)

		err := repo.Create(context.Background(), run)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByCronJob ---

func TestCronJobRunRepository_ListByCronJob(t *testing.T) {
	t.Run("returns ordered results", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		cronJobID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		endedAt := now.Add(30 * time.Second)
		exitCode := 0

		rows := sqlmock.NewRows(cronJobRunColumns).
			AddRow(uuid.New(), cronJobID, "completed", exitCode, now, endedAt, "done").
			AddRow(uuid.New(), cronJobID, "running", nil, now.Add(-time.Minute), nil, "")

		mock.ExpectQuery(`SELECT id, cron_job_id, status, exit_code, started_at, ended_at, log_output`).
			WithArgs(cronJobID, 50).
			WillReturnRows(rows)

		results, err := repo.ListByCronJob(context.Background(), cronJobID, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "completed", results[0].Status)
		assert.Equal(t, &exitCode, results[0].ExitCode)
		assert.NotNil(t, results[0].EndedAt)
		assert.Equal(t, "done", results[0].LogOutput)
		assert.Equal(t, "running", results[1].Status)
		assert.Nil(t, results[1].ExitCode)
		assert.Nil(t, results[1].EndedAt)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty result", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		cronJobID := uuid.New()
		mock.ExpectQuery(`SELECT id, cron_job_id, status, exit_code, started_at, ended_at, log_output`).
			WithArgs(cronJobID, 50).
			WillReturnRows(sqlmock.NewRows(cronJobRunColumns))

		results, err := repo.ListByCronJob(context.Background(), cronJobID, 0)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("custom limit", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		cronJobID := uuid.New()
		mock.ExpectQuery(`SELECT id, cron_job_id, status, exit_code, started_at, ended_at, log_output`).
			WithArgs(cronJobID, 10).
			WillReturnRows(sqlmock.NewRows(cronJobRunColumns))

		results, err := repo.ListByCronJob(context.Background(), cronJobID, 10)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("limit capped at 500", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		cronJobID := uuid.New()
		mock.ExpectQuery(`SELECT id, cron_job_id, status, exit_code, started_at, ended_at, log_output`).
			WithArgs(cronJobID, 500).
			WillReturnRows(sqlmock.NewRows(cronJobRunColumns))

		results, err := repo.ListByCronJob(context.Background(), cronJobID, 9999)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		cronJobID := uuid.New()
		mock.ExpectQuery(`SELECT id, cron_job_id, status, exit_code, started_at, ended_at, log_output`).
			WithArgs(cronJobID, 50).
			WillReturnError(sql.ErrConnDone)

		results, err := repo.ListByCronJob(context.Background(), cronJobID, 0)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestCronJobRunRepository_UpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		id := uuid.New()
		exitCode := 0

		mock.ExpectExec(`UPDATE cron_job_runs`).
			WithArgs("completed", &exitCode, sqlmock.AnyArg(), "all done", id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(context.Background(), id, "completed", &exitCode, "all done")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		id := uuid.New()
		exitCode := 1

		mock.ExpectExec(`UPDATE cron_job_runs`).
			WithArgs("failed", &exitCode, sqlmock.AnyArg(), "error output", id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.UpdateStatus(context.Background(), id, "failed", &exitCode, "error output")
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCronJobRunMockDB(t)
		defer cleanup()

		id := uuid.New()

		mock.ExpectExec(`UPDATE cron_job_runs`).
			WillReturnError(sql.ErrConnDone)

		err := repo.UpdateStatus(context.Background(), id, "completed", nil, "")
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- NewCronJobRunRepositoryWithTx ---

func TestNewCronJobRunRepositoryWithTx(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewCronJobRunRepositoryWithTx(db)
	assert.NotNil(t, repo)
}
