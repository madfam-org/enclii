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

func newClusterMockDB(t *testing.T) (*ClusterRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewClusterRepository(db)
	return repo, mock, func() { db.Close() }
}

func TestClusterRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		c := &types.Cluster{
			Name:     "foundry-core",
			Slug:     "foundry-core",
			Type:     types.ClusterTypeK3s,
			Endpoint: "https://10.0.0.1:6443",
			Region:   "us-east-1",
			Status:   types.ClusterStatusReady,
		}

		mock.ExpectQuery(`INSERT INTO clusters`).
			WithArgs(
				sqlmock.AnyArg(), "foundry-core", "foundry-core", types.ClusterTypeK3s,
				"https://10.0.0.1:6443", "", "us-east-1", types.ClusterStatusReady, sqlmock.AnyArg(),
			).
			WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

		err := repo.Create(context.Background(), c)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, c.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		c := &types.Cluster{Name: "fail", Slug: "fail", Type: types.ClusterTypeK8s, Status: types.ClusterStatusPending}
		mock.ExpectQuery(`INSERT INTO clusters`).WillReturnError(fmt.Errorf("duplicate slug"))

		err := repo.Create(context.Background(), c)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestClusterRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		rows := mockClusterRows()
		addClusterRow(rows, id, "foundry-core", "foundry-core")

		mock.ExpectQuery(`SELECT id, name, slug`).WithArgs(id).WillReturnRows(rows)

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "foundry-core", result.Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, name, slug`).WithArgs(id).WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestClusterRepository_List(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		rows := mockClusterRows()
		addClusterRow(rows, uuid.New(), "cluster-a", "cluster-a")
		addClusterRow(rows, uuid.New(), "cluster-b", "cluster-b")

		mock.ExpectQuery(`SELECT id, name, slug`).WillReturnRows(rows)

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, slug`).WillReturnRows(mockClusterRows())

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, slug`).WillReturnError(fmt.Errorf("timeout"))

		results, err := repo.List(context.Background())
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestClusterRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		c := &types.Cluster{
			ID:       uuid.New(),
			Name:     "updated-cluster",
			Slug:     "updated-cluster",
			Type:     types.ClusterTypeK3s,
			Endpoint: "https://10.0.0.2:6443",
			Region:   "eu-west-1",
			Status:   types.ClusterStatusReady,
		}

		mock.ExpectQuery(`UPDATE clusters`).
			WithArgs(
				c.ID, "updated-cluster", "updated-cluster", types.ClusterTypeK3s,
				"https://10.0.0.2:6443", "", "eu-west-1", types.ClusterStatusReady, sqlmock.AnyArg(),
			).
			WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

		err := repo.Update(context.Background(), c)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestClusterRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM clusters WHERE id`).
			WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newClusterMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM clusters WHERE id`).
			WithArgs(id).WillReturnError(fmt.Errorf("foreign key constraint"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
