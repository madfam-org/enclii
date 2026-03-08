package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newDeploymentGroupMockDB(t *testing.T) (*DeploymentGroupRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewDeploymentGroupRepository(db)
	return repo, mock, func() { db.Close() }
}

var deploymentGroupColumns = []string{
	"id", "project_id", "environment_id", "name", "status", "strategy",
	"triggered_by", "git_sha", "pr_url", "started_at", "completed_at",
	"error_message", "created_at", "updated_at",
}

// --- Create ---

func TestDeploymentGroupRepository_Create(t *testing.T) {
	t.Run("success with defaults", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		group := &DeploymentGroup{
			ProjectID:     uuid.New(),
			EnvironmentID: uuid.New(),
		}

		mock.ExpectExec(`INSERT INTO deployment_groups`).
			WithArgs(
				sqlmock.AnyArg(), group.ProjectID, group.EnvironmentID, sqlmock.AnyArg(),
				DeploymentGroupStatusPending, DeploymentGroupStrategyDependencyOrdered,
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), group)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, group.ID)
		assert.Equal(t, DeploymentGroupStatusPending, group.Status)
		assert.Equal(t, DeploymentGroupStrategyDependencyOrdered, group.Strategy)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		group := &DeploymentGroup{
			ProjectID:     uuid.New(),
			EnvironmentID: uuid.New(),
		}

		mock.ExpectExec(`INSERT INTO deployment_groups`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("foreign key violation"))

		err := repo.Create(context.Background(), group)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestDeploymentGroupRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		id := uuid.New()
		projID := uuid.New()
		envID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		name := "release-v1"

		mock.ExpectQuery(`SELECT id, project_id, environment_id, name, status, strategy`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(deploymentGroupColumns).
				AddRow(id, projID, envID,
					sql.NullString{String: name, Valid: true},
					DeploymentGroupStatusSucceeded, DeploymentGroupStrategyParallel,
					sql.NullString{String: "ci-bot", Valid: true},
					sql.NullString{String: "abc123", Valid: true},
					sql.NullString{},
					sql.NullTime{Time: now, Valid: true},
					sql.NullTime{Time: now, Valid: true},
					sql.NullString{},
					now, now))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, projID, result.ProjectID)
		assert.NotNil(t, result.Name)
		assert.Equal(t, name, *result.Name)
		assert.Equal(t, DeploymentGroupStatusSucceeded, result.Status)
		assert.NotNil(t, result.StartedAt)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, environment_id, name, status, strategy`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByProject ---

func TestDeploymentGroupRepository_ListByProject(t *testing.T) {
	t.Run("returns groups", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		projID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(deploymentGroupColumns).
			AddRow(uuid.New(), projID, uuid.New(),
				sql.NullString{}, DeploymentGroupStatusPending, DeploymentGroupStrategySequential,
				sql.NullString{}, sql.NullString{}, sql.NullString{},
				sql.NullTime{}, sql.NullTime{}, sql.NullString{},
				now, now)

		mock.ExpectQuery(`SELECT id, project_id, environment_id`).
			WithArgs(projID, 50, 0).
			WillReturnRows(rows)

		results, err := repo.ListByProject(context.Background(), projID, 50, 0)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		projID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, environment_id`).
			WithArgs(projID, 50, 0).
			WillReturnRows(sqlmock.NewRows(deploymentGroupColumns))

		results, err := repo.ListByProject(context.Background(), projID, 0, 0)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestDeploymentGroupRepository_UpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE deployment_groups`).
			WithArgs(DeploymentGroupStatusFailed, sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		errMsg := "pods crashed"
		err := repo.UpdateStatus(context.Background(), id, DeploymentGroupStatusFailed, &errMsg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStarted ---

func TestDeploymentGroupRepository_UpdateStarted(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE deployment_groups`).
			WithArgs(DeploymentGroupStatusInProgress, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStarted(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestDeploymentGroupRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDeploymentGroupMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM deployment_groups`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
