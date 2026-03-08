package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newCIRunMockDB(t *testing.T) (*CIRunRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewCIRunRepository(db)
	return repo, mock, func() { db.Close() }
}

var ciRunColumns = []string{
	"id", "service_id", "commit_sha", "workflow_name", "workflow_id",
	"run_id", "run_number", "status", "conclusion", "html_url",
	"branch", "event_type", "actor", "started_at", "completed_at",
	"created_at", "updated_at",
}

// --- Create ---

func TestCIRunRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCIRunMockDB(t)
		defer cleanup()

		run := &types.CIRun{
			ServiceID:    uuid.New(),
			CommitSHA:    "abc123",
			WorkflowName: "CI",
			WorkflowID:   100,
			RunID:        200,
			RunNumber:    1,
			Status:       types.CIRunStatusQueued,
		}

		mock.ExpectExec(`INSERT INTO ci_runs`).
			WithArgs(
				sqlmock.AnyArg(), run.ServiceID, "abc123", "CI", int64(100),
				int64(200), 1, types.CIRunStatusQueued, sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), run)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, run.ID)
		assert.False(t, run.CreatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCIRunMockDB(t)
		defer cleanup()

		run := &types.CIRun{ServiceID: uuid.New(), CommitSHA: "def456", Status: types.CIRunStatusQueued}

		mock.ExpectExec(`INSERT INTO ci_runs`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("unique constraint"))

		err := repo.Create(context.Background(), run)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByRunID ---

func TestCIRunRepository_GetByRunID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newCIRunMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		svcID := uuid.New()

		mock.ExpectQuery(`SELECT id, service_id, commit_sha, workflow_name`).
			WithArgs(int64(200)).
			WillReturnRows(sqlmock.NewRows(ciRunColumns).
				AddRow(uuid.New(), svcID, "abc123", "CI", int64(100),
					int64(200), 1, types.CIRunStatusCompleted,
					sql.NullString{String: "success", Valid: true},
					sql.NullString{String: "https://github.com/runs/200", Valid: true},
					sql.NullString{String: "main", Valid: true},
					sql.NullString{String: "push", Valid: true},
					sql.NullString{String: "bot", Valid: true},
					sql.NullTime{Time: now, Valid: true},
					sql.NullTime{Time: now, Valid: true},
					now, now))

		result, err := repo.GetByRunID(context.Background(), 200)
		assert.NoError(t, err)
		assert.Equal(t, svcID, result.ServiceID)
		assert.Equal(t, "abc123", result.CommitSHA)
		assert.Equal(t, types.CIRunStatusCompleted, result.Status)
		assert.NotNil(t, result.Conclusion)
		assert.Equal(t, types.CIRunConclusionSuccess, *result.Conclusion)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newCIRunMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, service_id, commit_sha, workflow_name`).
			WithArgs(int64(999)).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByRunID(context.Background(), 999)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestCIRunRepository_UpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCIRunMockDB(t)
		defer cleanup()

		conclusion := types.CIRunConclusionSuccess
		now := time.Now()

		mock.ExpectExec(`UPDATE ci_runs`).
			WithArgs(types.CIRunStatusCompleted, &conclusion, &now, int64(200)).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(context.Background(), 200, types.CIRunStatusCompleted, &conclusion, &now)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByCommitSHA ---

func TestCIRunRepository_ListByCommitSHA(t *testing.T) {
	t.Run("returns runs", func(t *testing.T) {
		repo, mock, cleanup := newCIRunMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := sqlmock.NewRows(ciRunColumns).
			AddRow(uuid.New(), uuid.New(), "abc123", "CI", int64(100),
				int64(200), 1, types.CIRunStatusCompleted,
				sql.NullString{String: "success", Valid: true},
				sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
				sql.NullTime{}, sql.NullTime{},
				now, now)

		mock.ExpectQuery(`SELECT id, service_id, commit_sha`).
			WithArgs("abc123").
			WillReturnRows(rows)

		results, err := repo.ListByCommitSHA(context.Background(), "abc123")
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newCIRunMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, service_id, commit_sha`).
			WithArgs("nonexistent").
			WillReturnRows(sqlmock.NewRows(ciRunColumns))

		results, err := repo.ListByCommitSHA(context.Background(), "nonexistent")
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Upsert ---

func TestCIRunRepository_Upsert(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCIRunMockDB(t)
		defer cleanup()

		run := &types.CIRun{
			ServiceID:    uuid.New(),
			CommitSHA:    "abc123",
			WorkflowName: "CI",
			WorkflowID:   100,
			RunID:        200,
			RunNumber:    1,
			Status:       types.CIRunStatusInProgress,
		}

		mock.ExpectExec(`INSERT INTO ci_runs`).
			WithArgs(
				sqlmock.AnyArg(), run.ServiceID, "abc123", "CI", int64(100),
				int64(200), 1, types.CIRunStatusInProgress, sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Upsert(context.Background(), run)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, run.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
