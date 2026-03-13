package api

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"go.uber.org/zap"
)

// BuildQueue defines the queue operations used by API handlers.
type BuildQueue interface {
	Enqueue(ctx context.Context, job *queue.BuildJob) error
	QueueLength(ctx context.Context) (int64, error)
	GetJob(ctx context.Context, jobID uuid.UUID) (*queue.BuildJob, queue.JobStatus, error)
	GetResult(ctx context.Context, jobID uuid.UUID) (*queue.BuildResult, error)
	ActiveWorkers(ctx context.Context) ([]string, error)
	StreamLogs(ctx context.Context, jobID uuid.UUID, fromID string) (<-chan string, error)
	UpdateStatus(ctx context.Context, jobID uuid.UUID, status queue.JobStatus, workerID string) error
}

// Handlers contains all API handlers
type Handlers struct {
	queue  BuildQueue
	logger *zap.Logger
}

// NewHandlers creates new API handlers
func NewHandlers(q BuildQueue, logger *zap.Logger) *Handlers {
	return &Handlers{
		queue:  q,
		logger: logger,
	}
}

// Enqueue handles internal build enqueue requests
func (h *Handlers) Enqueue(c *gin.Context) {
	var req queue.EnqueueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	job := &queue.BuildJob{
		ReleaseID:   req.ReleaseID,
		ServiceID:   req.ServiceID,
		ServiceName: req.ServiceName,
		ProjectID:   req.ProjectID,
		GitRepo:     req.GitRepo,
		GitSHA:      req.GitSHA,
		GitBranch:   req.GitBranch,
		BuildConfig: req.BuildConfig,
		CallbackURL: req.CallbackURL,
		Priority:    req.Priority,
	}

	if err := h.queue.Enqueue(c.Request.Context(), job); err != nil {
		h.logger.Error("failed to enqueue job", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enqueue build"})
		return
	}

	// Get queue position
	queueLen, _ := h.queue.QueueLength(c.Request.Context())

	c.JSON(http.StatusAccepted, queue.EnqueueResponse{
		JobID:    job.ID,
		Position: int(queueLen),
	})
}

// GetJob retrieves a job by ID
func (h *Handlers) GetJob(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
		return
	}

	job, status, err := h.queue.GetJob(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	result, _ := h.queue.GetResult(c.Request.Context(), id)

	c.JSON(http.StatusOK, gin.H{
		"job":    job,
		"status": status,
		"result": result,
	})
}

// ListJobs lists jobs with optional filtering
func (h *Handlers) ListJobs(c *gin.Context) {
	// For now, return queue stats
	// Full implementation would query database for historical jobs
	queueLen, err := h.queue.QueueLength(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get queue length"})
		return
	}

	workers, _ := h.queue.ActiveWorkers(c.Request.Context())

	c.JSON(http.StatusOK, gin.H{
		"queued_jobs":    queueLen,
		"active_workers": len(workers),
		"workers":        workers,
	})
}

// StreamLogs streams build logs via SSE
func (h *Handlers) StreamLogs(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
		return
	}

	fromID := c.Query("from")

	logChan, err := h.queue.StreamLogs(c.Request.Context(), id, fromID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stream logs"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	c.Stream(func(w io.Writer) bool {
		select {
		case line, ok := <-logChan:
			if !ok {
				return false
			}
			c.SSEvent("log", line)
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

// CancelJob cancels a queued or running job
func (h *Handlers) CancelJob(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
		return
	}

	_, status, err := h.queue.GetJob(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	if status != queue.StatusQueued && status != queue.StatusBuilding {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job cannot be cancelled"})
		return
	}

	if err := h.queue.UpdateStatus(c.Request.Context(), id, queue.StatusCancelled, ""); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel job"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "job cancelled"})
}

// RetryJob retries a failed job
func (h *Handlers) RetryJob(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
		return
	}

	job, status, err := h.queue.GetJob(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	if status != queue.StatusFailed && status != queue.StatusCancelled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only failed or cancelled jobs can be retried"})
		return
	}

	// Create new job with same parameters
	newJob := &queue.BuildJob{
		ReleaseID:   job.ReleaseID,
		ServiceID:   job.ServiceID,
		ServiceName: job.ServiceName,
		ProjectID:   job.ProjectID,
		GitRepo:     job.GitRepo,
		GitSHA:      job.GitSHA,
		GitBranch:   job.GitBranch,
		BuildConfig: job.BuildConfig,
		CallbackURL: job.CallbackURL,
		Priority:    job.Priority + 1, // Slightly higher priority for retries
	}

	if err := h.queue.Enqueue(c.Request.Context(), newJob); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retry job"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":         "job retry queued",
		"original_job_id": id,
		"new_job_id":      newJob.ID,
	})
}

// GetStats returns build statistics
func (h *Handlers) GetStats(c *gin.Context) {
	ctx := c.Request.Context()

	queueLen, _ := h.queue.QueueLength(ctx)
	workers, _ := h.queue.ActiveWorkers(ctx)

	c.JSON(http.StatusOK, gin.H{
		"queue": gin.H{
			"pending": queueLen,
		},
		"workers": gin.H{
			"active": len(workers),
			"list":   workers,
		},
	})
}

// GetWorkers returns active workers
func (h *Handlers) GetWorkers(c *gin.Context) {
	workers, err := h.queue.ActiveWorkers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get workers"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"workers": workers,
		"count":   len(workers),
	})
}

// HealthCheck returns service health
func (h *Handlers) HealthCheck(c *gin.Context) {
	// Check Redis connection
	if _, err := h.queue.QueueLength(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  "redis connection failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "roundhouse",
	})
}
