package clients

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NewRoundhouseClient
// ---------------------------------------------------------------------------

func TestNewRoundhouseClient(t *testing.T) {
	client := NewRoundhouseClient("http://roundhouse:8080", "test-api-key")

	assert.Equal(t, "http://roundhouse:8080", client.baseURL)
	assert.Equal(t, "test-api-key", client.apiKey)
	assert.NotNil(t, client.httpClient, "httpClient should be initialized")
	assert.NotZero(t, client.httpClient.Timeout, "httpClient should have a non-zero timeout")
}

// ---------------------------------------------------------------------------
// BuildServiceConfigToRoundhouse
// ---------------------------------------------------------------------------

func TestBuildServiceConfigToRoundhouse(t *testing.T) {
	t.Run("converts all fields", func(t *testing.T) {
		cfg := types.BuildConfig{
			Type:       types.BuildTypeDockerfile,
			Dockerfile: "Dockerfile.prod",
			Buildpack:  "",
			Context:    "./app",
			BuildArgs:  map[string]string{"GO_VERSION": "1.22"},
			Target:     "production",
		}

		result := BuildServiceConfigToRoundhouse(cfg)

		assert.Equal(t, "dockerfile", result.Type)
		assert.Equal(t, "Dockerfile.prod", result.Dockerfile)
		assert.Equal(t, "./app", result.Context)
		assert.Equal(t, map[string]string{"GO_VERSION": "1.22"}, result.BuildArgs)
		assert.Equal(t, "production", result.Target)
	})

	t.Run("defaults empty context to dot", func(t *testing.T) {
		cfg := types.BuildConfig{
			Type:    types.BuildTypeBuildpack,
			Context: "", // empty
		}

		result := BuildServiceConfigToRoundhouse(cfg)

		assert.Equal(t, ".", result.Context, "empty context should default to '.'")
		assert.Equal(t, "buildpack", result.Type)
	})
}

// ---------------------------------------------------------------------------
// Enqueue
// ---------------------------------------------------------------------------

func TestEnqueue_Success(t *testing.T) {
	jobID := uuid.New()
	var receivedReq EnqueueRequest
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/enqueue", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		receivedAuth = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &receivedReq))

		w.WriteHeader(http.StatusAccepted)
		resp := EnqueueResponse{JobID: jobID, Position: 3}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	client := NewRoundhouseClient(server.URL, "secret-key")
	releaseID := uuid.New()
	serviceID := uuid.New()
	projectID := uuid.New()

	req := &EnqueueRequest{
		ReleaseID:   releaseID,
		ServiceID:   serviceID,
		ServiceName: "web-api",
		ProjectID:   projectID,
		ProjectSlug: "acme-app",
		GitRepo:     "github.com/acme/app",
		GitSHA:      "abc123def",
		GitBranch:   "main",
		BuildConfig: RoundhouseBuildConfig{
			Type:    "dockerfile",
			Context: ".",
		},
		CallbackURL: "https://api.enclii.dev/callbacks/build",
		Priority:    1,
	}

	result, err := client.Enqueue(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, jobID, result.JobID)
	assert.Equal(t, 3, result.Position)

	// Verify request was correctly marshaled and sent
	assert.Equal(t, "Bearer secret-key", receivedAuth)
	assert.Equal(t, releaseID, receivedReq.ReleaseID)
	assert.Equal(t, serviceID, receivedReq.ServiceID)
	assert.Equal(t, "web-api", receivedReq.ServiceName)
	assert.Equal(t, projectID, receivedReq.ProjectID)
	assert.Equal(t, "acme-app", receivedReq.ProjectSlug)
	assert.Equal(t, "github.com/acme/app", receivedReq.GitRepo)
	assert.Equal(t, "abc123def", receivedReq.GitSHA)
	assert.Equal(t, "main", receivedReq.GitBranch)
	assert.Equal(t, 1, receivedReq.Priority)
}

func TestEnqueue_NoApiKey_OmitsAuthHeader(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		resp := EnqueueResponse{JobID: uuid.New(), Position: 0}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewRoundhouseClient(server.URL, "") // empty API key

	req := &EnqueueRequest{
		ReleaseID: uuid.New(),
		ServiceID: uuid.New(),
		ProjectID: uuid.New(),
	}

	_, err := client.Enqueue(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, receivedAuth, "Authorization header should be absent when apiKey is empty")
}

func TestEnqueue_ServerError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"400 bad request", http.StatusBadRequest, `{"error":"invalid release_id"}`},
		{"500 internal error", http.StatusInternalServerError, `{"error":"queue full"}`},
		{"503 unavailable", http.StatusServiceUnavailable, "service unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewRoundhouseClient(server.URL, "key")
			_, err := client.Enqueue(context.Background(), &EnqueueRequest{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), "roundhouse returned status")
		})
	}
}

func TestEnqueue_ConnectionFailure(t *testing.T) {
	// Point at an address that will refuse the connection
	client := NewRoundhouseClient("http://127.0.0.1:1", "key")

	_, err := client.Enqueue(context.Background(), &EnqueueRequest{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request to roundhouse")
}

// ---------------------------------------------------------------------------
// GetJobStatus
// ---------------------------------------------------------------------------

func TestGetJobStatus_Success(t *testing.T) {
	jobID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/internal/jobs/"+jobID.String()+"/status", r.URL.Path)
		assert.Equal(t, "Bearer my-key", r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "building"})
	}))
	defer server.Close()

	client := NewRoundhouseClient(server.URL, "my-key")
	status, err := client.GetJobStatus(context.Background(), jobID)

	require.NoError(t, err)
	assert.Equal(t, "building", status)
}

func TestGetJobStatus_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewRoundhouseClient(server.URL, "key")
	_, err := client.GetJobStatus(context.Background(), uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "roundhouse returned status 404")
}

// ---------------------------------------------------------------------------
// CancelJob
// ---------------------------------------------------------------------------

func TestCancelJob_Success(t *testing.T) {
	jobID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/jobs/"+jobID.String()+"/cancel", r.URL.Path)
		assert.Equal(t, "Bearer cancel-key", r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewRoundhouseClient(server.URL, "cancel-key")
	err := client.CancelJob(context.Background(), jobID)

	require.NoError(t, err)
}

func TestCancelJob_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	client := NewRoundhouseClient(server.URL, "key")
	err := client.CancelJob(context.Background(), uuid.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "roundhouse returned status 409")
}

// ---------------------------------------------------------------------------
// HealthCheck
// ---------------------------------------------------------------------------

func TestHealthCheck_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/health", r.URL.Path)
		// HealthCheck should NOT send Authorization header
		assert.Empty(t, r.Header.Get("Authorization"),
			"HealthCheck should not send auth headers (it uses a public endpoint)")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer server.Close()

	// Note: HealthCheck does not set Authorization, so even with a key present
	// the header should be absent. This tests the actual implementation behavior.
	client := NewRoundhouseClient(server.URL, "key")
	err := client.HealthCheck(context.Background())

	require.NoError(t, err)
}

func TestHealthCheck_Unhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewRoundhouseClient(server.URL, "key")
	err := client.HealthCheck(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "health check failed with status 503")
}

func TestHealthCheck_ConnectionRefused(t *testing.T) {
	client := NewRoundhouseClient("http://127.0.0.1:1", "key")
	err := client.HealthCheck(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect to roundhouse")
}
