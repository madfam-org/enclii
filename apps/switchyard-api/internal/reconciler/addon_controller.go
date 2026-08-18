package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// RetentionFinalizer tears down a retention-hold addon whose grace window has
// elapsed. Satisfied by *addons.AddonService.FinalizeExpiredDeletion. Kept as a
// narrow interface so the reconciler stays decoupled from the addon service and
// unit-testable with a fake (2026-08-17 audit #10).
type RetentionFinalizer interface {
	FinalizeExpiredDeletion(ctx context.Context, addon *types.DatabaseAddon) error
}

// AddonReconciler monitors and syncs database addon status
type AddonReconciler struct {
	repos         *db.Repositories
	k8sClient     *k8s.Client
	dynamicClient dynamic.Interface
	logger        *logrus.Logger
	stopCh        chan struct{}

	// finalizer tears down retention-hold addons past their window. May be nil
	// (older wiring) — the sweep is skipped in that case rather than panicking.
	finalizer RetentionFinalizer
}

// CloudNativePG Group Version Resource
var cnpgGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "clusters",
}

// NewAddonReconciler creates a new addon reconciler
func NewAddonReconciler(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *AddonReconciler {
	// Create dynamic client for CRD operations
	dynamicClient, err := dynamic.NewForConfig(k8sClient.Config())
	if err != nil {
		logger.WithError(err).Error("Failed to create dynamic client for addon reconciler")
	}

	return &AddonReconciler{
		repos:         repos,
		k8sClient:     k8sClient,
		dynamicClient: dynamicClient,
		logger:        logger,
		stopCh:        make(chan struct{}),
	}
}

// SetRetentionFinalizer wires the finalizer used by the retention sweep. Called
// from main after the addon service is built (the service and reconciler are
// constructed separately). Safe to leave unset — the sweep no-ops then.
func (r *AddonReconciler) SetRetentionFinalizer(f RetentionFinalizer) {
	r.finalizer = f
}

// Start begins the addon reconciliation loop
func (r *AddonReconciler) Start(ctx context.Context) {
	r.logger.Info("Starting addon reconciler")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial reconciliation
	r.reconcileAll(ctx)

	for {
		select {
		case <-ticker.C:
			r.reconcileAll(ctx)
		case <-r.stopCh:
			r.logger.Info("Addon reconciler stopped")
			return
		case <-ctx.Done():
			r.logger.Info("Addon reconciler context cancelled")
			return
		}
	}
}

// Stop gracefully shuts down the reconciler
func (r *AddonReconciler) Stop() {
	close(r.stopCh)
}

// reconcileAll checks all pending/provisioning addons and updates their status,
// then sweeps retention-hold addons whose grace window has elapsed.
func (r *AddonReconciler) reconcileAll(ctx context.Context) {
	// Retention sweep first: finalize any expired holds (audit #10). Runs even
	// when there are no pending/provisioning addons.
	r.sweepExpiredRetentionHolds(ctx)

	// Get all addons that need reconciliation
	addons, err := r.repos.DatabaseAddons.ListPending(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Failed to list pending addons")
		return
	}

	if len(addons) == 0 {
		return
	}

	r.logger.WithField("count", len(addons)).Debug("Reconciling addons")

	for _, addon := range addons {
		r.reconcileAddon(ctx, addon)
	}
}

// sweepExpiredRetentionHolds finds retention-hold addons whose
// deletion_scheduled_at has passed and finalizes their teardown. This is the
// deferred half of the audit #10 fix: a delete parks the addon in
// pending_deletion (data retained), and this sweep is what eventually destroys
// it once the window elapses. No-ops when no finalizer is wired.
func (r *AddonReconciler) sweepExpiredRetentionHolds(ctx context.Context) {
	if r.finalizer == nil {
		return
	}

	due, err := r.repos.DatabaseAddons.ListDeletionDue(ctx, time.Now())
	if err != nil {
		r.logger.WithError(err).Error("Failed to list retention-hold addons due for teardown")
		return
	}
	if len(due) == 0 {
		return
	}

	r.logger.WithField("count", len(due)).Info("Finalizing retention-hold addons past their window")

	for _, addon := range due {
		logger := r.logger.WithFields(logrus.Fields{
			"addon_id":              addon.ID,
			"deletion_scheduled_at": addon.DeletionScheduledAt,
		})
		if err := r.finalizer.FinalizeExpiredDeletion(ctx, addon); err != nil {
			// Leave the row in pending_deletion (unless finalize already moved
			// it to failed) so the next tick retries. A stuck teardown surfaces
			// via the addon's status_message / event ledger.
			logger.WithError(err).Error("Failed to finalize expired retention hold; will retry next tick")
			continue
		}
		logger.Info("Retention hold finalized; addon torn down")
	}
}

// reconcileAddon checks and updates a single addon's status
func (r *AddonReconciler) reconcileAddon(ctx context.Context, addon *types.DatabaseAddon) {
	logger := r.logger.WithFields(logrus.Fields{
		"addon_id":  addon.ID,
		"type":      addon.Type,
		"status":    addon.Status,
		"namespace": addon.K8sNamespace,
		"resource":  addon.K8sResourceName,
	})

	// Skip if no K8s info yet
	if addon.K8sNamespace == "" || addon.K8sResourceName == "" {
		logger.Debug("Addon missing K8s info, skipping reconciliation")
		return
	}

	switch addon.Type {
	case types.DatabaseAddonTypePostgres:
		r.reconcilePostgresAddon(ctx, addon, logger)
	case types.DatabaseAddonTypeRedis:
		r.reconcileRedisAddon(ctx, addon, logger)
	default:
		logger.Warn("Unknown addon type, skipping reconciliation")
	}
}

// reconcilePostgresAddon checks and updates a PostgreSQL addon's status
func (r *AddonReconciler) reconcilePostgresAddon(ctx context.Context, addon *types.DatabaseAddon, logger *logrus.Entry) {
	if r.dynamicClient == nil {
		logger.Warn("Dynamic client not available, skipping PostgreSQL reconciliation")
		return
	}

	// Get the CloudNativePG Cluster
	cluster, err := r.dynamicClient.Resource(cnpgGVR).Namespace(addon.K8sNamespace).Get(
		ctx,
		addon.K8sResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		logger.WithError(err).Warn("Failed to get CloudNativePG cluster")
		// If the cluster is being deleted and not found, mark as deleted
		if addon.Status == types.DatabaseAddonStatusDeleting {
			r.markAddonDeleted(ctx, addon, logger)
		}
		return
	}

	// Parse cluster status
	status, err := r.parseClusterStatus(cluster, addon)
	if err != nil {
		logger.WithError(err).Error("Failed to parse cluster status")
		return
	}

	// Update addon if status changed
	if r.shouldUpdateAddon(addon, status) {
		r.updateAddonFromStatus(ctx, addon, status, logger)
	}
}

// parseClusterStatus extracts status from a CloudNativePG Cluster
func (r *AddonReconciler) parseClusterStatus(cluster *unstructured.Unstructured, addon *types.DatabaseAddon) (*AddonStatusResult, error) {
	status, found, err := unstructured.NestedMap(cluster.Object, "status")
	if err != nil || !found {
		return &AddonStatusResult{
			Status:        types.DatabaseAddonStatusProvisioning,
			StatusMessage: "Cluster status not yet available",
		}, nil
	}

	// Extract key metrics
	readyInstances, _, _ := unstructured.NestedInt64(status, "readyInstances")
	instances, _, _ := unstructured.NestedInt64(status, "instances")
	phase, _, _ := unstructured.NestedString(status, "phase")
	writeService, _, _ := unstructured.NestedString(status, "writeService")

	result := &AddonStatusResult{
		DatabaseName: "app",
		Username:     "app",
	}

	// Set host from write service
	if writeService != "" {
		result.Host = fmt.Sprintf("%s.%s.svc.cluster.local", writeService, addon.K8sNamespace)
		result.Port = 5432
	}

	// Determine status based on phase
	switch phase {
	case "Cluster in healthy state":
		if readyInstances > 0 {
			result.Status = types.DatabaseAddonStatusReady
			result.StatusMessage = fmt.Sprintf("Cluster healthy with %d/%d instances", readyInstances, instances)
			result.Ready = true
		} else {
			result.Status = types.DatabaseAddonStatusProvisioning
			result.StatusMessage = "Cluster healthy but no ready instances"
		}
	case "Setting up primary":
		result.Status = types.DatabaseAddonStatusProvisioning
		result.StatusMessage = "Setting up primary instance"
	case "Creating replica":
		result.Status = types.DatabaseAddonStatusProvisioning
		result.StatusMessage = "Creating replica instances"
	case "Failed":
		result.Status = types.DatabaseAddonStatusFailed
		result.StatusMessage = "Cluster provisioning failed"
	default:
		// Check if ready based on instance count
		if readyInstances > 0 && readyInstances == instances {
			result.Status = types.DatabaseAddonStatusReady
			result.StatusMessage = fmt.Sprintf("Cluster ready (%s)", phase)
			result.Ready = true
		} else {
			result.Status = types.DatabaseAddonStatusProvisioning
			result.StatusMessage = fmt.Sprintf("Provisioning (%s)", phase)
		}
	}

	return result, nil
}

// reconcileRedisAddon checks and updates a Redis addon's status
func (r *AddonReconciler) reconcileRedisAddon(ctx context.Context, addon *types.DatabaseAddon, logger *logrus.Entry) {
	// Redis reconciliation - check StatefulSet or Redis Operator CRD
	// For now, check if the StatefulSet exists and is ready
	statefulSet, err := r.k8sClient.Clientset.AppsV1().StatefulSets(addon.K8sNamespace).Get(
		ctx,
		addon.K8sResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		logger.WithError(err).Warn("Failed to get Redis StatefulSet")
		if addon.Status == types.DatabaseAddonStatusDeleting {
			r.markAddonDeleted(ctx, addon, logger)
		}
		return
	}

	status := &AddonStatusResult{
		Host:         fmt.Sprintf("%s.%s.svc.cluster.local", addon.K8sResourceName, addon.K8sNamespace),
		Port:         6379,
		DatabaseName: "0", // Redis default DB
		Username:     "",
	}

	if statefulSet.Status.ReadyReplicas == *statefulSet.Spec.Replicas {
		status.Status = types.DatabaseAddonStatusReady
		status.StatusMessage = fmt.Sprintf("Redis ready with %d replicas", statefulSet.Status.ReadyReplicas)
		status.Ready = true
	} else {
		status.Status = types.DatabaseAddonStatusProvisioning
		status.StatusMessage = fmt.Sprintf("Redis provisioning: %d/%d replicas ready",
			statefulSet.Status.ReadyReplicas, *statefulSet.Spec.Replicas)
	}

	if r.shouldUpdateAddon(addon, status) {
		r.updateAddonFromStatus(ctx, addon, status, logger)
	}
}

// AddonStatusResult contains the parsed status of an addon
type AddonStatusResult struct {
	Status        types.DatabaseAddonStatus
	StatusMessage string
	Host          string
	Port          int
	DatabaseName  string
	Username      string
	Ready         bool
}

// shouldUpdateAddon checks if the addon should be updated based on status
func (r *AddonReconciler) shouldUpdateAddon(addon *types.DatabaseAddon, status *AddonStatusResult) bool {
	return addon.Status != status.Status ||
		addon.Host != status.Host ||
		addon.Port != status.Port ||
		addon.StatusMessage != status.StatusMessage
}

// updateAddonFromStatus updates the addon in the database
func (r *AddonReconciler) updateAddonFromStatus(ctx context.Context, addon *types.DatabaseAddon, status *AddonStatusResult, logger *logrus.Entry) {
	oldStatus := addon.Status

	addon.Status = status.Status
	addon.StatusMessage = status.StatusMessage
	addon.Host = status.Host
	addon.Port = status.Port
	addon.DatabaseName = status.DatabaseName
	addon.Username = status.Username
	addon.UpdatedAt = time.Now()

	if status.Ready && addon.ProvisionedAt == nil {
		now := time.Now()
		addon.ProvisionedAt = &now
	}

	if err := r.repos.DatabaseAddons.Update(ctx, addon); err != nil {
		logger.WithError(err).Error("Failed to update addon status")
		return
	}

	logger.WithFields(logrus.Fields{
		"old_status": oldStatus,
		"new_status": status.Status,
	}).Info("Addon status updated")
}

// markAddonDeleted marks an addon as deleted after K8s resource is gone
func (r *AddonReconciler) markAddonDeleted(ctx context.Context, addon *types.DatabaseAddon, logger *logrus.Entry) {
	if err := r.repos.DatabaseAddons.SoftDelete(ctx, addon.ID); err != nil {
		logger.WithError(err).Error("Failed to mark addon as deleted")
		return
	}
	logger.Info("Addon marked as deleted")
}

// LogAddonClusterStatus is a helper to log the raw cluster status (for debugging)
func (r *AddonReconciler) LogAddonClusterStatus(ctx context.Context, addon *types.DatabaseAddon) error {
	if addon.Type != types.DatabaseAddonTypePostgres {
		return fmt.Errorf("only PostgreSQL addons supported for cluster status logging")
	}

	if r.dynamicClient == nil {
		return fmt.Errorf("dynamic client not available")
	}

	cluster, err := r.dynamicClient.Resource(cnpgGVR).Namespace(addon.K8sNamespace).Get(
		ctx,
		addon.K8sResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	statusJSON, _ := json.MarshalIndent(cluster.Object["status"], "", "  ")
	r.logger.WithFields(logrus.Fields{
		"addon_id": addon.ID,
		"status":   string(statusJSON),
	}).Debug("CloudNativePG cluster status")

	return nil
}
