package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apperrors "github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// isTableNotExistError checks if an error is due to a missing database table
// This allows graceful degradation when migrations haven't been applied
func isTableNotExistError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "does not exist") ||
		strings.Contains(errStr, "relation") && strings.Contains(errStr, "does not exist")
}

// CreatePreviewRequest defines the request body for creating a preview environment
type CreatePreviewRequest struct {
	ServiceID    string `json:"service_id" binding:"required"`
	PRNumber     int    `json:"pr_number" binding:"required"`
	PRTitle      string `json:"pr_title,omitempty"`
	PRURL        string `json:"pr_url,omitempty"`
	PRAuthor     string `json:"pr_author,omitempty"`
	PRBranch     string `json:"pr_branch" binding:"required"`
	PRBaseBranch string `json:"pr_base_branch,omitempty"`
	CommitSHA    string `json:"commit_sha" binding:"required"`
}

// CreatePreviewComment defines the request body for creating a comment
type CreatePreviewCommentRequest struct {
	Content   string `json:"content" binding:"required"`
	Path      string `json:"path,omitempty"`
	XPosition *int   `json:"x_position,omitempty"`
	YPosition *int   `json:"y_position,omitempty"`
}

// ListPreviews returns preview environments for a service.
// Accepts an optional ?pr_number=<int> query parameter to filter to a single PR preview.
// GET /v1/services/:id/previews
func (h *Handler) ListPreviews(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service_id is required"})
		return
	}

	ctx := c.Request.Context()

	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id format"})
		return
	}

	var previews []*types.PreviewEnvironment

	prNumberStr := c.Query("pr_number")
	if prNumberStr != "" {
		// If pr_number is provided, filter by it
		var prNumber int
		if _, err := fmt.Sscanf(prNumberStr, "%d", &prNumber); err == nil {
			preview, err := h.repos.PreviewEnvironments.GetByServiceAndPR(ctx, serviceUUID, prNumber)
			if err != nil && err != sql.ErrNoRows {
				h.logger.Error(ctx, "Failed to get preview environment",
					logging.String("service_id", serviceID),
					logging.Int("pr_number", prNumber),
					logging.Error("error", err))
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get preview"})
				return
			}
			if preview != nil {
				previews = append(previews, preview)
			}
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pr_number format"})
			return
		}
	} else {
		// Otherwise, return all previews for the service
		var err error
		previews, err = h.repos.PreviewEnvironments.ListByService(ctx, serviceUUID)
		if err != nil {
			// Gracefully handle missing table (migrations not applied)
			if isTableNotExistError(err) {
				h.logger.Warn(ctx, "Preview environments table not found, returning empty list",
					logging.String("service_id", serviceID))
				c.JSON(http.StatusOK, gin.H{
					"previews": []*types.PreviewEnvironment{},
					"count":    0,
				})
				return
			}
			h.logger.Error(ctx, "Failed to list preview environments",
				logging.String("service_id", serviceID),
				logging.Error("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list previews"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"previews": previews,
		"count":    len(previews),
	})
}

// ListProjectPreviews returns all preview environments for a project
// GET /v1/projects/:slug/previews
func (h *Handler) ListProjectPreviews(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project slug is required"})
		return
	}

	ctx := c.Request.Context()

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	previews, err := h.repos.PreviewEnvironments.ListByProject(ctx, project.ID)
	if err != nil {
		// Gracefully handle missing table (migrations not applied)
		if isTableNotExistError(err) {
			h.logger.Warn(ctx, "Preview environments table not found, returning empty list",
				logging.String("project_slug", slug))
			c.JSON(http.StatusOK, gin.H{
				"previews": []*types.PreviewEnvironment{},
				"count":    0,
			})
			return
		}
		h.logger.Error(ctx, "Failed to list project preview environments",
			logging.String("project_slug", slug),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list previews"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"previews": previews,
		"count":    len(previews),
	})
}

// GetPreview returns a single preview environment
// GET /v1/previews/:id
func (h *Handler) GetPreview(c *gin.Context) {
	previewID := c.Param("id")
	if previewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview_id is required"})
		return
	}

	previewUUID, err := uuid.Parse(previewID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preview_id format"})
		return
	}

	preview, ok := h.loadPreviewWithAccess(c, previewUUID)
	if !ok {
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"preview": preview,
	})
}

// CreatePreview creates a new preview environment for a PR
// POST /v1/previews
func (h *Handler) CreatePreview(c *gin.Context) {
	var req CreatePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Parse service ID
	serviceUUID, err := uuid.Parse(req.ServiceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id format"})
		return
	}

	if !h.enforceServiceAccess(c, serviceUUID) {
		return
	}

	// Load service for preview metadata (access already verified)
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrServiceNotFound)
			return
		}
		h.logger.Error(ctx, "Failed to get service", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get service"})
		return
	}

	// Check if preview already exists for this PR
	existing, err := h.repos.PreviewEnvironments.GetByServiceAndPR(ctx, serviceUUID, req.PRNumber)
	if err != nil && err != sql.ErrNoRows {
		h.logger.Error(ctx, "Failed to check existing preview", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check existing preview"})
		return
	}

	if existing != nil {
		// Update existing preview with new commit
		if err := h.repos.PreviewEnvironments.UpdateCommit(ctx, existing.ID, req.CommitSHA); err != nil {
			h.logger.Error(ctx, "Failed to update preview commit", logging.Error("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update preview"})
			return
		}

		// Fetch updated preview
		existing, _ = h.repos.PreviewEnvironments.GetByID(ctx, existing.ID)
		c.JSON(http.StatusOK, gin.H{
			"preview": existing,
			"message": "Preview environment updated with new commit",
			"action":  "updated",
		})
		return
	}

	// Generate preview subdomain: pr-{number}-{service-slug}.preview.enclii.dev
	serviceSlug := strings.ToLower(strings.ReplaceAll(service.Name, " ", "-"))
	subdomain := fmt.Sprintf("pr-%d-%s", req.PRNumber, serviceSlug)
	previewURL := "https://" + subdomain + previewDomainSuffix

	// Set default base branch if not provided
	baseBranch := req.PRBaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	// Create new preview environment
	preview := &types.PreviewEnvironment{
		ProjectID:        service.ProjectID,
		ServiceID:        serviceUUID,
		PRNumber:         req.PRNumber,
		PRTitle:          req.PRTitle,
		PRURL:            req.PRURL,
		PRAuthor:         req.PRAuthor,
		PRBranch:         req.PRBranch,
		PRBaseBranch:     baseBranch,
		CommitSHA:        req.CommitSHA,
		PreviewSubdomain: subdomain,
		PreviewURL:       previewURL,
		Status:           types.PreviewStatusPending,
		AutoSleepAfter:   30, // 30 minutes default
	}

	if err := h.repos.PreviewEnvironments.Create(ctx, preview); err != nil {
		h.logger.Error(ctx, "Failed to create preview environment",
			logging.String("service_id", req.ServiceID),
			logging.Int("pr_number", req.PRNumber),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create preview"})
		return
	}

	h.logger.Info(ctx, "Preview environment created",
		logging.String("preview_id", preview.ID.String()),
		logging.String("service_id", req.ServiceID),
		logging.Int("pr_number", req.PRNumber),
		logging.String("preview_url", previewURL))

	// Trigger build for the preview environment (async)
	go h.triggerPreviewBuild(service, preview, req.CommitSHA)

	c.JSON(http.StatusCreated, gin.H{
		"preview": preview,
		"message": "Preview environment created, build starting",
		"action":  "created",
	})
}

// ClosePreview closes a preview environment (PR closed/merged)
// POST /v1/previews/:id/close
func (h *Handler) ClosePreview(c *gin.Context) {
	previewID := c.Param("id")
	if previewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview_id is required"})
		return
	}

	ctx := c.Request.Context()

	previewUUID, err := uuid.Parse(previewID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preview_id format"})
		return
	}

	preview, ok := h.loadPreviewWithAccess(c, previewUUID)
	if !ok {
		return
	}

	// Close the preview in the database
	if err := h.repos.PreviewEnvironments.Close(ctx, previewUUID); err != nil {
		h.logger.Error(ctx, "Failed to close preview",
			logging.String("preview_id", previewID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to close preview"})
		return
	}

	// Run full cleanup asynchronously (deletes namespace, K8s resources, CF tunnel)
	go h.cleanupPreviewResources(preview)

	h.logger.Info(ctx, "Preview environment closed",
		logging.String("preview_id", previewID),
		logging.Int("pr_number", preview.PRNumber))

	c.JSON(http.StatusOK, gin.H{
		"message": "Preview environment closed",
	})
}

// WakePreview wakes up a sleeping preview environment
// POST /v1/previews/:id/wake
func (h *Handler) WakePreview(c *gin.Context) {
	previewID := c.Param("id")
	if previewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview_id is required"})
		return
	}

	ctx := c.Request.Context()

	previewUUID, err := uuid.Parse(previewID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preview_id format"})
		return
	}

	preview, ok := h.loadPreviewWithAccess(c, previewUUID)
	if !ok {
		return
	}

	if preview.Status != types.PreviewStatusSleeping {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":          "preview is not sleeping",
			"current_status": preview.Status,
		})
		return
	}

	// Wake the preview in the database
	if err := h.repos.PreviewEnvironments.Wake(ctx, previewUUID); err != nil {
		h.logger.Error(ctx, "Failed to wake preview",
			logging.String("preview_id", previewID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to wake preview"})
		return
	}

	// Scale up the Kubernetes deployment to 1 replica
	previewNamespace := "enclii-preview-" + preview.PreviewSubdomain
	if h.k8sClient != nil {
		// Get service name for deployment
		service, err := h.repos.Services.GetByID(preview.ServiceID)
		if err == nil && service != nil {
			deploymentName := service.Name
			if err := h.k8sClient.ScaleDeployment(ctx, previewNamespace, deploymentName, 1); err != nil {
				h.logger.Error(ctx, "Failed to scale up preview deployment",
					logging.String("preview_id", previewID),
					logging.Error("error", err))
				// Revert database status
				_ = h.repos.PreviewEnvironments.UpdateStatus(ctx, previewUUID, types.PreviewStatusSleeping, "Failed to scale up deployment")
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scale up preview deployment"})
				return
			}
			h.logger.Info(ctx, "Preview deployment scaled to 1",
				logging.String("preview_id", previewID),
				logging.String("namespace", previewNamespace))
		}
	}

	h.logger.Info(ctx, "Preview environment woken up",
		logging.String("preview_id", previewID),
		logging.Int("pr_number", preview.PRNumber))

	c.JSON(http.StatusOK, gin.H{
		"message": "Preview environment is waking up",
	})
}

// DeletePreview permanently deletes a preview environment
// DELETE /v1/previews/:id
func (h *Handler) DeletePreview(c *gin.Context) {
	previewID := c.Param("id")
	if previewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview_id is required"})
		return
	}

	ctx := c.Request.Context()

	previewUUID, err := uuid.Parse(previewID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preview_id format"})
		return
	}

	preview, ok := h.loadPreviewWithAccess(c, previewUUID)
	if !ok {
		return
	}

	// Run full cleanup asynchronously (deletes namespace, K8s resources, CF tunnel)
	go h.cleanupPreviewResources(preview)

	// Delete the preview from the database
	if err := h.repos.PreviewEnvironments.Delete(ctx, previewUUID); err != nil {
		h.logger.Error(ctx, "Failed to delete preview from database",
			logging.String("preview_id", previewID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete preview"})
		return
	}

	h.logger.Info(ctx, "Preview environment deleted",
		logging.String("preview_id", previewID))

	c.JSON(http.StatusOK, gin.H{
		"message": "Preview environment deleted",
	})
}

// ListPreviewComments returns comments for a preview environment
// GET /v1/previews/:id/comments
func (h *Handler) ListPreviewComments(c *gin.Context) {
	previewID := c.Param("id")
	if previewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview_id is required"})
		return
	}

	ctx := c.Request.Context()

	previewUUID, err := uuid.Parse(previewID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preview_id format"})
		return
	}

	if _, ok := h.loadPreviewWithAccess(c, previewUUID); !ok {
		return
	}

	comments, err := h.repos.PreviewComments.ListByPreview(ctx, previewUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list preview comments",
			logging.String("preview_id", previewID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list comments"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"comments": comments,
		"count":    len(comments),
	})
}

// CreatePreviewComment creates a new comment on a preview
// POST /v1/previews/:id/comments
func (h *Handler) CreatePreviewComment(c *gin.Context) {
	previewID := c.Param("id")
	if previewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview_id is required"})
		return
	}

	var req CreatePreviewCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	previewUUID, err := uuid.Parse(previewID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preview_id format"})
		return
	}

	if _, ok := h.loadPreviewWithAccess(c, previewUUID); !ok {
		return
	}

	// Get user from context
	userID, _ := c.Get("user_id")
	userEmail, _ := c.Get("user_email")
	userName, _ := c.Get("user_name")

	userUUID, _ := uuid.Parse(fmt.Sprintf("%v", userID))
	email := fmt.Sprintf("%v", userEmail)
	name := ""
	if userName != nil {
		name = fmt.Sprintf("%v", userName)
	}

	comment := &types.PreviewComment{
		PreviewID: previewUUID,
		UserID:    userUUID,
		UserEmail: email,
		UserName:  name,
		Content:   req.Content,
		Path:      req.Path,
		XPosition: req.XPosition,
		YPosition: req.YPosition,
	}

	if err := h.repos.PreviewComments.Create(ctx, comment); err != nil {
		h.logger.Error(ctx, "Failed to create preview comment",
			logging.String("preview_id", previewID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create comment"})
		return
	}

	h.logger.Info(ctx, "Preview comment created",
		logging.String("preview_id", previewID),
		logging.String("comment_id", comment.ID.String()))

	c.JSON(http.StatusCreated, gin.H{
		"comment": comment,
		"message": "Comment created",
	})
}

// ResolvePreviewComment marks a comment as resolved
// POST /v1/previews/:id/comments/:comment_id/resolve
func (h *Handler) ResolvePreviewComment(c *gin.Context) {
	commentID := c.Param("comment_id")
	if commentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "comment_id is required"})
		return
	}

	ctx := c.Request.Context()

	commentUUID, err := uuid.Parse(commentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid comment_id format"})
		return
	}

	// Get user from context
	userID, _ := c.Get("user_id")
	userUUID, _ := uuid.Parse(fmt.Sprintf("%v", userID))

	if err := h.repos.PreviewComments.Resolve(ctx, commentUUID, userUUID); err != nil {
		h.logger.Error(ctx, "Failed to resolve comment",
			logging.String("comment_id", commentID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve comment"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Comment resolved",
	})
}

// RecordPreviewAccess records an access to a preview (for analytics/auto-sleep)
// POST /v1/previews/:id/access
func (h *Handler) RecordPreviewAccess(c *gin.Context) {
	previewID := c.Param("id")
	if previewID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preview_id is required"})
		return
	}

	ctx := c.Request.Context()

	previewUUID, err := uuid.Parse(previewID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid preview_id format"})
		return
	}
	if _, ok := h.loadPreviewWithAccess(c, previewUUID); !ok {
		return
	}

	// Update last accessed time
	if err := h.repos.PreviewEnvironments.MarkAccessed(ctx, previewUUID); err != nil {
		h.logger.Warn(ctx, "Failed to mark preview accessed",
			logging.String("preview_id", previewID),
			logging.Error("error", err))
		// Don't fail the request
	}

	// Log access (optional - get from request context)
	accessLog := &types.PreviewAccessLog{
		PreviewID: previewUUID,
		Path:      c.Query("path"),
		UserAgent: c.GetHeader("User-Agent"),
		IPAddress: c.ClientIP(),
	}

	// Optional user context
	if userID, exists := c.Get("user_id"); exists {
		if uid, err := uuid.Parse(fmt.Sprintf("%v", userID)); err == nil {
			accessLog.UserID = &uid
		}
	}

	// Log in background (don't block response)
	go func() {
		// Use background context for async logging since the request context may be cancelled
		bgCtx := context.Background()
		if err := h.repos.PreviewAccessLogs.Log(bgCtx, accessLog); err != nil {
			h.logger.Warn(bgCtx, "Failed to log preview access (non-critical)",
				logging.String("preview_id", accessLog.PreviewID.String()),
				logging.Error("error", err))
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"message": "Access recorded",
	})
}
