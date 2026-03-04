package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestAPIClient_CreateProject(t *testing.T) {
	projectID := uuid.New()

	// Setup test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/projects", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "enclii-cli/1.0.0", r.Header.Get("User-Agent"))

		// Parse request body
		var req map[string]string
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "Test Project", req["name"])
		assert.Equal(t, "test-project", req["slug"])

		// Return response
		project := types.Project{
			ID:        projectID,
			Name:      req["name"],
			Slug:      req["slug"],
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(project)
	}))
	defer server.Close()

	// Create client
	client := NewAPIClient(server.URL, "test-token")

	// Test successful creation
	ctx := context.Background()
	project, err := client.CreateProject(ctx, "Test Project", "test-project")

	require.NoError(t, err)
	assert.NotNil(t, project)
	assert.Equal(t, projectID, project.ID)
	assert.Equal(t, "Test Project", project.Name)
	assert.Equal(t, "test-project", project.Slug)
}

func TestAPIClient_GetProject(t *testing.T) {
	projectID := uuid.New()

	project := &types.Project{
		ID:        projectID,
		Name:      "Test Project",
		Slug:      "test-project",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/projects/test-project", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(project)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")

	ctx := context.Background()
	result, err := client.GetProject(ctx, "test-project")

	require.NoError(t, err)
	assert.Equal(t, project.ID, result.ID)
	assert.Equal(t, project.Name, result.Name)
	assert.Equal(t, project.Slug, result.Slug)
}

func TestAPIClient_ListProjects(t *testing.T) {
	project1ID := uuid.New()
	project2ID := uuid.New()

	projects := []*types.Project{
		{
			ID:        project1ID,
			Name:      "Project 1",
			Slug:      "project-1",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:        project2ID,
			Name:      "Project 2",
			Slug:      "project-2",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/projects", r.URL.Path)

		response := struct {
			Projects []*types.Project `json:"projects"`
		}{
			Projects: projects,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")

	ctx := context.Background()
	result, err := client.ListProjects(ctx)

	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, project1ID, result[0].ID)
	assert.Equal(t, project2ID, result[1].ID)
}

func TestAPIClient_BuildService(t *testing.T) {
	releaseID := uuid.New()
	serviceID := uuid.New()

	release := &types.Release{
		ID:        releaseID,
		ServiceID: serviceID,
		Version:   "v1.0.0",
		ImageURI:  "registry.example.com/service:v1.0.0",
		GitSHA:    "abc123def456",
		Status:    types.ReleaseStatusBuilding,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/build")

		var req map[string]string
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "abc123def456", req["git_sha"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")

	ctx := context.Background()
	result, err := client.BuildService(ctx, serviceID.String(), "abc123def456")

	require.NoError(t, err)
	assert.Equal(t, release.ID, result.ID)
	assert.Equal(t, release.Version, result.Version)
	assert.Equal(t, release.Status, result.Status)
}

func TestAPIClient_DeployService(t *testing.T) {
	deploymentID := uuid.New()
	releaseID := uuid.New()
	envID := uuid.New()
	serviceID := uuid.New()

	deployment := &types.Deployment{
		ID:            deploymentID,
		ReleaseID:     releaseID,
		EnvironmentID: envID,
		Status:        types.DeploymentStatusPending,
		Replicas:      2,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Contains(t, r.URL.Path, "/deploy")

		var req DeployRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, releaseID.String(), req.ReleaseID)
		assert.Equal(t, 2, req.Replicas)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(deployment)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")

	ctx := context.Background()
	deployReq := DeployRequest{
		ReleaseID:   releaseID.String(),
		Environment: map[string]string{"ENV": "production"},
		Replicas:    2,
	}

	result, err := client.DeployService(ctx, serviceID.String(), deployReq)

	require.NoError(t, err)
	assert.Equal(t, deployment.ID, result.ID)
	assert.Equal(t, deployment.Status, result.Status)
	assert.Equal(t, deployment.Replicas, result.Replicas)
}

func TestAPIClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Project not found",
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")

	ctx := context.Background()
	_, err := client.GetProject(ctx, "nonexistent")

	require.Error(t, err)
	var apiErr APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, 404, apiErr.StatusCode)
	assert.Equal(t, "Project not found", apiErr.Message)
}

func TestAPIClient_Health(t *testing.T) {
	health := &HealthResponse{
		Status:  "healthy",
		Service: "switchyard-api",
		Version: "1.0.0",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/health", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(health)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")

	ctx := context.Background()
	result, err := client.Health(ctx)

	require.NoError(t, err)
	assert.Equal(t, "healthy", result.Status)
	assert.Equal(t, "switchyard-api", result.Service)
	assert.Equal(t, "1.0.0", result.Version)
}

// Test authentication header handling
func TestAPIClient_Authentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Unauthorized",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{Status: "healthy"})
	}))
	defer server.Close()

	// Test with valid token
	client := NewAPIClient(server.URL, "secret-token")
	ctx := context.Background()

	_, err := client.Health(ctx)
	require.NoError(t, err)

	// Test with invalid token
	client = NewAPIClient(server.URL, "invalid-token")
	_, err = client.Health(ctx)

	require.Error(t, err)
	var apiErr APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, 401, apiErr.StatusCode)
}

func TestAPIClient_OnboardProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/admin/onboard", r.URL.Path)

		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "madfam-org/test", req.RepoFullName)
		assert.Equal(t, "test", req.ProjectName)
		assert.Equal(t, "test-secrets", req.SecretName)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "completed",
			"repo":      req.RepoFullName,
			"project":   req.ProjectName,
			"namespace": "test",
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	branch := "main"
	var result map[string]interface{}
	err := client.OnboardProject(ctx, &types.OnboardingRequest{
		RepoFullName: "madfam-org/test",
		ProjectName:  "test",
		SecretName:   "test-secrets",
		Branch:       &branch,
	}, &result)

	require.NoError(t, err)
	assert.Equal(t, "completed", result["status"])
}

func TestAPIClient_PreflightOnboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/admin/onboard/preflight", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.PreflightResult{
			Pass: false,
			Violations: []types.PreflightIssue{
				{File: "deployment.yaml", Kind: "Deployment", Name: "test", Message: "image not qualified"},
			},
		})
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	var result types.PreflightResult
	err := client.PreflightOnboard(ctx, &types.OnboardingRequest{
		RepoFullName: "madfam-org/test",
		ProjectName:  "test",
	}, &result)

	require.NoError(t, err)
	assert.False(t, result.Pass)
	assert.Len(t, result.Violations, 1)
	assert.Equal(t, "deployment.yaml", result.Violations[0].File)
}

// Benchmark tests
func BenchmarkAPIClient_GetProject(b *testing.B) {
	projectID := uuid.New()

	project := &types.Project{
		ID:   projectID,
		Name: "Test Project",
		Slug: "test-project",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(project)
	}))
	defer server.Close()

	client := NewAPIClient(server.URL, "test-token")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.GetProject(ctx, "test-project")
		if err != nil {
			b.Fatal(err)
		}
	}
}
