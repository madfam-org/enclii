package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/clients"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// BuildService triggers a build for a service from a given git SHA
func (h *Handler) BuildService(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	serviceID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	var req struct {
		GitSHA    string `json:"git_sha" binding:"required"`
		GitBranch string `json:"git_branch"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.AbortBadRequest(c, "Invalid request body")
		return
	}

	// Default to main branch if not specified
	gitBranch := req.GitBranch
	if gitBranch == "" {
		gitBranch = "main"
	}

	// Get service details
	service, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service", logging.Error("db_error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Create release record
	// Get project for scoped image naming
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get project", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		return
	}

	// Project-scoped image URI: ghcr.io/madfam-org/{project-slug}/{service-name}:{sha}
	imageURI := h.config.Registry + "/" + project.Slug + "/" + service.Name + ":" + req.GitSHA[:7]

	release := &types.Release{
		ID:        uuid.New(),
		ServiceID: serviceID,
		Version:   "v" + time.Now().Format("20060102-150405") + "-" + req.GitSHA[:7],
		ImageURI:  imageURI,
		GitSHA:    req.GitSHA,
		Status:    types.ReleaseStatusBuilding,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.repos.Releases.Create(release); err != nil {
		h.logger.Error(ctx, "Failed to create release", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create release"})
		return
	}

	// Trigger async build process (mode-aware)
	h.triggerBuildAsync(service, release, req.GitSHA, gitBranch)

	c.JSON(http.StatusCreated, release)
}

// triggerBuildAsync routes builds to either in-process execution or Roundhouse queue
// based on the ENCLII_BUILD_MODE configuration
// This function returns immediately and processes builds in the background to avoid
// blocking webhook responses (GitHub has a 10-second timeout)
func (h *Handler) triggerBuildAsync(service *types.Service, release *types.Release, gitSHA, gitBranch string) {
	if h.config.BuildMode == "roundhouse" && h.roundhouseClient != nil {
		// Enqueue to Roundhouse for fault-tolerant, scalable builds
		// Run in goroutine to avoid blocking webhook response
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			h.enqueueToRoundhouse(ctx, service, release, gitSHA, gitBranch)
		}()
	} else {
		// Fall back to in-process builds (legacy behavior)
		go h.triggerBuild(service, release, gitSHA)
	}
}

// enqueueToRoundhouse sends a build job to the Roundhouse worker queue
func (h *Handler) enqueueToRoundhouse(ctx context.Context, service *types.Service, release *types.Release, gitSHA, gitBranch string) {
	// Debug: Log the service's build config to verify context is populated
	h.logger.Info(ctx, "Enqueueing build to Roundhouse",
		logging.String("service_id", service.ID.String()),
		logging.String("service_name", service.Name),
		logging.String("release_id", release.ID.String()),
		logging.String("git_sha", gitSHA),
		logging.String("build_config_context", service.BuildConfig.Context),
		logging.String("build_config_dockerfile", service.BuildConfig.Dockerfile))

	// Get project for context
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get project for build enqueue",
			logging.String("project_id", service.ProjectID.String()),
			logging.Error("db_error", err))
		errMsg := fmt.Sprintf("Roundhouse enqueue failed before project lookup: %v", err)
		if statusErr := h.repos.Releases.UpdateStatusWithError(release.ID, types.ReleaseStatusFailed, &errMsg); statusErr != nil {
			h.logger.Error(ctx, "Failed to mark release failed after Roundhouse project lookup error",
				logging.String("release_id", release.ID.String()),
				logging.Error("error", statusErr))
		}
		return
	}

	// Build callback URL
	callbackURL := fmt.Sprintf("%s/v1/callbacks/build-complete", h.config.SelfURL)

	req := &clients.EnqueueRequest{
		ReleaseID:   release.ID,
		ServiceID:   service.ID,
		ServiceName: service.Name, // Human-readable name for correct image tagging
		ProjectID:   project.ID,
		ProjectSlug: project.Slug, // Project slug for scoped image naming
		GitRepo:     service.GitRepo,
		GitSHA:      gitSHA,
		GitBranch:   gitBranch,
		BuildConfig: clients.BuildServiceConfigToRoundhouse(service.BuildConfig),
		CallbackURL: callbackURL,
		Priority:    1, // Normal priority
	}

	resp, err := h.roundhouseClient.Enqueue(ctx, req)
	if err != nil {
		h.logger.Error(ctx, "Failed to enqueue build to Roundhouse",
			logging.String("release_id", release.ID.String()),
			logging.Error("roundhouse_error", err))
		errMsg := fmt.Sprintf("Roundhouse enqueue failed: %v", err)
		if statusErr := h.repos.Releases.UpdateStatusWithError(release.ID, types.ReleaseStatusFailed, &errMsg); statusErr != nil {
			h.logger.Error(ctx, "Failed to mark release failed after Roundhouse enqueue error",
				logging.String("release_id", release.ID.String()),
				logging.Error("error", statusErr))
		}
		return
	}

	h.logger.Info(ctx, "Build enqueued to Roundhouse successfully",
		logging.String("job_id", resp.JobID.String()),
		logging.Int("queue_position", resp.Position),
		logging.String("release_id", release.ID.String()))
}

// triggerBuild is a helper method that executes the build process asynchronously
// Uses a semaphore to serialize builds and prevent OOM from concurrent operations
func (h *Handler) triggerBuild(service *types.Service, release *types.Release, gitSHA string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Acquire build semaphore (blocks if another build is running)
	h.logger.Info(ctx, "Waiting for build slot",
		logging.String("service_id", service.ID.String()),
		logging.String("release_id", release.ID.String()))

	select {
	case h.buildSemaphore <- struct{}{}:
		// Acquired semaphore, ensure we release it when done
		defer func() { <-h.buildSemaphore }()
	case <-ctx.Done():
		h.logger.Error(ctx, "Build timed out waiting for semaphore",
			logging.String("release_id", release.ID.String()))
		errMsg := "Build timed out waiting for an in-process build slot"
		if statusErr := h.repos.Releases.UpdateStatusWithError(release.ID, types.ReleaseStatusFailed, &errMsg); statusErr != nil {
			h.logger.Error(ctx, "Failed to update release status after timeout",
				logging.String("release_id", release.ID.String()),
				logging.Error("error", statusErr))
		}
		return
	}

	h.logger.Info(ctx, "Starting build process",
		logging.String("service_id", service.ID.String()),
		logging.String("release_id", release.ID.String()),
		logging.String("git_sha", gitSHA))

	// Execute the build
	buildResult := h.builder.BuildFromGit(ctx, service, gitSHA)

	if !buildResult.Success {
		h.logger.Error(ctx, "Build failed",
			logging.String("release_id", release.ID.String()),
			logging.Error("build_error", buildResult.Error))

		monitoring.RecordBuild("failed", "git", buildResult.Duration)

		// Update release status to failed
		errMsg := "Build failed"
		if buildResult.Error != nil {
			errMsg = buildResult.Error.Error()
		}
		if err := h.repos.Releases.UpdateStatusWithError(release.ID, types.ReleaseStatusFailed, &errMsg); err != nil {
			h.logger.Error(ctx, "Failed to update release status", logging.Error("db_error", err))
		}

		// Store build logs (in production, we'd save these to a logging service or database)
		h.logger.Error(ctx, "Build logs", logging.String("logs", fmt.Sprintf("%v", buildResult.Logs)))
		return
	}

	// Update release with image URI and mark as ready
	release.ImageURI = buildResult.ImageURI

	// Persist the actual image URI to the database (builder generates versioned tags)
	if err := h.repos.Releases.UpdateImageURI(release.ID, buildResult.ImageURI); err != nil {
		h.logger.Error(ctx, "Failed to update release image URI", logging.Error("db_error", err))
		errMsg := fmt.Sprintf("Failed to persist release image URI: %v", err)
		if statusErr := h.repos.Releases.UpdateStatusWithError(release.ID, types.ReleaseStatusFailed, &errMsg); statusErr != nil {
			h.logger.Error(ctx, "Failed to update release status after image URI error",
				logging.String("release_id", release.ID.String()),
				logging.Error("error", statusErr))
		}
		return
	}
	h.logger.Info(ctx, "✓ Release image URI updated", logging.String("image_uri", buildResult.ImageURI))

	// Store SBOM if generated
	if buildResult.SBOMGenerated && buildResult.SBOM != nil {
		h.logger.Info(ctx, "Storing SBOM",
			logging.String("release_id", release.ID.String()),
			logging.String("format", buildResult.SBOMFormat),
			logging.Int("package_count", buildResult.SBOM.PackageCount))

		if err := h.repos.Releases.UpdateSBOM(ctx, release.ID, buildResult.SBOM.Content, buildResult.SBOMFormat); err != nil {
			// SBOM storage failure is non-fatal - log warning and continue
			h.logger.Error(ctx, "Failed to store SBOM (non-fatal)", logging.Error("db_error", err))
		} else {
			h.logger.Info(ctx, "✓ SBOM stored successfully")
		}
	}

	// Store signature if generated
	if buildResult.ImageSigned && buildResult.Signature != nil {
		h.logger.Info(ctx, "Storing image signature",
			logging.String("release_id", release.ID.String()),
			logging.String("signing_method", buildResult.Signature.SigningMethod))

		if err := h.repos.Releases.UpdateSignature(ctx, release.ID, buildResult.Signature.Signature); err != nil {
			// Signature storage failure is non-fatal - log warning and continue
			h.logger.Error(ctx, "Failed to store signature (non-fatal)", logging.Error("db_error", err))
		} else {
			h.logger.Info(ctx, "✓ Image signature stored successfully")
		}
	}

	if err := h.repos.Releases.UpdateStatus(release.ID, types.ReleaseStatusReady); err != nil {
		h.logger.Error(ctx, "Failed to update release status", logging.Error("db_error", err))
		errMsg := fmt.Sprintf("Failed to mark release ready: %v", err)
		if statusErr := h.repos.Releases.UpdateStatusWithError(release.ID, types.ReleaseStatusFailed, &errMsg); statusErr != nil {
			h.logger.Error(ctx, "Failed to update release status to failed after ready error",
				logging.String("release_id", release.ID.String()),
				logging.Error("error", statusErr))
		}
		return
	}

	h.logger.Info(ctx, "Build completed successfully",
		logging.String("release_id", release.ID.String()),
		logging.String("image_uri", buildResult.ImageURI),
		logging.String("duration", buildResult.Duration.String()))

	// Log build details for debugging
	for _, log := range buildResult.Logs {
		h.logger.Debug(ctx, "Build log", logging.String("line", log))
	}

	// Record build metrics
	monitoring.RecordBuild("success", "git", buildResult.Duration)

	// Auto-deploy if enabled for this service
	if service.AutoDeploy && service.AutoDeployEnv != "" {
		h.triggerAutoDeploy(ctx, service, release)
	}
}

// triggerAutoDeploy creates a deployment for the successful build if auto-deploy is configured
func (h *Handler) triggerAutoDeploy(ctx context.Context, service *types.Service, release *types.Release) {
	h.logger.Info(ctx, "Auto-deploy triggered",
		logging.String("service_id", service.ID.String()),
		logging.String("service_name", service.Name),
		logging.String("release_id", release.ID.String()),
		logging.String("target_env", service.AutoDeployEnv))

	// Get project to build consistent namespace name
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		h.logger.Error(ctx, "Auto-deploy failed: could not get project",
			logging.String("project_id", service.ProjectID.String()),
			logging.Error("db_error", err))
		return
	}

	// Look up the target environment
	env, err := h.repos.Environments.GetByProjectAndName(service.ProjectID, service.AutoDeployEnv)
	if err != nil {
		// Environment doesn't exist - auto-create it
		h.logger.Info(ctx, "Auto-creating missing environment for auto-deploy",
			logging.String("environment", service.AutoDeployEnv),
			logging.String("project_id", service.ProjectID.String()))

		kubeNamespace := autoDeployKubeNamespace(project, service.AutoDeployEnv)

		env = &types.Environment{
			ProjectID:     service.ProjectID,
			Name:          service.AutoDeployEnv,
			KubeNamespace: kubeNamespace,
		}
		if err := h.repos.Environments.Create(env); err != nil {
			h.logger.Error(ctx, "Auto-deploy failed: could not create environment",
				logging.String("environment", service.AutoDeployEnv),
				logging.Error("db_error", err))
			return
		}

		h.logger.Info(ctx, "Successfully created environment for auto-deploy",
			logging.String("environment_id", env.ID.String()),
			logging.String("environment", service.AutoDeployEnv),
			logging.String("kube_namespace", kubeNamespace))
	} else if repairedNamespace, shouldRepair := autoDeployNamespaceRepair(project, service.AutoDeployEnv, env.KubeNamespace); shouldRepair {
		if err := h.repos.Environments.UpdateKubeNamespace(ctx, env.ID, repairedNamespace); err != nil {
			h.logger.Error(ctx, "Auto-deploy failed: could not repair environment namespace",
				logging.String("environment_id", env.ID.String()),
				logging.String("environment", service.AutoDeployEnv),
				logging.String("current_namespace", env.KubeNamespace),
				logging.String("repaired_namespace", repairedNamespace),
				logging.Error("db_error", err))
			return
		}
		h.logger.Warn(ctx, "Repaired legacy auto-deploy environment namespace",
			logging.String("environment_id", env.ID.String()),
			logging.String("environment", service.AutoDeployEnv),
			logging.String("previous_namespace", env.KubeNamespace),
			logging.String("repaired_namespace", repairedNamespace))
		env.KubeNamespace = repairedNamespace
	}

	// GUARDRAIL: Ensure registry credentials exist in target namespace before deploying
	// This prevents ImagePullBackOff errors that cause 502s
	if err := h.ensureRegistryCredentials(ctx, env.KubeNamespace); err != nil {
		h.logger.Error(ctx, "Auto-deploy blocked: failed to ensure registry credentials",
			logging.String("namespace", env.KubeNamespace),
			logging.String("service", service.Name),
			logging.Error("error", err))
		return
	}

	// Check if a deployment already exists for this release + environment
	existingDeployments, err := h.repos.Deployments.ListByRelease(ctx, release.ID.String())
	if err != nil {
		h.logger.Warn(ctx, "Auto-deploy: could not check for existing deployments",
			logging.String("release_id", release.ID.String()),
			logging.Error("db_error", err))
		// Continue anyway - the Create will fail if duplicate
	} else {
		for _, existing := range existingDeployments {
			if existing.EnvironmentID == env.ID {
				h.logger.Info(ctx, "Auto-deploy: deployment already exists for this release + environment, skipping",
					logging.String("existing_deployment_id", existing.ID.String()),
					logging.String("release_id", release.ID.String()),
					logging.String("environment_id", env.ID.String()),
					logging.String("status", string(existing.Status)))
				return
			}
		}
	}

	// Create deployment record
	deployment := &types.Deployment{
		ID:            uuid.New(),
		ReleaseID:     release.ID,
		EnvironmentID: env.ID,
		Replicas:      1, // Default to 1 replica for auto-deploy
		Status:        types.DeploymentStatusPending,
		Health:        types.HealthStatusUnknown,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := h.repos.Deployments.Create(deployment); err != nil {
		// Check if this is a duplicate key error (UNIQUE constraint violation)
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "UNIQUE") {
			h.logger.Info(ctx, "Auto-deploy: deployment already exists (duplicate key), skipping",
				logging.String("release_id", release.ID.String()),
				logging.String("environment_id", env.ID.String()))
			return
		}
		h.logger.Error(ctx, "Auto-deploy failed: could not create deployment",
			logging.String("service_id", service.ID.String()),
			logging.String("release_id", release.ID.String()),
			logging.String("environment_id", env.ID.String()),
			logging.Error("db_error", err))
		return
	}

	// Schedule deployment with reconciler (high priority)
	if err := h.reconciler.ScheduleReconciliation(deployment.ID.String(), 1); err != nil {
		h.logger.Warn(ctx, "Reconciler queue full, work queued for retry",
			logging.String("deployment_id", deployment.ID.String()),
			logging.Error("queue_error", err))
	}

	h.logger.Info(ctx, "Auto-deploy scheduled successfully",
		logging.String("deployment_id", deployment.ID.String()),
		logging.String("service_name", service.Name),
		logging.String("environment", service.AutoDeployEnv))
}

// ensureRegistryCredentials ensures the target namespace has the registry credentials secret
// If missing, it copies from the enclii namespace. This prevents ImagePullBackOff errors.
func (h *Handler) ensureRegistryCredentials(ctx context.Context, targetNamespace string) error {
	const secretName = "ghcr-credentials" // #nosec G101 -- secret reference name, not a credential
	const sourceNamespace = "enclii"

	// Check if secret already exists in target namespace
	_, err := h.k8sClient.Clientset.CoreV1().Secrets(targetNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		// Secret exists, nothing to do
		h.logger.Debug(ctx, "Registry credentials already exist in namespace",
			logging.String("namespace", targetNamespace))
		return nil
	}

	// If error is not "not found", return it
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check for registry credentials: %w", err)
	}

	// Secret doesn't exist - copy from source namespace
	h.logger.Info(ctx, "Copying registry credentials to namespace",
		logging.String("source", sourceNamespace),
		logging.String("target", targetNamespace))

	// Get the source secret
	sourceSecret, err := h.k8sClient.Clientset.CoreV1().Secrets(sourceNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get source registry credentials from %s: %w", sourceNamespace, err)
	}

	// Create a copy for the target namespace
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: targetNamespace,
			Labels: map[string]string{
				"enclii.dev/managed-by":  "switchyard",
				"enclii.dev/copied-from": sourceNamespace,
			},
		},
		Type: sourceSecret.Type,
		Data: sourceSecret.Data,
	}

	_, err = h.k8sClient.Clientset.CoreV1().Secrets(targetNamespace).Create(ctx, newSecret, metav1.CreateOptions{})
	if err != nil {
		// If it already exists (race condition), that's fine
		if errors.IsAlreadyExists(err) {
			h.logger.Debug(ctx, "Registry credentials created by another process",
				logging.String("namespace", targetNamespace))
			return nil
		}
		return fmt.Errorf("failed to create registry credentials in %s: %w", targetNamespace, err)
	}

	h.logger.Info(ctx, "Successfully copied registry credentials to namespace",
		logging.String("namespace", targetNamespace))
	return nil
}

func autoDeployKubeNamespace(project *types.Project, envName string) string {
	if strings.EqualFold(strings.TrimSpace(envName), defaultProductionEnvironmentName) {
		return defaultProductionNamespace(project)
	}
	return legacyAutoDeployKubeNamespace(project, envName)
}

func legacyAutoDeployKubeNamespace(project *types.Project, envName string) string {
	projectSlug := ""
	if project != nil {
		projectSlug = strings.TrimSpace(project.Slug)
	}
	envNameNormalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(envName), "_", "-"))
	return fmt.Sprintf("enclii-%s-%s", projectSlug, envNameNormalized)
}

func autoDeployNamespaceRepair(project *types.Project, envName, currentNamespace string) (string, bool) {
	currentNamespace = strings.TrimSpace(currentNamespace)
	desiredNamespace := autoDeployKubeNamespace(project, envName)
	if desiredNamespace == "" || currentNamespace == "" || currentNamespace == desiredNamespace {
		return "", false
	}
	if !strings.EqualFold(strings.TrimSpace(envName), defaultProductionEnvironmentName) {
		return "", false
	}
	if currentNamespace != legacyAutoDeployKubeNamespace(project, envName) {
		return "", false
	}
	return desiredNamespace, true
}

// ListReleases returns all releases for a given service
func (h *Handler) ListReleases(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	serviceID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	releases, err := h.repos.Releases.ListByService(serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list releases", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list releases"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"releases": releases})
}
