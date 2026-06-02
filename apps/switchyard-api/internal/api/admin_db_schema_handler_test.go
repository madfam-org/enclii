package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

func TestGetAdminDBSchema(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	embedded, err := db.LatestEmbeddedMigration()
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT version, dirty FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "dirty"}).AddRow(embedded.Version, false))
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs("services", "rollout_blocked_reason").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	h := &Handler{repos: db.NewRepositories(sqlDB)}

	router := gin.New()
	router.GET("/v1/admin/db/schema", h.GetAdminDBSchema)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/db/schema", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var report db.SchemaReport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	assert.Equal(t, embedded.Version, report.Status.Version)
	assert.False(t, report.Status.Dirty)
	assert.True(t, report.SchemaTableSeen)
	assert.True(t, report.Healthy)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAdminDBSchema_UnavailableDB(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	router := gin.New()
	router.GET("/v1/admin/db/schema", h.GetAdminDBSchema)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/db/schema", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
