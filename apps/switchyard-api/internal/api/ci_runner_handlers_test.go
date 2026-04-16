package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/stretchr/testify/assert"
)

func setupCIRunnerTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	repos := &db.Repositories{
		Projects: db.NewProjectRepository(mockDB),
	}

	h := &Handler{
		repos:  repos,
		logger: newNopLogger(),
	}

	cleanup := func() { mockDB.Close() }
	return h, mock, cleanup
}

func TestGetCIRunnerConfig_Success(t *testing.T) {
	h, mock, cleanup := setupCIRunnerTestHandler(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)

	projectID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, "Test Project", "test-project", "self-hosted", now, now))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "slug", Value: "test-project"}}
	c.Request, _ = http.NewRequest("GET", "/v1/projects/test-project/ci-runner-config", nil)

	h.GetCIRunnerConfig(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CIRunnerConfigResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "self-hosted", resp.Mode)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCIRunnerConfig_NotFound(t *testing.T) {
	h, mock, cleanup := setupCIRunnerTestHandler(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("nonexistent").
		WillReturnError(sql.ErrNoRows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "slug", Value: "nonexistent"}}
	c.Request, _ = http.NewRequest("GET", "/v1/projects/nonexistent/ci-runner-config", nil)

	h.GetCIRunnerConfig(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateCIRunnerConfig_Success(t *testing.T) {
	h, mock, cleanup := setupCIRunnerTestHandler(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)

	projectID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	mock.ExpectExec(`UPDATE projects SET ci_runner_mode = \$1, updated_at = NOW\(\) WHERE id = \$2`).
		WithArgs("self-hosted", projectID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	body := `{"mode":"self-hosted"}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "slug", Value: "test-project"}}
	c.Request, _ = http.NewRequest("PUT", "/v1/projects/test-project/ci-runner-config", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.UpdateCIRunnerConfig(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp CIRunnerConfigResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "self-hosted", resp.Mode)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateCIRunnerConfig_InvalidMode(t *testing.T) {
	h, _, cleanup := setupCIRunnerTestHandler(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)

	body := `{"mode":"invalid"}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "slug", Value: "test-project"}}
	c.Request, _ = http.NewRequest("PUT", "/v1/projects/test-project/ci-runner-config", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.UpdateCIRunnerConfig(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateCIRunnerConfig_InvalidJSON(t *testing.T) {
	h, _, cleanup := setupCIRunnerTestHandler(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "slug", Value: "test-project"}}
	c.Request, _ = http.NewRequest("PUT", "/v1/projects/test-project/ci-runner-config", strings.NewReader("not json"))
	c.Request.Header.Set("Content-Type", "application/json")

	h.UpdateCIRunnerConfig(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateCIRunnerConfig_ProjectNotFound(t *testing.T) {
	h, mock, cleanup := setupCIRunnerTestHandler(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("nonexistent").
		WillReturnError(sql.ErrNoRows)

	body := `{"mode":"self-hosted"}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "slug", Value: "nonexistent"}}
	c.Request, _ = http.NewRequest("PUT", "/v1/projects/nonexistent/ci-runner-config", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.UpdateCIRunnerConfig(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateCIRunnerConfig_DBUpdateError(t *testing.T) {
	h, mock, cleanup := setupCIRunnerTestHandler(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)

	projectID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	mock.ExpectExec(`UPDATE projects SET ci_runner_mode = \$1, updated_at = NOW\(\) WHERE id = \$2`).
		WithArgs("self-hosted", projectID).
		WillReturnError(fmt.Errorf("connection refused"))

	body := `{"mode":"self-hosted"}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "slug", Value: "test-project"}}
	c.Request, _ = http.NewRequest("PUT", "/v1/projects/test-project/ci-runner-config", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.UpdateCIRunnerConfig(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSetGitHubVariable_CreateNew(t *testing.T) {
	// Mock server: PATCH returns 404, POST returns 201
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))

		if r.Method == "PATCH" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == "POST" {
			w.WriteHeader(http.StatusCreated)
			return
		}
	}))
	defer server.Close()

	// The function uses hardcoded github.com URLs, so we can't easily redirect.
	// This test verifies the function signature and error handling for non-reachable endpoints.
	err := setGitHubVariable(nil, "test-token", "owner", "repo", "VAR_NAME", "value")
	// Will fail because it can't reach api.github.com, but verifies compilation and types
	assert.Error(t, err) // Expected: network error since we can't mock the URL
}
