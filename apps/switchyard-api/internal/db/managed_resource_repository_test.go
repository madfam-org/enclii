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

func newManagedResourceMockDB(t *testing.T) (*ManagedResourceRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewManagedResourceRepository(db)
	return repo, mock, func() { db.Close() }
}

func TestManagedResourceRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		mr := &types.ManagedResource{
			Name:             "my-rds",
			APIVersion:       "database.aws.crossplane.io/v1beta1",
			Kind:             "RDSInstance",
			Provider:         "aws",
			ManagementPolicy: types.ManagementPolicyFullControl,
			SyncStatus:       types.SyncStatusSynced,
			SpecHash:         "abc123",
		}

		mock.ExpectQuery(`INSERT INTO managed_resources`).
			WithArgs(
				sqlmock.AnyArg(), "my-rds", "database.aws.crossplane.io/v1beta1", "RDSInstance",
				"aws", nil, types.ManagementPolicyFullControl, types.SyncStatusSynced,
				sqlmock.AnyArg(), "abc123", sqlmock.AnyArg(),
			).
			WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

		err := repo.Create(context.Background(), mr)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, mr.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		mr := &types.ManagedResource{Name: "fail", Kind: "Bucket", Provider: "aws", ManagementPolicy: types.ManagementPolicyObserveOnly, SyncStatus: types.SyncStatusUnknown}
		mock.ExpectQuery(`INSERT INTO managed_resources`).WillReturnError(fmt.Errorf("constraint"))

		err := repo.Create(context.Background(), mr)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestManagedResourceRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		rows := mockManagedResourceRows().
			AddRow(id, "my-rds", "database.aws/v1", "RDSInstance", "aws", nil,
				"FullControl", "Synced", []byte("[]"), "hash", []byte("{}"), now, now)

		mock.ExpectQuery(`SELECT id, name, api_version`).WithArgs(id).WillReturnRows(rows)

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "my-rds", result.Name)
		assert.Equal(t, types.ManagementPolicy("FullControl"), result.ManagementPolicy)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, name, api_version`).WithArgs(id).WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestManagedResourceRepository_List(t *testing.T) {
	t.Run("no filters", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := mockManagedResourceRows().
			AddRow(uuid.New(), "rds-1", "v1", "RDSInstance", "aws", nil, "FullControl", "Synced", []byte("[]"), "", []byte("{}"), now, now).
			AddRow(uuid.New(), "bucket-1", "v1", "Bucket", "gcp", nil, "ObserveOnly", "OutOfSync", []byte("[]"), "", []byte("{}"), now, now)

		mock.ExpectQuery(`SELECT id, name, api_version`).
			WithArgs("", "", "").WillReturnRows(rows)

		results, err := repo.List(context.Background(), "", "", "")
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("filter by provider", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := mockManagedResourceRows().
			AddRow(uuid.New(), "rds-1", "v1", "RDSInstance", "aws", nil, "FullControl", "Synced", []byte("[]"), "", []byte("{}"), now, now)

		mock.ExpectQuery(`SELECT id, name, api_version`).
			WithArgs("aws", "", "").WillReturnRows(rows)

		results, err := repo.List(context.Background(), "aws", "", "")
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, api_version`).
			WithArgs("", "", "").WillReturnError(fmt.Errorf("timeout"))

		results, err := repo.List(context.Background(), "", "", "")
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestManagedResourceRepository_UpdateSyncStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		id := uuid.New()
		conditions := json.RawMessage(`[{"type":"Ready","status":"True"}]`)
		mock.ExpectExec(`UPDATE managed_resources SET sync_status`).
			WithArgs(id, types.SyncStatusSynced, conditions).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateSyncStatus(context.Background(), id, types.SyncStatusSynced, conditions)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestManagedResourceRepository_UpdatePolicy(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE managed_resources SET management_policy`).
			WithArgs(id, types.ManagementPolicyOrphanOnDelete).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdatePolicy(context.Background(), id, types.ManagementPolicyOrphanOnDelete)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestManagedResourceRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM managed_resources WHERE id`).
			WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newManagedResourceMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM managed_resources WHERE id`).
			WithArgs(id).WillReturnError(fmt.Errorf("foreign key"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
