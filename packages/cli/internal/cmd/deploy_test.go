package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestNewDeployCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewDeployCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "deploy", cmd.Use)

	// Verify flags exist with correct defaults
	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "dev", envFlag.DefValue)

	waitFlag := cmd.Flags().Lookup("wait")
	require.NotNil(t, waitFlag)
	assert.Equal(t, "false", waitFlag.DefValue)

	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)

	// Verify shorthand flags
	assert.Equal(t, "e", envFlag.Shorthand)
	assert.Equal(t, "w", waitFlag.Shorthand)
	assert.Equal(t, "f", fileFlag.Shorthand)
}

func TestEnsureProject_Existing(t *testing.T) {
	projectID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/projects/my-project", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.Project{
			ID:   projectID,
			Name: "My Project",
			Slug: "my-project",
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	project, err := ensureProject(ctx, apiClient, "my-project")
	require.NoError(t, err)
	require.NotNil(t, project)
	assert.Equal(t, projectID, project.ID)
	assert.Equal(t, "My Project", project.Name)
}

func TestEnsureProject_Creates(t *testing.T) {
	projectID := uuid.New()
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if count == 1 {
			// First request: GET returns 404
			assert.Equal(t, "GET", r.Method)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Project not found",
			})
			return
		}

		// Second request: POST creates project
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/projects", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(types.Project{
			ID:   projectID,
			Name: "new-project",
			Slug: "new-project",
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	project, err := ensureProject(ctx, apiClient, "new-project")
	require.NoError(t, err)
	require.NotNil(t, project)
	assert.Equal(t, projectID, project.ID)
}

func TestEnsureProject_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	project, err := ensureProject(ctx, apiClient, "broken-project")
	require.Error(t, err)
	assert.Nil(t, project)
}

func TestEnsureService_Existing(t *testing.T) {
	projectID := uuid.New()
	serviceID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/v1/projects/my-project/services")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Services []*types.Service `json:"services"`
		}{
			Services: []*types.Service{
				{
					ID:        serviceID,
					ProjectID: projectID,
					Name:      "my-service",
					GitRepo:   "https://github.com/org/repo",
				},
			},
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	project := &types.Project{
		ID:   projectID,
		Name: "My Project",
		Slug: "my-project",
	}

	serviceSpec := &types.ServiceSpec{
		Metadata: types.ServiceMetadata{
			Name:    "my-service",
			Project: "my-project",
		},
	}

	service, err := ensureService(ctx, apiClient, project, serviceSpec)
	require.NoError(t, err)
	require.NotNil(t, service)
	assert.Equal(t, serviceID, service.ID)
	assert.Equal(t, "my-service", service.Name)
}

func TestEnsureService_Creates(t *testing.T) {
	projectID := uuid.New()
	serviceID := uuid.New()
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if count == 1 {
			// First request: list returns empty
			assert.Equal(t, "GET", r.Method)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Services []*types.Service `json:"services"`
			}{
				Services: []*types.Service{},
			})
			return
		}

		// Second request: POST creates service
		assert.Equal(t, "POST", r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(types.Service{
			ID:        serviceID,
			ProjectID: projectID,
			Name:      "new-service",
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	project := &types.Project{
		ID:   projectID,
		Name: "My Project",
		Slug: "my-project",
	}

	serviceSpec := &types.ServiceSpec{
		Metadata: types.ServiceMetadata{
			Name:    "new-service",
			Project: "my-project",
		},
		Spec: types.ServiceSpecConfig{
			Build: types.BuildSpec{
				Type: "buildpack",
			},
		},
	}

	service, err := ensureService(ctx, apiClient, project, serviceSpec)
	require.NoError(t, err)
	require.NotNil(t, service)
	assert.Equal(t, serviceID, service.ID)
}

func TestEnsureService_CreatesDockerfile(t *testing.T) {
	projectID := uuid.New()
	serviceID := uuid.New()
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if count == 1 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Services []*types.Service `json:"services"`
			}{
				Services: []*types.Service{},
			})
			return
		}

		// Verify the build config in the create request
		assert.Equal(t, "POST", r.Method)
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		buildConfig := req["build_config"].(map[string]interface{})
		assert.Equal(t, "dockerfile", buildConfig["type"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(types.Service{
			ID:        serviceID,
			ProjectID: projectID,
			Name:      "docker-service",
			BuildConfig: types.BuildConfig{
				Type: types.BuildTypeDockerfile,
			},
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	project := &types.Project{ID: projectID, Slug: "my-project"}
	serviceSpec := &types.ServiceSpec{
		Metadata: types.ServiceMetadata{Name: "docker-service", Project: "my-project"},
		Spec: types.ServiceSpecConfig{
			Build: types.BuildSpec{Type: "dockerfile", Dockerfile: "Dockerfile.prod"},
		},
	}

	service, err := ensureService(ctx, apiClient, project, serviceSpec)
	require.NoError(t, err)
	require.NotNil(t, service)
	assert.Equal(t, types.BuildTypeDockerfile, service.BuildConfig.Type)
}

func TestEnsureEnvironment_AlreadyExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/projects/my-project/environments", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Conflict: environment already exists",
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := ensureEnvironment(ctx, apiClient, "my-project", "dev")
	assert.NoError(t, err, "should not return error when environment already exists (409)")
}

func TestEnsureEnvironment_Creates(t *testing.T) {
	envID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/projects/my-project/environments", r.URL.Path)

		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "staging", req["name"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(types.Environment{
			ID:   envID,
			Name: "staging",
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := ensureEnvironment(ctx, apiClient, "my-project", "staging")
	assert.NoError(t, err)
}

func TestEnsureEnvironment_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error",
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := ensureEnvironment(ctx, apiClient, "my-project", "broken")
	require.Error(t, err)
}

func TestWaitForBuild_Success(t *testing.T) {
	serviceID := uuid.New()
	releaseID := uuid.New()
	var callCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)

		var status types.ReleaseStatus
		if count <= 2 {
			status = types.ReleaseStatusBuilding
		} else {
			status = types.ReleaseStatusReady
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Releases []*types.Release `json:"releases"`
		}{
			Releases: []*types.Release{
				{
					ID:        releaseID,
					ServiceID: serviceID,
					Version:   "v1.0.0",
					Status:    status,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			},
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := waitForBuild(ctx, apiClient, serviceID.String(), releaseID.String())
	assert.NoError(t, err)
}

func TestWaitForBuild_Failure(t *testing.T) {
	serviceID := uuid.New()
	releaseID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Releases []*types.Release `json:"releases"`
		}{
			Releases: []*types.Release{
				{
					ID:        releaseID,
					ServiceID: serviceID,
					Version:   "v1.0.0",
					Status:    types.ReleaseStatusFailed,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			},
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := waitForBuild(ctx, apiClient, serviceID.String(), releaseID.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build failed")
}

func TestWaitForBuild_ContextCanceled(t *testing.T) {
	serviceID := uuid.New()
	releaseID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Releases []*types.Release `json:"releases"`
		}{
			Releases: []*types.Release{
				{
					ID:     releaseID,
					Status: types.ReleaseStatusBuilding,
				},
			},
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately to trigger the ctx.Done() path
	cancel()

	err := waitForBuild(ctx, apiClient, serviceID.String(), releaseID.String())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWaitForDeployment_Success(t *testing.T) {
	serviceID := uuid.New()
	var callCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)

		var health types.HealthStatus
		if count <= 1 {
			health = types.HealthStatusUnknown
		} else {
			health = types.HealthStatusHealthy
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(client.ServiceStatus{
			ServiceID: serviceID.String(),
			Health:    health,
			Status:    types.DeploymentStatusRunning,
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	err := waitForDeployment(ctx, apiClient, serviceID.String())
	assert.NoError(t, err)
}

func TestWaitForDeployment_ContextCanceled(t *testing.T) {
	serviceID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(client.ServiceStatus{
			ServiceID: serviceID.String(),
			Health:    types.HealthStatusUnknown,
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "test-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForDeployment(ctx, apiClient, serviceID.String())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
