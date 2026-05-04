package reconciler

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
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

		// Also scan StatefulSets for health propagation (databases, caches)
		statefulSets, err := c.k8sClient.ListStatefulSets(ctx, ns)
		if err != nil {
			logger.WithFields(logrus.Fields{"namespace": ns}).
				WithError(err).Warn("Failed to list K8s statefulsets")
		} else {
			for _, ss := range statefulSets {
				c.syncStatefulSetHealth(ctx, ss, logger)
			}
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
//
// As of the rollout-state-truth migration, this honors EvaluateRolloutState
// (internal/k8s/rollout_state.go) so a stuck-but-old-RS-still-serving deployment
// gets surfaced as unhealthy, not lied about as healthy.
func (c *Controller) propagateServiceHealth(ctx context.Context, service *types.Service, k8sDep *appsv1.Deployment, logger *logrus.Entry) {
	replicas := int32(1)
	if k8sDep.Spec.Replicas != nil {
		replicas = *k8sDep.Spec.Replicas
	}
	availableReplicas := k8sDep.Status.AvailableReplicas

	health, status := deriveServiceHealthStatus(ctx, c.k8sClient, k8sDep, replicas, availableReplicas, logger)

	if err := c.repositories.Services.UpdateHealthStatus(ctx, service.ID, health, status, replicas, availableReplicas); err != nil {
		logger.WithError(err).Warn("Failed to propagate health to service")
	}
}

// updateDeploymentHealth updates the health status of an existing deployment based on K8s state
//
// Like propagateServiceHealth, this now consults EvaluateRolloutState so a
// "blocked" rollout (newest RS unready past grace, older RS still serving)
// flips the deployment record to unhealthy instead of riding on the misleading
// Deployment.AvailableReplicas signal.
func (c *Controller) updateDeploymentHealth(ctx context.Context, service *types.Service, deployment *types.Deployment, k8sDep *appsv1.Deployment, logger *logrus.Entry) {
	replicas := int32(1)
	if k8sDep.Spec.Replicas != nil {
		replicas = *k8sDep.Spec.Replicas
	}
	availableReplicas := k8sDep.Status.AvailableReplicas

	expectedHealth, _ := deriveServiceHealthStatus(ctx, c.k8sClient, k8sDep, replicas, availableReplicas, logger)

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

// syncStatefulSetHealth propagates health from a K8s StatefulSet to a matching DB service.
// Health-only — no deployment/release record creation (StatefulSets are infrastructure, not app deploys).
//
// TODO(rollout-state): EvaluateRolloutState only handles Deployments + ReplicaSets
// (the rolling-update model). StatefulSets use a different revision model
// (ControllerRevisions, partitioned updates) and would need a separate evaluator.
// In-cluster StatefulSets today are limited to data plane infra
// (data/postgres-pravara, monitoring/prometheus, data/redpanda, data/redis-ha)
// where the simple ready-replicas signal is acceptable. Skipping for this PR.
// Tracking issue: https://github.com/madfam-org/enclii/issues/new?labels=rollout-state&title=EvaluateRolloutState%3A+StatefulSet+support
func (c *Controller) syncStatefulSetHealth(ctx context.Context, ss appsv1.StatefulSet, logger *logrus.Entry) {
	// Respect opt-out annotation
	if val, ok := ss.Annotations["enclii.dev/reconcile"]; ok && val == "disabled" {
		return
	}

	service, err := c.repositories.Services.GetByName(ss.Name)
	if err != nil || service == nil {
		return // Silent skip — most StatefulSets won't have a matching DB service
	}

	replicas := int32(1)
	if ss.Spec.Replicas != nil {
		replicas = *ss.Spec.Replicas
	}
	ready := ss.Status.ReadyReplicas

	var health types.HealthStatus
	switch {
	case ready == replicas && replicas > 0:
		health = types.HealthStatusHealthy
	case ready > 0:
		health = types.HealthStatusUnhealthy
	default:
		health = types.HealthStatusUnknown
	}

	status := "unknown"
	if ready > 0 {
		status = string(types.DeploymentStatusRunning)
	}

	if err := c.repositories.Services.UpdateHealthStatus(ctx, service.ID, health, status, replicas, ready); err != nil {
		logger.WithError(err).WithField("statefulset", ss.Name).Warn("Failed to propagate StatefulSet health")
	}
}

// deriveServiceHealthStatus is the rollout-state-aware health/status derivation
// used by both propagateServiceHealth and updateDeploymentHealth.
//
// Behavior:
//   - Naïve baseline (matches pre-rollout-state code): healthy iff
//     availableReplicas == replicas, unhealthy if availableReplicas > 0,
//     else unknown. Status = running iff availableReplicas > 0.
//   - Rollout-state overlay: ask EvaluateRolloutState whether the newest RS
//     has actually landed. If state == blocked, downgrade health → unhealthy
//     (the newest revision is broken even though the old RS is still serving)
//     and log the blocked reason. If state == progressing or ok, leave the
//     baseline alone — progressing is normal, ok matches the truthful path.
//   - On K8s error or missing client: silently fall back to baseline. The
//     reconciler runs every 60s; transient errors must not flip every service
//     to unknown.
//
// NOTE: services.rollout_blocked_reason DB column does not yet exist. The
// reason is logged at warn level so operators can grep for it. Adding a
// column is left to a follow-up migration (see PR description).
func deriveServiceHealthStatus(
	ctx context.Context,
	k8sClient *k8s.Client,
	k8sDep *appsv1.Deployment,
	replicas, availableReplicas int32,
	logger *logrus.Entry,
) (types.HealthStatus, string) {
	// Rollout-state overlay. Skip if no client (test paths) or the deployment
	// has no namespace yet — fall back to baseline-only derivation.
	if k8sClient == nil || !k8sClient.IsValid() || k8sDep == nil || k8sDep.Namespace == "" {
		return applyServiceRolloutState(replicas, availableReplicas, nil, nil, logger, "")
	}

	eval, err := k8s.EvaluateRolloutState(
		ctx,
		k8sClient.Clientset,
		k8sDep.Namespace,
		k8sDep.Name,
		time.Now(),
		k8s.DefaultRolloutGrace,
	)
	if err != nil {
		// Non-fatal: keep the baseline so we don't downgrade everything to
		// unknown on a transient API hiccup.
		logger.WithError(err).WithFields(logrus.Fields{
			"deployment": k8sDep.Name,
			"namespace":  k8sDep.Namespace,
		}).Debug("EvaluateRolloutState failed; keeping baseline health")
		return applyServiceRolloutState(replicas, availableReplicas, nil, nil, logger, "")
	}

	return applyServiceRolloutState(replicas, availableReplicas, &eval, k8sDep, logger, k8sDep.Namespace)
}

// applyServiceRolloutState is the pure decision function: given baseline replica
// counts and an optional RolloutEvaluation, decide the (health, status) tuple.
// Extracted so unit tests can exercise the truth table without a K8s client.
//
// Pass eval=nil to take the baseline-only path (i.e., simulate "no rollout
// information available" — this MUST match the legacy
// any-replica-Ready-means-healthy behavior so existing callers don't regress).
func applyServiceRolloutState(
	replicas, availableReplicas int32,
	eval *k8s.RolloutEvaluation,
	k8sDep *appsv1.Deployment,
	logger *logrus.Entry,
	namespaceForLog string,
) (types.HealthStatus, string) {
	// Baseline derivation (preserves backward compatibility when rollout-state
	// evaluation is unavailable).
	var health types.HealthStatus
	switch {
	case availableReplicas == replicas && replicas > 0:
		health = types.HealthStatusHealthy
	case availableReplicas > 0:
		health = types.HealthStatusUnhealthy
	default:
		health = types.HealthStatusUnknown
	}

	status := "unknown"
	if availableReplicas > 0 {
		status = string(types.DeploymentStatusRunning)
	}

	if eval == nil {
		return health, status
	}

	if eval.State == k8s.RolloutStateBlocked {
		// The naïve "any-Ready means healthy" lie. Override.
		// TODO: persist eval.BlockedReason when the services schema gains a
		// rollout_blocked_reason column (follow-up migration).
		fields := logrus.Fields{
			"rollout_state":          string(eval.State),
			"rollout_blocked_reason": string(eval.BlockedReason),
		}
		if k8sDep != nil {
			fields["deployment"] = k8sDep.Name
		}
		if namespaceForLog != "" {
			fields["namespace"] = namespaceForLog
		}
		if logger != nil {
			logger.WithFields(fields).Warn("Rollout blocked: newest ReplicaSet unready past grace; marking service unhealthy")
		}
		return types.HealthStatusUnhealthy, string(types.DeploymentStatusRunning)
	}

	return health, status
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
