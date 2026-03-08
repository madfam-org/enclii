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

// --- helpers ---

func newRouteMockDB(t *testing.T) (*RouteRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewRouteRepository(db)
	return repo, mock, func() { db.Close() }
}

var routeColumns = []string{
	"id", "service_id", "environment_id", "path", "path_type", "port",
	"created_at", "updated_at",
}

// --- Create ---

func TestRouteRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		envID := uuid.New()
		route := &types.Route{
			ServiceID:     svcID,
			EnvironmentID: envID,
			Path:          "/api/v1",
			PathType:      "Prefix",
			Port:          8080,
		}

		now := time.Now()
		id := uuid.New()
		mock.ExpectQuery(`INSERT INTO routes`).
			WithArgs(sqlmock.AnyArg(), svcID, envID, "/api/v1", "Prefix", 8080).
			WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
				AddRow(id, now, now))

		err := repo.Create(context.Background(), route)
		assert.NoError(t, err)
		assert.False(t, route.CreatedAt.IsZero())
		assert.False(t, route.UpdatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		route := &types.Route{
			ServiceID:     uuid.New(),
			EnvironmentID: uuid.New(),
			Path:          "/fail",
			PathType:      "Exact",
			Port:          3000,
		}

		mock.ExpectQuery(`INSERT INTO routes`).
			WithArgs(sqlmock.AnyArg(), route.ServiceID, route.EnvironmentID, "/fail", "Exact", 3000).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(context.Background(), route)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create route")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestRouteRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		id := uuid.New()
		svcID := uuid.New()
		envID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, service_id, environment_id, path, path_type, port`).
			WithArgs(id.String()).
			WillReturnRows(sqlmock.NewRows(routeColumns).
				AddRow(id, svcID, envID, "/api/v1", "Prefix", 8080, now, now))

		result, err := repo.GetByID(context.Background(), id.String())
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "/api/v1", result.Path)
		assert.Equal(t, "Prefix", result.PathType)
		assert.Equal(t, 8080, result.Port)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, environment_id, path, path_type, port`).
			WithArgs(id.String()).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id.String())
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "route not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, environment_id, path, path_type, port`).
			WithArgs(id.String()).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByID(context.Background(), id.String())
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get route")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByServiceAndEnvironment ---

func TestRouteRepository_GetByServiceAndEnvironment(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		envID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(routeColumns).
			AddRow(uuid.New(), svcID, envID, "/api/v1", "Prefix", 8080, now, now).
			AddRow(uuid.New(), svcID, envID, "/health", "Exact", 8080, now, now)

		mock.ExpectQuery(`SELECT id, service_id, environment_id, path, path_type, port`).
			WithArgs(svcID.String(), envID.String()).
			WillReturnRows(rows)

		results, err := repo.GetByServiceAndEnvironment(context.Background(), svcID.String(), envID.String())
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "/api/v1", results[0].Path)
		assert.Equal(t, "/health", results[1].Path)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty results", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		envID := uuid.New()

		mock.ExpectQuery(`SELECT id, service_id, environment_id, path, path_type, port`).
			WithArgs(svcID.String(), envID.String()).
			WillReturnRows(sqlmock.NewRows(routeColumns))

		results, err := repo.GetByServiceAndEnvironment(context.Background(), svcID.String(), envID.String())
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		envID := uuid.New()

		mock.ExpectQuery(`SELECT id, service_id, environment_id, path, path_type, port`).
			WithArgs(svcID.String(), envID.String()).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.GetByServiceAndEnvironment(context.Background(), svcID.String(), envID.String())
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Update ---

func TestRouteRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now()
		route := &types.Route{
			ID:       id,
			Path:     "/api/v2",
			PathType: "Prefix",
			Port:     9090,
		}

		mock.ExpectQuery(`UPDATE routes`).
			WithArgs("/api/v2", "Prefix", 9090, id).
			WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))

		err := repo.Update(context.Background(), route)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		route := &types.Route{
			ID:       uuid.New(),
			Path:     "/fail",
			PathType: "Exact",
			Port:     3000,
		}

		mock.ExpectQuery(`UPDATE routes`).
			WithArgs("/fail", "Exact", 3000, route.ID).
			WillReturnError(fmt.Errorf("update failed"))

		err := repo.Update(context.Background(), route)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update route")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestRouteRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM routes WHERE id`).
			WithArgs(id.String()).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id.String())
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM routes WHERE id`).
			WithArgs(id.String()).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id.String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "route not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM routes WHERE id`).
			WithArgs(id.String()).
			WillReturnError(fmt.Errorf("foreign key violation"))

		err := repo.Delete(context.Background(), id.String())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete route")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- DeleteByServiceID ---

func TestRouteRepository_DeleteByServiceID(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectExec(`DELETE FROM routes WHERE service_id`).
			WithArgs(svcID.String()).
			WillReturnResult(sqlmock.NewResult(0, 3))

		err := repo.DeleteByServiceID(context.Background(), svcID.String())
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newRouteMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectExec(`DELETE FROM routes WHERE service_id`).
			WithArgs(svcID.String()).
			WillReturnError(fmt.Errorf("db unavailable"))

		err := repo.DeleteByServiceID(context.Background(), svcID.String())
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
