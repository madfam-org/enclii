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

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

var projectProcessServiceColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env",
	"k8s_namespace", "health", "status",
	"desired_replicas", "ready_replicas", "last_health_check",
	"last_deployment", "last_commit_message", "last_commit_branch",
	"current_image_uri", "current_release_id", "current_release_created_at",
	"recent_releases", "created_at", "updated_at", "jobs", "type", "region",
}

func TestLifecycleProcessKindAndStatus(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		kind      string
		status    string
	}{
		{name: "push", eventType: types.LifecyclePushReceived, kind: "git_push", status: "succeeded"},
		{name: "build started", eventType: types.LifecycleBuildStarted, kind: "build", status: "running"},
		{name: "build failed", eventType: types.LifecycleBuildFailed, kind: "build", status: "failed"},
		{name: "deploy synced", eventType: types.LifecycleDeploySynced, kind: "gitops_sync", status: "succeeded"},
		{name: "deploy degraded", eventType: types.LifecycleDeployDegraded, kind: "rollout", status: "blocked"},
		{name: "unknown", eventType: "provider_probe", kind: "operator", status: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, status := lifecycleProcessKindAndStatus(tt.eventType)
			assert.Equal(t, tt.kind, kind)
			assert.Equal(t, tt.status, status)
		})
	}
}

func TestSummarizeProjectProcessesCountsAndLimits(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Slug: "selva"}
	serviceID := uuid.New().String()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	summary := summarizeProjectProcesses(project, []projectProcess{
		{
			ID:          "old-success",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			Kind:        "build",
			Status:      "succeeded",
			UpdatedAt:   now.Add(-2 * time.Hour),
		},
		{
			ID:          "blocked",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			ServiceName: "api",
			Kind:        "rollout",
			Status:      "blocked",
			UpdatedAt:   now,
		},
		{
			ID:          "running",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			ServiceName: "api",
			Kind:        "ci",
			Status:      "running",
			UpdatedAt:   now.Add(-time.Minute),
		},
	}, 2)

	require.NotNil(t, summary.Latest)
	assert.Equal(t, "blocked", summary.Latest.ID)
	assert.Equal(t, 1, summary.ActiveCount)
	assert.Equal(t, 0, summary.FailedCount)
	assert.Equal(t, 1, summary.BlockedCount)
	assert.Len(t, summary.Processes, 2)
	assert.Len(t, summary.Services, 1)
	assert.Equal(t, serviceID, summary.Services[0].ServiceID)
	assert.Equal(t, 1, summary.Services[0].ActiveCount)
	assert.Equal(t, 1, summary.Services[0].BlockedCount)
	require.NotNil(t, summary.Services[0].Latest)
	assert.Equal(t, "blocked", summary.Services[0].Latest.ID)
}

func TestCompactProjectProcessesForActiveSummaryDropsSupersededFailures(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Slug: "selva"}
	serviceID := uuid.New().String()
	now := time.Date(2026, 5, 18, 18, 0, 0, 0, time.UTC)

	processes := compactProjectProcessesForActiveSummary([]projectProcess{
		{
			ID:          "deploy-failed",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			Kind:        "deploy",
			Status:      "failed",
			Environment: "production",
			UpdatedAt:   now.Add(-30 * time.Minute),
		},
		{
			ID:          "deploy-healthy",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			Kind:        "deploy",
			Status:      "succeeded",
			Environment: "production",
			UpdatedAt:   now.Add(-20 * time.Minute),
		},
		{
			ID:          "build-failed",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			Kind:        "build",
			Status:      "failed",
			UpdatedAt:   now.Add(-15 * time.Minute),
		},
		{
			ID:          "image-pushed",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			Kind:        "image",
			Status:      "succeeded",
			UpdatedAt:   now.Add(-10 * time.Minute),
		},
	}, now)

	assert.Empty(t, processes)
}

func TestCompactProjectProcessesForActiveSummaryKeepsCurrentFailure(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Slug: "selva"}
	serviceID := uuid.New().String()
	now := time.Date(2026, 5, 18, 18, 0, 0, 0, time.UTC)

	processes := compactProjectProcessesForActiveSummary([]projectProcess{
		{
			ID:          "deploy-healthy",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			Kind:        "deploy",
			Status:      "succeeded",
			Environment: "production",
			UpdatedAt:   now.Add(-30 * time.Minute),
		},
		{
			ID:          "deploy-failed",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			Kind:        "deploy",
			Status:      "failed",
			Environment: "production",
			UpdatedAt:   now.Add(-20 * time.Minute),
		},
	}, now)

	require.Len(t, processes, 1)
	assert.Equal(t, "deploy-failed", processes[0].ID)
}

func TestCompactProjectProcessesForActiveSummaryKeepsLiveServiceState(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Slug: "selva"}
	serviceID := uuid.New().String()
	now := time.Date(2026, 5, 18, 18, 0, 0, 0, time.UTC)

	processes := compactProjectProcessesForActiveSummary([]projectProcess{
		{
			ID:            "service-state:" + serviceID + ":rollout_blocked",
			CorrelationID: "service:" + serviceID + ":rollout_blocked",
			ProjectID:     project.ID.String(),
			ProjectSlug:   project.Slug,
			ServiceID:     serviceID,
			Kind:          "rollout",
			Status:        "blocked",
			Environment:   "production",
			UpdatedAt:     now.Add(-12 * time.Hour),
		},
		{
			ID:          "deploy-healthy",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			Kind:        "deploy",
			Status:      "succeeded",
			Environment: "production",
			UpdatedAt:   now.Add(-10 * time.Minute),
		},
	}, now)

	require.Len(t, processes, 1)
	assert.Equal(t, "service-state:"+serviceID+":rollout_blocked", processes[0].ID)
	assert.Equal(t, "blocked", processes[0].Status)
}

func TestCompactProjectProcessesForActiveSummaryDropsStaleRunningLifecycle(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Slug: "selva"}
	serviceID := uuid.New().String()
	now := time.Date(2026, 5, 18, 18, 0, 0, 0, time.UTC)

	processes := compactProjectProcessesForActiveSummary([]projectProcess{
		{
			ID:          "deploy-synced",
			ProjectID:   project.ID.String(),
			ProjectSlug: project.Slug,
			ServiceID:   serviceID,
			Kind:        "gitops_sync",
			Status:      "running",
			Environment: "production",
			UpdatedAt:   now.Add(-3 * time.Hour),
		},
	}, now)

	assert.Empty(t, processes)
}

func TestProcessFromLifecycleEventBuildsCorrelationAndLinks(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Slug: "enclii"}
	serviceID := uuid.New()
	deploymentID := uuid.New()
	env := "production"
	message := "deploy started"
	createdAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	process := processFromLifecycleEvent(project, map[string]string{
		serviceID.String(): "switchyard-api",
	}, types.DeploymentLifecycleEvent{
		ID:           uuid.New(),
		DeploymentID: &deploymentID,
		ProjectID:    &project.ID,
		ServiceID:    &serviceID,
		CommitSHA:    "abc123def",
		Branch:       "main",
		TargetEnv:    &env,
		EventType:    types.LifecycleDeployStarted,
		Source:       types.SourceArgocdCallback,
		Message:      &message,
		CreatedAt:    createdAt,
	})

	assert.Equal(t, "deploy", process.Kind)
	assert.Equal(t, "running", process.Status)
	assert.Equal(t, "argocd", process.Source)
	assert.Equal(t, "switchyard-api", process.ServiceName)
	assert.Equal(t, serviceID.String()+":abc123def:production", process.CorrelationID)
	assert.Equal(t, "/deployments/"+deploymentID.String(), process.Links["deployment"])
	assert.Equal(t, "/projects/enclii/deployments?commit=abc123def", process.Links["lifecycle"])
	require.NotNil(t, process.StartedAt)
	assert.Equal(t, createdAt, *process.StartedAt)
}

func TestProcessFromServiceStateExposesBlockedRollout(t *testing.T) {
	project := &types.Project{ID: uuid.New(), Slug: "enclii"}
	service := &types.Service{
		ID:                   uuid.New(),
		ProjectID:            project.ID,
		Name:                 "api",
		Status:               "running",
		RolloutState:         "blocked",
		RolloutBlockedReason: "readiness_timeout",
		AutoDeployEnv:        "production",
		UpdatedAt:            time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
	}

	process, ok := processFromServiceState(project, service)

	require.True(t, ok)
	assert.Equal(t, "rollout", process.Kind)
	assert.Equal(t, "blocked", process.Status)
	assert.Equal(t, "rollout_blocked", process.Phase)
	assert.Contains(t, process.Message, "readiness_timeout")
	assert.Equal(t, "/projects/enclii/services/"+service.ID.String()+"/logs", process.Links["logs"])
}

func TestParseProjectProcessIDsDedupesAndValidates(t *testing.T) {
	id := uuid.New()

	ids, err := parseProjectProcessIDs(id.String() + "," + id.String())

	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.Equal(t, id, ids[0])

	_, err = parseProjectProcessIDs("not-a-uuid")
	require.Error(t, err)
}

func TestProjectProcessStreamIntervalBounds(t *testing.T) {
	assert.Equal(t, 10*time.Second, projectProcessStreamInterval(""))
	assert.Equal(t, 5*time.Second, projectProcessStreamInterval("1"))
	assert.Equal(t, 15*time.Second, projectProcessStreamInterval("15000"))
	assert.Equal(t, 60*time.Second, projectProcessStreamInterval("90000"))
}

func TestStreamProjectProcessSummariesOnceEmitsSummaryEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	projectID := uuid.New()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE id = \$1`).
		WithArgs(projectID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at",
		}).AddRow(projectID, "Selva", "selva", "", now, now))
	mock.ExpectQuery(`(?s)FROM services s WHERE s\.project_id = \$1`).
		WithArgs(projectID).
		WillReturnRows(sqlmock.NewRows(projectProcessServiceColumns))

	handler := &Handler{
		repos: &db.Repositories{
			Projects: db.NewProjectRepository(mockDB),
			Services: db.NewServiceRepository(mockDB),
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/project-processes/stream?project_ids="+projectID.String()+"&once=true",
		nil,
	)

	handler.StreamProjectProcessSummaries(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.Contains(t, recorder.Body.String(), "event: summary")
	assert.Contains(t, recorder.Body.String(), `"count":1`)
	assert.Contains(t, recorder.Body.String(), `"processes":[]`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectProcessSummaryResponseJSONShape(t *testing.T) {
	payload, err := json.Marshal(projectProcessSummaryResponse{
		Count:     0,
		Summaries: []projectProcessSummary{},
	})

	require.NoError(t, err)
	assert.JSONEq(t, `{"count":0,"summaries":[]}`, string(payload))
}
