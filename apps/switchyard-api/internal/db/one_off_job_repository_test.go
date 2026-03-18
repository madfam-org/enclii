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

func newOneOffJobMockDB(t *testing.T) (*OneOffJobRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewOneOffJobRepository(db)
	return repo, mock, func() { db.Close() }
}

var oneOffJobColumns = []string{
	"id", "project_id", "service_id", "name", "command", "image",
	"timeout", "run_at", "status", "exit_code",
	"created_at", "started_at", "ended_at",
}

func sampleOneOffJob(projectID, serviceID uuid.UUID) *types.OneOffJob {
	now := time.Now().Truncate(time.Microsecond)
	return &types.OneOffJob{
		ID:        uuid.New(),
		ProjectID: projectID,
		ServiceID: serviceID,
		Name:      "db-migration",
		Command:   "migrate up",
		Image:     "migrate/migrate:v4",
		Timeout:   120,
		Status:    "pending",
		CreatedAt: now,
	}
}

func oneOffJobRow(job *types.OneOffJob) *sqlmock.Rows {
	var image sql.NullString
	if job.Image != "" {
		image = sql.NullString{String: job.Image, Valid: true}
	}
	var runAt, startedAt, endedAt interface{}
	if job.RunAt != nil {
		runAt = *job.RunAt
	}
	if job.StartedAt != nil {
		startedAt = *job.StartedAt
	}
	if job.EndedAt != nil {
		endedAt = *job.EndedAt
	}
	var exitCode interface{}
	if job.ExitCode != nil {
		exitCode = int64(*job.ExitCode)
	}
	return sqlmock.NewRows(oneOffJobColumns).
		AddRow(
			job.ID, job.ProjectID, job.ServiceID, job.Name, job.Command, image,
			job.Timeout, runAt, job.Status, exitCode,
			job.CreatedAt, startedAt, endedAt,
		)
}

// --- Create ---

func TestOneOffJobRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		job := &types.OneOffJob{
			ProjectID: projectID,
			ServiceID: serviceID,
			Name:      "db-migration",
			Command:   "migrate up",
			Image:     "migrate/migrate:v4",
			Timeout:   120,
		}

		mock.ExpectExec(`INSERT INTO one_off_jobs`).
			WithArgs(
				sqlmock.AnyArg(), projectID, serviceID, "db-migration", "migrate up",
				"migrate/migrate:v4", 120, (*time.Time)(nil), "pending", sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), job)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, job.ID)
		assert.Equal(t, "pending", job.Status)
		assert.False(t, job.CreatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		job := &types.OneOffJob{
			ProjectID: uuid.New(),
			ServiceID: uuid.New(),
			Name:      "fail-job",
			Command:   "echo fail",
		}

		mock.ExpectExec(`INSERT INTO one_off_jobs`).
			WillReturnError(sql.ErrConnDone)

		err := repo.Create(context.Background(), job)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestOneOffJobRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		expected := sampleOneOffJob(projectID, serviceID)

		mock.ExpectQuery(`SELECT id, project_id, service_id, name, command, image`).
			WithArgs(expected.ID).
			WillReturnRows(oneOffJobRow(expected))

		result, err := repo.GetByID(context.Background(), expected.ID)
		assert.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, expected.ID, result.ID)
		assert.Equal(t, expected.ProjectID, result.ProjectID)
		assert.Equal(t, expected.ServiceID, result.ServiceID)
		assert.Equal(t, expected.Name, result.Name)
		assert.Equal(t, expected.Command, result.Command)
		assert.Equal(t, expected.Image, result.Image)
		assert.Equal(t, expected.Timeout, result.Timeout)
		assert.Equal(t, expected.Status, result.Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, name, command, image`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, name, command, image`).
			WithArgs(id).
			WillReturnError(sql.ErrConnDone)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByProject ---

func TestOneOffJobRepository_ListByProject(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(oneOffJobColumns).
			AddRow(uuid.New(), projectID, serviceID, "migration-01", "migrate up", sql.NullString{Valid: false}, 120, nil, "completed", int64(0), now, now, now).
			AddRow(uuid.New(), projectID, serviceID, "seed-data", "seed run", sql.NullString{String: "node:20", Valid: true}, 60, nil, "pending", nil, now, nil, nil)

		mock.ExpectQuery(`SELECT id, project_id, service_id, name, command, image`).
			WithArgs(projectID).
			WillReturnRows(rows)

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "migration-01", results[0].Name)
		assert.Equal(t, "completed", results[0].Status)
		assert.Equal(t, "seed-data", results[1].Name)
		assert.Equal(t, "node:20", results[1].Image)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, name, command, image`).
			WithArgs(projectID).
			WillReturnRows(sqlmock.NewRows(oneOffJobColumns))

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestOneOffJobRepository_UpdateStatus_Running(t *testing.T) {
	repo, mock, cleanup := newOneOffJobMockDB(t)
	defer cleanup()

	id := uuid.New()
	mock.ExpectExec(`UPDATE one_off_jobs SET status`).
		WithArgs("running", sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateStatus(context.Background(), id, "running", nil)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOneOffJobRepository_UpdateStatus_Completed(t *testing.T) {
	repo, mock, cleanup := newOneOffJobMockDB(t)
	defer cleanup()

	id := uuid.New()
	exitCode := 0
	mock.ExpectExec(`UPDATE one_off_jobs SET status`).
		WithArgs("completed", &exitCode, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateStatus(context.Background(), id, "completed", &exitCode)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOneOffJobRepository_UpdateStatus_Failed(t *testing.T) {
	repo, mock, cleanup := newOneOffJobMockDB(t)
	defer cleanup()

	id := uuid.New()
	exitCode := 1
	mock.ExpectExec(`UPDATE one_off_jobs SET status`).
		WithArgs("failed", &exitCode, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateStatus(context.Background(), id, "failed", &exitCode)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOneOffJobRepository_UpdateStatus_NotFound(t *testing.T) {
	repo, mock, cleanup := newOneOffJobMockDB(t)
	defer cleanup()

	id := uuid.New()
	mock.ExpectExec(`UPDATE one_off_jobs SET status`).
		WithArgs("running", sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.UpdateStatus(context.Background(), id, "running", nil)
	assert.ErrorIs(t, err, sql.ErrNoRows)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- ListPending ---

func TestOneOffJobRepository_ListPending(t *testing.T) {
	t.Run("returns pending jobs", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		projectID := uuid.New()
		serviceID := uuid.New()

		rows := sqlmock.NewRows(oneOffJobColumns).
			AddRow(uuid.New(), projectID, serviceID, "pending-job", "echo run", sql.NullString{Valid: false}, 300, nil, "pending", nil, now, nil, nil)

		mock.ExpectQuery(`SELECT id, project_id, service_id, name, command, image`).
			WillReturnRows(rows)

		results, err := repo.ListPending(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "pending", results[0].Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newOneOffJobMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, service_id, name, command, image`).
			WillReturnRows(sqlmock.NewRows(oneOffJobColumns))

		results, err := repo.ListPending(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
