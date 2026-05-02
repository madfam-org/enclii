package db

import (
	"context"
	"database/sql"
	"encoding/json"
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

func newDatabaseAddonMockDB(t *testing.T) (*DatabaseAddonRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewDatabaseAddonRepository(db)
	return repo, mock, func() { db.Close() }
}

// --- Create ---

func TestDatabaseAddonRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		addon := &types.DatabaseAddon{
			ProjectID: uuid.New(),
			Type:      types.DatabaseAddonTypePostgres,
			Name:      "my-db",
			Config:    types.DatabaseAddonConfig{},
		}

		mock.ExpectExec(`INSERT INTO database_addons`).
			WithArgs(
				sqlmock.AnyArg(), addon.ProjectID, sqlmock.AnyArg(),
				types.DatabaseAddonTypePostgres, "my-db", "standard-0", types.DatabaseAddonStatusPending, sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), addon)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, addon.ID)
		assert.Equal(t, types.DatabaseAddonStatusPending, addon.Status)
		// Plan defaulted to standard-0 because caller left it blank.
		assert.Equal(t, "standard-0", addon.Plan)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		addon := &types.DatabaseAddon{
			ProjectID: uuid.New(),
			Type:      types.DatabaseAddonTypePostgres,
			Name:      "fail-db",
			Config:    types.DatabaseAddonConfig{},
		}

		mock.ExpectExec(`INSERT INTO database_addons`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("unique constraint"))

		err := repo.Create(context.Background(), addon)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestDatabaseAddonRepository_GetByID(t *testing.T) {
	addonGetColumns := []string{
		"id", "project_id", "environment_id", "type", "name", "plan", "status", "status_message",
		"config", "k8s_namespace", "k8s_resource_name", "connection_secret",
		"host", "port", "database_name", "username",
		"storage_used_bytes", "connections_active", "last_backup_at",
		"created_by", "created_by_email", "created_at", "updated_at", "provisioned_at", "deleted_at",
	}

	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		id := uuid.New()
		projID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		configJSON, _ := json.Marshal(types.DatabaseAddonConfig{})

		mock.ExpectQuery(`SELECT id, project_id, environment_id, type, name`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(addonGetColumns).
				AddRow(id, projID,
					sql.NullString{},
					types.DatabaseAddonTypePostgres, "my-db", "standard-0", types.DatabaseAddonStatusReady,
					sql.NullString{String: "provisioned", Valid: true},
					configJSON,
					sql.NullString{String: "enclii", Valid: true},
					sql.NullString{String: "pg-my-db", Valid: true},
					sql.NullString{String: "my-db-conn", Valid: true},
					sql.NullString{String: "10.0.0.5", Valid: true},
					sql.NullInt64{Int64: 5432, Valid: true},
					sql.NullString{String: "mydb", Valid: true},
					sql.NullString{String: "admin", Valid: true},
					int64(1024), 5,
					sql.NullTime{},
					sql.NullString{},
					sql.NullString{},
					now, now,
					sql.NullTime{Time: now, Valid: true},
					sql.NullTime{}))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, projID, result.ProjectID)
		assert.Equal(t, types.DatabaseAddonStatusReady, result.Status)
		assert.Equal(t, "10.0.0.5", result.Host)
		assert.Equal(t, 5432, result.Port)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, environment_id`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestDatabaseAddonRepository_UpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE database_addons`).
			WithArgs(types.DatabaseAddonStatusReady, "provisioned", sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(context.Background(), id, types.DatabaseAddonStatusReady, "provisioned")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE database_addons`).
			WithArgs(types.DatabaseAddonStatusFailed, "error", sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.UpdateStatus(context.Background(), id, types.DatabaseAddonStatusFailed, "error")
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- SoftDelete ---

func TestDatabaseAddonRepository_SoftDelete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE database_addons`).
			WithArgs(types.DatabaseAddonStatusDeleted, sqlmock.AnyArg(), sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.SoftDelete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE database_addons`).
			WithArgs(types.DatabaseAddonStatusDeleted, sqlmock.AnyArg(), sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.SoftDelete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestDatabaseAddonRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM database_addons`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM database_addons`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByTeam (XC-2 Round 5 enforcement) ---

func TestDatabaseAddonRepository_ListByTeam(t *testing.T) {
	addonScanColumns := []string{
		"id", "project_id", "environment_id", "type", "name", "plan", "status", "status_message",
		"config", "k8s_namespace", "k8s_resource_name", "connection_secret",
		"host", "port", "database_name", "username",
		"storage_used_bytes", "connections_active", "last_backup_at",
		"created_by", "created_by_email", "created_at", "updated_at", "provisioned_at", "deleted_at",
	}

	t.Run("team match returns rows", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		addonID := uuid.New()
		projID := uuid.New()
		now := time.Now()

		mock.ExpectQuery(`(?s)FROM database_addons a\s+JOIN projects p ON p\.id = a\.project_id\s+WHERE p\.team_id = \$1 AND a\.deleted_at IS NULL`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows(addonScanColumns).AddRow(
				addonID, projID, nil,
				types.DatabaseAddonTypePostgres, "tenant-db", "standard-0", types.DatabaseAddonStatusReady, "",
				[]byte("{}"), nil, nil, nil,
				nil, nil, nil, nil,
				int64(0), 0, nil,
				nil, "", now, now, nil, nil,
			))

		out, err := repo.ListByTeam(context.Background(), teamID)
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, "tenant-db", out[0].Name)
		assert.Equal(t, projID, out[0].ProjectID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("team mismatch returns empty", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)WHERE p\.team_id = \$1 AND a\.deleted_at IS NULL`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows(addonScanColumns))

		out, err := repo.ListByTeam(context.Background(), teamID)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("no rows", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)WHERE p\.team_id`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows(addonScanColumns))

		out, err := repo.ListByTeam(context.Background(), teamID)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newDatabaseAddonMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`(?s)WHERE p\.team_id`).
			WithArgs(teamID).
			WillReturnError(fmt.Errorf("connection refused"))

		_, err := repo.ListByTeam(context.Background(), teamID)
		require.Error(t, err)
	})
}
