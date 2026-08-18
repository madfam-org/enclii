package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

var previewEnvColumns = []string{
	"id", "project_id", "service_id", "pr_number", "pr_title", "pr_url", "pr_author",
	"pr_branch", "pr_base_branch", "commit_sha", "preview_subdomain", "preview_url",
	"status", "status_message", "auto_sleep_after", "last_accessed_at", "sleeping_since",
	"deployment_id", "build_logs_url", "created_at", "updated_at", "closed_at",
}

func setupPreviewLifecycleHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	h, mock, cleanup := setupAuthzHandler(t)
	h.config = newTestConfig()
	h.buildSemaphore = make(chan struct{}, 1)
	return h, mock, cleanup
}

func expectServiceByID(mock sqlmock.Sqlmock, serviceID, projectID uuid.UUID, name, gitRepo string) {
	now := time.Now()
	mock.ExpectQuery(`FROM services WHERE id`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows(serviceGetByIDColumns).AddRow(
			serviceID, projectID, name, gitRepo, "", []byte(`{}`), []byte(`[]`),
			true, testDefaultBranch, "production", now, now, []byte(`[]`), "web", "default", nil,
		))
}

func expectServiceByGitRepo(mock sqlmock.Sqlmock, serviceID, projectID uuid.UUID, name, gitRepo string) {
	now := time.Now()
	mock.ExpectQuery(`FROM services WHERE git_repo = \$1`).
		WithArgs(gitRepo).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "name", "git_repo", "app_path", "build_config",
			"auto_deploy", "auto_deploy_branch", "auto_deploy_env", "created_at", "updated_at", "type", "region",
		}).AddRow(
			serviceID, projectID, name, gitRepo, "", []byte(`{}`),
			true, testDefaultBranch, "production", now, now, "web", "default",
		))
}

func previewEnvironmentRow(
	previewID, projectID, serviceID uuid.UUID,
	prNumber int,
	subdomain, previewURL, status string,
	now time.Time,
) *sqlmock.Rows {
	return sqlmock.NewRows(previewEnvColumns).AddRow(
		previewID, projectID, serviceID, prNumber,
		"Fix preview lifecycle", "https://github.com/test-org/test-project/pull/42", "preview-dev",
		"feature/preview", testDefaultBranch, testSHA(),
		subdomain, previewURL,
		status, "Preview environment created, starting build", 30,
		nil, nil, nil, "", now, now, nil,
	)
}

func TestPreviewLifecycle_APICreateGetClose(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, mock, cleanup := setupPreviewLifecycleHandler(t)
	defer cleanup()

	projectID := uuid.New()
	serviceID := uuid.New()
	now := time.Now()
	prNumber := 42
	subdomain := fmt.Sprintf("pr-%d-my-api", prNumber)
	previewURL := fmt.Sprintf("https://%s.preview.enclii.dev", subdomain)

	expectServiceByID(mock, serviceID, projectID, "my-api", testRepoHTMLURL)
	expectServiceByID(mock, serviceID, projectID, "my-api", testRepoHTMLURL)

	mock.ExpectQuery(`FROM preview_environments\s+WHERE service_id`).
		WithArgs(serviceID, prNumber).
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(`INSERT INTO preview_environments`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(uuid.New(), "admin"))
	engine.POST("/v1/previews", h.CreatePreview)
	engine.GET("/v1/previews/:id", h.GetPreview)
	engine.POST("/v1/previews/:id/close", h.ClosePreview)

	createBody := fmt.Sprintf(`{
		"service_id": %q,
		"pr_number": %d,
		"pr_title": "Fix preview lifecycle",
		"pr_url": "https://github.com/test-org/test-project/pull/42",
		"pr_author": "preview-dev",
		"pr_branch": "feature/preview",
		"pr_base_branch": "main",
		"commit_sha": %q
	}`, serviceID.String(), prNumber, testSHA())

	reqCreate, _ := http.NewRequest(http.MethodPost, "/v1/previews", bytes.NewBufferString(createBody))
	reqCreate.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, reqCreate)

	require.Equal(t, http.StatusCreated, w.Code)

	var createResp struct {
		Preview struct {
			ID         string `json:"id"`
			PreviewURL string `json:"preview_url"`
			Status     string `json:"status"`
		} `json:"preview"`
		Action string `json:"action"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	assert.Equal(t, "created", createResp.Action)
	assert.Equal(t, previewURL, createResp.Preview.PreviewURL)
	assert.Equal(t, string(types.PreviewStatusPending), createResp.Preview.Status)
	require.NotEmpty(t, createResp.Preview.ID)

	parsedPreviewID, err := uuid.Parse(createResp.Preview.ID)
	require.NoError(t, err)

	w = httptest.NewRecorder()
	mock.ExpectQuery(`FROM preview_environments WHERE id`).
		WithArgs(parsedPreviewID).
		WillReturnRows(previewEnvironmentRow(parsedPreviewID, projectID, serviceID, prNumber, subdomain, previewURL, "pending", now))

	reqGet, _ := http.NewRequest(http.MethodGet, "/v1/previews/"+parsedPreviewID.String(), nil)
	engine.ServeHTTP(w, reqGet)
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	mock.ExpectQuery(`FROM preview_environments WHERE id`).
		WithArgs(parsedPreviewID).
		WillReturnRows(previewEnvironmentRow(parsedPreviewID, projectID, serviceID, prNumber, subdomain, previewURL, "active", now))
	mock.ExpectExec(`UPDATE preview_environments\s+SET status = 'closed'`).
		WithArgs(parsedPreviewID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	reqClose, _ := http.NewRequest(http.MethodPost, "/v1/previews/"+parsedPreviewID.String()+"/close", nil)
	engine.ServeHTTP(w, reqClose)
	require.Equal(t, http.StatusOK, w.Code)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPreviewLifecycle_CreatePreview_CrossTenantDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, mock, cleanup := setupPreviewLifecycleHandler(t)
	defer cleanup()

	userA := uuid.New()
	projectB := uuid.New()
	serviceID := uuid.New()

	expectServiceByID(mock, serviceID, projectB, "other-svc", testRepoHTMLURL)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM project_access`).
		WithArgs(userA, projectB).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.Use(withUserContext(userA, "developer"))
	engine.POST("/v1/previews", h.CreatePreview)

	body := fmt.Sprintf(`{
		"service_id": %q,
		"pr_number": 7,
		"pr_branch": "feature/x",
		"commit_sha": %q
	}`, serviceID.String(), testSHA())

	req, _ := http.NewRequest(http.MethodPost, "/v1/previews", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assertErrorCode(t, w, "NOT_FOUND")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPreviewLifecycle_WebhookPROpenCreates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, mock, cleanup := setupPreviewLifecycleHandler(t)
	defer cleanup()

	projectID := uuid.New()
	serviceID := uuid.New()
	prNumber := 42
	sha := testSHA()

	expectServiceByGitRepo(mock, serviceID, projectID, "my-api", testRepoCloneURL)
	mock.ExpectQuery(`FROM preview_environments\s+WHERE service_id`).
		WithArgs(serviceID, prNumber).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO preview_environments`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	engine := gin.New()
	engine.POST("/v1/webhooks/github", h.GitHubWebhook)

	body := newTestPREvent("opened", prNumber, testRepoFullName, "feature/preview", sha, false)
	w := sendWebhook(engine, "pull_request", body, testWebhookSecret)

	require.Equal(t, http.StatusCreated, w.Code)

	var resp struct {
		Message    string `json:"message"`
		PreviewURL string `json:"preview_url"`
		PRNumber   int    `json:"pr_number"`
		Subdomain  string `json:"subdomain"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Preview environment created", resp.Message)
	assert.Equal(t, prNumber, resp.PRNumber)
	assert.Contains(t, resp.PreviewURL, fmt.Sprintf("pr-%d-my-api.preview.enclii.dev", prNumber))
	assert.Contains(t, resp.Subdomain, fmt.Sprintf("pr-%d-my-api", prNumber))

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPreviewLifecycle_WebhookPRClosedClosesPreview(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h, mock, cleanup := setupPreviewLifecycleHandler(t)
	defer cleanup()

	projectID := uuid.New()
	serviceID := uuid.New()
	previewID := uuid.New()
	prNumber := 42
	now := time.Now()
	subdomain := fmt.Sprintf("pr-%d-my-api", prNumber)
	previewURL := fmt.Sprintf("https://%s.preview.enclii.dev", subdomain)

	expectServiceByGitRepo(mock, serviceID, projectID, "my-api", testRepoCloneURL)
	mock.ExpectQuery(`FROM preview_environments\s+WHERE service_id`).
		WithArgs(serviceID, prNumber).
		WillReturnRows(previewEnvironmentRow(previewID, projectID, serviceID, prNumber, subdomain, previewURL, "active", now))
	mock.ExpectExec(`UPDATE preview_environments\s+SET status = 'closed'`).
		WithArgs(previewID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE preview_environments\s+SET status = \$1, status_message = \$2`).
		WithArgs(types.PreviewStatusClosed, "PR closed", previewID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	engine := gin.New()
	engine.POST("/v1/webhooks/github", h.GitHubWebhook)

	body := newTestPREvent("closed", prNumber, testRepoFullName, "feature/preview", testSHA(), false)
	w := sendWebhook(engine, "pull_request", body, testWebhookSecret)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Message   string `json:"message"`
		PreviewID string `json:"preview_id"`
		PRNumber  int    `json:"pr_number"`
		Reason    string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Preview environment closed", resp.Message)
	assert.Equal(t, previewID.String(), resp.PreviewID)
	assert.Equal(t, prNumber, resp.PRNumber)
	assert.Equal(t, "PR closed", resp.Reason)

	assert.NoError(t, mock.ExpectationsWereMet())
}
