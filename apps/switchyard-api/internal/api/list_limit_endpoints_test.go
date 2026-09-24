package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// Every list endpoint below used to replace an out-of-range limit (or
// limit_per_project / offset) with its default, or clamp it, without saying
// so. Each now answers 400 naming the accepted range, before it reads
// anything: the sqlmock expects no query at all.

type limitEndpointCase struct {
	name    string
	route   string
	handler func(h *Handler) gin.HandlerFunc
	path    string // request path, without the query
	extra   string // query parameters the endpoint needs besides the limit
	param   string // limit, offset or limit_per_project
	max     int    // 0 for offset
}

func limitEndpointCases() []limitEndpointCase {
	projectUUID := uuid.NewString()
	return []limitEndpointCase{
		{name: "deployment groups limit", route: "/v1/projects/:slug/deployment-groups", path: "/v1/projects/p/deployment-groups",
			handler: func(h *Handler) gin.HandlerFunc { return h.ListDeploymentGroups }, param: "limit", max: 100},
		{name: "deployment groups offset", route: "/v1/projects/:slug/deployment-groups", path: "/v1/projects/p/deployment-groups",
			handler: func(h *Handler) gin.HandlerFunc { return h.ListDeploymentGroups }, param: "offset"},
		{name: "function logs", route: "/v1/functions/:id/logs", path: "/v1/functions/" + uuid.NewString() + "/logs",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetFunctionLogs }, param: "limit", max: 1000},
		{name: "domains limit", route: "/v1/domains", path: "/v1/domains",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetAllDomains }, param: "limit", max: 500},
		{name: "domains offset", route: "/v1/domains", path: "/v1/domains",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetAllDomains }, param: "offset"},
		{name: "lifecycle timeline", route: "/v1/lifecycle/timeline/:owner/:repo", path: "/v1/lifecycle/timeline/o/r",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetLifecycleTimeline }, param: "limit", max: 200},
		{name: "lifecycle branch", route: "/v1/lifecycle/branch/:owner/:repo/:branch", path: "/v1/lifecycle/branch/o/r/main",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetLifecycleBranch }, param: "limit", max: 200},
		{name: "lifecycle events", route: "/v1/lifecycle/events", path: "/v1/lifecycle/events",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetLifecycleEvents }, param: "limit", max: 200},
		{name: "observability errors", route: "/v1/observability/errors", path: "/v1/observability/errors",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetRecentErrors }, param: "limit", max: 200},
		{name: "project processes", route: "/v1/projects/:slug/processes", path: "/v1/projects/p/processes",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetProjectProcesses }, param: "limit", max: 100},
		{name: "project process summaries", route: "/v1/project-processes/summary", path: "/v1/project-processes/summary",
			extra: "project_ids=" + projectUUID, handler: func(h *Handler) gin.HandlerFunc { return h.GetProjectProcessSummaries },
			param: "limit_per_project", max: 20},
		{name: "project process summary stream", route: "/v1/project-processes/stream", path: "/v1/project-processes/stream",
			extra: "project_ids=" + projectUUID, handler: func(h *Handler) gin.HandlerFunc { return h.StreamProjectProcessSummaries },
			param: "limit_per_project", max: 20},
		{name: "storage objects", route: "/v1/projects/:slug/storage/buckets/:bucket/objects", path: "/v1/projects/p/storage/buckets/b/objects",
			handler: func(h *Handler) gin.HandlerFunc { return h.ListObjects }, param: "limit", max: 1000},
		{name: "featured templates", route: "/v1/templates/featured", path: "/v1/templates/featured",
			handler: func(h *Handler) gin.HandlerFunc { return h.GetFeaturedTemplates }, param: "limit", max: 100},
		{name: "template search", route: "/v1/templates/search", path: "/v1/templates/search", extra: "q=go",
			handler: func(h *Handler) gin.HandlerFunc { return h.SearchTemplates }, param: "limit", max: 100},
	}
}

// newLimitTestHandler wires every repository these handlers touch to one
// sqlmock, so "no query was run" is a single ExpectationsWereMet.
func newLimitTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return &Handler{
		repos: &db.Repositories{
			Projects:         db.NewProjectRepository(database),
			Services:         db.NewServiceRepository(database),
			LifecycleEvents:  db.NewLifecycleEventRepository(database),
			Templates:        db.NewTemplateRepository(database),
			AuditLogs:        db.NewAuditLogRepository(database),
			DeploymentGroups: db.NewDeploymentGroupRepository(database),
		},
		logger: testLogger(t),
	}, mock
}

func TestListEndpoints_OutOfRangeLimitIs400BeforeAnyQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range limitEndpointCases() {
		var bad []string
		var wantErr string
		if tc.max == 0 {
			bad = []string{"-1", "x"}
			wantErr = "must be a non-negative integer"
		} else {
			bad = []string{"0", "-3", "abc", "1.5", strconv.Itoa(tc.max + 1)}
			wantErr = "must be an integer from 1 to " + strconv.Itoa(tc.max)
		}
		for _, v := range bad {
			t.Run(tc.name+" "+tc.param+"="+v, func(t *testing.T) {
				h, mock := newLimitTestHandler(t)
				router := gin.New()
				withTestAdminContext(router)
				router.GET(tc.route, tc.handler(h))

				query := tc.param + "=" + v
				if tc.extra != "" {
					query = tc.extra + "&" + query
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path+"?"+query, nil))

				require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
				var body struct {
					Error string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Contains(t, body.Error, "invalid "+tc.param+` "`+v+`"`)
				assert.Contains(t, body.Error, wantErr)
				require.NoError(t, mock.ExpectationsWereMet(), "no query on bad input")
			})
		}
	}
}

var lifecycleEventColumns = []string{
	"id", "deployment_id", "release_id", "ci_run_id", "project_id", "service_id",
	"repo_full_name", "commit_sha", "branch", "ref", "target_env",
	"event_type", "source", "message", "metadata", "created_at",
}

func TestGetLifecycleEvents_MaxLimitReachesQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock := newLimitTestHandler(t)
	mock.ExpectQuery(`FROM deployment_lifecycle_events`).WithArgs(200).
		WillReturnRows(sqlmock.NewRows(lifecycleEventColumns))

	router := gin.New()
	router.GET("/v1/lifecycle/events", h.GetLifecycleEvents)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/lifecycle/events?limit=200", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetLifecycleEvents_DefaultLimitWhenAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock := newLimitTestHandler(t)
	mock.ExpectQuery(`FROM deployment_lifecycle_events`).WithArgs(50).
		WillReturnRows(sqlmock.NewRows(lifecycleEventColumns))

	router := gin.New()
	router.GET("/v1/lifecycle/events", h.GetLifecycleEvents)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/lifecycle/events", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSearchTemplates_InRangeLimitReachesQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock := newLimitTestHandler(t)
	mock.ExpectQuery(`FROM templates`).WithArgs("%go%", "go", 100).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	router := gin.New()
	router.GET("/v1/templates/search", h.SearchTemplates)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/templates/search?q=go&limit=100", nil))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())
}
