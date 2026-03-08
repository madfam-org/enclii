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

func newVClusterMockDB(t *testing.T) (*VirtualClusterRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewVirtualClusterRepository(db)
	return repo, mock, func() { db.Close() }
}

func TestVirtualClusterRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		vc := &types.VirtualCluster{
			Name:            "tenant-vc",
			HostClusterID:   uuid.New(),
			TenantID:        "tenant-1",
			Namespace:       "vc-tenant-1",
			K8sVersion:      "1.28",
			Status:          types.VClusterStatusPending,
			HelmReleaseName: "vc-tenant-1",
		}

		mock.ExpectQuery(`INSERT INTO virtual_clusters`).
			WithArgs(
				sqlmock.AnyArg(), "tenant-vc", vc.HostClusterID, "tenant-1",
				"vc-tenant-1", "1.28", types.VClusterStatusPending, "vc-tenant-1", sqlmock.AnyArg(),
			).
			WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

		err := repo.Create(context.Background(), vc)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, vc.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		vc := &types.VirtualCluster{Name: "fail-vc", HostClusterID: uuid.New(), Status: types.VClusterStatusPending}
		mock.ExpectQuery(`INSERT INTO virtual_clusters`).WillReturnError(fmt.Errorf("constraint violation"))

		err := repo.Create(context.Background(), vc)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestVirtualClusterRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		hostID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		rows := mockVirtualClusterRows().
			AddRow(id, "tenant-vc", hostID, "tenant-1", "vc-ns", "1.28", "running", "vc-release", []byte("{}"), now, now)

		mock.ExpectQuery(`SELECT id, name, host_cluster_id`).WithArgs(id).WillReturnRows(rows)

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "tenant-vc", result.Name)
		assert.Equal(t, hostID, result.HostClusterID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, name, host_cluster_id`).WithArgs(id).WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestVirtualClusterRepository_List(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := mockVirtualClusterRows().
			AddRow(uuid.New(), "vc-a", uuid.New(), "t1", "ns-a", "1.28", "running", "rel-a", []byte("{}"), now, now).
			AddRow(uuid.New(), "vc-b", uuid.New(), "t2", "ns-b", "1.29", "paused", "rel-b", []byte("{}"), now, now)

		mock.ExpectQuery(`SELECT id, name, host_cluster_id`).WillReturnRows(rows)

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, host_cluster_id`).WillReturnRows(mockVirtualClusterRows())

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestVirtualClusterRepository_UpdateStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE virtual_clusters SET status`).
			WithArgs(id, types.VClusterStatusRunning).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(context.Background(), id, types.VClusterStatusRunning)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE virtual_clusters SET status`).
			WithArgs(id, types.VClusterStatusError).
			WillReturnError(fmt.Errorf("connection lost"))

		err := repo.UpdateStatus(context.Background(), id, types.VClusterStatusError)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestVirtualClusterRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM virtual_clusters WHERE id`).
			WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newVClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM virtual_clusters WHERE id`).
			WithArgs(id).WillReturnError(fmt.Errorf("foreign key"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
