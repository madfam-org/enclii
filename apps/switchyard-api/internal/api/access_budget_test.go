package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

func TestEnforceBudgetNotThrottled_Blocked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	h := &Handler{
		repos: &db.Repositories{
			WaybillThrottles: db.NewWaybillThrottleRepository(mockDB),
		},
		logger: testLogger(t),
	}

	projectID := uuid.New()
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(projectID, "non-production").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	ok := h.enforceBudgetNotThrottled(c, projectID, "staging")
	assert.False(t, ok)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assertErrorCode(t, w, "BUDGET_THROTTLED")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEnforceBudgetNotThrottled_Allowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	h := &Handler{
		repos: &db.Repositories{
			WaybillThrottles: db.NewWaybillThrottleRepository(mockDB),
		},
		logger: testLogger(t),
	}

	projectID := uuid.New()
	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs(projectID, "production").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	ok := h.enforceBudgetNotThrottled(c, projectID, "production")
	assert.True(t, ok)
	assert.NoError(t, mock.ExpectationsWereMet())
}
