package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// LifecycleEventCallback handles external CI/CD lifecycle event reports
// POST /v1/callbacks/lifecycle-event
// Auth: Bearer token (same as ArgoCD callback)
func (h *Handler) LifecycleEventCallback(c *gin.Context) {
	ctx := c.Request.Context()

	// Verify auth via bearer token (same pattern as ArgoCD callback)
	authHeader := c.GetHeader("Authorization")
	expectedAuth := "Bearer " + h.config.ArgocdWebhookSecret
	if h.config.ArgocdWebhookSecret == "" || authHeader != expectedAuth {
		h.logger.Warn(ctx, "Lifecycle callback unauthorized",
			logging.String("remote_addr", c.ClientIP()))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req types.LifecycleEventCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(ctx, "Invalid lifecycle event request",
			logging.Error("parse_error", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Auto-derive target_env from branch if not provided
	if req.TargetEnv == nil {
		env := types.DeriveTargetEnv(req.Branch)
		req.TargetEnv = &env
	}

	event := &types.DeploymentLifecycleEvent{
		DeploymentID: req.DeploymentID,
		ReleaseID:    req.ReleaseID,
		CIRunID:      req.CIRunID,
		ProjectID:    req.ProjectID,
		ServiceID:    req.ServiceID,
		RepoFullName: req.RepoFullName,
		CommitSHA:    req.CommitSHA,
		Branch:       req.Branch,
		Ref:          req.Ref,
		TargetEnv:    req.TargetEnv,
		EventType:    req.EventType,
		Source:       req.Source,
		Message:      req.Message,
		Metadata:     req.Metadata,
	}

	if err := h.repos.LifecycleEvents.Create(ctx, event); err != nil {
		h.logger.Error(ctx, "Failed to create lifecycle event",
			logging.String("event_type", req.EventType),
			logging.String("repo", req.RepoFullName),
			logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store event"})
		return
	}

	h.logger.Info(ctx, "Lifecycle event recorded",
		logging.String("event_id", event.ID.String()),
		logging.String("event_type", req.EventType),
		logging.String("repo", req.RepoFullName),
		logging.String("branch", req.Branch),
		logging.String("commit", req.CommitSHA))

	c.JSON(http.StatusOK, gin.H{
		"status":   "recorded",
		"event_id": event.ID,
	})
}

// GetLifecycleTimeline returns the event timeline for a repo
// GET /v1/lifecycle/timeline/:owner/:repo
// Query params: ?branch=main&env=production&since=2026-02-01&event_type=deploy_healthy&limit=50
func (h *Handler) GetLifecycleTimeline(c *gin.Context) {
	ctx := c.Request.Context()

	owner := c.Param("owner")
	repo := c.Param("repo")
	repoFullName := owner + "/" + repo

	q := types.LifecycleTimelineQuery{
		RepoFullName: &repoFullName,
	}

	if branch := c.Query("branch"); branch != "" {
		q.Branch = &branch
	}
	if env := c.Query("env"); env != "" {
		q.TargetEnv = &env
	}
	if eventType := c.Query("event_type"); eventType != "" {
		q.EventTypes = strings.Split(eventType, ",")
	}
	if since := c.Query("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			q.Since = &t
		} else if t, err := time.Parse("2006-01-02", since); err == nil {
			q.Since = &t
		}
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			q.Limit = l
		}
	}

	events, err := h.repos.LifecycleEvents.GetTimeline(ctx, q)
	if err != nil {
		h.logger.Error(ctx, "Failed to get lifecycle timeline",
			logging.String("repo", repoFullName),
			logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query timeline"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"repo":   repoFullName,
		"count":  len(events),
		"events": events,
	})
}

// GetLifecycleBranch returns events for a specific branch
// GET /v1/lifecycle/branch/:owner/:repo/:branch
func (h *Handler) GetLifecycleBranch(c *gin.Context) {
	ctx := c.Request.Context()

	owner := c.Param("owner")
	repo := c.Param("repo")
	branch := c.Param("branch")
	repoFullName := owner + "/" + repo

	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	events, err := h.repos.LifecycleEvents.GetByBranch(ctx, repoFullName, branch, limit)
	if err != nil {
		h.logger.Error(ctx, "Failed to get lifecycle branch events",
			logging.String("repo", repoFullName),
			logging.String("branch", branch),
			logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query branch events"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"repo":   repoFullName,
		"branch": branch,
		"count":  len(events),
		"events": events,
	})
}

// GetLifecycleCommit returns all events for a specific commit
// GET /v1/lifecycle/commit/:sha
func (h *Handler) GetLifecycleCommit(c *gin.Context) {
	ctx := c.Request.Context()

	sha := c.Param("sha")
	if len(sha) < 7 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "SHA must be at least 7 characters"})
		return
	}

	events, err := h.repos.LifecycleEvents.GetByCommit(ctx, sha)
	if err != nil {
		h.logger.Error(ctx, "Failed to get lifecycle commit events",
			logging.String("sha", sha),
			logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query commit events"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"commit_sha": sha,
		"count":      len(events),
		"events":     events,
	})
}

// GetLifecycleEvents returns recent events, filterable
// GET /v1/lifecycle/events
// Query params: ?project_id=...&env=production&event_type=deploy_healthy&branch=main&limit=50
func (h *Handler) GetLifecycleEvents(c *gin.Context) {
	ctx := c.Request.Context()

	q := types.LifecycleTimelineQuery{}

	if repo := c.Query("repo"); repo != "" {
		q.RepoFullName = &repo
	}
	if branch := c.Query("branch"); branch != "" {
		q.Branch = &branch
	}
	if env := c.Query("env"); env != "" {
		q.TargetEnv = &env
	}
	if projectID := c.Query("project_id"); projectID != "" {
		if id, err := uuid.Parse(projectID); err == nil {
			q.ProjectID = &id
		}
	}
	if eventType := c.Query("event_type"); eventType != "" {
		q.EventTypes = strings.Split(eventType, ",")
	}
	if since := c.Query("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			q.Since = &t
		} else if t, err := time.Parse("2006-01-02", since); err == nil {
			q.Since = &t
		}
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			q.Limit = l
		}
	}

	events, err := h.repos.LifecycleEvents.GetTimeline(ctx, q)
	if err != nil {
		h.logger.Error(ctx, "Failed to get lifecycle events",
			logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query events"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count":  len(events),
		"events": events,
	})
}

// emitLifecycleEvent is a helper to emit a lifecycle event (non-blocking, best-effort)
func (h *Handler) emitLifecycleEvent(event *types.DeploymentLifecycleEvent) {
	if event.TargetEnv == nil {
		env := types.DeriveTargetEnv(event.Branch)
		event.TargetEnv = &env
	}
	go func() {
		if err := h.repos.LifecycleEvents.Create(
			// Use background context since this runs async
			context.Background(),
			event,
		); err != nil {
			// Non-blocking — log but don't fail the parent operation
			h.logger.Error(context.Background(), "Failed to emit lifecycle event (non-blocking)",
				logging.String("event_type", event.EventType),
				logging.String("repo", event.RepoFullName),
				logging.Error("db_error", err))
		}
	}()
}
