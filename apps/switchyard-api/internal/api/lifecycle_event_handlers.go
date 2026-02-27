package api

import (
	"context"
	"fmt"
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

	// Create deployment records for pipeline-visible events (synchronous, best-effort)
	h.createDeploymentFromLifecycleEvent(ctx, req, event)

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

// createDeploymentFromLifecycleEvent creates a Deployment record from CI lifecycle events
// so that image_pushed shows as "deploying" and build_failed shows as "failed" in the UI.
// Runs synchronously — errors are logged but don't affect the callback response.
func (h *Handler) createDeploymentFromLifecycleEvent(ctx context.Context, req types.LifecycleEventCreate, event *types.DeploymentLifecycleEvent) {

	// Only handle events that should create deployment records
	var deployStatus types.DeploymentStatus
	var deployHealth types.HealthStatus
	var releaseStatus types.ReleaseStatus

	switch req.EventType {
	case types.LifecycleImagePushed:
		deployStatus = types.DeploymentStatusDeploying
		deployHealth = types.HealthStatusUnknown
		releaseStatus = types.ReleaseStatusReady
	case types.LifecycleBuildFailed:
		deployStatus = types.DeploymentStatusFailed
		deployHealth = types.HealthStatusUnhealthy
		releaseStatus = types.ReleaseStatusFailed
	default:
		return
	}

	// Resolve service name from metadata
	serviceName, _ := req.Metadata["service"].(string)
	if serviceName == "" {
		// Try to extract from repo name
		parts := strings.Split(req.RepoFullName, "/")
		if len(parts) >= 2 {
			serviceName = parts[len(parts)-1]
		}
	}
	if serviceName == "" {
		return
	}

	// Try candidate service names (same pattern as ArgoCD callback for nested images)
	// Always include the explicit service name first, then image-derived candidates (deduped)
	candidates := []string{serviceName}
	if imageURI, ok := req.Metadata["image"].(string); ok && imageURI != "" {
		imageCandidates := extractServiceCandidates(imageURI)
		seen := map[string]bool{serviceName: true}
		for _, c := range imageCandidates {
			if !seen[c] {
				candidates = append(candidates, c)
				seen[c] = true
			}
		}
	}

	var service *types.Service
	for _, candidate := range candidates {
		svc, err := h.repos.Services.GetByName(candidate)
		if err == nil {
			service = svc
			break
		}
	}
	// Fallback: look up by git repo URL (handles mono-service repos
	// where the DB service name doesn't match image-derived candidates)
	if service == nil && req.RepoFullName != "" {
		repoURL := "https://github.com/" + req.RepoFullName
		services, err := h.repos.Services.ListByGitRepo(repoURL)
		if err == nil && len(services) > 0 {
			service = services[0]
		}
	}
	if service == nil {
		h.logger.Warn(ctx, "No matching service for lifecycle event deployment",
			logging.String("service_name", serviceName),
			logging.String("candidates", strings.Join(candidates, ",")),
			logging.String("event_type", req.EventType))
		// Track resolution failure in event metadata for debugging
		if event.Metadata == nil {
			event.Metadata = make(map[string]interface{})
		}
		event.Metadata["service_resolution_failed"] = true
		event.Metadata["service_candidates"] = candidates
		_ = h.repos.LifecycleEvents.UpdateMetadata(ctx, event.ID, event.Metadata)
		return
	}

	// Find or create release
	imageURI, _ := req.Metadata["image"].(string)
	if imageURI == "" {
		imageURI = "ci://" + req.RepoFullName + ":" + shortSHA(req.CommitSHA)
	}

	release, err := h.findOrCreateReleaseWithStatus(ctx, service, imageURI, req.CommitSHA, releaseStatus)
	if err != nil {
		h.logger.Error(ctx, "Failed to find/create release for lifecycle deployment",
			logging.String("service_name", service.Name),
			logging.Error("error", err))
		return
	}

	// Find environment
	envName := service.AutoDeployEnv
	if envName == "" {
		envName = "production"
	}
	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, envName)
	if err != nil {
		h.logger.Error(ctx, "Failed to get environment for lifecycle deployment",
			logging.String("env_name", envName),
			logging.Error("db_error", err))
		return
	}

	// Before creating deploying record, check if ArgoCD already created a running deployment
	// (race condition: ArgoCD can sync before this goroutine runs)
	if deployStatus == types.DeploymentStatusDeploying {
		latestDeployment, _ := h.repos.Deployments.GetLatestByService(ctx, service.ID.String())
		if latestDeployment != nil &&
			latestDeployment.Status == types.DeploymentStatusRunning &&
			time.Since(latestDeployment.CreatedAt) < 5*time.Minute {
			h.logger.Info(ctx, "Skipping deploying record — ArgoCD already created running deployment",
				logging.String("service_name", service.Name),
				logging.String("existing_deployment_id", latestDeployment.ID.String()))
			return
		}
	}

	// Create deployment record
	deployment := &types.Deployment{
		ID:            uuid.New(),
		ReleaseID:     release.ID,
		EnvironmentID: env.ID,
		Replicas:      1,
		Status:        deployStatus,
		Health:        deployHealth,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// For build_failed, add error message
	if req.EventType == types.LifecycleBuildFailed {
		msg := "Build failed"
		if req.Message != nil {
			msg = *req.Message
		}
		deployment.ErrorMessage = &msg
	}

	if err := h.repos.Deployments.Create(deployment); err != nil {
		h.logger.Error(ctx, "Failed to create deployment from lifecycle event",
			logging.String("service_name", service.Name),
			logging.String("event_type", req.EventType),
			logging.Error("db_error", err))
		return
	}

	h.logger.Info(ctx, "Deployment created from lifecycle event",
		logging.String("deployment_id", deployment.ID.String()),
		logging.String("service_name", service.Name),
		logging.String("status", string(deployStatus)),
		logging.String("event_type", req.EventType))
}

// findOrCreateReleaseWithStatus is like findOrCreateRelease but allows setting the release status
func (h *Handler) findOrCreateReleaseWithStatus(ctx interface{ Value(any) any }, service *types.Service, imageURI, gitSHA string, status types.ReleaseStatus) (*types.Release, error) {
	// Try to find existing release by service + git SHA
	releases, err := h.repos.Releases.ListByService(service.ID)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}

	for _, r := range releases {
		if r.GitSHA == gitSHA && r.ServiceID == service.ID {
			return r, nil
		}
	}

	// No matching release — create one
	release := &types.Release{
		ServiceID: service.ID,
		Version:   fmt.Sprintf("ci-%s", shortSHA(gitSHA)),
		ImageURI:  imageURI,
		GitSHA:    gitSHA,
		Status:    status,
	}

	if err := h.repos.Releases.Create(release); err != nil {
		return nil, fmt.Errorf("create release: %w", err)
	}

	return release, nil
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
