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

func newEnvironmentMockDB(t *testing.T) (*EnvironmentRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewEnvironmentRepository(db)
	return repo, mock, func() { db.Close() }
}

var environmentColumns = []string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}

func environmentRow(e *types.Environment) *sqlmock.Rows {
	return sqlmock.NewRows(environmentColumns).
		AddRow(e.ID, e.ProjectID, e.Name, e.KubeNamespace, e.CreatedAt, e.UpdatedAt)
}

// --- Create ---

func TestEnvironmentRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		env := &types.Environment{
			ProjectID:     projectID,
			Name:          "staging",
			KubeNamespace: "enclii-staging",
		}

		mock.ExpectExec(`INSERT INTO environments`).
			WithArgs(sqlmock.AnyArg(), projectID, "staging", "enclii-staging", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(env)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, env.ID, "ID should be assigned")
		assert.False(t, env.CreatedAt.IsZero(), "CreatedAt should be set")
		assert.False(t, env.UpdatedAt.IsZero(), "UpdatedAt should be set")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		env := &types.Environment{
			ProjectID:     uuid.New(),
			Name:          "fail",
			KubeNamespace: "fail-ns",
		}

		mock.ExpectExec(`INSERT INTO environments`).
			WithArgs(sqlmock.AnyArg(), env.ProjectID, "fail", "fail-ns", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(env)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestEnvironmentRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		id := uuid.New()
		projectID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		expected := &types.Environment{
			ID: id, ProjectID: projectID, Name: "production",
			KubeNamespace: "enclii-prod", CreatedAt: now, UpdatedAt: now,
		}

		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE id = \$1`).
			WithArgs(id).
			WillReturnRows(environmentRow(expected))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, expected.ID, result.ID)
		assert.Equal(t, expected.ProjectID, result.ProjectID)
		assert.Equal(t, expected.Name, result.Name)
		assert.Equal(t, expected.KubeNamespace, result.KubeNamespace)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByProjectAndName ---

func TestEnvironmentRepository_GetByProjectAndName(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		expected := &types.Environment{
			ID: uuid.New(), ProjectID: projectID, Name: "staging",
			KubeNamespace: "enclii-staging", CreatedAt: now, UpdatedAt: now,
		}

		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = \$1 AND name = \$2`).
			WithArgs(projectID, "staging").
			WillReturnRows(environmentRow(expected))

		result, err := repo.GetByProjectAndName(projectID, "staging")
		assert.NoError(t, err)
		assert.Equal(t, expected.Name, result.Name)
		assert.Equal(t, expected.ProjectID, result.ProjectID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = \$1 AND name = \$2`).
			WithArgs(projectID, "nonexistent").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByProjectAndName(projectID, "nonexistent")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByProject ---

func TestEnvironmentRepository_ListByProject(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		rows := sqlmock.NewRows(environmentColumns).
			AddRow(uuid.New(), projectID, "dev", "enclii-dev", now, now).
			AddRow(uuid.New(), projectID, "prod", "enclii-prod", now, now)

		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = \$1 ORDER BY name`).
			WithArgs(projectID).
			WillReturnRows(rows)

		results, err := repo.ListByProject(projectID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "dev", results[0].Name)
		assert.Equal(t, "prod", results[1].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = \$1 ORDER BY name`).
			WithArgs(projectID).
			WillReturnRows(sqlmock.NewRows(environmentColumns))

		results, err := repo.ListByProject(projectID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE project_id = \$1 ORDER BY name`).
			WithArgs(projectID).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.ListByProject(projectID)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListAll ---

func TestEnvironmentRepository_ListAll(t *testing.T) {
	t.Run("returns all environments", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := sqlmock.NewRows(environmentColumns).
			AddRow(uuid.New(), uuid.New(), "prod", "ns-prod", now, now).
			AddRow(uuid.New(), uuid.New(), "staging", "ns-staging", now, now)

		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments ORDER BY created_at DESC`).
			WillReturnRows(rows)

		results, err := repo.ListAll()
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments ORDER BY created_at DESC`).
			WillReturnError(fmt.Errorf("connection lost"))

		results, err := repo.ListAll()
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByKubeNamespace ---

func TestEnvironmentRepository_GetByKubeNamespace(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		expected := &types.Environment{
			ID: uuid.New(), ProjectID: uuid.New(), Name: "prod",
			KubeNamespace: "enclii-prod", CreatedAt: now, UpdatedAt: now,
		}

		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE kube_namespace = \$1`).
			WithArgs("enclii-prod").
			WillReturnRows(environmentRow(expected))

		result, err := repo.GetByKubeNamespace("enclii-prod")
		assert.NoError(t, err)
		assert.Equal(t, "enclii-prod", result.KubeNamespace)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newEnvironmentMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, project_id, name, kube_namespace, created_at, updated_at FROM environments WHERE kube_namespace = \$1`).
			WithArgs("nonexistent-ns").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByKubeNamespace("nonexistent-ns")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
