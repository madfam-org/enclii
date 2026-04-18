package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPlanRepoMockDB(t *testing.T) (*ManagedDBPlanRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return NewManagedDBPlanRepository(db), mock, func() { db.Close() }
}

var planColumns = []string{
	"code", "engine", "display_name", "tier", "storage_gb",
	"cpu_request", "memory_request", "max_connections",
	"ha_enabled", "replica_count", "available",
	"price_cents_month", "created_at", "updated_at",
}

func TestManagedDBPlanRepository_GetByCode(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newPlanRepoMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(`FROM managed_db_plans WHERE code = \$1`).
			WithArgs("standard-0").
			WillReturnRows(sqlmock.NewRows(planColumns).AddRow(
				"standard-0", "postgres", "Standard 0", "standard", 1,
				"100m", "256Mi", 10, false, 1, true, int64(0), now, now,
			))

		plan, err := repo.GetByCode(context.Background(), "standard-0")
		require.NoError(t, err)
		assert.Equal(t, "standard-0", plan.Code)
		assert.Equal(t, "postgres", plan.Engine)
		assert.Equal(t, 1, plan.StorageGB)
		assert.True(t, plan.Available)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newPlanRepoMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`FROM managed_db_plans WHERE code = \$1`).
			WithArgs("does-not-exist").
			WillReturnError(sql.ErrNoRows)

		_, err := repo.GetByCode(context.Background(), "does-not-exist")
		assert.ErrorIs(t, err, sql.ErrNoRows)
	})

	t.Run("empty code rejected", func(t *testing.T) {
		repo, _, cleanup := newPlanRepoMockDB(t)
		defer cleanup()

		_, err := repo.GetByCode(context.Background(), "")
		assert.Error(t, err)
	})
}

func TestManagedDBPlanRepository_ListAvailable(t *testing.T) {
	t.Run("filtered by engine", func(t *testing.T) {
		repo, mock, cleanup := newPlanRepoMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(`WHERE available = true AND engine = \$1`).
			WithArgs("postgres").
			WillReturnRows(sqlmock.NewRows(planColumns).
				AddRow("standard-0", "postgres", "Standard 0", "standard", 1, "100m", "256Mi", 10, false, 1, true, int64(0), now, now).
				AddRow("standard-1", "postgres", "Standard 1", "standard", 10, "500m", "1Gi", 40, false, 1, true, int64(0), now, now))

		plans, err := repo.ListAvailable(context.Background(), "postgres")
		require.NoError(t, err)
		assert.Len(t, plans, 2)
		assert.Equal(t, "standard-0", plans[0].Code)
	})

	t.Run("no engine filter", func(t *testing.T) {
		repo, mock, cleanup := newPlanRepoMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(`WHERE available = true\s*ORDER BY engine`).
			WillReturnRows(sqlmock.NewRows(planColumns).
				AddRow("standard-0", "postgres", "Standard 0", "standard", 1, "100m", "256Mi", 10, false, 1, true, int64(0), now, now))

		plans, err := repo.ListAvailable(context.Background(), "")
		require.NoError(t, err)
		assert.Len(t, plans, 1)
	})

	t.Run("empty result is []", func(t *testing.T) {
		repo, mock, cleanup := newPlanRepoMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`WHERE available = true AND engine = \$1`).
			WithArgs("redis").
			WillReturnRows(sqlmock.NewRows(planColumns))

		plans, err := repo.ListAvailable(context.Background(), "redis")
		require.NoError(t, err)
		assert.NotNil(t, plans)
		assert.Len(t, plans, 0)
	})
}

func TestManagedDBPlanRepository_ListAll(t *testing.T) {
	repo, mock, cleanup := newPlanRepoMockDB(t)
	defer cleanup()

	now := time.Now()
	mock.ExpectQuery(`FROM managed_db_plans ORDER BY engine`).
		WillReturnRows(sqlmock.NewRows(planColumns).
			AddRow("standard-0", "postgres", "S0", "standard", 1, "100m", "256Mi", 10, false, 1, true, int64(0), now, now).
			AddRow("deprecated-1", "postgres", "Old", "standard", 100, "1", "4Gi", 500, false, 1, false, int64(0), now, now))

	plans, err := repo.ListAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, plans, 2)
	// Unavailable plans surface in ListAll.
	assert.False(t, plans[1].Available)
}
