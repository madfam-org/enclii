package api

import (
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// parseRFC3339 parses an RFC3339 time string
func parseRFC3339(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// CreateCronJobRequest defines the request body for creating a cron job
type CreateCronJobRequest struct {
	ServiceID   string `json:"service_id" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Schedule    string `json:"schedule" binding:"required"`
	Command     string `json:"command" binding:"required"`
	Image       string `json:"image,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
	Retries     int    `json:"retries,omitempty"`
	Concurrency string `json:"concurrency,omitempty"`
}

// UpdateCronJobRequest defines the request body for updating a cron job
type UpdateCronJobRequest struct {
	Name        *string `json:"name,omitempty"`
	Schedule    *string `json:"schedule,omitempty"`
	Command     *string `json:"command,omitempty"`
	Image       *string `json:"image,omitempty"`
	Timeout     *int    `json:"timeout,omitempty"`
	Retries     *int    `json:"retries,omitempty"`
	Suspended   *bool   `json:"suspended,omitempty"`
	Concurrency *string `json:"concurrency,omitempty"`
}

// CreateOneOffJobRequest defines the request body for creating a one-off job
type CreateOneOffJobRequest struct {
	ServiceID string `json:"service_id" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Command   string `json:"command" binding:"required"`
	Image     string `json:"image,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
	RunAt     string `json:"run_at,omitempty"` // RFC3339
}

// cronExprRegex validates basic cron expressions (5-field standard cron)
var cronExprRegex = regexp.MustCompile(`^(\S+\s+){4}\S+$`)

// validConcurrencyPolicies are the allowed concurrency policies for cron jobs
var validConcurrencyPolicies = map[string]bool{
	"allow":   true,
	"forbid":  true,
	"replace": true,
}

// CreateCronJob creates a new cron job for a project
// POST /v1/projects/:slug/cron-jobs
func (h *Handler) CreateCronJob(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	var req CreateCronJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate cron expression
	if !cronExprRegex.MatchString(req.Schedule) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron expression: must be 5-field standard cron format"})
		return
	}

	// Validate concurrency policy
	concurrency := "forbid"
	if req.Concurrency != "" {
		if !validConcurrencyPolicies[req.Concurrency] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid concurrency policy: must be 'allow', 'forbid', or 'replace'"})
			return
		}
		concurrency = req.Concurrency
	}

	// Validate timeout
	timeout := 300
	if req.Timeout > 0 {
		timeout = req.Timeout
	}

	// Get project
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	// Parse service ID
	serviceID, err := uuid.Parse(req.ServiceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id"})
		return
	}

	job := &types.CronJob{
		ProjectID:   project.ID,
		ServiceID:   serviceID,
		Name:        req.Name,
		Schedule:    req.Schedule,
		Command:     req.Command,
		Image:       req.Image,
		Timeout:     timeout,
		Retries:     req.Retries,
		Concurrency: concurrency,
	}

	if err := h.repos.CronJobs.Create(ctx, job); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{"error": "cron job with this name already exists in project"})
			return
		}
		h.logger.Error(ctx, "Failed to create cron job", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create cron job"})
		return
	}

	h.logger.Info(ctx, "Cron job created",
		logging.String("name", job.Name),
		logging.String("project", slug))

	c.JSON(http.StatusCreated, gin.H{
		"cron_job": job,
		"message":  "Cron job created successfully",
	})
}

// ListCronJobs lists all cron jobs for a project
// GET /v1/projects/:slug/cron-jobs
func (h *Handler) ListCronJobs(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	jobs, err := h.repos.CronJobs.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list cron jobs", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list cron jobs"})
		return
	}

	if jobs == nil {
		jobs = []*types.CronJob{}
	}

	c.JSON(http.StatusOK, gin.H{
		"cron_jobs": jobs,
		"total":     len(jobs),
	})
}

// GetCronJob retrieves a single cron job by ID
// GET /v1/cron-jobs/:id
func (h *Handler) GetCronJob(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron job ID"})
		return
	}

	job, err := h.repos.CronJobs.GetByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "cron job not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get cron job", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get cron job"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// UpdateCronJob updates a cron job
// PATCH /v1/cron-jobs/:id
func (h *Handler) UpdateCronJob(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron job ID"})
		return
	}

	var req UpdateCronJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get existing job
	job, err := h.repos.CronJobs.GetByID(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "cron job not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get cron job", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get cron job"})
		return
	}

	// Apply updates
	if req.Name != nil {
		job.Name = *req.Name
	}
	if req.Schedule != nil {
		if !cronExprRegex.MatchString(*req.Schedule) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron expression"})
			return
		}
		job.Schedule = *req.Schedule
	}
	if req.Command != nil {
		job.Command = *req.Command
	}
	if req.Image != nil {
		job.Image = *req.Image
	}
	if req.Timeout != nil {
		if *req.Timeout <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "timeout must be positive"})
			return
		}
		job.Timeout = *req.Timeout
	}
	if req.Retries != nil {
		job.Retries = *req.Retries
	}
	if req.Suspended != nil {
		job.Suspended = *req.Suspended
	}
	if req.Concurrency != nil {
		if !validConcurrencyPolicies[*req.Concurrency] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid concurrency policy"})
			return
		}
		job.Concurrency = *req.Concurrency
	}

	if err := h.repos.CronJobs.Update(ctx, job); err != nil {
		h.logger.Error(ctx, "Failed to update cron job", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update cron job"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// DeleteCronJob deletes a cron job
// DELETE /v1/cron-jobs/:id
func (h *Handler) DeleteCronJob(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron job ID"})
		return
	}

	if err := h.repos.CronJobs.Delete(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "cron job not found"})
			return
		}
		h.logger.Error(ctx, "Failed to delete cron job", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete cron job"})
		return
	}

	h.logger.Info(ctx, "Cron job deleted", logging.String("id", id.String()))
	c.JSON(http.StatusOK, gin.H{"message": "cron job deleted"})
}

// ListCronJobRuns lists execution runs for a cron job
// GET /v1/cron-jobs/:id/runs
func (h *Handler) ListCronJobRuns(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cron job ID"})
		return
	}

	// Verify cron job exists
	if _, err := h.repos.CronJobs.GetByID(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "cron job not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get cron job", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get cron job"})
		return
	}

	runs, err := h.repos.CronJobRuns.ListByCronJob(ctx, id, 50)
	if err != nil {
		h.logger.Error(ctx, "Failed to list cron job runs", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list cron job runs"})
		return
	}

	if runs == nil {
		runs = []*types.CronJobRun{}
	}

	c.JSON(http.StatusOK, gin.H{
		"runs":  runs,
		"total": len(runs),
	})
}

// CreateOneOffJob creates a one-off job for a project
// POST /v1/projects/:slug/one-off-jobs
func (h *Handler) CreateOneOffJob(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	var req CreateOneOffJobRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get project
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	// Parse service ID
	serviceID, err := uuid.Parse(req.ServiceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id"})
		return
	}

	timeout := 300
	if req.Timeout > 0 {
		timeout = req.Timeout
	}

	job := &types.OneOffJob{
		ProjectID: project.ID,
		ServiceID: serviceID,
		Name:      req.Name,
		Command:   req.Command,
		Image:     req.Image,
		Timeout:   timeout,
	}

	// Parse optional run_at time
	if req.RunAt != "" {
		t, err := parseRFC3339(req.RunAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_at: must be RFC3339 format"})
			return
		}
		job.RunAt = &t
	}

	if err := h.repos.OneOffJobs.Create(ctx, job); err != nil {
		h.logger.Error(ctx, "Failed to create one-off job", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create one-off job"})
		return
	}

	h.logger.Info(ctx, "One-off job created",
		logging.String("name", job.Name),
		logging.String("project", slug))

	c.JSON(http.StatusCreated, gin.H{
		"one_off_job": job,
		"message":     "One-off job created successfully",
	})
}
