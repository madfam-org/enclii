package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReconstructionAddonIsNotUnboundWhileSuspendedBindingExists(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer database.Close()
	addon, service := uuid.New(), uuid.New()
	at := time.Now()
	mock.ExpectQuery("SELECT .* FROM database_addon_bindings WHERE addon_id=\\$1 AND status <> 'deleted'").WithArgs(addon).WillReturnRows(sqlmock.NewRows([]string{"id", "addon_id", "service_id", "env_var_name", "status", "created_at", "updated_at"}).AddRow(uuid.New(), addon, service, "DATABASE_URL", "suspended", at, at))
	rows, err := NewDatabaseAddonRepository(database).GetNonDeletedBindingsByAddon(context.Background(), addon)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "suspended", string(rows[0].Status))
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestReconstructionBindingReadDoesNotTurnUncertaintyIntoEmpty(t *testing.T) {
	for _, kind := range []string{"query failure", "partial row failure", "empty verified"} {
		t.Run(kind, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			addon := uuid.New()
			query := mock.ExpectQuery("SELECT .* FROM database_addon_bindings WHERE addon_id=\\$1 AND status <> 'deleted'").WithArgs(addon)
			if kind == "query failure" {
				query.WillReturnError(errors.New("unavailable"))
			} else {
				rows := sqlmock.NewRows([]string{"id", "addon_id", "service_id", "env_var_name", "status", "created_at", "updated_at"})
				if kind == "partial row failure" {
					rows.AddRow(uuid.New(), addon, uuid.New(), "DATABASE_URL", "suspended", time.Now(), time.Now()).RowError(0, errors.New("partial"))
				}
				query.WillReturnRows(rows)
			}
			rows, err := NewDatabaseAddonRepository(database).GetNonDeletedBindingsByAddon(context.Background(), addon)
			if kind == "empty verified" {
				require.NoError(t, err)
				require.Empty(t, rows)
			} else {
				require.Error(t, err)
				require.Nil(t, rows)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestReconstructionActiveBindingReadsRejectPartialAuthority(t *testing.T) {
	for _, scope := range []string{"service_id", "addon_id"} {
		t.Run(scope, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			id := uuid.New()
			at := time.Now()
			rows := sqlmock.NewRows([]string{"id", "addon_id", "service_id", "env_var_name", "status", "created_at", "updated_at"}).AddRow(uuid.New(), uuid.New(), uuid.New(), "DATABASE_URL", "active", at, at).AddRow(uuid.New(), uuid.New(), uuid.New(), "DATABASE_URL", "active", at, at).RowError(1, errors.New("second binding unreadable"))
			mock.ExpectQuery("SELECT .* FROM database_addon_bindings WHERE " + scope + " = \\$1 AND status = 'active'").WithArgs(id).WillReturnRows(rows)
			repository := NewDatabaseAddonRepository(database)
			if scope == "service_id" {
				bindings, e := repository.GetBindingsByService(context.Background(), id)
				require.Error(t, e)
				require.Nil(t, bindings)
			} else {
				bindings, e := repository.GetBindingsByAddon(context.Background(), id)
				require.Error(t, e)
				require.Nil(t, bindings)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
