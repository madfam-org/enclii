package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPropagationMockDB(t *testing.T) (*PropagationPolicyRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewPropagationPolicyRepository(db)
	return repo, mock, func() { db.Close() }
}

func TestPropagationPolicyRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		cID1 := uuid.New()
		cID2 := uuid.New()
		pp := &types.PropagationPolicy{
			Name:              "spread-policy",
			ClusterIDs:        []uuid.UUID{cID1, cID2},
			PlacementStrategy: types.PlacementStrategySpread,
			GPURequired:       false,
			Priority:          10,
		}

		mock.ExpectQuery(`INSERT INTO propagation_policies`).
			WithArgs(
				sqlmock.AnyArg(), "spread-policy",
				pq.Array([]string{cID1.String(), cID2.String()}),
				sqlmock.AnyArg(), types.PlacementStrategySpread, false, 10,
			).
			WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

		err := repo.Create(context.Background(), pp)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, pp.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		pp := &types.PropagationPolicy{Name: "fail", PlacementStrategy: types.PlacementStrategyBinpack}
		mock.ExpectQuery(`INSERT INTO propagation_policies`).WillReturnError(fmt.Errorf("constraint"))

		err := repo.Create(context.Background(), pp)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPropagationPolicyRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		id := uuid.New()
		cID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		rows := mockPropagationPolicyRows().
			AddRow(id, "spread-policy", pq.Array([]string{cID.String()}), []byte("[]"),
				"Spread", false, 10, now, now)

		mock.ExpectQuery(`SELECT id, name, cluster_ids`).WithArgs(id).WillReturnRows(rows)

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "spread-policy", result.Name)
		assert.Len(t, result.ClusterIDs, 1)
		assert.Equal(t, cID, result.ClusterIDs[0])
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, name, cluster_ids`).WithArgs(id).WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPropagationPolicyRepository_List(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		cID := uuid.New()
		rows := mockPropagationPolicyRows().
			AddRow(uuid.New(), "policy-a", pq.Array([]string{cID.String()}), []byte("[]"), "Spread", false, 20, now, now).
			AddRow(uuid.New(), "policy-b", pq.Array([]string{cID.String()}), []byte("[]"), "GPUAffinity", true, 10, now, now)

		mock.ExpectQuery(`SELECT id, name, cluster_ids`).WillReturnRows(rows)

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, cluster_ids`).WillReturnRows(mockPropagationPolicyRows())

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, cluster_ids`).WillReturnError(fmt.Errorf("timeout"))

		results, err := repo.List(context.Background())
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPropagationPolicyRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM propagation_policies WHERE id`).
			WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newPropagationMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM propagation_policies WHERE id`).
			WithArgs(id).WillReturnError(fmt.Errorf("cascade"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
