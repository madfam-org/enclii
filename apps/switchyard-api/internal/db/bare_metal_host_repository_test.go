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

func newBMHMockDB(t *testing.T) (*BareMetalHostRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewBareMetalHostRepository(db)
	return repo, mock, func() { db.Close() }
}

func TestBareMetalHostRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		host := &types.BareMetalHost{
			Name:             "node-01",
			BMCAddress:       "10.0.0.1",
			BootMode:         "UEFI",
			State:            types.BMHStateDiscovered,
			PowerState:       types.BMHPowerOff,
			CostPerHourCents: 100,
		}

		mock.ExpectQuery(`INSERT INTO bare_metal_hosts`).
			WithArgs(
				sqlmock.AnyArg(), "node-01", nil, "10.0.0.1", "", "",
				"UEFI", types.BMHStateDiscovered, types.BMHPowerOff,
				sqlmock.AnyArg(), "", sqlmock.AnyArg(), sqlmock.AnyArg(), 100,
			).
			WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

		err := repo.Create(context.Background(), host)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, host.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		host := &types.BareMetalHost{Name: "fail-node", BootMode: "UEFI", State: types.BMHStateDiscovered, PowerState: types.BMHPowerOff}
		mock.ExpectQuery(`INSERT INTO bare_metal_hosts`).
			WillReturnError(fmt.Errorf("constraint violation"))

		err := repo.Create(context.Background(), host)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestBareMetalHostRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		rows := mockBareMetalHostRows().
			AddRow(id, "node-01", nil, "10.0.0.1", "vault:bmc", "aa:bb:cc:dd:ee:ff",
				"UEFI", "available", "on", []byte("{}"), "1.0", []byte("{}"), []byte("{}"), 100, nil, now, now)

		mock.ExpectQuery(`SELECT id, name, cluster_id`).
			WithArgs(id).WillReturnRows(rows)

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "node-01", result.Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, name, cluster_id`).
			WithArgs(id).WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestBareMetalHostRepository_List(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := mockBareMetalHostRows().
			AddRow(uuid.New(), "node-a", nil, "10.0.0.1", "", "", "UEFI", "available", "on", []byte("{}"), "", []byte("{}"), []byte("{}"), 50, nil, now, now).
			AddRow(uuid.New(), "node-b", nil, "10.0.0.2", "", "", "UEFI", "provisioned", "on", []byte("{}"), "", []byte("{}"), []byte("{}"), 75, nil, now, now)

		mock.ExpectQuery(`SELECT id, name, cluster_id`).WillReturnRows(rows)

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, cluster_id`).WillReturnRows(mockBareMetalHostRows())

		results, err := repo.List(context.Background())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestBareMetalHostRepository_UpdateState(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE bare_metal_hosts SET state`).
			WithArgs(id, types.BMHStateProvisioned, types.BMHPowerOn).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateState(context.Background(), id, types.BMHStateProvisioned, types.BMHPowerOn)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE bare_metal_hosts SET state`).
			WithArgs(id, types.BMHStateError, types.BMHPowerUnknown).
			WillReturnError(fmt.Errorf("connection lost"))

		err := repo.UpdateState(context.Background(), id, types.BMHStateError, types.BMHPowerUnknown)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestBareMetalHostRepository_UpdateHardwareProfile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newBMHMockDB(t)
		defer cleanup()

		id := uuid.New()
		profile := json.RawMessage(`{"cpu":"AMD Ryzen 5","ram":"64GB"}`)
		mock.ExpectExec(`UPDATE bare_metal_hosts SET hardware_profile`).
			WithArgs(id, profile).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateHardwareProfile(context.Background(), id, profile)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
