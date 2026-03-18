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

func newCronJobMockDB(t *testing.T) (*CronJobRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewCronJobRepository(db)
	return repo, mock, func() { db.Close() }
}

var cronJobColumns = []string{
	"id", "project_id", "service_id", "name", "schedule", "command", "image",
	"timeout", "retries", "suspended", "concurrency",
	"created_at", "updated_at", "last_run_at", "next_run_at",
}

func sampleCronJob(projectID, serviceID uuid.UUID) *types.CronJob {
	now := time.Now().Truncate(time.Microsecond)
	return &types.CronJob{
		ID:          uuid.New(),
		ProjectID:   projectID,
		ServiceID:   serviceID,
		Name:        "nightly-backup",
		Schedule:    "0 2 * * *",
		Command:     "pg_dump -U admin mydb",
		Image:       "postgres:16",
		Timeout:     600,
		Retries:     3,
		Suspended:   false,
		Concurrency: "forbid",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func cronJobRow(job *types.CronJob) *sqlmock.Rows {
	var lastRunAt, nextRunAt interface{}
	if job.LastRunAt != nil {
		lastRunAt = *job.LastRunAt
	}
	if job.NextRunAt != nil {
		nextRunAt = *job.NextRunAt
	}
	return sqlmock.NewRows(cronJobColumns).
		AddRow(
			job.ID, job.ProjectID, job.ServiceID, job.Name, job.Schedule, job.Command,
			sql.NullString{String: job.Image, Valid: job.Image != ""},
			job.Timeout, job.Retries, job.Suspended, job.Concurrency,
			job.CreatedAt, job.UpdatedAt,
			lastRunAt, nextRunAt,
		)
}

// --- Create ---

func TestCronJobRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		job := &types.CronJob{
			ProjectID:   projectID,
			ServiceID:   serviceID,
			Name:        "nightly-backup",
			Schedule:    "0 2 * * *",
			Command:     "pg_dump mydb",
			Image:       "postgres:16",
			Timeout:     600,
			Retries:     3,
			Concurrency: "forbid",
		}

		mock.ExpectExec(`INSERT INTO cron_jobs`).
			WithArgs(
				sqlmock.AnyArg(), projectID, serviceID,
				"nightly-backup", "0 2 * * *", "pg_dump mydb", "postgres:16",
				600, 3, false, "forbid",
				sqlmock.AnyArg(), sqlmock.AnyArg(), (*time.Time)(nil),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), job)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, job.ID)
		assert.False(t, job.CreatedAt.IsZero())
		assert.False(t, job.UpdatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		job := &types.CronJob{
			ProjectID:   uuid.New(),
			ServiceID:   uuid.New(),
			Name:        "fail-job",
			Schedule:    "* * * * *",
			Command:     "echo fail",
			Concurrency: "allow",
		}

		mock.ExpectExec(`INSERT INTO cron_jobs`).
			WillReturnError(sql.ErrConnDone)

		err := repo.Create(context.Background(), job)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestCronJobRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		expected := sampleCronJob(projectID, serviceID)

		mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
			WithArgs(expected.ID).
			WillReturnRows(cronJobRow(expected))

		result, err := repo.GetByID(context.Background(), expected.ID)
		assert.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, expected.ID, result.ID)
		assert.Equal(t, expected.ProjectID, result.ProjectID)
		assert.Equal(t, expected.ServiceID, result.ServiceID)
		assert.Equal(t, expected.Name, result.Name)
		assert.Equal(t, expected.Schedule, result.Schedule)
		assert.Equal(t, expected.Command, result.Command)
		assert.Equal(t, expected.Image, result.Image)
		assert.Equal(t, expected.Timeout, result.Timeout)
		assert.Equal(t, expected.Retries, result.Retries)
		assert.Equal(t, expected.Suspended, result.Suspended)
		assert.Equal(t, expected.Concurrency, result.Concurrency)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
			WithArgs(id).
			WillReturnError(sql.ErrConnDone)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByProject ---

func TestCronJobRepository_ListByProject(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(cronJobColumns).
			AddRow(uuid.New(), projectID, serviceID, "job-alpha", "0 * * * *", "echo alpha", sql.NullString{Valid: false}, 300, 0, false, "forbid", now, now, nil, nil).
			AddRow(uuid.New(), projectID, serviceID, "job-beta", "*/5 * * * *", "echo beta", sql.NullString{String: "alpine:3", Valid: true}, 60, 2, false, "allow", now, now, nil, nil)

		mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
			WithArgs(projectID).
			WillReturnRows(rows)

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "job-alpha", results[0].Name)
		assert.Equal(t, "job-beta", results[1].Name)
		assert.Equal(t, "alpine:3", results[1].Image)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
			WithArgs(projectID).
			WillReturnRows(sqlmock.NewRows(cronJobColumns))

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
			WithArgs(projectID).
			WillReturnError(sql.ErrConnDone)

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Update ---

func TestCronJobRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		job := sampleCronJob(uuid.New(), uuid.New())

		mock.ExpectExec(`UPDATE cron_jobs`).
			WithArgs(
				job.Name, job.Schedule, job.Command, job.Image,
				job.Timeout, job.Retries, job.Suspended, job.Concurrency,
				sqlmock.AnyArg(), (*time.Time)(nil),
				job.ID,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Update(context.Background(), job)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		job := sampleCronJob(uuid.New(), uuid.New())

		mock.ExpectExec(`UPDATE cron_jobs`).
			WithArgs(
				job.Name, job.Schedule, job.Command, job.Image,
				job.Timeout, job.Retries, job.Suspended, job.Concurrency,
				sqlmock.AnyArg(), (*time.Time)(nil),
				job.ID,
			).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Update(context.Background(), job)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateLastRun ---

func TestCronJobRepository_UpdateLastRun(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		lastRun := time.Now()
		nextRun := lastRun.Add(24 * time.Hour)

		mock.ExpectExec(`UPDATE cron_jobs`).
			WithArgs(lastRun, nextRun, sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateLastRun(context.Background(), id, lastRun, nextRun)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		lastRun := time.Now()
		nextRun := lastRun.Add(time.Hour)

		mock.ExpectExec(`UPDATE cron_jobs`).
			WithArgs(lastRun, nextRun, sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.UpdateLastRun(context.Background(), id, lastRun, nextRun)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestCronJobRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM cron_jobs WHERE id`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM cron_jobs WHERE id`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM cron_jobs WHERE id`).
			WithArgs(id).
			WillReturnError(sql.ErrConnDone)

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListActive ---

func TestCronJobRepository_ListActive(t *testing.T) {
	t.Run("returns non-suspended jobs", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		projectID := uuid.New()
		serviceID := uuid.New()

		rows := sqlmock.NewRows(cronJobColumns).
			AddRow(uuid.New(), projectID, serviceID, "active-job", "*/5 * * * *", "echo active", sql.NullString{Valid: false}, 300, 0, false, "forbid", now, now, nil, nil)

		mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
			WillReturnRows(rows)

		results, err := repo.ListActive(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "active-job", results[0].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty when all suspended", func(t *testing.T) {
		repo, mock, cleanup := newCronJobMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
			WillReturnRows(sqlmock.NewRows(cronJobColumns))

		results, err := repo.ListActive(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
