package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockQueue implements BuildQueue for testing.
type mockQueue struct {
	enqueueFunc       func(ctx context.Context, job *queue.BuildJob) error
	queueLengthFunc   func(ctx context.Context) (int64, error)
	getJobFunc        func(ctx context.Context, id uuid.UUID) (*queue.BuildJob, queue.JobStatus, error)
	getResultFunc     func(ctx context.Context, id uuid.UUID) (*queue.BuildResult, error)
	activeWorkersFunc func(ctx context.Context) ([]string, error)
	streamLogsFunc    func(ctx context.Context, id uuid.UUID, fromID string) (<-chan string, error)
	updateStatusFunc  func(ctx context.Context, id uuid.UUID, status queue.JobStatus, workerID string) error
}

func (m *mockQueue) Enqueue(ctx context.Context, job *queue.BuildJob) error {
	if m.enqueueFunc != nil {
		return m.enqueueFunc(ctx, job)
	}
	return nil
}
func (m *mockQueue) QueueLength(ctx context.Context) (int64, error) {
	if m.queueLengthFunc != nil {
		return m.queueLengthFunc(ctx)
	}
	return 0, nil
}
func (m *mockQueue) GetJob(ctx context.Context, id uuid.UUID) (*queue.BuildJob, queue.JobStatus, error) {
	if m.getJobFunc != nil {
		return m.getJobFunc(ctx, id)
	}
	return nil, "", nil
}
func (m *mockQueue) GetResult(ctx context.Context, id uuid.UUID) (*queue.BuildResult, error) {
	if m.getResultFunc != nil {
		return m.getResultFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockQueue) ActiveWorkers(ctx context.Context) ([]string, error) {
	if m.activeWorkersFunc != nil {
		return m.activeWorkersFunc(ctx)
	}
	return nil, nil
}
func (m *mockQueue) StreamLogs(ctx context.Context, id uuid.UUID, fromID string) (<-chan string, error) {
	if m.streamLogsFunc != nil {
		return m.streamLogsFunc(ctx, id, fromID)
	}
	return nil, nil
}
func (m *mockQueue) UpdateStatus(ctx context.Context, id uuid.UUID, status queue.JobStatus, workerID string) error {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, id, status, workerID)
	}
	return nil
}

func setupTestRouter(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/enqueue", h.Enqueue)
	r.GET("/jobs/:id", h.GetJob)
	r.GET("/jobs", h.ListJobs)
	r.POST("/jobs/:id/cancel", h.CancelJob)
	r.POST("/jobs/:id/retry", h.RetryJob)
	r.GET("/stats", h.GetStats)
	r.GET("/workers", h.GetWorkers)
	r.GET("/health", h.HealthCheck)
	return r
}

func TestHealthCheck(t *testing.T) {
	h := NewHandlers(&mockQueue{
		queueLengthFunc: func(ctx context.Context) (int64, error) { return 0, nil },
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "healthy", resp["status"])
	assert.Equal(t, "roundhouse", resp["service"])
}

func TestHealthCheckUnhealthy(t *testing.T) {
	h := NewHandlers(&mockQueue{
		queueLengthFunc: func(ctx context.Context) (int64, error) {
			return 0, assert.AnError
		},
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestEnqueueSuccess(t *testing.T) {
	var enqueued bool
	h := NewHandlers(&mockQueue{
		enqueueFunc: func(ctx context.Context, job *queue.BuildJob) error {
			enqueued = true
			return nil
		},
		queueLengthFunc: func(ctx context.Context) (int64, error) { return 3, nil },
	}, zap.NewNop())
	r := setupTestRouter(h)

	body, _ := json.Marshal(queue.EnqueueRequest{
		ReleaseID:   uuid.New(),
		ServiceID:   uuid.New(),
		ServiceName: "api",
		ProjectID:   uuid.New(),
		GitRepo:     "https://github.com/org/repo",
		GitSHA:      "abc123",
		BuildConfig: queue.BuildConfig{Type: "auto"},
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/enqueue", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.True(t, enqueued)

	var resp queue.EnqueueResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, 3, resp.Position)
}

func TestEnqueueBadRequest(t *testing.T) {
	h := NewHandlers(&mockQueue{}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/enqueue", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetJobSuccess(t *testing.T) {
	jobID := uuid.New()
	h := NewHandlers(&mockQueue{
		getJobFunc: func(ctx context.Context, id uuid.UUID) (*queue.BuildJob, queue.JobStatus, error) {
			return &queue.BuildJob{ID: jobID, ServiceName: "api"}, queue.StatusBuilding, nil
		},
		getResultFunc: func(ctx context.Context, id uuid.UUID) (*queue.BuildResult, error) {
			return nil, nil
		},
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/jobs/"+jobID.String(), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "building", resp["status"])
}

func TestGetJobInvalidID(t *testing.T) {
	h := NewHandlers(&mockQueue{}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/jobs/not-a-uuid", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetJobNotFound(t *testing.T) {
	h := NewHandlers(&mockQueue{
		getJobFunc: func(ctx context.Context, id uuid.UUID) (*queue.BuildJob, queue.JobStatus, error) {
			return nil, "", assert.AnError
		},
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/jobs/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCancelJobSuccess(t *testing.T) {
	jobID := uuid.New()
	h := NewHandlers(&mockQueue{
		getJobFunc: func(ctx context.Context, id uuid.UUID) (*queue.BuildJob, queue.JobStatus, error) {
			return &queue.BuildJob{ID: jobID}, queue.StatusQueued, nil
		},
		updateStatusFunc: func(ctx context.Context, id uuid.UUID, status queue.JobStatus, workerID string) error {
			return nil
		},
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/jobs/"+jobID.String()+"/cancel", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCancelJobAlreadyCompleted(t *testing.T) {
	jobID := uuid.New()
	h := NewHandlers(&mockQueue{
		getJobFunc: func(ctx context.Context, id uuid.UUID) (*queue.BuildJob, queue.JobStatus, error) {
			return &queue.BuildJob{ID: jobID}, queue.StatusCompleted, nil
		},
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/jobs/"+jobID.String()+"/cancel", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRetryJobSuccess(t *testing.T) {
	jobID := uuid.New()
	h := NewHandlers(&mockQueue{
		getJobFunc: func(ctx context.Context, id uuid.UUID) (*queue.BuildJob, queue.JobStatus, error) {
			return &queue.BuildJob{
				ID:          jobID,
				ServiceName: "api",
				GitRepo:     "repo",
				GitSHA:      "sha",
				BuildConfig: queue.BuildConfig{Type: "auto"},
			}, queue.StatusFailed, nil
		},
		enqueueFunc: func(ctx context.Context, job *queue.BuildJob) error { return nil },
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/jobs/"+jobID.String()+"/retry", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, jobID.String(), resp["original_job_id"])
}

func TestRetryJobNotFailed(t *testing.T) {
	jobID := uuid.New()
	h := NewHandlers(&mockQueue{
		getJobFunc: func(ctx context.Context, id uuid.UUID) (*queue.BuildJob, queue.JobStatus, error) {
			return &queue.BuildJob{ID: jobID}, queue.StatusBuilding, nil
		},
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/jobs/"+jobID.String()+"/retry", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetStats(t *testing.T) {
	h := NewHandlers(&mockQueue{
		queueLengthFunc:   func(ctx context.Context) (int64, error) { return 5, nil },
		activeWorkersFunc: func(ctx context.Context) ([]string, error) { return []string{"w1", "w2"}, nil },
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/stats", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	queueMap := resp["queue"].(map[string]interface{})
	assert.Equal(t, float64(5), queueMap["pending"])
}

func TestGetWorkers(t *testing.T) {
	h := NewHandlers(&mockQueue{
		activeWorkersFunc: func(ctx context.Context) ([]string, error) {
			return []string{"worker-1", "worker-2"}, nil
		},
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/workers", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, float64(2), resp["count"])
}

func TestListJobs(t *testing.T) {
	h := NewHandlers(&mockQueue{
		queueLengthFunc:   func(ctx context.Context) (int64, error) { return 10, nil },
		activeWorkersFunc: func(ctx context.Context) ([]string, error) { return []string{"w1"}, nil },
	}, zap.NewNop())
	r := setupTestRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/jobs", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(10), resp["queued_jobs"])
	assert.Equal(t, float64(1), resp["active_workers"])
}
