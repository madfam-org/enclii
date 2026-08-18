package switchyard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient(server.URL, "test-key", zap.NewNop())
	return client, server
}

func TestCreatePreview(t *testing.T) {
	c, _ := setupTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/previews", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req CreatePreviewRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, 42, req.PRNumber)
		assert.Equal(t, "feat-branch", req.PRBranch)

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"preview":{"id":"prev-1","preview_url":"https://pr-42.preview.enclii.dev","status":"active"},"action":"created"}`))
	})

	resp, err := c.CreatePreview(context.Background(), &CreatePreviewRequest{
		ServiceID: "svc-1",
		PRNumber:  42,
		PRBranch:  "feat-branch",
		CommitSHA: "abc12345",
	})
	require.NoError(t, err)
	assert.Equal(t, "prev-1", resp.Preview.ID)
	assert.Equal(t, "https://pr-42.preview.enclii.dev", resp.Preview.PreviewURL)
	assert.Equal(t, "created", resp.Action)
}

func TestCreatePreviewError(t *testing.T) {
	c, _ := setupTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	})

	_, err := c.CreatePreview(context.Background(), &CreatePreviewRequest{
		ServiceID: "svc-1",
		PRNumber:  1,
		PRBranch:  "branch",
		CommitSHA: "sha12345",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestGetServicesByRepo(t *testing.T) {
	c, _ := setupTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.String(), "git_repo=https://github.com/org/repo")

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"services":[{"id":"svc-1","name":"api","project_id":"proj-1"},{"id":"svc-2","name":"web","project_id":"proj-1"}]}`))
	})

	resp, err := c.GetServicesByRepo(context.Background(), "https://github.com/org/repo")
	require.NoError(t, err)
	assert.Len(t, resp.Services, 2)
	assert.Equal(t, "api", resp.Services[0].Name)
	assert.Equal(t, "web", resp.Services[1].Name)
}

func TestGetServicesByRepoError(t *testing.T) {
	c, _ := setupTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})

	_, err := c.GetServicesByRepo(context.Background(), "https://github.com/org/missing")
	assert.Error(t, err)
}

func TestClosePreviewByPR(t *testing.T) {
	c, _ := setupTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			// List previews
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"previews":[{"id":"prev-1"}]}`))
		} else if r.Method == "POST" {
			// Close preview
			assert.Contains(t, r.URL.Path, "/close")
			w.WriteHeader(http.StatusOK)
		}
	})

	err := c.ClosePreviewByPR(context.Background(), "svc-1", 42)
	assert.NoError(t, err)
}

func TestClosePreviewByPRNotFound(t *testing.T) {
	c, _ := setupTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	err := c.ClosePreviewByPR(context.Background(), "svc-1", 999)
	assert.NoError(t, err) // Not found is not an error
}

func TestNewClient(t *testing.T) {
	c := NewClient("https://api.example.com", "my-key", zap.NewNop())
	assert.Equal(t, "https://api.example.com", c.baseURL)
	assert.Equal(t, "my-key", c.apiKey)
	assert.NotNil(t, c.httpClient)
}
