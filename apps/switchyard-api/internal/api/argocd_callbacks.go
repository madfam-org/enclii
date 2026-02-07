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

		var deployment *types.Deployment
		if existingDeployment != nil {
			// Update the existing deploying record to running/healthy
			if err := h.repos.Deployments.UpdateStatus(existingDeployment.ID, types.DeploymentStatusRunning, types.HealthStatusHealthy); err != nil {
				h.logger.Error(ctx, "Failed to update deploying deployment from ArgoCD sync",
					logging.String("service_name", serviceName),
					logging.Error("db_error", err))
				continue
			}
			deployment = existingDeployment
			deployment.Status = types.DeploymentStatusRunning
			deployment.Health = types.HealthStatusHealthy
			h.logger.Info(ctx, "Updated existing deploying deployment to running",
				logging.String("deployment_id", deployment.ID.String()),
				logging.String("service_name", serviceName))
		} else {
			// Create new deployment record (backward compatible — no prior CI event)
			deployment = &types.Deployment{
				ID:            uuid.New(),
				ReleaseID:     release.ID,
				EnvironmentID: env.ID,
				Replicas:      1,
				Status:        types.DeploymentStatusRunning,
				Health:        types.HealthStatusHealthy,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
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
			},
		})
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
	// External repos (tezca, dhanam) push lifecycle events via CI callbacks
	// that carry commit metadata we can attach to the release.
	if events, err := h.repos.LifecycleEvents.GetByCommit(context.Background(), gitSHA); err == nil && len(events) > 0 {
		for _, evt := range events {
			if evt.Message != nil && *evt.Message != "" && release.CommitMessage == "" {
				release.CommitMessage = *evt.Message
			}
			if evt.Branch != "" && release.GitBranch == "" {
				release.GitBranch = evt.Branch
			}
			if evt.RepoFullName != "" && release.RepoURL == "" {
				release.RepoURL = "https://github.com/" + evt.RepoFullName
			}
			if author, ok := evt.Metadata["author"].(string); ok && author != "" && release.CommitAuthorName == "" {
				release.CommitAuthorName = author
			}
			if email, ok := evt.Metadata["author_email"].(string); ok && email != "" && release.CommitAuthorEmail == "" {
				release.CommitAuthorEmail = email
			}
			break // Use first matching event
		}
	}

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
	case syncStatus == "Synced" && healthStatus == "Degraded":
		return types.LifecycleDeployDegraded
	case syncStatus == "Synced":
		return types.LifecycleDeployHealthy
	default:
		return types.LifecycleDeploySynced
	}
}
