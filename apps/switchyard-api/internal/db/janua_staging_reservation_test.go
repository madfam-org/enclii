package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestJanuaReservationForeignScopeOrExistingStageCausesNoInsert(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			pool, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer pool.Close()
			r := NewRepositories(pool)
			ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
			mock.ExpectBegin()
			query := mock.ExpectQuery(`SELECT plan.code`).WithArgs(ids[0], ids[1], ids[2], "standard-0", ids[5])
			if !conflict {
				query.WillReturnError(sql.ErrNoRows)
			} else {
				query.WillReturnRows(januaReservationPlanRows())
				mock.ExpectQuery(`(?s)SELECT.*COUNT`).WithArgs(ids[3], ids[0], ids[4], ids[1]).WillReturnRows(sqlmock.NewRows([]string{"conflicts"}).AddRow(1))
			}
			mock.ExpectRollback()
			_, err = r.ReserveJanuaStaging(context.Background(), ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], "standard-0", "isolated source fixture")
			require.Error(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func januaReservationPlanRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"code", "engine", "storage_gb", "replica_count", "cpu_request", "memory_request", "max_connections", "ha_enabled", "available"}).AddRow("standard-0", "postgres", 1, 1, "100m", "256Mi", 100, false, true)
}

func TestJanuaReservationServiceAddonAndAuditCommitAtomically(t *testing.T) {
	for _, auditFails := range []bool{false, true} {
		t.Run(fmt.Sprint(auditFails), func(t *testing.T) {
			pool, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer pool.Close()
			r := NewRepositories(pool)
			ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT plan.code`).WithArgs(ids[0], ids[1], ids[2], "standard-0", ids[5]).WillReturnRows(januaReservationPlanRows())
			mock.ExpectQuery(`(?s)SELECT.*COUNT`).WithArgs(ids[3], ids[0], ids[4], ids[1]).WillReturnRows(sqlmock.NewRows([]string{"conflicts"}).AddRow(0))
			mock.ExpectExec(`INSERT INTO services`).WithArgs(ids[3], ids[0]).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`INSERT INTO database_addons`).WithArgs(ids[4], ids[0], ids[1], "standard-0", sqlmock.AnyArg(), "project-"+ids[0].String()[:8], ids[5]).WillReturnResult(sqlmock.NewResult(0, 1))
			audit := mock.ExpectExec(`INSERT INTO managed_db_addon_events`)
			if auditFails {
				audit.WillReturnError(fmt.Errorf("audit unavailable"))
				mock.ExpectRollback()
			} else {
				audit.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			addon, err := r.ReserveJanuaStaging(context.Background(), ids[0], ids[1], ids[2], ids[3], ids[4], ids[5], ids[6], "standard-0", "isolated source fixture")
			if auditFails {
				require.Error(t, err)
				require.Nil(t, addon)
			} else {
				require.NoError(t, err)
				require.Equal(t, ids[4], addon.ID)
				require.Equal(t, "janua_staging_runtime", addon.Config.RuntimeRole)
				require.Equal(t, "janua_staging", addon.Config.DatabaseName)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
