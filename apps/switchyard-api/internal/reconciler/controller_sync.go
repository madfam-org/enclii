package reconciler

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// k8sSyncScheduler runs the K8s→DB sync job every 60 seconds
func (c *Controller) k8sSyncScheduler(ctx context.Context) {
	defer c.wg.Done()

	logger := c.logger.WithField("component", "k8s-sync")
	logger.Info("Starting K8s→DB sync scheduler")

	// Run initial sync immediately
	c.runK8sSync(ctx, logger)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			logger.Debug("K8s sync scheduler stopping")
			return
		case <-ctx.Done():
			logger.Debug("K8s sync scheduler context cancelled")
			return
		case <-ticker.C:
			c.runK8sSync(ctx, logger)
		}
	}
}

// runK8sSync synchronizes K8s deployment state to database
func (c *Controller) runK8sSync(ctx context.Context, logger *logrus.Entry) {
	if c.k8sClient == nil {
		logger.Debug("K8s client not available, skipping sync")
		return
	}

	// Dynamic namespace discovery: Get all unique namespaces from environments table
	// This ensures new environment patterns (e.g., enclii-{project_slug}-{env_name}) are monitored
	envs, err := c.repositories.Environments.ListAll()
	if err != nil {
		logger.WithError(err).Error("Failed to list environments for namespace sync")
		return
	}

	// Build unique namespace set for scanning.
	// The ownership check (enclii.dev/managed-by: switchyard) ensures only Enclii-created
	// deployments are imported, regardless of which namespace they're in.
	namespaceSet := make(map[string]bool)
	// Include core namespaces for health monitoring
	for _, ns := range []string{"enclii", "janua", "data", "monitoring"} {
		namespaceSet[ns] = true
	}
	// Add all environment namespaces from database
	for _, env := range envs {
		if env.KubeNamespace != "" {
			namespaceSet[env.KubeNamespace] = true
		}
	}
	// Convert set to slice
	namespaces := make([]string, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaces = append(namespaces, ns)
	}

	logger.WithField("namespace_count", len(namespaces)).Debug("Syncing K8s deployments from namespaces")

	for _, ns := range namespaces {
		deployments, err := c.k8sClient.ListDeployments(ctx, ns)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"namespace": ns,
				"error":     err,
			}).Warn("Failed to list K8s deployments, skipping namespace")
			continue
		}

		for _, dep := range deployments {
			c.syncDeploymentToDatabase(ctx, ns, dep, logger)
		}
	}

	// Clean up stale "deploying" records that never received an ArgoCD sync
	cleaned, err := c.repositories.Deployments.CleanupAllStaleDeploying(ctx, 30*time.Minute)
	if err != nil {
		logger.WithError(err).Warn("Failed to cleanup stale deploying records")
	} else if cleaned > 0 {
		logger.WithField("count", cleaned).Info("Cleaned up stale deploying records (timed out)")
	}
}

// syncDeploymentToDatabase checks if a K8s deployment has corresponding DB records.
// Health propagation is label-agnostic (updates any service matching by name),
// while deployment record management is label-gated (only for enclii.dev/managed-by: switchyard).
func (c *Controller) syncDeploymentToDatabase(ctx context.Context, namespace string, k8sDep appsv1.Deployment, logger *logrus.Entry) {
	deploymentName := k8sDep.Name

	// Respect opt-out annotation (label-agnostic)
	if val, ok := k8sDep.Annotations["enclii.dev/reconcile"]; ok && val == "disabled" {
		logger.WithField("deployment", deploymentName).Debug("Deployment has reconciliation disabled, skipping")
		return
	}

	// STEP 1: Service health propagation (label-agnostic).
	// Dashboard health should reflect K8s reality regardless of who created the deployment.
	service, err := c.repositories.Services.GetByName(deploymentName)
	if err == nil && service != nil {
		c.propagateServiceHealth(ctx, service, &k8sDep, logger)
	}

	// STEP 2: Deployment record management (label-gated).
	// Only Enclii-managed deployments get release/deployment records created/updated.
	if !isEncliiManagedDeployment(&k8sDep) {
		return
	}

	if service == nil {
		logger.WithFields(logrus.Fields{
			"deployment": deploymentName,
			"namespace":  namespace,
		}).Warn("Enclii-managed deployment has no matching service in database")
		return
	}

	// Check if deployment record already exists
	existingDep, err := c.repositories.Deployments.GetLatestByService(ctx, service.ID.String())
	if err == nil && existingDep != nil {
		// Deployment record exists, update health status if needed
		c.updateDeploymentHealth(ctx, service, existingDep, &k8sDep, logger)
		return
	}

	// No deployment record exists - create missing release + deployment records
	if len(k8sDep.Spec.Template.Spec.Containers) == 0 {
		logger.WithField("deployment", deploymentName).Warn("K8s deployment has no containers, skipping")
		return
	}

	imageURI := k8sDep.Spec.Template.Spec.Containers[0].Image
	replicas := int32(1)
	if k8sDep.Spec.Replicas != nil {
		replicas = *k8sDep.Spec.Replicas
	}
	availableReplicas := k8sDep.Status.AvailableReplicas

	c.createMissingRecords(ctx, service, namespace, imageURI, replicas, availableReplicas, logger)
}

// propagateServiceHealth updates the service's health fields based on K8s deployment state.
// Called for ALL deployments matching a registered service, regardless of labels.
func (c *Controller) propagateServiceHealth(ctx context.Context, service *types.Service, k8sDep *appsv1.Deployment, logger *logrus.Entry) {
	replicas := int32(1)
	if k8sDep.Spec.Replicas != nil {
		replicas = *k8sDep.Spec.Replicas
	}
	availableReplicas := k8sDep.Status.AvailableReplicas

	var health types.HealthStatus
	if availableReplicas == replicas && replicas > 0 {
		health = types.HealthStatusHealthy
	} else if availableReplicas > 0 {
		health = types.HealthStatusUnhealthy
	} else {
		health = types.HealthStatusUnknown
	}

	status := "unknown"
	if availableReplicas > 0 {
		status = string(types.DeploymentStatusRunning)
	}

	if err := c.repositories.Services.UpdateHealthStatus(ctx, service.ID, health, status, replicas, availableReplicas); err != nil {
		logger.WithError(err).Warn("Failed to propagate health to service")
	}
}

// updateDeploymentHealth updates the health status of an existing deployment based on K8s state
func (c *Controller) updateDeploymentHealth(ctx context.Context, service *types.Service, deployment *types.Deployment, k8sDep *appsv1.Deployment, logger *logrus.Entry) {
	replicas := int32(1)
	if k8sDep.Spec.Replicas != nil {
		replicas = *k8sDep.Spec.Replicas
	}
	availableReplicas := k8sDep.Status.AvailableReplicas

	// Determine expected health status
	var expectedHealth types.HealthStatus
	if availableReplicas == replicas && replicas > 0 {
		expectedHealth = types.HealthStatusHealthy
	} else if availableReplicas > 0 {
		expectedHealth = types.HealthStatusUnhealthy
	} else {
		expectedHealth = types.HealthStatusUnknown
	}

	// Determine expected deployment status based on K8s state
	// If K8s shows healthy pods but deployment is stuck at pending or failed, transition to running
	newStatus := deployment.Status
	if expectedHealth == types.HealthStatusHealthy {
		if deployment.Status == types.DeploymentStatusPending {
			newStatus = types.DeploymentStatusRunning
			logger.WithFields(logrus.Fields{
				"deployment_id": deployment.ID,
				"old_status":    deployment.Status,
				"new_status":    newStatus,
			}).Info("Transitioning deployment from pending to running based on K8s state")
		} else if deployment.Status == types.DeploymentStatusFailed {
			// Recovery: If K8s shows healthy pods but deployment was marked failed,
			// transition to running (the deployment has actually succeeded)
			newStatus = types.DeploymentStatusRunning
			logger.WithFields(logrus.Fields{
				"deployment_id": deployment.ID,
				"old_status":    deployment.Status,
				"new_status":    newStatus,
			}).Info("Recovering failed deployment to running - K8s shows healthy pods")
		}
	}

	// Only update if health or status changed
	if deployment.Health != expectedHealth || deployment.Status != newStatus {
		if err := c.repositories.Deployments.UpdateStatus(deployment.ID, newStatus, expectedHealth); err != nil {
			logger.WithFields(logrus.Fields{
				"deployment_id": deployment.ID,
				"old_health":    deployment.Health,
				"new_health":    expectedHealth,
				"old_status":    deployment.Status,
				"new_status":    newStatus,
				"error":         err,
			}).Warn("Failed to update deployment status")
		} else {
			logger.WithFields(logrus.Fields{
				"deployment_id": deployment.ID,
				"old_health":    deployment.Health,
				"new_health":    expectedHealth,
				"old_status":    deployment.Status,
				"new_status":    newStatus,
			}).Debug("Updated deployment status")
		}
	}

}

// createMissingRecords creates release and deployment records for a K8s deployment
func (c *Controller) createMissingRecords(ctx context.Context, service *types.Service, namespace string, imageURI string, replicas int32, availableReplicas int32, logger *logrus.Entry) {
	// Extract version and git SHA from image URI
	version := extractVersionFromImage(imageURI)
	gitSHA := extractGitSHAFromImage(imageURI)

	// Get environment by namespace
	env, err := c.repositories.Environments.GetByKubeNamespace(namespace)
	if err != nil {
		logger.WithFields(logrus.Fields{
			"namespace": namespace,
			"service":   service.Name,
			"error":     err,
		}).Warn("No environment found for namespace, skipping")
		return
	}

	// Create release record
	release := &types.Release{
		ServiceID: service.ID,
		Version:   version,
		ImageURI:  imageURI,
		GitSHA:    gitSHA,
		Status:    types.ReleaseStatusReady,
	}

	if err := c.repositories.Releases.Create(release); err != nil {
		logger.WithFields(logrus.Fields{
			"service": service.Name,
			"error":   err,
		}).Error("Failed to create release record")
		return
	}

	// Determine health status based on replica counts
	health := types.HealthStatusUnknown
	if availableReplicas == replicas && replicas > 0 {
		health = types.HealthStatusHealthy
	} else if availableReplicas > 0 {
		health = types.HealthStatusUnhealthy
	}

	// Create deployment record
	deployment := &types.Deployment{
		ReleaseID:     release.ID,
		EnvironmentID: env.ID,
		Replicas:      int(replicas),
		Status:        types.DeploymentStatusRunning,
		Health:        health,
	}

	if err := c.repositories.Deployments.Create(deployment); err != nil {
		logger.WithFields(logrus.Fields{
			"service":    service.Name,
			"release_id": release.ID,
			"error":      err,
		}).Error("Failed to create deployment record")
		return
	}

	logger.WithFields(logrus.Fields{
		"service":       service.Name,
		"namespace":     namespace,
		"release_id":    release.ID,
		"deployment_id": deployment.ID,
		"version":       version,
	}).Info("Created missing deployment record from K8s state")
}

// extractVersionFromImage extracts version string from an image URI
// e.g., "ghcr.io/madfam-org/enclii/waybill:1ead1b30fdb4" -> "1ead1b30"
func extractVersionFromImage(imageURI string) string {
	// Find the tag after the last ":"
	lastColon := -1
	for i := len(imageURI) - 1; i >= 0; i-- {
		if imageURI[i] == ':' {
			lastColon = i
			break
		}
	}

	if lastColon == -1 || lastColon == len(imageURI)-1 {
		return "unknown"
	}

	tag := imageURI[lastColon+1:]
	// If tag is longer than 12 chars, truncate for version display
	if len(tag) > 12 {
		return tag[:12]
	}
	return tag
}

// extractGitSHAFromImage extracts git SHA from an image URI
// e.g., "ghcr.io/madfam-org/enclii/waybill:1ead1b30fdb4" -> "1ead1b30fdb4"
func extractGitSHAFromImage(imageURI string) string {
	// Find the tag after the last ":"
	lastColon := -1
	for i := len(imageURI) - 1; i >= 0; i-- {
		if imageURI[i] == ':' {
			lastColon = i
			break
		}
	}

	if lastColon == -1 || lastColon == len(imageURI)-1 {
		return ""
	}

	return imageURI[lastColon+1:]
}

// isEncliiManagedDeployment checks if a K8s deployment is managed by Enclii.
// Only deployments created by Enclii have the "enclii.dev/managed-by: switchyard" label.
// This prevents auto-importing external services (Janua, ingress, etc.) that happen to
// have a matching name in the services table.
func isEncliiManagedDeployment(dep *appsv1.Deployment) bool {
	if dep.Labels == nil {
		return false
	}
	managedBy, exists := dep.Labels["enclii.dev/managed-by"]
	return exists && managedBy == "switchyard"
}
