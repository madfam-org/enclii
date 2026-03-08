package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

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

		// Look up service for auto-deploy and GitOps digest commit
		service, err := h.repos.Services.GetByID(release.ServiceID)
		if err != nil {
			h.logger.Error(ctx, "Failed to get service for post-build actions",
				logging.String("service_id", release.ServiceID.String()),
				logging.Error("db_error", err))
			// Non-fatal - build succeeded, just can't auto-deploy or commit digest
		} else {
			// Commit image digest to target repo's kustomization.yaml (GitOps deploy)
			// This triggers ArgoCD auto-sync — the primary deployment mechanism for external repos
			if req.ImageDigest != "" {
				go h.commitDigestToTargetRepo(context.Background(), service, release, req.ImageURI, req.ImageDigest)
			}

			// Also trigger reconciler-based auto-deploy if configured
			if service.AutoDeploy && service.AutoDeployEnv != "" {
				h.logger.Info(ctx, "Triggering auto-deploy from Roundhouse callback",
					logging.String("service_name", service.Name),
					logging.String("target_env", service.AutoDeployEnv))

				// Log auto-deploy to Activity feed for dashboard visibility
				_ = h.repos.AuditLogs.Log(ctx, &types.AuditLog{
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

// commitDigestToTargetRepo commits an image digest to the target repo's kustomization.yaml
// via the GitHub Contents API. This triggers ArgoCD auto-sync for GitOps-based deployments.
// Runs in a goroutine — failures are logged but non-fatal to the build callback.
func (h *Handler) commitDigestToTargetRepo(ctx context.Context, service *types.Service, release *types.Release, imageURI, imageDigest string) {
	if h.config.GitHubToken == "" {
		h.logger.Debug(ctx, "Skipping digest commit: ENCLII_GITHUB_TOKEN not configured",
			logging.String("service", service.Name))
		return
	}

	// Extract owner/repo from service's git URL
	owner, repo := parseGitHubOwnerRepo(service.GitRepo)
	if owner == "" || repo == "" {
		h.logger.Warn(ctx, "Cannot commit digest: unable to parse git repo URL",
			logging.String("git_repo", service.GitRepo),
			logging.String("service", service.Name))
		return
	}

	// Look up the kustomization path from onboarding config snapshot
	repoFullName := owner + "/" + repo
	kustomizationPath := h.resolveKustomizationPath(ctx, repoFullName, service)
	if kustomizationPath == "" {
		h.logger.Warn(ctx, "Cannot commit digest: no kustomization path found",
			logging.String("repo", repoFullName),
			logging.String("service", service.Name))
		return
	}

	// Build the kustomize image reference: name=registry/image@digest
	// Extract the image name (without tag) from the full image URI
	imageName := imageURI
	if idx := strings.LastIndex(imageName, ":"); idx != -1 {
		imageName = imageName[:idx]
	}
	if idx := strings.LastIndex(imageName, "@"); idx != -1 {
		imageName = imageName[:idx]
	}

	// Read the current kustomization.yaml
	kustomizationFile := kustomizationPath + "/kustomization.yaml"
	currentContent, currentSHA, err := getGitHubFileContent(ctx, h.config.GitHubToken, owner, repo, kustomizationFile, "main")
	if err != nil {
		h.logger.Warn(ctx, "Failed to read kustomization.yaml from target repo (non-fatal)",
			logging.String("repo", repoFullName),
			logging.String("path", kustomizationFile),
			logging.Error("error", err))
		return
	}

	// Update the image digest in the kustomization content
	updatedContent := updateKustomizationImage(currentContent, imageName, service.Name, imageDigest)
	if updatedContent == currentContent {
		h.logger.Info(ctx, "Kustomization already up to date, skipping commit",
			logging.String("repo", repoFullName),
			logging.String("service", service.Name))
		return
	}

	// Commit the updated kustomization.yaml
	commitMsg := fmt.Sprintf("build: update %s image digest\n\nImage: %s@%s\nRelease: %s\nCommit: %s\n\nAuto-committed by Enclii platform build pipeline",
		service.Name, imageName, imageDigest[:12], release.ID.String()[:8], release.GitSHA[:8])

	commitSHA, err := createOrUpdateGitHubFileWithSHA(
		ctx,
		h.config.GitHubToken,
		owner, repo,
		kustomizationFile,
		[]byte(updatedContent),
		commitMsg,
		"main",
		currentSHA,
	)
	if err != nil {
		h.logger.Error(ctx, "Failed to commit digest to target repo (non-fatal)",
			logging.String("repo", repoFullName),
			logging.String("service", service.Name),
			logging.Error("error", err))

		// Emit lifecycle event for failed digest commit
		failMsg := "Failed to commit image digest to target repo: " + err.Error()
		h.emitLifecycleEvent(&types.DeploymentLifecycleEvent{
			ReleaseID:    &release.ID,
			ProjectID:    &service.ProjectID,
			RepoFullName: repoFullName,
			CommitSHA:    release.GitSHA,
			Branch:       release.GitBranch,
			EventType:    types.LifecycleDeployFailed,
			Source:       types.SourcePlatform,
			Message:      &failMsg,
		})
		return
	}

	h.logger.Info(ctx, "Successfully committed image digest to target repo",
		logging.String("repo", repoFullName),
		logging.String("service", service.Name),
		logging.String("digest", imageDigest[:12]),
		logging.String("commit", commitSHA))

	// Emit lifecycle event for successful digest commit
	deployMsg := fmt.Sprintf("Image digest committed to %s — ArgoCD will auto-sync", repoFullName)
	h.emitLifecycleEvent(&types.DeploymentLifecycleEvent{
		ReleaseID:    &release.ID,
		ProjectID:    &service.ProjectID,
		RepoFullName: repoFullName,
		CommitSHA:    release.GitSHA,
		Branch:       release.GitBranch,
		EventType:    types.LifecycleDeployStarted,
		Source:       types.SourcePlatform,
		Message:      &deployMsg,
		Metadata: map[string]interface{}{
			"digest_commit_sha": commitSHA,
			"image_digest":      imageDigest,
			"kustomization":     kustomizationFile,
		},
	})
}

// resolveKustomizationPath determines where the kustomization.yaml lives for a given repo/service.
// It checks: 1) onboarding config snapshot, 2) service build config context, 3) default path.
func (h *Handler) resolveKustomizationPath(ctx context.Context, repoFullName string, service *types.Service) string {
	// Try onboarding registration first (most reliable source)
	reg, err := h.repos.Onboardings.GetByRepo(ctx, repoFullName)
	if err == nil && reg != nil && reg.ConfigSnapshot != nil {
		if manifestPath, ok := reg.ConfigSnapshot["manifest_path"].(string); ok && manifestPath != "" {
			return manifestPath
		}
	}

	// Default to infra/k8s/production
	return "infra/k8s/production"
}

// parseGitHubOwnerRepo extracts owner and repo from a GitHub URL.
// Handles: https://github.com/owner/repo, https://github.com/owner/repo.git
func parseGitHubOwnerRepo(gitURL string) (string, string) {
	// Strip trailing .git
	gitURL = strings.TrimSuffix(gitURL, ".git")

	// Try HTTPS format: https://github.com/owner/repo
	re := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`)
	matches := re.FindStringSubmatch(gitURL)
	if len(matches) == 3 {
		return matches[1], matches[2]
	}

	// Try owner/repo format directly
	parts := strings.SplitN(gitURL, "/", 2)
	if len(parts) == 2 && !strings.Contains(parts[0], ".") {
		return parts[0], parts[1]
	}

	return "", ""
}

// updateKustomizationImage updates or adds an image digest entry in kustomization.yaml content.
// It handles the standard kustomize images format:
//
//	images:
//	  - name: image-name
//	    newName: registry/image
//	    digest: sha256:...
func updateKustomizationImage(content, imageName, serviceName, digest string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inImages := false
	foundImage := false
	imageIndent := ""
	i := 0

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Detect "images:" section
		if trimmed == "images:" {
			inImages = true
			result = append(result, line)
			i++
			continue
		}

		// Inside images section
		if inImages {
			// Check if we've left the images section (non-indented, non-empty line)
			if trimmed != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "#") {
				inImages = false
				// If we haven't found our image yet, add it before leaving
				if !foundImage {
					result = append(result, addImageEntry(imageIndent, imageName, serviceName, digest)...)
					foundImage = true
				}
				result = append(result, line)
				i++
				continue
			}

			// Check for our image entry: "- name: <imageName>" or "- name: <serviceName>"
			if strings.HasPrefix(trimmed, "- name:") {
				nameValue := strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
				// Match by full image name or service name (short name used in kustomize)
				if nameValue == imageName || nameValue == serviceName ||
					strings.HasSuffix(imageName, "/"+nameValue) {
					foundImage = true
					imageIndent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
					result = append(result, line)
					i++
					// Process sub-fields (newName, newTag, digest) — update digest, drop newTag
					for i < len(lines) {
						subLine := lines[i]
						subTrimmed := strings.TrimSpace(subLine)
						// Still in this image entry (indented more than the "- name:" line, not a new entry)
						if subTrimmed == "" || (strings.HasPrefix(subLine, imageIndent+" ") && !strings.HasPrefix(subTrimmed, "- ")) {
							if strings.HasPrefix(subTrimmed, "digest:") {
								// Replace existing digest
								result = append(result, imageIndent+"  digest: "+digest)
							} else if strings.HasPrefix(subTrimmed, "newTag:") {
								// Drop newTag when using digest
								// (kustomize uses either newTag or digest, not both)
							} else {
								result = append(result, subLine)
							}
							i++
						} else {
							// Add digest if not already present
							hasDigest := false
							for _, r := range result {
								if strings.Contains(r, "digest:") && strings.HasPrefix(strings.TrimSpace(r), "digest:") {
									hasDigest = true
									break
								}
							}
							if !hasDigest {
								result = append(result, imageIndent+"  digest: "+digest)
							}
							break
						}
					}
					continue
				}
			}

			// Track indent for image entries
			if strings.HasPrefix(trimmed, "- ") {
				imageIndent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			}
		}

		result = append(result, line)
		i++
	}

	// If images section ended at EOF without finding our image
	if inImages && !foundImage {
		result = append(result, addImageEntry(imageIndent, imageName, serviceName, digest)...)
		foundImage = true
	}

	// If no images section exists at all, add one
	if !foundImage {
		result = append(result, "images:")
		result = append(result, addImageEntry("", imageName, serviceName, digest)...)
	}

	return strings.Join(result, "\n")
}

// addImageEntry generates YAML lines for a new kustomize image entry
func addImageEntry(indent, imageName, serviceName, digest string) []string {
	if indent == "" {
		indent = "  "
	}
	return []string{
		indent + "- name: " + serviceName,
		indent + "  newName: " + imageName,
		indent + "  digest: " + digest,
	}
}
