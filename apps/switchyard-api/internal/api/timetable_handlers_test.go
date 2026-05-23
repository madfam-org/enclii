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

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// testLogger creates a structured logger suitable for tests (writes to stdout, text format, error level).
func testLogger(t *testing.T) logging.Logger {
	t.Helper()
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{
		Level:       "error",
		Format:      "text",
		Output:      "stdout",
		ServiceName: "test",
		Version:     "test",
		Environment: "test",
	})
	require.NoError(t, err)
	return logger
}

// setupTimetableTestHandler creates a Handler wired with sqlmock-backed repositories.
// Returns the handler, the sqlmock for setting expectations, and a cleanup function.
func setupTimetableTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)

	repos := &db.Repositories{
		Projects:    db.NewProjectRepository(database),
		Services:    db.NewServiceRepository(database),
		CronJobs:    db.NewCronJobRepository(database),
		CronJobRuns: db.NewCronJobRunRepository(database),
		OneOffJobs:  db.NewOneOffJobRepository(database),
	}

	h := &Handler{
		repos:  repos,
		logger: testLogger(t),
	}
	return h, mock, func() { database.Close() }
}

// withTestAdminContext marks the request as a platform admin (unit tests skip real JWT).
func withTestAdminContext(router *gin.Engine) {
	router.Use(func(c *gin.Context) {
		c.Set("user_roles", []string{"admin"})
		c.Set("user_id", uuid.New().String())
		c.Next()
	})
}

// projectSelectColumns matches the columns scanned by ProjectRepository.GetBySlug
var projectSelectColumns = []string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}

// cronJobSelectColumns matches the columns scanned by CronJobRepository.GetByID/ListByProject
var cronJobSelectColumns = []string{
	"id", "project_id", "service_id", "name", "schedule", "command", "image",
	"timeout", "retries", "suspended", "concurrency",
	"created_at", "updated_at", "last_run_at", "next_run_at",
}

// cronJobRunSelectColumns matches the columns scanned by CronJobRunRepository.ListByCronJob
var cronJobRunSelectColumns = []string{
	"id", "cron_job_id", "status", "exit_code", "started_at", "ended_at", "log_output",
}

// oneOffJobSelectColumns matches the columns scanned by OneOffJobRepository (not used in handler currently, but needed for completeness)
var oneOffJobSelectColumns = []string{
	"id", "project_id", "service_id", "name", "command", "image",
	"timeout", "run_at", "status", "exit_code",
	"created_at", "started_at", "ended_at",
}

// --- CreateCronJob ---

func TestCreateCronJob_Success(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	projectID := uuid.New()
	serviceID := uuid.New()
	now := time.Now()

	// Mock: GetBySlug (uses QueryRow without context)
	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows(serviceListByGitRepoColumns).
			AddRow(serviceID, projectID, "api", "https://github.com/org/repo", "", []byte("{}"), true, "main", "production", now, now, []byte("[]"), "web", ""))

	// Mock: CronJobs.Create (INSERT)
	mock.ExpectExec(`INSERT INTO cron_jobs`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/cron-jobs", h.CreateCronJob)

	body := fmt.Sprintf(`{
		"service_id": "%s",
		"name": "nightly-backup",
		"schedule": "0 2 * * *",
		"command": "pg_dump mydb"
	}`, serviceID.String())

	req := httptest.NewRequest("POST", "/v1/projects/test-project/cron-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Cron job created successfully", resp["message"])
	assert.NotNil(t, resp["cron_job"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateCronJob_InvalidCron(t *testing.T) {
	h, _, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/cron-jobs", h.CreateCronJob)

	serviceID := uuid.New()
	body := fmt.Sprintf(`{
		"service_id": "%s",
		"name": "bad-cron",
		"schedule": "not-a-cron",
		"command": "echo fail"
	}`, serviceID.String())

	req := httptest.NewRequest("POST", "/v1/projects/test-project/cron-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid cron expression")
}

func TestCreateCronJob_InvalidConcurrency(t *testing.T) {
	h, _, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/cron-jobs", h.CreateCronJob)

	serviceID := uuid.New()
	body := fmt.Sprintf(`{
		"service_id": "%s",
		"name": "bad-policy",
		"schedule": "* * * * *",
		"command": "echo hi",
		"concurrency": "invalid"
	}`, serviceID.String())

	req := httptest.NewRequest("POST", "/v1/projects/test-project/cron-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid concurrency policy")
}

func TestCreateCronJob_ProjectNotFound(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	// Mock: GetBySlug returns not found
	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("nonexistent").
		WillReturnError(sql.ErrNoRows)

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/cron-jobs", h.CreateCronJob)

	serviceID := uuid.New()
	body := fmt.Sprintf(`{
		"service_id": "%s",
		"name": "orphan-job",
		"schedule": "0 * * * *",
		"command": "echo orphan"
	}`, serviceID.String())

	req := httptest.NewRequest("POST", "/v1/projects/nonexistent/cron-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "project not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateCronJob_InvalidServiceID(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	projectID := uuid.New()
	now := time.Now()

	// Mock: GetBySlug succeeds
	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/cron-jobs", h.CreateCronJob)

	body := `{
		"service_id": "not-a-uuid",
		"name": "bad-svc-id",
		"schedule": "0 * * * *",
		"command": "echo fail"
	}`

	req := httptest.NewRequest("POST", "/v1/projects/test-project/cron-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid service_id")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateCronJob_MissingRequiredFields(t *testing.T) {
	h, _, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/cron-jobs", h.CreateCronJob)

	// Missing name, schedule, command, service_id
	body := `{}`

	req := httptest.NewRequest("POST", "/v1/projects/test-project/cron-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- ListCronJobs ---

func TestListCronJobs_Success(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	projectID := uuid.New()
	serviceID := uuid.New()
	now := time.Now()

	// Mock: GetBySlug
	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	// Mock: ListByProject returns 2 cron jobs
	rows := sqlmock.NewRows(cronJobSelectColumns).
		AddRow(uuid.New(), projectID, serviceID, "job-alpha", "0 * * * *", "echo alpha", sql.NullString{Valid: false}, 300, 0, false, "forbid", now, now, nil, nil).
		AddRow(uuid.New(), projectID, serviceID, "job-beta", "*/5 * * * *", "echo beta", sql.NullString{Valid: false}, 60, 1, false, "allow", now, now, nil, nil)

	mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
		WithArgs(projectID).
		WillReturnRows(rows)

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/projects/:slug/cron-jobs", h.ListCronJobs)

	req := httptest.NewRequest("GET", "/v1/projects/test-project/cron-jobs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(2), resp["total"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListCronJobs_Empty(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	projectID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("empty-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Empty Project", "empty-project", "github", now, now))

	// Return empty result set
	mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
		WithArgs(projectID).
		WillReturnRows(sqlmock.NewRows(cronJobSelectColumns))

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/projects/:slug/cron-jobs", h.ListCronJobs)

	req := httptest.NewRequest("GET", "/v1/projects/empty-project/cron-jobs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["total"])
	// Verify cron_jobs is an empty array (not null)
	cronJobs, ok := resp["cron_jobs"].([]interface{})
	require.True(t, ok)
	assert.Empty(t, cronJobs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- GetCronJob ---

func TestGetCronJob_Success(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	cronJobID := uuid.New()
	projectID := uuid.New()
	serviceID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
		WithArgs(cronJobID).
		WillReturnRows(sqlmock.NewRows(cronJobSelectColumns).
			AddRow(cronJobID, projectID, serviceID, "nightly-job", "0 2 * * *", "backup.sh", sql.NullString{Valid: false}, 600, 3, false, "forbid", now, now, nil, nil))

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/cron-jobs/:id", h.GetCronJob)

	req := httptest.NewRequest("GET", "/v1/cron-jobs/"+cronJobID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, cronJobID.String(), resp["id"])
	assert.Equal(t, "nightly-job", resp["name"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCronJob_NotFound(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	cronJobID := uuid.New()

	mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
		WithArgs(cronJobID).
		WillReturnError(sql.ErrNoRows)

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/cron-jobs/:id", h.GetCronJob)

	req := httptest.NewRequest("GET", "/v1/cron-jobs/"+cronJobID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "cron job not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetCronJob_InvalidID(t *testing.T) {
	h, _, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/cron-jobs/:id", h.GetCronJob)

	req := httptest.NewRequest("GET", "/v1/cron-jobs/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid cron job ID")
}

// --- DeleteCronJob ---

func TestDeleteCronJob_Success(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	cronJobID := uuid.New()

	mock.ExpectExec(`DELETE FROM cron_jobs WHERE id`).
		WithArgs(cronJobID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	router := gin.New()
	withTestAdminContext(router)
	router.DELETE("/v1/cron-jobs/:id", h.DeleteCronJob)

	req := httptest.NewRequest("DELETE", "/v1/cron-jobs/"+cronJobID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "cron job deleted")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteCronJob_NotFound(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	cronJobID := uuid.New()

	mock.ExpectExec(`DELETE FROM cron_jobs WHERE id`).
		WithArgs(cronJobID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	router := gin.New()
	withTestAdminContext(router)
	router.DELETE("/v1/cron-jobs/:id", h.DeleteCronJob)

	req := httptest.NewRequest("DELETE", "/v1/cron-jobs/"+cronJobID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "cron job not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- CreateOneOffJob ---

func TestCreateOneOffJob_Success(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	projectID := uuid.New()
	serviceID := uuid.New()
	now := time.Now()

	// Mock: GetBySlug
	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	// Mock: OneOffJobs.Create
	mock.ExpectExec(`INSERT INTO one_off_jobs`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/one-off-jobs", h.CreateOneOffJob)

	body := fmt.Sprintf(`{
		"service_id": "%s",
		"name": "db-migration",
		"command": "migrate up"
	}`, serviceID.String())

	req := httptest.NewRequest("POST", "/v1/projects/test-project/one-off-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "One-off job created successfully", resp["message"])
	assert.NotNil(t, resp["one_off_job"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateOneOffJob_WithRunAt(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	projectID := uuid.New()
	serviceID := uuid.New()
	now := time.Now()
	futureTime := now.Add(24 * time.Hour).Format(time.RFC3339)

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	mock.ExpectExec(`INSERT INTO one_off_jobs`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/one-off-jobs", h.CreateOneOffJob)

	body := fmt.Sprintf(`{
		"service_id": "%s",
		"name": "scheduled-migration",
		"command": "migrate up",
		"run_at": "%s"
	}`, serviceID.String(), futureTime)

	req := httptest.NewRequest("POST", "/v1/projects/test-project/one-off-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateOneOffJob_InvalidRunAt(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	projectID := uuid.New()
	serviceID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("test-project").
		WillReturnRows(sqlmock.NewRows(projectSelectColumns).
			AddRow(projectID, "Test Project", "test-project", "github", now, now))

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/one-off-jobs", h.CreateOneOffJob)

	body := fmt.Sprintf(`{
		"service_id": "%s",
		"name": "bad-time",
		"command": "echo fail",
		"run_at": "not-a-time"
	}`, serviceID.String())

	req := httptest.NewRequest("POST", "/v1/projects/test-project/one-off-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid run_at")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateOneOffJob_ProjectNotFound(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug`).
		WithArgs("nonexistent").
		WillReturnError(sql.ErrNoRows)

	router := gin.New()
	withTestAdminContext(router)
	router.POST("/v1/projects/:slug/one-off-jobs", h.CreateOneOffJob)

	serviceID := uuid.New()
	body := fmt.Sprintf(`{
		"service_id": "%s",
		"name": "orphan",
		"command": "echo orphan"
	}`, serviceID.String())

	req := httptest.NewRequest("POST", "/v1/projects/nonexistent/one-off-jobs", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "project not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- ListCronJobRuns ---

func TestListCronJobRuns_Success(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	cronJobID := uuid.New()
	projectID := uuid.New()
	serviceID := uuid.New()
	now := time.Now()

	// Mock: CronJobs.GetByID (verify exists)
	mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
		WithArgs(cronJobID).
		WillReturnRows(sqlmock.NewRows(cronJobSelectColumns).
			AddRow(cronJobID, projectID, serviceID, "job", "* * * * *", "echo", sql.NullString{Valid: false}, 300, 0, false, "forbid", now, now, nil, nil))

	// Mock: CronJobRuns.ListByCronJob
	rows := sqlmock.NewRows(cronJobRunSelectColumns).
		AddRow(uuid.New(), cronJobID, "completed", int64(0), now, now, "output").
		AddRow(uuid.New(), cronJobID, "failed", int64(1), now, now, "error output")

	mock.ExpectQuery(`SELECT id, cron_job_id, status, exit_code, started_at, ended_at, log_output`).
		WithArgs(cronJobID, 50).
		WillReturnRows(rows)

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/cron-jobs/:id/runs", h.ListCronJobRuns)

	req := httptest.NewRequest("GET", "/v1/cron-jobs/"+cronJobID.String()+"/runs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(2), resp["total"])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListCronJobRuns_CronJobNotFound(t *testing.T) {
	h, mock, cleanup := setupTimetableTestHandler(t)
	defer cleanup()

	cronJobID := uuid.New()

	mock.ExpectQuery(`SELECT id, project_id, service_id, name, schedule, command, image`).
		WithArgs(cronJobID).
		WillReturnError(sql.ErrNoRows)

	router := gin.New()
	withTestAdminContext(router)
	router.GET("/v1/cron-jobs/:id/runs", h.ListCronJobRuns)

	req := httptest.NewRequest("GET", "/v1/cron-jobs/"+cronJobID.String()+"/runs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "cron job not found")
	assert.NoError(t, mock.ExpectationsWereMet())
}
