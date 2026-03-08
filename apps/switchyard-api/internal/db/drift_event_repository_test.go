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

func newDriftMockDB(t *testing.T) (*DriftEventRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewDriftEventRepository(db)
	return repo, mock, func() { db.Close() }
}

func TestDriftEventRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		clusterID := uuid.New()
		de := &types.DriftEvent{
			Source:       types.DriftSourceArgoCD,
			ResourceType: "Deployment",
			ResourceName: "switchyard-api",
			ClusterID:    &clusterID,
			Severity:     types.DriftSeverityHigh,
		}

		mock.ExpectQuery(`INSERT INTO drift_events`).
			WithArgs(
				sqlmock.AnyArg(), types.DriftSourceArgoCD, "Deployment", "switchyard-api",
				&clusterID, sqlmock.AnyArg(), types.DriftSeverityHigh,
			).
			WillReturnRows(sqlmock.NewRows([]string{"detected_at", "created_at"}).AddRow(now, now))

		err := repo.Create(context.Background(), de)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, de.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		de := &types.DriftEvent{Source: types.DriftSourceManual, ResourceType: "Pod", ResourceName: "test", Severity: types.DriftSeverityLow}
		mock.ExpectQuery(`INSERT INTO drift_events`).WillReturnError(fmt.Errorf("constraint"))

		err := repo.Create(context.Background(), de)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDriftEventRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		rows := mockDriftEventRows().
			AddRow(id, "argocd", "Deployment", "api", nil, []byte("{}"), "high", false, nil, now, now)

		mock.ExpectQuery(`SELECT id, source, resource_type`).WithArgs(id).WillReturnRows(rows)

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, types.DriftSourceArgoCD, result.Source)
		assert.Equal(t, types.DriftSeverityHigh, result.Severity)
		assert.False(t, result.Resolved)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, source, resource_type`).WithArgs(id).WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDriftEventRepository_List(t *testing.T) {
	t.Run("all events", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := mockDriftEventRows().
			AddRow(uuid.New(), "argocd", "Deployment", "api", nil, []byte("{}"), "high", false, nil, now, now).
			AddRow(uuid.New(), "manual", "Service", "web", nil, []byte("{}"), "low", true, &now, now, now)

		mock.ExpectQuery(`SELECT id, source, resource_type`).
			WithArgs(nil).WillReturnRows(rows)

		results, err := repo.List(context.Background(), nil)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("unresolved only", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		resolved := false
		rows := mockDriftEventRows().
			AddRow(uuid.New(), "crossplane", "Database", "pg", nil, []byte("{}"), "medium", false, nil, now, now)

		mock.ExpectQuery(`SELECT id, source, resource_type`).
			WithArgs(&resolved).WillReturnRows(rows)

		results, err := repo.List(context.Background(), &resolved)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.False(t, results[0].Resolved)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, source, resource_type`).
			WithArgs(nil).WillReturnRows(mockDriftEventRows())

		results, err := repo.List(context.Background(), nil)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDriftEventRepository_Resolve(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE drift_events SET resolved=true`).
			WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Resolve(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newDriftMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE drift_events SET resolved=true`).
			WithArgs(id).WillReturnError(fmt.Errorf("connection lost"))

		err := repo.Resolve(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
