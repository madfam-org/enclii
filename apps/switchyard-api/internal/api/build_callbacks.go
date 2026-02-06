package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// BuildCallbackRequest represents the callback payload from Roundhouse after a build completes
// This matches the BuildResult type in apps/roundhouse/internal/queue/types.go
type BuildCallbackRequest struct {
	JobID          uuid.UUID `json:"job_id" binding:"required"`
	ReleaseID      uuid.UUID `json:"release_id" binding:"required"`
	Success        bool      `json:"success"`
	ImageURI       string    `json:"image_uri"`
	ImageDigest    string    `json:"image_digest"`
	ImageSizeMB    float64   `json:"image_size_mb"`
	SBOM           string    `json:"sbom"`
	SBOMFormat     string    `json:"sbom_format"`
	ImageSignature string    `json:"image_signature"`
	DurationSecs   float64   `json:"duration_secs"`
	ErrorMessage   string    `json:"error_message"`
	LogsURL        string    `json:"logs_url"`
}

// BuildCompleteCallback handles the callback from Roundhouse when a build finishes
// This is called by the Roundhouse worker after completing a build job
// POST /v1/callbacks/build-complete
func (h *Handler) BuildCompleteCallback(c *gin.Context) {
	ctx := c.Request.Context()

	// Verify the request comes from Roundhouse (API key auth)
	authHeader := c.GetHeader("Authorization")
	expectedAuth := "Bearer " + h.config.RoundhouseAPIKey
	if h.config.RoundhouseAPIKey != "" && authHeader != expectedAuth {
		h.logger.Warn(ctx, "Build callback unauthorized",
			logging.String("remote_addr", c.ClientIP()))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req BuildCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(ctx, "Invalid build callback request",
			logging.Error("parse_error", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info(ctx, "Received build callback from Roundhouse",
		logging.String("job_id", req.JobID.String()),
		logging.String("release_id", req.ReleaseID.String()),
		logging.Bool("success", req.Success))

	// Process the callback
	if err := h.processBuildCallback(ctx, &req); err != nil {
		h.logger.Error(ctx, "Failed to process build callback",
			logging.String("release_id", req.ReleaseID.String()),
			logging.Error("process_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process callback"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "processed",
		"release_id": req.ReleaseID,
	})
}

// processBuildCallback updates the release and triggers auto-deploy if applicable
func (h *Handler) processBuildCallback(ctx context.Context, req *BuildCallbackRequest) error {
	// Get the release
	release, err := h.repos.Releases.GetByID(req.ReleaseID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get release for callback",
			logging.String("release_id", req.ReleaseID.String()),
			logging.Error("db_error", err))
		return err
	}

	// Emit build lifecycle event
	buildEventType := types.LifecycleBuildSucceeded
	if !req.Success {
		buildEventType = types.LifecycleBuildFailed
	}
	buildMsg := "Build completed"
	if !req.Success && req.ErrorMessage != "" {
		buildMsg = "Build failed: " + req.ErrorMessage
	}
	h.emitLifecycleEvent(&types.DeploymentLifecycleEvent{
		ReleaseID:    &req.ReleaseID,
		ProjectID:    nil, // Will be resolved below if service lookup succeeds
		RepoFullName: release.RepoURL,
		CommitSHA:    release.GitSHA,
		Branch:       release.GitBranch,
		Ref:          "refs/heads/" + release.GitBranch,
		EventType:    buildEventType,
		Source:       types.SourceCICallback,
		Message:      &buildMsg,
		Metadata: map[string]interface{}{
			"job_id":        req.JobID.String(),
			"image_uri":     req.ImageURI,
			"image_digest":  req.ImageDigest,
			"duration_secs": req.DurationSecs,
		},
	})

	if req.Success {
		// Update release with build results
		if req.ImageURI != "" {
			if err := h.repos.Releases.UpdateImageURI(req.ReleaseID, req.ImageURI); err != nil {
				h.logger.Error(ctx, "Failed to update release image URI",
					logging.String("release_id", req.ReleaseID.String()),
					logging.Error("db_error", err))
				return err
			}
			h.logger.Info(ctx, "✓ Release image URI updated",
				logging.String("release_id", req.ReleaseID.String()),
				logging.String("image_uri", req.ImageURI))
		}

		// Store SBOM if provided
		if req.SBOM != "" {
			if err := h.repos.Releases.UpdateSBOM(ctx, req.ReleaseID, req.SBOM, req.SBOMFormat); err != nil {
				// SBOM storage failure is non-fatal
				h.logger.Warn(ctx, "Failed to store SBOM (non-fatal)",
					logging.String("release_id", req.ReleaseID.String()),
					logging.Error("db_error", err))
			} else {
				h.logger.Info(ctx, "✓ SBOM stored successfully",
					logging.String("release_id", req.ReleaseID.String()),
					logging.String("format", req.SBOMFormat))
			}
		}

		// Store signature if provided
		if req.ImageSignature != "" {
			if err := h.repos.Releases.UpdateSignature(ctx, req.ReleaseID, req.ImageSignature); err != nil {
				// Signature storage failure is non-fatal
				h.logger.Warn(ctx, "Failed to store signature (non-fatal)",
					logging.String("release_id", req.ReleaseID.String()),
					logging.Error("db_error", err))
			} else {
				h.logger.Info(ctx, "✓ Image signature stored successfully",
					logging.String("release_id", req.ReleaseID.String()))
			}
		}

		// Mark release as ready
		if err := h.repos.Releases.UpdateStatus(req.ReleaseID, types.ReleaseStatusReady); err != nil {
			h.logger.Error(ctx, "Failed to update release status to ready",
				logging.String("release_id", req.ReleaseID.String()),
				logging.Error("db_error", err))
			return err
		}

		h.logger.Info(ctx, "Build completed successfully (via Roundhouse)",
			logging.String("release_id", req.ReleaseID.String()),
			logging.String("job_id", req.JobID.String()),
			logging.Float64("duration_secs", req.DurationSecs),
			logging.String("image_uri", req.ImageURI))

		// Trigger auto-deploy if enabled
		service, err := h.repos.Services.GetByID(release.ServiceID)
		if err != nil {
			h.logger.Error(ctx, "Failed to get service for auto-deploy check",
				logging.String("service_id", release.ServiceID.String()),
				logging.Error("db_error", err))
			// Non-fatal - build succeeded, just can't auto-deploy
		} else if service.AutoDeploy && service.AutoDeployEnv != "" {
			h.logger.Info(ctx, "Triggering auto-deploy from Roundhouse callback",
				logging.String("service_name", service.Name),
				logging.String("target_env", service.AutoDeployEnv))

			// Log auto-deploy to Activity feed for dashboard visibility
			h.repos.AuditLogs.Log(ctx, &types.AuditLog{
				ActorID:      nil, // System action (auto-deploy)
				ActorEmail:   "auto-deploy@system.enclii.dev",
				ActorRole:    types.RoleSystem,
				Action:       "deployment.auto_triggered",
				ResourceType: "release",
				ResourceID:   release.ID.String(),
				ResourceName: service.Name,
				ProjectID:    &service.ProjectID,
				Outcome:      "success",
				Context: map[string]interface{}{
					"service_name": service.Name,
					"service_id":   service.ID.String(),
					"release_id":   release.ID.String(),
					"target_env":   service.AutoDeployEnv,
					"trigger":      "build_success",
					"commit_sha":   release.GitSHA,
					"image":        req.ImageURI,
				},
			})

			h.triggerAutoDeploy(ctx, service, release)
		}
	} else {
		// Build failed - store the error message for debugging
		var errorMsg *string
		if req.ErrorMessage != "" {
			errorMsg = &req.ErrorMessage
		}
		if err := h.repos.Releases.UpdateStatusWithError(req.ReleaseID, types.ReleaseStatusFailed, errorMsg); err != nil {
			h.logger.Error(ctx, "Failed to update release status to failed",
				logging.String("release_id", req.ReleaseID.String()),
				logging.Error("db_error", err))
			return err
		}

		h.logger.Error(ctx, "Build failed (via Roundhouse)",
			logging.String("release_id", req.ReleaseID.String()),
			logging.String("job_id", req.JobID.String()),
			logging.String("error", req.ErrorMessage),
			logging.String("logs_url", req.LogsURL))
	}

	return nil
}
