package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ArgocdSyncRequest represents the callback payload from ArgoCD Notifications
type ArgocdSyncRequest struct {
	AppName      string   `json:"app_name" binding:"required"`
	SyncStatus   string   `json:"sync_status"`
	HealthStatus string   `json:"health_status"`
	Revision     string   `json:"revision"`
	Images       []string `json:"images"`
	StartedAt    string   `json:"started_at"`
	FinishedAt   string   `json:"finished_at"`
	ErrorMessage string   `json:"error_message"`
}

// ArgocdSyncCallback handles the callback from ArgoCD when a sync completes
// POST /v1/callbacks/argocd-sync
func (h *Handler) ArgocdSyncCallback(c *gin.Context) {
	ctx := c.Request.Context()

	// Verify auth via bearer token
	authHeader := c.GetHeader("Authorization")
	expectedAuth := "Bearer " + h.config.ArgocdWebhookSecret
	if h.config.ArgocdWebhookSecret == "" || authHeader != expectedAuth {
		h.logger.Warn(ctx, "ArgoCD callback unauthorized",
			logging.String("remote_addr", c.ClientIP()))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req ArgocdSyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(ctx, "Invalid ArgoCD callback request",
			logging.Error("parse_error", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info(ctx, "Received ArgoCD sync callback",
		logging.String("app_name", req.AppName),
		logging.String("sync_status", req.SyncStatus),
		logging.String("health_status", req.HealthStatus),
		logging.String("revision", req.Revision),
		logging.Int("image_count", len(req.Images)))

	// Detect sync failure/degradation
	isSyncFailure := req.SyncStatus == "OutOfSync" || req.SyncStatus == "Unknown" ||
		req.SyncStatus == "Error" || req.SyncStatus == "Failed" ||
		req.HealthStatus == "Degraded" || req.HealthStatus == "Missing"

	deploymentsCreated := 0

	for _, imageURI := range req.Images {
		candidateNames := extractServiceCandidates(imageURI)
		if len(candidateNames) == 0 {
			h.logger.Warn(ctx, "Could not extract service name from image",
				logging.String("image", imageURI))
			continue
		}

		// Look up service by name, trying candidates in order
		var service *types.Service
		var serviceName string
		for _, candidate := range candidateNames {
			svc, err := h.repos.Services.GetByName(candidate)
			if err == nil {
				service = svc
				serviceName = candidate
				break
			}
			if err != sql.ErrNoRows {
				h.logger.Error(ctx, "Failed to look up service",
					logging.String("service_name", candidate),
					logging.Error("db_error", err))
			}
		}
		// Fallback: derive repo URL from image and look up by git repo
		// (handles mono-service repos like yantra4d where DB service name
		// doesn't match image-derived candidates)
		if service == nil {
			repoName := repoFullNameFromImage(imageURI)
			if repoName != "" {
				repoURL := "https://github.com/" + repoName
				services, lookupErr := h.repos.Services.ListByGitRepo(repoURL)
				if lookupErr == nil && len(services) > 0 {
					service = services[0]
					serviceName = service.Name
				}
			}
		}
		if service == nil {
			h.logger.Debug(ctx, "No matching service for image",
				logging.String("candidates", strings.Join(candidateNames, ",")),
				logging.String("image", imageURI))
			continue
		}

		// Find the latest release for this service, or create one
		release, err := h.findOrCreateRelease(ctx, service, imageURI, req.Revision)
		if err != nil {
			h.logger.Error(ctx, "Failed to find/create release for ArgoCD sync",
				logging.String("service_name", serviceName),
				logging.Error("error", err))
			continue
		}

		// Skip if this service already has a running deployment with the same release
		// (happens when ArgoCD syncs an Application but not all images changed)
		latestDeployment, _ := h.repos.Deployments.GetLatestByService(ctx, service.ID.String())
		if latestDeployment != nil && latestDeployment.ReleaseID == release.ID &&
			latestDeployment.Status == types.DeploymentStatusRunning {
			h.logger.Debug(ctx, "Service already deployed with this release, skipping",
				logging.String("service_name", serviceName),
				logging.String("release_id", release.ID.String()))
			continue
		}

		// Find environment — use the service's auto-deploy env, fallback to "production"
		envName := service.AutoDeployEnv
		if envName == "" {
			envName = "production"
		}
		env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, envName)
		if err != nil {
			h.logger.Error(ctx, "Failed to get environment for ArgoCD deployment",
				logging.String("env_name", envName),
				logging.String("project_id", service.ProjectID.String()),
				logging.Error("db_error", err))
			continue
		}

		// Check for existing deployment with 'deploying' status (created by CI lifecycle event)
		existingDeployment, findErr := h.repos.Deployments.FindDeployingByServiceAndSHA(ctx, service.ID, req.Revision)
		if findErr != nil {
			h.logger.Warn(ctx, "Error looking up existing deploying deployment",
				logging.String("service_name", serviceName),
				logging.Error("error", findErr))
		}

		// Fallback 1: digest-commit CI job creates a new commit (B) after the original push (A).
		// CI lifecycle events use SHA=A, but ArgoCD syncs with SHA=B. Try matching via
		// the release's git_sha (which stores the original push SHA).
		if existingDeployment == nil && findErr == nil {
			existingDeployment, findErr = h.repos.Deployments.FindDeployingByServiceAndReleaseSHA(ctx, service.ID, release.GitSHA)
			if findErr != nil {
				h.logger.Warn(ctx, "Error in release-SHA deploying deployment lookup",
					logging.String("service_name", serviceName),
					logging.Error("error", findErr))
			}
			if existingDeployment != nil {
				h.logger.Info(ctx, "Found deploying deployment via release-SHA lookup",
					logging.String("service_name", serviceName),
					logging.String("release_git_sha", release.GitSHA),
					logging.String("deployment_id", existingDeployment.ID.String()))
			}
		}

		// Fallback 2: Time-window fallback (last resort — tightened to 15 minutes)
		if existingDeployment == nil && findErr == nil {
			existingDeployment, findErr = h.repos.Deployments.FindRecentDeployingByService(ctx, service.ID, 15*time.Minute)
			if findErr != nil {
				h.logger.Warn(ctx, "Error in time-window deploying deployment lookup",
					logging.String("service_name", serviceName),
					logging.Error("error", findErr))
			}
			if existingDeployment != nil {
				h.logger.Info(ctx, "Found deploying deployment via time-window fallback (SHA mismatch)",
					logging.String("service_name", serviceName),
					logging.String("argocd_revision", req.Revision),
					logging.String("deployment_id", existingDeployment.ID.String()))
			}
		}

		// Determine deployment status based on sync result
		var targetStatus types.DeploymentStatus
		var targetHealth types.HealthStatus
		if isSyncFailure {
			targetStatus = types.DeploymentStatusFailed
			targetHealth = types.HealthStatusUnhealthy
		} else {
			targetStatus = types.DeploymentStatusRunning
			targetHealth = types.HealthStatusHealthy
		}

		var deployment *types.Deployment
		if existingDeployment != nil {
			// Update the existing deploying record
			if isSyncFailure && req.ErrorMessage != "" {
				if err := h.repos.Deployments.UpdateStatusWithError(existingDeployment.ID, targetStatus, targetHealth, &req.ErrorMessage); err != nil {
					h.logger.Error(ctx, "Failed to update deploying deployment from ArgoCD sync",
						logging.String("service_name", serviceName),
						logging.Error("db_error", err))
					continue
				}
			} else {
				if err := h.repos.Deployments.UpdateStatus(existingDeployment.ID, targetStatus, targetHealth); err != nil {
					h.logger.Error(ctx, "Failed to update deploying deployment from ArgoCD sync",
						logging.String("service_name", serviceName),
						logging.Error("db_error", err))
					continue
				}
			}
			deployment = existingDeployment
			deployment.Status = targetStatus
			deployment.Health = targetHealth
			h.logger.Info(ctx, "Updated existing deploying deployment",
				logging.String("deployment_id", deployment.ID.String()),
				logging.String("service_name", serviceName),
				logging.String("status", string(targetStatus)))
		} else {
			// Create new deployment record (backward compatible — no prior CI event)
			deployment = &types.Deployment{
				ID:            uuid.New(),
				ReleaseID:     release.ID,
				EnvironmentID: env.ID,
				Replicas:      1,
				Status:        targetStatus,
				Health:        targetHealth,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}
			if isSyncFailure && req.ErrorMessage != "" {
				deployment.ErrorMessage = &req.ErrorMessage
			}

			if err := h.repos.Deployments.Create(deployment); err != nil {
				h.logger.Error(ctx, "Failed to create deployment from ArgoCD sync",
					logging.String("service_name", serviceName),
					logging.Error("db_error", err))
				continue
			}
		}

		deploymentsCreated++

		// Log to audit trail
		h.repos.AuditLogs.Log(ctx, &types.AuditLog{
			ActorEmail:   "argocd@system.enclii.dev",
			ActorRole:    types.RoleSystem,
			Action:       "deployment.argocd_sync",
			ResourceType: "deployment",
			ResourceID:   deployment.ID.String(),
			ResourceName: serviceName,
			ProjectID:    &service.ProjectID,
			Outcome:      "success",
			Context: map[string]interface{}{
				"app_name":      req.AppName,
				"sync_status":   req.SyncStatus,
				"health_status": req.HealthStatus,
				"revision":      req.Revision,
				"image":         imageURI,
			},
		})

		h.logger.Info(ctx, "Deployment created from ArgoCD sync",
			logging.String("deployment_id", deployment.ID.String()),
			logging.String("service_name", serviceName),
			logging.String("revision", req.Revision))

		// Emit lifecycle event for the deploy
		lifecycleEventType := argocdEventType(req.SyncStatus, req.HealthStatus)
		deployMsg := fmt.Sprintf("ArgoCD sync %s: %s/%s", req.SyncStatus, req.AppName, serviceName)
		h.emitLifecycleEvent(&types.DeploymentLifecycleEvent{
			DeploymentID: &deployment.ID,
			ReleaseID:    &release.ID,
			ProjectID:    &service.ProjectID,
			ServiceID:    &service.ID,
			RepoFullName: repoFullNameFromImage(imageURI),
			CommitSHA:    req.Revision,
			Branch:       "main",
			Ref:          "refs/heads/main",
			EventType:    lifecycleEventType,
			Source:       types.SourceArgocdCallback,
			Message:      &deployMsg,
			Metadata: map[string]interface{}{
				"app_name":      req.AppName,
				"sync_status":   req.SyncStatus,
				"health_status": req.HealthStatus,
				"image":         imageURI,
				"service":       serviceName,
			},
		})

		// Clean up any orphaned deploying records for this service
		// (from race conditions where CI goroutine ran after ArgoCD sync)
		if err := h.repos.Deployments.CleanupStaleDeploying(ctx, service.ID, 30*time.Minute); err != nil {
			h.logger.Warn(ctx, "Failed to cleanup stale deploying records",
				logging.String("service_name", serviceName),
				logging.Error("error", err))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":              "processed",
		"deployments_created": deploymentsCreated,
	})
}

// findOrCreateRelease finds an existing release matching the image+SHA or creates a new one
func (h *Handler) findOrCreateRelease(ctx interface{ Value(any) any }, service *types.Service, imageURI, gitSHA string) (*types.Release, error) {
	// Try to find existing release by service + git SHA
	releases, err := h.repos.Releases.ListByService(service.ID)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}

	for _, r := range releases {
		if r.GitSHA == gitSHA && r.ServiceID == service.ID {
			// Enrich existing release if it's missing metadata (race condition:
			// CI created the release first without commit metadata)
			if r.CommitMessage == "" || r.CommitAuthorName == "" || r.GitBranch == "" {
				h.enrichReleaseFromLifecycleEvents(ctx, r, gitSHA)
			}
			return r, nil
		}
	}

	// No matching release — create one
	release := &types.Release{
		ServiceID: service.ID,
		Version:   fmt.Sprintf("argocd-%s", shortSHA(gitSHA)),
		ImageURI:  imageURI,
		GitSHA:    gitSHA,
		Status:    types.ReleaseStatusReady,
	}

	// Enrich with metadata from lifecycle events (if available)
	h.enrichReleaseFields(release, gitSHA)

	if err := h.repos.Releases.Create(release); err != nil {
		return nil, fmt.Errorf("create release: %w", err)
	}

	return release, nil
}

// extractServiceCandidates returns candidate service names from a container image URI.
// For nested image paths like "ghcr.io/madfam-org/tezca/api", it returns both the
// simple name ("api") and the prefixed name ("tezca-api") so the caller can try both.
// e.g., "ghcr.io/madfam-org/enclii/switchyard-api:latest" → ["switchyard-api"]
// e.g., "ghcr.io/madfam-org/tezca/api@sha256:..." → ["tezca-api", "api"]
// e.g., "ghcr.io/madfam-org/dhanam/admin:main" → ["dhanam-admin", "admin"]
func extractServiceCandidates(imageURI string) []string {
	// Remove tag/digest
	ref := imageURI
	if idx := strings.LastIndex(ref, "@"); idx != -1 {
		ref = ref[:idx]
	}
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		ref = ref[:idx]
	}

	// Remove registry prefix (e.g., "ghcr.io/")
	cleaned := strings.TrimPrefix(ref, "ghcr.io/")
	cleaned = strings.TrimPrefix(cleaned, "docker.io/")

	parts := strings.Split(cleaned, "/")
	simpleName := path.Base(ref)

	// For paths like org/project/service (3+ segments after registry),
	// try project-service first (e.g., "tezca-api"), then simple name ("api")
	if len(parts) >= 3 {
		project := parts[len(parts)-2]
		prefixed := project + "-" + simpleName
		if prefixed != simpleName {
			return []string{prefixed, simpleName}
		}
	}

	return []string{simpleName}
}

// extractServiceName extracts a service name from a container image URI (simple version).
// Kept for backward compatibility.
func extractServiceName(imageURI string) string {
	candidates := extractServiceCandidates(imageURI)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

// shortSHA returns the first 7 characters of a SHA
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// repoFullNameFromImage extracts the org/repo from a GHCR image URI
// e.g., "ghcr.io/madfam-org/enclii/switchyard-api:latest" → "madfam-org/enclii"
// e.g., "ghcr.io/madfam-org/dhanam/api:main" → "madfam-org/dhanam"
func repoFullNameFromImage(imageURI string) string {
	// Remove tag/digest
	ref := imageURI
	if idx := strings.LastIndex(ref, "@"); idx != -1 {
		ref = ref[:idx]
	}
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		ref = ref[:idx]
	}

	// Remove registry prefix (e.g., "ghcr.io/")
	ref = strings.TrimPrefix(ref, "ghcr.io/")

	// Split into parts: org/repo/service
	parts := strings.Split(ref, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return ref
}

// argocdEventType determines the lifecycle event type from ArgoCD sync/health status.
// SyncStatus is the primary signal: "Synced" means ArgoCD finished applying.
// HealthStatus refines it: "Degraded" is the only negative terminal state.
func argocdEventType(syncStatus, healthStatus string) string {
	switch {
	case syncStatus == "Error" || syncStatus == "Failed":
		return types.LifecycleDeployFailed
	case healthStatus == "Degraded":
		return types.LifecycleDeployDegraded
	case healthStatus == "Missing":
		return types.LifecycleDeployFailed
	case syncStatus == "Synced" && healthStatus == "Healthy":
		return types.LifecycleDeployHealthy
	case syncStatus == "Synced":
		return types.LifecycleDeployHealthy
	default:
		return types.LifecycleDeploySynced
	}
}

// enrichReleaseFields populates empty metadata fields on a release from lifecycle events.
// Used when creating new releases in findOrCreateRelease.
// Prefers CI/webhook events over ArgoCD events — ArgoCD status messages like
// "ArgoCD sync Synced: ..." should never be stored as commit messages.
func (h *Handler) enrichReleaseFields(release *types.Release, gitSHA string) {
	events, err := h.repos.LifecycleEvents.GetByCommit(context.Background(), gitSHA)
	if err != nil || len(events) == 0 {
		return
	}

	// Prefer CI/webhook events over ArgoCD events for enrichment
	var bestEvt *types.DeploymentLifecycleEvent
	for i := range events {
		if events[i].Source == types.SourceCICallback || events[i].Source == types.SourceGitHubWebhook {
			bestEvt = &events[i]
			break
		}
	}
	// Fallback to any event for non-message fields
	if bestEvt == nil {
		bestEvt = &events[0]
	}

	// Only use message as CommitMessage from CI/webhook events (not ArgoCD status messages)
	if bestEvt.Message != nil && *bestEvt.Message != "" && release.CommitMessage == "" &&
		bestEvt.Source != types.SourceArgocdCallback {
		release.CommitMessage = *bestEvt.Message
	}
	if bestEvt.Branch != "" && release.GitBranch == "" {
		release.GitBranch = bestEvt.Branch
	}
	if bestEvt.RepoFullName != "" && release.RepoURL == "" {
		release.RepoURL = "https://github.com/" + bestEvt.RepoFullName
	}
	// Check both "author" and "actor" keys — external CI may use either
	if author, ok := bestEvt.Metadata["author"].(string); ok && author != "" && release.CommitAuthorName == "" {
		release.CommitAuthorName = author
	} else if actor, ok := bestEvt.Metadata["actor"].(string); ok && actor != "" && release.CommitAuthorName == "" {
		release.CommitAuthorName = actor
	}
	if email, ok := bestEvt.Metadata["author_email"].(string); ok && email != "" && release.CommitAuthorEmail == "" {
		release.CommitAuthorEmail = email
	}
}

// enrichReleaseFromLifecycleEvents enriches an existing release that has empty metadata
// and persists the update to the database. Used when CI created the release first
// (without commit metadata) and ArgoCD later finds it.
func (h *Handler) enrichReleaseFromLifecycleEvents(ctx interface{ Value(any) any }, release *types.Release, gitSHA string) {
	h.enrichReleaseFields(release, gitSHA)

	// Persist enriched metadata to DB
	if err := h.repos.Releases.UpdateMetadata(
		context.Background(), release.ID,
		release.CommitMessage, release.CommitAuthorName, release.CommitAuthorEmail,
		release.GitBranch, release.RepoURL,
	); err != nil {
		h.logger.Warn(context.Background(), "Failed to persist release metadata enrichment",
			logging.String("release_id", release.ID.String()),
			logging.Error("error", err))
	}
}
