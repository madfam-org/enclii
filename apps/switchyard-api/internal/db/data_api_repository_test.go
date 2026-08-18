package db

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func newDataAPIRepoMockDB(t *testing.T) (*DataAPIRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return NewDataAPIRepository(db), mock, func() { _ = db.Close() }
}

var dataAPICols = []string{
	"addon_id", "project_id", "status", "status_message", "schemas", "anon_role", "db_pool",
	"jwt_secret_name", "host", "k8s_resource_name",
	"created_at", "updated_at", "enabled_at", "disabled_at",
}

func TestDataAPIRepository_Upsert(t *testing.T) {
	repo, mock, cleanup := newDataAPIRepoMockDB(t)
	defer cleanup()

	addonID := uuid.New()
	projectID := uuid.New()
	d := &types.DataAPI{
		AddonID:         addonID,
		ProjectID:       projectID,
		Status:          types.DataAPIStatusPending,
		Schemas:         "public",
		AnonRole:        "anon",
		DBPool:          10,
		JWTSecretName:   "data-abcdef12-jwt",
		Host:            "orders.data.enclii.dev",
		K8sResourceName: "data-abcdef12",
	}

	mock.ExpectExec(`INSERT INTO managed_db_data_apis`).
		WithArgs(
			addonID, projectID, types.DataAPIStatusPending, "", "public", "anon", 10,
			"data-abcdef12-jwt", "orders.data.enclii.dev", "data-abcdef12",
			sqlmock.AnyArg(), sqlmock.AnyArg(), nil, nil,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Upsert(context.Background(), d))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataAPIRepository_GetByAddon(t *testing.T) {
	repo, mock, cleanup := newDataAPIRepoMockDB(t)
	defer cleanup()

	addonID := uuid.New()
	projectID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM managed_db_data_apis WHERE addon_id = \$1`).
		WithArgs(addonID).
		WillReturnRows(sqlmock.NewRows(dataAPICols).AddRow(
			addonID, projectID, "ready", "Data-API ready", "public,api", "anon", 8,
			"data-abcdef12-jwt", "orders.data.enclii.dev", "data-abcdef12",
			now, now, now, nil,
		))

	got, err := repo.GetByAddon(context.Background(), addonID)
	require.NoError(t, err)
	assert.Equal(t, addonID, got.AddonID)
	assert.Equal(t, types.DataAPIStatusReady, got.Status)
	assert.Equal(t, "public,api", got.Schemas)
	assert.Equal(t, 8, got.DBPool)
	require.NotNil(t, got.EnabledAt)
	assert.Nil(t, got.DisabledAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataAPIRepository_UpdateStatus(t *testing.T) {
	repo, mock, cleanup := newDataAPIRepoMockDB(t)
	defer cleanup()

	addonID := uuid.New()
	mock.ExpectExec(`UPDATE managed_db_data_apis`).
		WithArgs(types.DataAPIStatusReady, "up", sqlmock.AnyArg(), addonID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.UpdateStatus(context.Background(), addonID, types.DataAPIStatusReady, "up"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataAPIRepository_MarkDisabled(t *testing.T) {
	repo, mock, cleanup := newDataAPIRepoMockDB(t)
	defer cleanup()

	addonID := uuid.New()
	mock.ExpectExec(`UPDATE managed_db_data_apis`).
		WithArgs(types.DataAPIStatusDisabled, sqlmock.AnyArg(), sqlmock.AnyArg(), addonID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.MarkDisabled(context.Background(), addonID))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataAPIRepository_ListReconcilable(t *testing.T) {
	repo, mock, cleanup := newDataAPIRepoMockDB(t)
	defer cleanup()

	addonID := uuid.New()
	projectID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM managed_db_data_apis\s+WHERE status IN`).
		WillReturnRows(sqlmock.NewRows(dataAPICols).AddRow(
			addonID, projectID, "pending", "", "public", "anon", 10,
			"n-jwt", "h.data.enclii.dev", "n",
			now, now, nil, nil,
		))

	rows, err := repo.ListReconcilable(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, types.DataAPIStatusPending, rows[0].Status)
	require.NoError(t, mock.ExpectationsWereMet())
}
