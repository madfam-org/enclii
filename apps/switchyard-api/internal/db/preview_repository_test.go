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

func newPreviewMockDB(t *testing.T) (*PreviewEnvironmentRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewPreviewEnvironmentRepository(db)
	return repo, mock, func() { db.Close() }
}

var previewColumns = []string{
	"id", "project_id", "service_id", "pr_number", "pr_title", "pr_url", "pr_author",
	"pr_branch", "pr_base_branch", "commit_sha", "preview_subdomain", "preview_url",
	"status", "status_message", "auto_sleep_after", "last_accessed_at", "sleeping_since",
	"deployment_id", "build_logs_url", "created_at", "updated_at", "closed_at",
}

func previewRow(id, projID, svcID uuid.UUID, now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows(previewColumns).
		AddRow(id, projID, svcID, 42,
			sql.NullString{String: "Fix bug", Valid: true},
			sql.NullString{String: "https://github.com/pr/42", Valid: true},
			sql.NullString{String: "dev", Valid: true},
			"feature/fix", "main", "abc123",
			"pr-42.preview.enclii.dev", "https://pr-42.preview.enclii.dev",
			types.PreviewStatusActive,
			sql.NullString{},
			30,
			sql.NullTime{Time: now, Valid: true},
			sql.NullTime{},
			sql.NullString{},
			sql.NullString{},
			now, now, sql.NullTime{})
}

// --- Create ---

func TestPreviewEnvironmentRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		preview := &types.PreviewEnvironment{
			ProjectID:        uuid.New(),
			ServiceID:        uuid.New(),
			PRNumber:         42,
			PRBranch:         "feature/fix",
			PRBaseBranch:     "main",
			CommitSHA:        "abc123",
			PreviewSubdomain: "pr-42",
			PreviewURL:       "https://pr-42.preview.enclii.dev",
			Status:           types.PreviewStatusPending,
			AutoSleepAfter:   30,
		}

		mock.ExpectExec(`INSERT INTO preview_environments`).
			WithArgs(
				sqlmock.AnyArg(), preview.ProjectID, preview.ServiceID, 42,
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				"feature/fix", "main", "abc123", "pr-42",
				"https://pr-42.preview.enclii.dev",
				types.PreviewStatusPending, sqlmock.AnyArg(), 30,
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), preview)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, preview.ID)
		assert.NotNil(t, preview.LastAccessedAt)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestPreviewEnvironmentRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		id := uuid.New()
		projID := uuid.New()
		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, project_id, service_id, pr_number`).
			WithArgs(id).
			WillReturnRows(previewRow(id, projID, svcID, now))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, 42, result.PRNumber)
		assert.Equal(t, types.PreviewStatusActive, result.Status)
		assert.Equal(t, "Fix bug", result.PRTitle)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, pr_number`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestPreviewEnvironmentRepository_UpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE preview_environments`).
			WithArgs(types.PreviewStatusActive, "deployed", id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(context.Background(), id, types.PreviewStatusActive, "deployed")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE preview_environments`).
			WithArgs(types.PreviewStatusFailed, "build error", id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.UpdateStatus(context.Background(), id, types.PreviewStatusFailed, "build error")
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Sleep / Wake ---

func TestPreviewEnvironmentRepository_Sleep(t *testing.T) {
	repo, mock, cleanup := newPreviewMockDB(t)
	defer cleanup()

	id := uuid.New()
	mock.ExpectExec(`UPDATE preview_environments`).
		WithArgs(id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Sleep(context.Background(), id)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPreviewEnvironmentRepository_Wake(t *testing.T) {
	repo, mock, cleanup := newPreviewMockDB(t)
	defer cleanup()

	id := uuid.New()
	mock.ExpectExec(`UPDATE preview_environments`).
		WithArgs(id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Wake(context.Background(), id)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Close ---

func TestPreviewEnvironmentRepository_Close(t *testing.T) {
	repo, mock, cleanup := newPreviewMockDB(t)
	defer cleanup()

	id := uuid.New()
	mock.ExpectExec(`UPDATE preview_environments`).
		WithArgs(id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Close(context.Background(), id)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Delete ---

func TestPreviewEnvironmentRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM preview_environments`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM preview_environments`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByService ---

func TestPreviewEnvironmentRepository_ListByService(t *testing.T) {
	t.Run("returns previews", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, project_id, service_id, pr_number`).
			WithArgs(svcID).
			WillReturnRows(previewRow(uuid.New(), uuid.New(), svcID, now))

		results, err := repo.ListByService(context.Background(), svcID)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newPreviewMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id`).
			WithArgs(svcID).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.ListByService(context.Background(), svcID)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
