package api

import (
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

func TestGetPreview_CrossTenantDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupAuthzHandler(t)
	defer cleanup()

	userA := uuid.New()
	projectB := uuid.New()
	previewID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM preview_environments WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "service_id", "pr_number", "pr_title", "pr_url", "pr_author",
			"pr_branch", "pr_base_branch", "commit_sha", "preview_subdomain", "preview_url",
			"status", "status_message", "auto_sleep_after", "last_accessed_at", "sleeping_since",
			"deployment_id", "build_logs_url", "created_at", "updated_at", "closed_at",
		}).AddRow(
			previewID, projectB, uuid.New(), 1, "", "", "", "feat", "main", "abc",
			"pr-1", "https://pr-1.preview.enclii.dev", "active", "", 60, nil, nil,
			nil, "", now, now, nil,
		))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(userA, projectB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userA, "developer"))
	engine.GET("/v1/previews/:id", h.GetPreview)

	req, _ := http.NewRequest(http.MethodGet, "/v1/previews/"+previewID.String(), nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "NOT_FOUND")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetFunction_CrossTenantDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupAuthzHandler(t)
	defer cleanup()

	userA := uuid.New()
	projectB := uuid.New()
	fnID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM functions WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "name", "config", "status", "status_message",
			"k8s_namespace", "k8s_resource_name", "image_uri", "endpoint",
			"available_replicas", "invocation_count", "avg_duration_ms", "last_invoked_at",
			"created_by", "created_by_email", "created_at", "updated_at", "deployed_at", "deleted_at",
		}).AddRow(
			fnID, projectB, "fn", []byte(`{}`), "ready", "",
			"", "", "", "",
			0, 0, 0, nil,
			nil, "", now, now, nil, nil,
		))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(userA, projectB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userA, "developer"))
	engine.GET("/v1/functions/:id", h.GetFunction)

	req, _ := http.NewRequest(http.MethodGet, "/v1/functions/"+fnID.String(), nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "NOT_FOUND")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetWebhook_CrossTenantDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	h := &Handler{
		repos: &db.Repositories{
			Projects:      db.NewProjectRepository(database),
			Webhooks:      db.NewWebhookRepository(database),
			ProjectAccess: db.NewProjectAccessRepository(database),
		},
		logger: testLogger(t),
	}

	userA := uuid.New()
	projectB := uuid.New()
	webhookID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM webhook_destinations WHERE id`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "name", "type", "webhook_url",
			"telegram_bot_token", "telegram_chat_id", "custom_headers", "signing_secret",
			"events", "enabled", "last_delivery_at", "last_delivery_status", "last_delivery_error",
			"consecutive_failures", "auto_disabled_at",
			"created_by", "created_by_email", "created_at", "updated_at",
		}).AddRow(
			webhookID, projectB, "alerts", "slack", "https://hooks.example.com",
			nil, nil, []byte(`{}`), nil,
			[]byte(`["deployment.succeeded"]`), true, nil, nil, nil,
			0, nil,
			nil, nil, now, now,
		))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(userA, projectB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userA, "developer"))
	engine.GET("/v1/webhooks/:id", h.GetWebhook)

	req, _ := http.NewRequest(http.MethodGet, "/v1/webhooks/"+webhookID.String(), nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "NOT_FOUND")
	assert.NoError(t, mock.ExpectationsWereMet())
}
