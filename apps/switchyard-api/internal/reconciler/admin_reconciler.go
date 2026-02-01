package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// AdminReconciler syncs admin control plane tables with live K8s state.
// Runs every 60s to update clusters, fleet (bare metal hosts), ArgoCD drift events, and cost allocations.
type AdminReconciler struct {
	repos     *db.Repositories
	k8sClient *k8s.Client
	logger    *logrus.Logger
	stopCh    chan struct{}
}

// ArgoCD Application GVR
var argoCDAppGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

// NewAdminReconciler creates a new admin reconciler
func NewAdminReconciler(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *AdminReconciler {
	return &AdminReconciler{
		repos:     repos,
		k8sClient: k8sClient,
		logger:    logger,
		stopCh:    make(chan struct{}),
	}
}

// Start begins the admin reconciliation loop
func (r *AdminReconciler) Start(ctx context.Context) {
	r.logger.Info("Starting admin reconciler")

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Initial reconciliation
	r.reconcileAll(ctx)

	for {
		select {
		case <-ticker.C:
			r.reconcileAll(ctx)
		case <-r.stopCh:
			r.logger.Info("Admin reconciler stopped")
			return
		case <-ctx.Done():
			r.logger.Info("Admin reconciler context cancelled")
			return
		}
	}
}

// Stop gracefully shuts down the reconciler
func (r *AdminReconciler) Stop() {
	close(r.stopCh)
}

// reconcileAll runs all admin sync methods sequentially
func (r *AdminReconciler) reconcileAll(ctx context.Context) {
	start := time.Now()
	r.syncClusterStatus(ctx)
	r.syncFleetStatus(ctx)
	r.syncArgoCDDrift(ctx)
	r.calculateCosts(ctx)
	r.logger.WithField("duration", time.Since(start)).Debug("Admin reconciler: cycle complete")
}

// syncClusterStatus syncs K8s node status into the clusters table
func (r *AdminReconciler) syncClusterStatus(ctx context.Context) {
	clusters, err := r.repos.Clusters.List(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Admin reconciler: failed to list clusters")
		return
	}
	if len(clusters) == 0 {
		return
	}

	nodes, err := r.k8sClient.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		r.logger.WithError(err).Error("Admin reconciler: failed to list K8s nodes")
		return
	}

	// Count ready vs total nodes
	totalNodes := len(nodes.Items)
	readyNodes := 0
	var k8sVersion string
	for _, node := range nodes.Items {
		if k8sVersion == "" {
			k8sVersion = node.Status.NodeInfo.KubeletVersion
		}
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				readyNodes++
				break
			}
		}
	}

	// Derive cluster status
	var status types.ClusterStatus
	switch {
	case totalNodes == 0 || readyNodes == 0:
		status = types.ClusterStatusOffline
	case readyNodes < totalNodes:
		status = types.ClusterStatusDegraded
	default:
		status = types.ClusterStatusReady
	}

	// Get cluster metrics (best-effort)
	var totalCPU, totalMemory int64
	var totalPods int
	metrics, metricsErr := r.k8sClient.GetClusterMetrics(ctx)
	if metricsErr != nil {
		r.logger.WithError(metricsErr).Warn("Admin reconciler: cluster metrics unavailable")
	} else if metrics.MetricsEnabled {
		totalCPU = metrics.TotalCPU
		totalMemory = metrics.TotalMemory
		totalPods = metrics.TotalPods
	}

	// Build metadata
	metadata := map[string]interface{}{
		"node_count":     totalNodes,
		"ready_nodes":    readyNodes,
		"k8s_version":    k8sVersion,
		"cpu_millicores": totalCPU,
		"memory_bytes":   totalMemory,
		"pod_count":      totalPods,
		"synced_at":      time.Now().UTC().Format(time.RFC3339),
	}
	metadataJSON, _ := json.Marshal(metadata)

	// Update all registered clusters with this info
	for _, cluster := range clusters {
		cluster.Status = status
		cluster.Metadata = metadataJSON
		if err := r.repos.Clusters.Update(ctx, cluster); err != nil {
			r.logger.WithError(err).WithField("cluster_id", cluster.ID).Error("Admin reconciler: failed to update cluster")
		}
	}

	r.logger.WithFields(logrus.Fields{
		"status":       status,
		"ready_nodes":  readyNodes,
		"total_nodes":  totalNodes,
		"cpu_milli":    totalCPU,
		"memory_bytes": totalMemory,
		"pod_count":    totalPods,
	}).Info("Admin reconciler: cluster status synced")
}

// syncFleetStatus syncs K8s node info into the bare_metal_hosts table
func (r *AdminReconciler) syncFleetStatus(ctx context.Context) {
	hosts, err := r.repos.BareMetalHosts.List(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Admin reconciler: failed to list bare metal hosts")
		return
	}
	if len(hosts) == 0 {
		return
	}

	nodes, err := r.k8sClient.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		r.logger.WithError(err).Error("Admin reconciler: failed to list K8s nodes")
		return
	}

	// Index nodes by name
	nodeMap := make(map[string]corev1.Node, len(nodes.Items))
	for _, node := range nodes.Items {
		nodeMap[node.Name] = node
	}

	for _, host := range hosts {
		node, found := nodeMap[host.Name]
		if !found {
			// Try case-insensitive match
			for nodeName, n := range nodeMap {
				if strings.EqualFold(nodeName, host.Name) {
					node = n
					found = true
					break
				}
			}
		}
		if !found {
			r.logger.WithFields(logrus.Fields{
				"host":            host.Name,
				"available_nodes": nodeNames(nodeMap),
			}).Warn("Admin reconciler: no matching K8s node for host")
			// No matching K8s node — mark power unknown if currently on
			if host.PowerState == types.BMHPowerOn {
				if err := r.repos.BareMetalHosts.UpdateState(ctx, host.ID, host.State, types.BMHPowerUnknown); err != nil {
					r.logger.WithError(err).WithField("host_id", host.ID).Warn("Admin reconciler: failed to update BMH power state")
				}
			}
			continue
		}

		// Derive power state from Node Ready condition
		powerState := types.BMHPowerOff
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				powerState = types.BMHPowerOn
				break
			}
		}

		// Transition state: discovered → provisioned when node is healthy
		state := host.State
		if powerState == types.BMHPowerOn && (state == types.BMHStateDiscovered || state == types.BMHStateAvailable) {
			state = types.BMHStateProvisioned
		}

		if state != host.State || powerState != host.PowerState {
			if err := r.repos.BareMetalHosts.UpdateState(ctx, host.ID, state, powerState); err != nil {
				r.logger.WithError(err).WithField("host_id", host.ID).Warn("Admin reconciler: failed to update BMH state")
			}
		}

		// Enrich hardware profile from node info
		capacity := node.Status.Capacity
		nodeInfo := node.Status.NodeInfo
		profile := map[string]interface{}{
			"cpu_cores":         capacity.Cpu().Value(),
			"memory_bytes":      capacity.Memory().Value(),
			"os":                nodeInfo.OSImage,
			"arch":              nodeInfo.Architecture,
			"kernel":            nodeInfo.KernelVersion,
			"container_runtime": nodeInfo.ContainerRuntimeVersion,
			"kubelet_version":   nodeInfo.KubeletVersion,
			"synced_at":         time.Now().UTC().Format(time.RFC3339),
		}
		profileJSON, _ := json.Marshal(profile)
		if err := r.repos.BareMetalHosts.UpdateHardwareProfile(ctx, host.ID, profileJSON); err != nil {
			r.logger.WithError(err).WithField("host_id", host.ID).Warn("Admin reconciler: failed to update hardware profile")
		}
	}

	r.logger.WithField("host_count", len(hosts)).Debug("Admin reconciler: fleet status synced")
}

// syncArgoCDDrift syncs ArgoCD Application CRDs into the drift_events table
func (r *AdminReconciler) syncArgoCDDrift(ctx context.Context) {
	appList, err := r.k8sClient.DynamicClient.Resource(argoCDAppGVR).Namespace("argocd").List(ctx, metav1.ListOptions{})
	if err != nil {
		// ArgoCD CRD may not exist — not an error
		r.logger.WithError(err).Debug("Admin reconciler: ArgoCD apps not available (CRD may not be installed)")
		return
	}

	// Get unresolved drift events
	resolved := false
	existingDrifts, err := r.repos.DriftEvents.List(ctx, &resolved)
	if err != nil {
		r.logger.WithError(err).Error("Admin reconciler: failed to list drift events")
		return
	}

	// Index existing drifts by resource name
	driftByResource := make(map[string]*types.DriftEvent, len(existingDrifts))
	for _, de := range existingDrifts {
		if de.Source == types.DriftSourceArgoCD {
			driftByResource[de.ResourceName] = de
		}
	}

	// Track which apps we've seen (for auto-resolving deleted apps)
	seenApps := make(map[string]bool)

	for _, item := range appList.Items {
		appName := item.GetName()
		seenApps[appName] = true

		syncStatus, _, _ := unstructured.NestedString(item.Object, "status", "sync", "status")
		healthStatus, _, _ := unstructured.NestedString(item.Object, "status", "health", "status")

		if syncStatus == "OutOfSync" || healthStatus == "Degraded" {
			// Create drift event if none exists for this app
			if _, exists := driftByResource[appName]; !exists {
				severity := types.DriftSeverityMedium
				if healthStatus == "Degraded" {
					severity = types.DriftSeverityHigh
				}
				details := map[string]string{
					"sync_status":   syncStatus,
					"health_status": healthStatus,
				}
				detailsJSON, _ := json.Marshal(details)

				de := &types.DriftEvent{
					Source:       types.DriftSourceArgoCD,
					ResourceType: "Application",
					ResourceName: appName,
					DriftDetails: detailsJSON,
					Severity:     severity,
				}
				if err := r.repos.DriftEvents.Create(ctx, de); err != nil {
					r.logger.WithError(err).WithField("app", appName).Warn("Admin reconciler: failed to create drift event")
				} else {
					r.logger.WithField("app", appName).Info("Admin reconciler: drift detected for ArgoCD app")
				}
			}
		} else if syncStatus == "Synced" {
			// Auto-resolve existing drift for this app
			if de, exists := driftByResource[appName]; exists {
				if err := r.repos.DriftEvents.Resolve(ctx, de.ID); err != nil {
					r.logger.WithError(err).WithField("app", appName).Warn("Admin reconciler: failed to resolve drift event")
				} else {
					r.logger.WithField("app", appName).Debug("Admin reconciler: drift auto-resolved for ArgoCD app")
				}
			}
		}
	}

	// Auto-resolve drift events for deleted apps
	for resourceName, de := range driftByResource {
		if !seenApps[resourceName] {
			if err := r.repos.DriftEvents.Resolve(ctx, de.ID); err != nil {
				r.logger.WithError(err).WithField("app", resourceName).Warn("Admin reconciler: failed to resolve drift for deleted app")
			}
		}
	}
}

// calculateCosts calculates current month costs from BMH hourly rates
func (r *AdminReconciler) calculateCosts(ctx context.Context) {
	hosts, err := r.repos.BareMetalHosts.List(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Admin reconciler: failed to list hosts for cost calculation")
		return
	}

	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	for _, host := range hosts {
		if host.CostPerHourCents <= 0 {
			continue
		}

		// Calculate elapsed hours this month
		elapsed := now.Sub(periodStart)
		elapsedHours := elapsed.Hours()
		costCents := int(elapsedHours * float64(host.CostPerHourCents))

		existing, err := r.repos.CostAllocations.ListByHost(ctx, host.ID, periodStart, periodEnd)
		if err != nil {
			r.logger.WithError(err).WithField("host_id", host.ID).Warn("Admin reconciler: failed to list cost allocations")
			continue
		}

		if len(existing) > 0 {
			// Update existing allocation
			if err := r.repos.CostAllocations.UpdateCostCents(ctx, existing[0].ID, costCents); err != nil {
				r.logger.WithError(err).WithField("host_id", host.ID).Warn("Admin reconciler: failed to update cost allocation")
			}
		} else {
			// Create new allocation for this month
			ca := &types.CostAllocation{
				BareMetalHostID:   host.ID,
				TenantID:          "platform", // Platform-level cost
				AllocationPercent: 100.0,
				PeriodStart:       periodStart,
				PeriodEnd:         periodEnd,
				CostCents:         costCents,
			}
			if err := r.repos.CostAllocations.Create(ctx, ca); err != nil {
				r.logger.WithError(err).WithField("host_id", host.ID).Warn("Admin reconciler: failed to create cost allocation")
			} else {
				r.logger.WithFields(logrus.Fields{
					"host_id":    host.ID,
					"cost_cents": costCents,
					"period":     fmt.Sprintf("%s to %s", periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02")),
				}).Debug("Admin reconciler: created cost allocation")
			}
		}
	}
}

// nodeNames extracts node names from a map for diagnostic logging
func nodeNames(m map[string]corev1.Node) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	return names
}
