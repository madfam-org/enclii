package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

func TestQueryLimitOffsetOr400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		query      string
		wantOK     bool
		wantLimit  int
		wantOffset int
		wantErr    string
	}{
		{query: "", wantOK: true, wantLimit: 50},
		{query: "limit=&offset=", wantOK: true, wantLimit: 50},
		{query: "limit=1", wantOK: true, wantLimit: 1},
		{query: "limit=100&offset=250", wantOK: true, wantLimit: 100, wantOffset: 250},
		{query: "limit=0", wantErr: `invalid limit "0": must be an integer from 1 to 100`},
		{query: "limit=101", wantErr: `invalid limit "101": must be an integer from 1 to 100`},
		{query: "limit=-5", wantErr: "invalid limit"},
		{query: "limit=ten", wantErr: "invalid limit"},
		{query: "limit=2.5", wantErr: "invalid limit"},
		{query: "offset=-1", wantErr: `invalid offset "-1": must be a non-negative integer`},
		{query: "offset=abc", wantErr: "invalid offset"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/x?"+tc.query, nil)
			limit, offset, ok := queryLimitOffsetOr400(c, 50, 100)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantLimit, limit)
				assert.Equal(t, tc.wantOffset, offset)
				return
			}
			assert.Equal(t, http.StatusBadRequest, w.Code)
			var body struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Contains(t, body.Error, tc.wantErr)
		})
	}
}

func TestGetActivity_LimitOffset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newHandler := func(t *testing.T) (*Handler, sqlmock.Sqlmock) {
		database, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = database.Close() })
		return &Handler{repos: &db.Repositories{AuditLogs: db.NewAuditLogRepository(database)}, logger: testLogger(t)}, mock
	}

	t.Run("in-range values reach the query and are echoed", func(t *testing.T) {
		h, mock := newHandler(t)
		mock.ExpectQuery(`FROM audit_logs`).WithArgs(100, 200).
			WillReturnRows(sqlmock.NewRows(activityScanColumns))
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity?limit=100&offset=200", nil)
		h.GetActivity(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body ActivityListResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, 100, body.Limit)
		assert.Equal(t, 200, body.Offset)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	for _, q := range []string{"limit=500", "limit=0", "limit=x", "offset=-1"} {
		t.Run(q+" is a 400, not the default", func(t *testing.T) {
			h, mock := newHandler(t)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/activity?"+q, nil)
			h.GetActivity(c)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			require.NoError(t, mock.ExpectationsWereMet(), "no query on bad input")
		})
	}
}

func TestListAllDeployments_OutOfRangeLimitIs400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{logger: testLogger(t)} // no repos: a 400 must come first
	for _, q := range []string{"limit=101", "limit=0", "limit=all"} {
		t.Run(q, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/deployments?"+q, nil)
			h.ListAllDeployments(c)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "must be an integer from 1 to 100")
		})
	}
}

func TestListCronJobRuns_Pages(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()
	cronJobID, projectID, serviceID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
		WithArgs(cronJobID).
		WillReturnRows(sqlmock.NewRows(cronJobSelectColumns).
			AddRow(cronJobID, projectID, serviceID, "job", "* * * * *", "echo", sql.NullString{Valid: false}, 300, 0, false, "forbid", now, now, nil, nil))
	mock.ExpectQuery(`(?s)FROM cron_job_runs.*LIMIT \$2 OFFSET \$3`).
		WithArgs(cronJobID, 10, 20).
		WillReturnRows(sqlmock.NewRows(cronJobRunSelectColumns).
			AddRow(uuid.New(), cronJobID, "completed", int64(0), now, now, "out"))

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/cron-jobs/:id/runs", h.ListCronJobRuns)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cron-jobs/"+cronJobID.String()+"/runs?limit=10&offset=20", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["total"])
	assert.Equal(t, float64(10), resp["limit"])
	assert.Equal(t, float64(20), resp["offset"])
	assert.Len(t, resp["runs"], 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListCronJobRuns_OutOfRangeLimitIs400BeforeLookups(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()
	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/cron-jobs/:id/runs", h.ListCronJobRuns)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/cron-jobs/"+uuid.NewString()+"/runs?limit=500", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "must be an integer from 1 to 100")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListOneOffJobs_Pages(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()
	projectID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))
	mock.ExpectQuery(`(?s)FROM one_off_jobs.*LIMIT \$2 OFFSET \$3`).
		WithArgs(projectID, 25, 50).
		WillReturnRows(sqlmock.NewRows(oneOffJobSelectColumns))

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/projects/:slug/one-off-jobs", h.ListOneOffJobs)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/projects/test-project/one-off-jobs?limit=25&offset=50", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["total"])
	assert.Equal(t, float64(25), resp["limit"])
	assert.Equal(t, float64(50), resp["offset"])
	assert.Equal(t, []any{}, resp["one_off_jobs"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListOneOffJobs_BadOffsetIs400BeforeLookups(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()
	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/projects/:slug/one-off-jobs", h.ListOneOffJobs)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/projects/test-project/one-off-jobs?offset=-3", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid offset")
	require.NoError(t, mock.ExpectationsWereMet())
}
