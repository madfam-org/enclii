package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCostMockDB(t *testing.T) (*CostAllocationRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewCostAllocationRepository(db)
	return repo, mock, func() { db.Close() }
}

func TestCostAllocationRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCostMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		hostID := uuid.New()
		ca := &types.CostAllocation{
			BareMetalHostID:   hostID,
			TenantID:          "tenant-1",
			AllocationPercent: 50.0,
			PeriodStart:       now.Add(-24 * time.Hour),
			PeriodEnd:         now,
			CostCents:         1200,
		}

		mock.ExpectQuery(`INSERT INTO cost_allocations`).
			WithArgs(
				sqlmock.AnyArg(), hostID, "tenant-1", 50.0,
				ca.PeriodStart, ca.PeriodEnd, 1200,
			).
			WillReturnRows(sqlmock.NewRows([]string{"created_at"}).AddRow(now))

		err := repo.Create(context.Background(), ca)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, ca.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCostMockDB(t)
		defer cleanup()

		ca := &types.CostAllocation{BareMetalHostID: uuid.New(), TenantID: "t1", CostCents: 100}
		mock.ExpectQuery(`INSERT INTO cost_allocations`).WillReturnError(fmt.Errorf("constraint"))

		err := repo.Create(context.Background(), ca)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCostAllocationRepository_ListByTenant(t *testing.T) {
	t.Run("with results", func(t *testing.T) {
		repo, mock, cleanup := newCostMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		start := now.Add(-48 * time.Hour)
		end := now
		rows := mockCostAllocationRows().
			AddRow(uuid.New(), uuid.New(), "tenant-1", 50.0, start, end, 1200, now).
			AddRow(uuid.New(), uuid.New(), "tenant-1", 30.0, start, end, 720, now)

		mock.ExpectQuery(`SELECT id, bare_metal_host_id`).
			WithArgs("tenant-1", start, end).WillReturnRows(rows)

		results, err := repo.ListByTenant(context.Background(), "tenant-1", start, end)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newCostMockDB(t)
		defer cleanup()

		now := time.Now()
		mock.ExpectQuery(`SELECT id, bare_metal_host_id`).
			WithArgs("missing-tenant", now, now).WillReturnRows(mockCostAllocationRows())

		results, err := repo.ListByTenant(context.Background(), "missing-tenant", now, now)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCostAllocationRepository_ListByHost(t *testing.T) {
	t.Run("with results", func(t *testing.T) {
		repo, mock, cleanup := newCostMockDB(t)
		defer cleanup()

		hostID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		start := now.Add(-24 * time.Hour)
		rows := mockCostAllocationRows().
			AddRow(uuid.New(), hostID, "t1", 60.0, start, now, 1440, now)

		mock.ExpectQuery(`SELECT id, bare_metal_host_id`).
			WithArgs(hostID, start, now).WillReturnRows(rows)

		results, err := repo.ListByHost(context.Background(), hostID, start, now)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, hostID, results[0].BareMetalHostID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCostAllocationRepository_UpdateCostCents(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCostMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE cost_allocations SET cost_cents`).
			WithArgs(id, 2500).WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateCostCents(context.Background(), id, 2500)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newCostMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE cost_allocations SET cost_cents`).
			WithArgs(id, 0).WillReturnError(fmt.Errorf("deadlock"))

		err := repo.UpdateCostCents(context.Background(), id, 0)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestCostAllocationRepository_GetSummary(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newCostMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		start := now.Add(-7 * 24 * time.Hour)
		// GetSummary scans idStr as string, not uuid
		summaryRows := sqlmock.NewRows([]string{
			"id", "bare_metal_host_id", "tenant_id",
			"allocation_percent", "period_start", "period_end",
			"cost_cents", "created_at",
		}).AddRow("", uuid.New(), "tenant-1", 100.0, start, now, 8400, now)

		mock.ExpectQuery(`SELECT '' AS id, bare_metal_host_id`).
			WithArgs(start, now).WillReturnRows(summaryRows)

		results, err := repo.GetSummary(context.Background(), start, now)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, 8400, results[0].CostCents)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
