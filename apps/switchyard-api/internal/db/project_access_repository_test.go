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

func newProjectAccessMockDB(t *testing.T) (*ProjectAccessRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewProjectAccessRepository(db)
	return repo, mock, func() { db.Close() }
}

func TestProjectAccessRepository_Grant(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		projectID := uuid.New()
		grantedBy := uuid.New()
		access := &types.ProjectAccess{
			UserID:    userID,
			ProjectID: projectID,
			Role:      types.RoleDeveloper,
			GrantedBy: grantedBy,
		}

		mock.ExpectExec(`INSERT INTO project_access`).
			WithArgs(
				sqlmock.AnyArg(), userID, projectID, nil,
				types.RoleDeveloper, grantedBy, sqlmock.AnyArg(), nil,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Grant(context.Background(), access)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, access.ID)
		assert.False(t, access.GrantedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		access := &types.ProjectAccess{UserID: uuid.New(), ProjectID: uuid.New(), Role: types.RoleViewer, GrantedBy: uuid.New()}
		mock.ExpectExec(`INSERT INTO project_access`).WillReturnError(fmt.Errorf("constraint"))

		err := repo.Grant(context.Background(), access)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestProjectAccessRepository_Revoke(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		projectID := uuid.New()
		mock.ExpectExec(`DELETE FROM project_access`).
			WithArgs(userID, projectID, nil).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Revoke(context.Background(), userID, projectID, nil)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("with environment", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		projectID := uuid.New()
		envID := uuid.New()
		mock.ExpectExec(`DELETE FROM project_access`).
			WithArgs(userID, projectID, &envID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Revoke(context.Background(), userID, projectID, &envID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestProjectAccessRepository_UserHasAccess(t *testing.T) {
	t.Run("has access", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		projectID := uuid.New()
		mock.ExpectQuery(`SELECT COUNT`).
			WithArgs(userID, projectID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		has, err := repo.UserHasAccess(context.Background(), userID, projectID)
		assert.NoError(t, err)
		assert.True(t, has)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no access", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		projectID := uuid.New()
		mock.ExpectQuery(`SELECT COUNT`).
			WithArgs(userID, projectID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		has, err := repo.UserHasAccess(context.Background(), userID, projectID)
		assert.NoError(t, err)
		assert.False(t, has)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT COUNT`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(fmt.Errorf("connection lost"))

		_, err := repo.UserHasAccess(context.Background(), uuid.New(), uuid.New())
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestProjectAccessRepository_GetUserRole(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		projectID := uuid.New()
		mock.ExpectQuery(`SELECT role FROM project_access`).
			WithArgs(userID, projectID, nil).
			WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("admin"))

		role, err := repo.GetUserRole(context.Background(), userID, projectID, nil)
		assert.NoError(t, err)
		assert.Equal(t, types.RoleAdmin, role)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT role FROM project_access`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), nil).
			WillReturnError(sql.ErrNoRows)

		role, err := repo.GetUserRole(context.Background(), uuid.New(), uuid.New(), nil)
		assert.Error(t, err)
		assert.Equal(t, types.Role(""), role)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestProjectAccessRepository_HasAccess(t *testing.T) {
	t.Run("admin has developer access", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		projectID := uuid.New()
		mock.ExpectQuery(`SELECT role FROM project_access`).
			WithArgs(userID, projectID, nil).
			WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("admin"))

		has, err := repo.HasAccess(context.Background(), userID, projectID, nil, types.RoleDeveloper)
		assert.NoError(t, err)
		assert.True(t, has)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("viewer denied admin access", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		projectID := uuid.New()
		mock.ExpectQuery(`SELECT role FROM project_access`).
			WithArgs(userID, projectID, nil).
			WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("viewer"))

		has, err := repo.HasAccess(context.Background(), userID, projectID, nil, types.RoleAdmin)
		assert.NoError(t, err)
		assert.False(t, has)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no access record returns false", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT role FROM project_access`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), nil).
			WillReturnError(sql.ErrNoRows)

		has, err := repo.HasAccess(context.Background(), uuid.New(), uuid.New(), nil, types.RoleViewer)
		assert.NoError(t, err)
		assert.False(t, has)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestProjectAccessRepository_ListByUser(t *testing.T) {
	t.Run("with results", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		userID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		rows := mockProjectAccessRows().
			AddRow(uuid.New(), userID, uuid.New(), nil, "developer", uuid.New(), now, nil).
			AddRow(uuid.New(), userID, uuid.New(), nil, "admin", uuid.New(), now, nil)

		mock.ExpectQuery(`SELECT id, user_id, project_id`).
			WithArgs(userID).WillReturnRows(rows)

		results, err := repo.ListByUser(context.Background(), userID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, user_id, project_id`).
			WithArgs(sqlmock.AnyArg()).WillReturnRows(mockProjectAccessRows())

		results, err := repo.ListByUser(context.Background(), uuid.New())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestProjectAccessRepository_ListByProject(t *testing.T) {
	t.Run("with results", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		rows := mockProjectAccessRows().
			AddRow(uuid.New(), uuid.New(), projectID, nil, "admin", uuid.New(), now, nil)

		mock.ExpectQuery(`SELECT id, user_id, project_id`).
			WithArgs(projectID).WillReturnRows(rows)

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, projectID, results[0].ProjectID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newProjectAccessMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, user_id, project_id`).
			WithArgs(sqlmock.AnyArg()).WillReturnError(fmt.Errorf("timeout"))

		results, err := repo.ListByProject(context.Background(), uuid.New())
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
