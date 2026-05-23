package api

import (
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

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// serviceListByGitRepoColumns matches the columns scanned by
// ServiceRepository.ListByGitRepo (no release fields — those come from a
// separate enrichment query).
var serviceListByGitRepoColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env",
	"created_at", "updated_at", "jobs", "type", "region",
}

// setupServiceTestHandler builds a Handler with a sqlmock-backed Services
// repo and a no-op logger so handler tests can assert on the wire response.
func setupServiceTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)

	h := &Handler{
		config: &config.Config{AuthMode: "local", Environment: "development"},
		repos: &db.Repositories{
			Services: db.NewServiceRepository(database),
		},
		logger: testLogger(t),
	}
	return h, mock, func() { _ = database.Close() }
}

// TestListServicesByGitRepo_MissingParam asserts the 400 path when the
// caller forgets the git_repo query string.
func TestListServicesByGitRepo_MissingParam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _, cleanup := setupServiceTestHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/services", h.ListServicesByGitRepo)

	req, _ := http.NewRequest("GET", "/v1/services", nil)
	engine.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestListServicesByGitRepo_IncludesImageAgeFields asserts Pillar 3.5: the
// public response carries current_image_uri, current_release_created_at,
// and recent_releases sourced from the latest succeeded release.
func TestListServicesByGitRepo_IncludesImageAgeFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupServiceTestHandler(t)
	defer cleanup()

	serviceID := uuid.New()
	projectID := uuid.New()
	releaseID := uuid.New()
	releaseCreated := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	// 1. ListByGitRepo returns one service.
	mock.ExpectQuery(`(?s)FROM services\s+WHERE REPLACE\(REPLACE\(git_repo`).
		WillReturnRows(sqlmock.NewRows(serviceListByGitRepoColumns).AddRow(
			serviceID, projectID, "api", "https://github.com/madfam-org/enclii", "",
			[]byte(`{"type":"dockerfile"}`),
			true, "main", "production",
			time.Now(), time.Now(), []byte(`[]`), "web", "default",
		))

	// 2. EnrichWithLatestRelease: current succeeded release.
	mock.ExpectQuery(`SELECT image_uri, created_at FROM releases\s+WHERE service_id = \$1 AND status = 'succeeded'`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows([]string{"image_uri", "created_at"}).
			AddRow("ghcr.io/madfam-org/enclii-api@sha256:abc123", releaseCreated))

	// 3. EnrichWithLatestRelease: recent 5 releases.
	mock.ExpectQuery(`SELECT id, version, image_uri, git_sha, status, created_at FROM releases\s+WHERE service_id = \$1`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "image_uri", "git_sha", "status", "created_at"}).
			AddRow(releaseID, "v42", "ghcr.io/madfam-org/enclii-api@sha256:abc123", "deadbeef", "succeeded", releaseCreated))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/services", h.ListServicesByGitRepo)

	req, _ := http.NewRequest("GET", "/v1/services?git_repo=https://github.com/madfam-org/enclii", nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Services []map[string]interface{} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Services, 1)

	svc := body.Services[0]
	// Backward-compatible base fields.
	assert.Equal(t, serviceID.String(), svc["id"])
	assert.Equal(t, "api", svc["name"])
	assert.Equal(t, projectID.String(), svc["project_id"])

	// Pillar 3.5 fields.
	assert.Equal(t, "ghcr.io/madfam-org/enclii-api@sha256:abc123", svc["current_image_uri"])
	assert.NotNil(t, svc["current_release_created_at"], "current_release_created_at must be present when a succeeded release exists")
	require.NotNil(t, svc["recent_releases"], "recent_releases must be present")
	releases, ok := svc["recent_releases"].([]interface{})
	require.True(t, ok, "recent_releases must serialize as an array")
	require.Len(t, releases, 1)
	release := releases[0].(map[string]interface{})
	assert.Equal(t, "ghcr.io/madfam-org/enclii-api@sha256:abc123", release["image_uri"])
	assert.Equal(t, "succeeded", release["status"])

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListServicesByGitRepo_NoReleasesYet asserts the response shape for a
// service that has been onboarded but has not produced any release. The new
// Pillar 3.5 fields are omitted via `omitempty` so existing consumers are
// undisturbed.
func TestListServicesByGitRepo_NoReleasesYet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupServiceTestHandler(t)
	defer cleanup()

	serviceID := uuid.New()
	projectID := uuid.New()

	mock.ExpectQuery(`(?s)FROM services\s+WHERE REPLACE\(REPLACE\(git_repo`).
		WillReturnRows(sqlmock.NewRows(serviceListByGitRepoColumns).AddRow(
			serviceID, projectID, "api", "https://github.com/madfam-org/enclii", "",
			[]byte(`{"type":"dockerfile"}`),
			true, "main", "production",
			time.Now(), time.Now(), []byte(`[]`), "web", "default",
		))

	// Current succeeded release: none.
	mock.ExpectQuery(`SELECT image_uri, created_at FROM releases\s+WHERE service_id = \$1 AND status = 'succeeded'`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows([]string{"image_uri", "created_at"}))

	// Recent releases: none.
	mock.ExpectQuery(`SELECT id, version, image_uri, git_sha, status, created_at FROM releases\s+WHERE service_id = \$1`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "image_uri", "git_sha", "status", "created_at"}))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/services", h.ListServicesByGitRepo)

	req, _ := http.NewRequest("GET", "/v1/services?git_repo=https://github.com/madfam-org/enclii", nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Services []map[string]interface{} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Services, 1)

	svc := body.Services[0]
	// Base fields always present.
	assert.Equal(t, serviceID.String(), svc["id"])
	assert.Equal(t, "api", svc["name"])
	// Pillar 3.5 fields omitted when no releases exist (omitempty).
	_, hasImage := svc["current_image_uri"]
	_, hasTime := svc["current_release_created_at"]
	_, hasRecent := svc["recent_releases"]
	assert.False(t, hasImage, "current_image_uri must be omitted when no succeeded release")
	assert.False(t, hasTime, "current_release_created_at must be omitted when no succeeded release")
	assert.False(t, hasRecent, "recent_releases must be omitted when no releases")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListServicesByGitRepo_NoSensitiveFields is a regression guard: the
// public response must NOT leak fields that the auth'd ListByProject
// projection carries (k8s_namespace, build_config, env vars, health
// internals, etc.). Pillar 3.5 is deliberately additive — only image
// digests and timestamps, which are already discoverable via GHCR.
func TestListServicesByGitRepo_NoSensitiveFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupServiceTestHandler(t)
	defer cleanup()

	serviceID := uuid.New()
	projectID := uuid.New()

	mock.ExpectQuery(`(?s)FROM services\s+WHERE REPLACE\(REPLACE\(git_repo`).
		WillReturnRows(sqlmock.NewRows(serviceListByGitRepoColumns).AddRow(
			serviceID, projectID, "api", "https://github.com/madfam-org/enclii", "apps/api",
			[]byte(`{"type":"dockerfile","build_args":{"NPM_TOKEN":"super-secret"}}`),
			true, "main", "production",
			time.Now(), time.Now(), []byte(`[]`), "web", "default",
		))
	mock.ExpectQuery(`SELECT image_uri, created_at FROM releases`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows([]string{"image_uri", "created_at"}))
	mock.ExpectQuery(`SELECT id, version, image_uri, git_sha, status, created_at FROM releases`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "version", "image_uri", "git_sha", "status", "created_at"}))

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/services", h.ListServicesByGitRepo)

	req, _ := http.NewRequest("GET", "/v1/services?git_repo=https://github.com/madfam-org/enclii", nil)
	engine.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Services []map[string]interface{} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Services, 1)

	svc := body.Services[0]
	for _, forbidden := range []string{
		"build_config", "git_repo", "app_path", "auto_deploy",
		"auto_deploy_branch", "auto_deploy_env", "k8s_namespace",
		"health", "status", "desired_replicas", "ready_replicas",
		"last_health_check", "last_deployment", "last_commit_message",
		"created_at", "updated_at",
	} {
		_, present := svc[forbidden]
		assert.False(t, present, "public response must not include %q (least-info principle)", forbidden)
	}
}
