package reconciler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// labelEncliiService is the canonical label that tags Enclii-managed
// workloads with their service name. Verified by inspection of
// internal/reconciler/manifest.go (line 219, key "enclii.dev/service")
// — this is the same label the deployment reconciler writes when it
// builds the Deployment manifest.
//
// We deliberately do NOT use enclii.dev/service-id (which doesn't exist
// in the codebase). The discoverer joins by service NAME because that
// is what manifest.go writes.
const labelEncliiService = "enclii.dev/service"

const (
	defaultNamespaceDiscoveryInterval = 5 * time.Minute

	// orphanRetention is how long to keep an orphan row after its
	// last_seen update before reaping. 24h matches the spec; if a
	// workload has not been re-observed in 24h it has been deleted
	// from the cluster.
	orphanRetention = 24 * time.Hour

	envDiscoveryInterval = "RECONCILER_NAMESPACE_DISCOVERY_INTERVAL"
)

// kindDeployment / kindStatefulSet are the only kinds tracked by the
// namespace discoverer. Jobs and CronJobs are owned by the Timetable
// reconciler and are not included.
const (
	kindDeployment  = "Deployment"
	kindStatefulSet = "StatefulSet"
)

// NamespaceDiscoverer is a read-only reconciler that compares the live K8s
// cluster state (Deployments + StatefulSets) against the services table.
// It runs every RECONCILER_NAMESPACE_DISCOVERY_INTERVAL (default 5m) and
// classifies each workload as:
//
//   - Healthy: K8s workload + matching service row → update replica counts
//     and last_reconciled_at on the service.
//   - Orphan : K8s workload, no matching service row → upsert into
//     discovered_orphans with last_seen=NOW().
//   - Zombie : service row with k8s_namespace pinned, no matching K8s
//     workload → set services.zombie_since=NOW() if currently NULL.
//
// The reconciler is strictly read-only against the cluster (no mutations).
// It is idempotent: running two passes in immediate succession is a no-op
// after the first.
type NamespaceDiscoverer struct {
	repos     *db.Repositories
	k8sClient *k8s.Client
	logger    *logrus.Logger

	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once

	// State for change-detection logging: we log when a service flips
	// between healthy/zombie or when an orphan first appears, but stay
	// quiet on every subsequent tick if nothing changed. Keys are
	// stable identifiers (service UUID, "ns/name/kind" for orphans).
	mu              sync.Mutex
	lastZombieState map[uuid.UUID]bool
	knownOrphanKeys map[string]struct{}
}

// NewNamespaceDiscoverer constructs a discoverer with the configured interval.
// If RECONCILER_NAMESPACE_DISCOVERY_INTERVAL is set and parses as a Go
// duration (e.g. "5m", "30s"), it overrides the 5-minute default.
func NewNamespaceDiscoverer(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *NamespaceDiscoverer {
	interval := defaultNamespaceDiscoveryInterval
	if v := os.Getenv(envDiscoveryInterval); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		} else {
			logger.WithFields(logrus.Fields{
				"env":   envDiscoveryInterval,
				"value": v,
			}).Warn("NamespaceDiscoverer: invalid duration, using default 5m")
		}
	}

	return &NamespaceDiscoverer{
		repos:           repos,
		k8sClient:       k8sClient,
		logger:          logger,
		interval:        interval,
		stopCh:          make(chan struct{}),
		lastZombieState: make(map[uuid.UUID]bool),
		knownOrphanKeys: make(map[string]struct{}),
	}
}

// Start launches the reconcile loop in a goroutine. Returns immediately.
// The first pass runs after one full interval (not at startup) to give the
// API time to finish boot and avoid a thundering-herd against the K8s API
// server when several pods restart simultaneously.
func (d *NamespaceDiscoverer) Start(ctx context.Context) {
	d.logger.WithField("interval", d.interval).Info("Starting namespace discoverer")

	go func() {
		ticker := time.NewTicker(d.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				d.reconcile(ctx)
			case <-d.stopCh:
				d.logger.Info("Namespace discoverer stopped")
				return
			case <-ctx.Done():
				d.logger.Info("Namespace discoverer context cancelled")
				return
			}
		}
	}()
}

// Stop gracefully shuts down the discoverer. Safe to call multiple times.
func (d *NamespaceDiscoverer) Stop() {
	d.stopOnce.Do(func() { close(d.stopCh) })
}

// ----------------------------------------------------------------------
// Core reconciliation
// ----------------------------------------------------------------------

// workloadRef is the minimal projection of a K8s workload that the
// discoverer cares about. Decoupling from appsv1 types lets us mock the
// lister cleanly in tests.
type workloadRef struct {
	Namespace       string
	Name            string
	Kind            string
	ServiceLabel    string // value of enclii.dev/service label, may be ""
	Image           string
	ReplicasDesired int32
	ReplicasReady   int32
}

// workloadLister abstracts the K8s API away from the comparison logic so
// tests can supply a fake without standing up envtest. Implementations
// must NOT mutate cluster state.
type workloadLister interface {
	ListWorkloads(ctx context.Context) ([]workloadRef, error)
}

// servicesView is the read+write subset of the services repository the
// discoverer needs. Defining it as an interface lets tests inject a
// fake that does not require sqlmock setup.
// *db.ServiceRepository satisfies this interface.
type servicesView interface {
	ListAll(ctx context.Context) ([]*types.Service, error)
	MarkReconciledHealthy(ctx context.Context, id uuid.UUID, desiredReplicas, readyReplicas int32) error
	MarkReconciledZombie(ctx context.Context, id uuid.UUID) error
}

// orphansView is the write subset of the discovered_orphans repository.
// *db.DiscoveredOrphanRepository satisfies this interface.
type orphansView interface {
	Upsert(ctx context.Context, o *db.DiscoveredOrphan) error
	DeleteStale(ctx context.Context, cutoff time.Time) (int64, error)
}

// reconcile runs a single classification pass. Errors are logged, never
// returned — the loop must continue ticking even if a single pass fails
// (e.g. transient K8s API server flake).
func (d *NamespaceDiscoverer) reconcile(ctx context.Context) {
	if d.repos == nil || d.repos.DiscoveredOrphans == nil || d.repos.Services == nil {
		d.logger.Warn("NamespaceDiscoverer: repositories not configured, skipping pass")
		return
	}

	if d.k8sClient == nil || !d.k8sClient.IsValid() {
		d.logger.Debug("NamespaceDiscoverer: K8s client not available, skipping pass")
		return
	}

	d.reconcileWith(ctx, &realWorkloadLister{client: d.k8sClient}, d.repos.Services, d.repos.DiscoveredOrphans)
}

// reconcileWith is the testable core: it accepts an injected lister, services
// view, and orphans view so unit tests can drive deterministic scenarios
// without a live cluster or database.
func (d *NamespaceDiscoverer) reconcileWith(ctx context.Context, lister workloadLister, svcs servicesView, orphans orphansView) {
	workloads, err := lister.ListWorkloads(ctx)
	if err != nil {
		d.logger.WithError(err).Error("NamespaceDiscoverer: failed to list cluster workloads")
		return
	}

	services, err := svcs.ListAll(ctx)
	if err != nil {
		d.logger.WithError(err).Error("NamespaceDiscoverer: failed to list services from DB")
		return
	}

	// Build name → service index for O(1) lookup. Index by service.Name
	// because that is what manifest.go writes into the enclii.dev/service
	// label. If multiple services share a name (shouldn't happen — the
	// services table has a unique constraint per project, but names can
	// recur across projects), we prefer the one whose k8s_namespace
	// matches the workload's namespace.
	byName := make(map[string][]*types.Service, len(services))
	for _, svc := range services {
		byName[svc.Name] = append(byName[svc.Name], svc)
	}

	matchedServiceIDs := make(map[uuid.UUID]struct{}, len(services))
	currentOrphanKeys := make(map[string]struct{}, len(workloads))

	for _, wl := range workloads {
		// An Enclii-managed workload must carry the enclii.dev/service
		// label. Workloads without it (system DaemonSets, ArgoCD, etc.)
		// are intentionally ignored — they are not in scope for the
		// services table.
		if wl.ServiceLabel == "" {
			continue
		}

		svc := pickServiceForWorkload(byName[wl.ServiceLabel], wl.Namespace)
		if svc != nil {
			matchedServiceIDs[svc.ID] = struct{}{}
			if err := svcs.MarkReconciledHealthy(ctx, svc.ID, wl.ReplicasDesired, wl.ReplicasReady); err != nil {
				d.logger.WithError(err).WithFields(logrus.Fields{
					"service_id": svc.ID,
					"namespace":  wl.Namespace,
					"name":       wl.Name,
				}).Warn("NamespaceDiscoverer: failed to mark service healthy")
			}
			d.observeZombieFlip(svc.ID, false, svc.Name, wl.Namespace)
			continue
		}

		// No matching service → orphan.
		orphan := &db.DiscoveredOrphan{
			Namespace:       wl.Namespace,
			Name:            wl.Name,
			Kind:            wl.Kind,
			Image:           wl.Image,
			ReplicasDesired: wl.ReplicasDesired,
			ReplicasReady:   wl.ReplicasReady,
		}
		if err := orphans.Upsert(ctx, orphan); err != nil {
			d.logger.WithError(err).WithFields(logrus.Fields{
				"namespace": wl.Namespace,
				"name":      wl.Name,
				"kind":      wl.Kind,
			}).Warn("NamespaceDiscoverer: failed to upsert orphan")
			continue
		}

		key := orphanKey(wl.Namespace, wl.Name, wl.Kind)
		currentOrphanKeys[key] = struct{}{}
		d.observeOrphan(key, wl)
	}

	// Zombies: services with k8s_namespace pinned that did NOT match a
	// workload this pass. We deliberately scope to services whose
	// k8s_namespace is non-null — services that were never deployed
	// (no namespace pinned) are not zombies, just unreleased.
	for _, svc := range services {
		if _, matched := matchedServiceIDs[svc.ID]; matched {
			continue
		}
		if svc.K8sNamespace == nil || *svc.K8sNamespace == "" {
			continue
		}
		if err := svcs.MarkReconciledZombie(ctx, svc.ID); err != nil {
			d.logger.WithError(err).WithField("service_id", svc.ID).Warn("NamespaceDiscoverer: failed to mark zombie")
			continue
		}
		d.observeZombieFlip(svc.ID, true, svc.Name, *svc.K8sNamespace)
	}

	// Reap orphans whose last_seen is older than the retention window —
	// the workload is gone from cluster.
	cutoff := time.Now().Add(-orphanRetention)
	if reaped, err := orphans.DeleteStale(ctx, cutoff); err != nil {
		d.logger.WithError(err).Warn("NamespaceDiscoverer: failed to reap stale orphans")
	} else if reaped > 0 {
		d.logger.WithField("reaped", reaped).Info("NamespaceDiscoverer: reaped stale orphan rows")
	}

	// Update the in-memory orphan-known set for change detection. This
	// is only used to decide whether to log on the next pass; it is not
	// the source of truth (the DB is).
	d.mu.Lock()
	d.knownOrphanKeys = currentOrphanKeys
	d.mu.Unlock()
}

// pickServiceForWorkload selects the best service candidate for a workload.
// When multiple services share the same name across projects, prefer the
// one whose k8s_namespace matches the workload's namespace; otherwise fall
// back to the first one (deterministic by query order).
func pickServiceForWorkload(candidates []*types.Service, namespace string) *types.Service {
	if len(candidates) == 0 {
		return nil
	}
	for _, c := range candidates {
		if c.K8sNamespace != nil && *c.K8sNamespace == namespace {
			return c
		}
	}
	return candidates[0]
}

// orphanKey builds the stable identifier for change-detection logging.
func orphanKey(namespace, name, kind string) string {
	return fmt.Sprintf("%s/%s/%s", namespace, name, kind)
}

// observeOrphan emits an INFO log only when an orphan is newly seen this
// session. Re-observations stay silent.
func (d *NamespaceDiscoverer) observeOrphan(key string, wl workloadRef) {
	d.mu.Lock()
	_, seen := d.knownOrphanKeys[key]
	d.mu.Unlock()
	if seen {
		return
	}
	d.logger.WithFields(logrus.Fields{
		"namespace": wl.Namespace,
		"name":      wl.Name,
		"kind":      wl.Kind,
		"image":     wl.Image,
	}).Info("NamespaceDiscoverer: discovered new orphan workload (in cluster, not in DB)")
}

// observeZombieFlip emits an INFO log only when a service flips between
// zombie and healthy states. Steady-state passes stay silent.
func (d *NamespaceDiscoverer) observeZombieFlip(svcID uuid.UUID, nowZombie bool, svcName, namespace string) {
	d.mu.Lock()
	prev, hadState := d.lastZombieState[svcID]
	d.lastZombieState[svcID] = nowZombie
	d.mu.Unlock()

	if hadState && prev == nowZombie {
		return // no flip, stay quiet
	}

	if nowZombie {
		d.logger.WithFields(logrus.Fields{
			"service_id":   svcID,
			"service_name": svcName,
			"namespace":    namespace,
		}).Info("NamespaceDiscoverer: service became zombie (DB record, no live workload)")
	} else if hadState {
		// Only log "recovered" when we'd previously seen it as zombie.
		d.logger.WithFields(logrus.Fields{
			"service_id":   svcID,
			"service_name": svcName,
			"namespace":    namespace,
		}).Info("NamespaceDiscoverer: zombie service recovered (workload reappeared)")
	}
}

// ----------------------------------------------------------------------
// Real K8s lister (production path)
// ----------------------------------------------------------------------

// realWorkloadLister queries the K8s API server cluster-wide. It does NOT
// mutate any cluster state — only metav1.ListOptions{} reads.
type realWorkloadLister struct {
	client *k8s.Client
}

func (l *realWorkloadLister) ListWorkloads(ctx context.Context) ([]workloadRef, error) {
	if l.client == nil || l.client.Clientset == nil {
		return nil, errors.New("k8s client not initialized")
	}

	out := make([]workloadRef, 0, 64)

	deps, err := l.client.Clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	for i := range deps.Items {
		out = append(out, deploymentToRef(&deps.Items[i]))
	}

	sts, err := l.client.Clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}
	for i := range sts.Items {
		out = append(out, statefulSetToRef(&sts.Items[i]))
	}

	return out, nil
}

func deploymentToRef(d *appsv1.Deployment) workloadRef {
	desired := int32(0)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return workloadRef{
		Namespace:       d.Namespace,
		Name:            d.Name,
		Kind:            kindDeployment,
		ServiceLabel:    d.Labels[labelEncliiService],
		Image:           firstContainerImage(d.Spec.Template.Spec.Containers),
		ReplicasDesired: desired,
		ReplicasReady:   d.Status.ReadyReplicas,
	}
}

func statefulSetToRef(s *appsv1.StatefulSet) workloadRef {
	desired := int32(0)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	return workloadRef{
		Namespace:       s.Namespace,
		Name:            s.Name,
		Kind:            kindStatefulSet,
		ServiceLabel:    s.Labels[labelEncliiService],
		Image:           firstContainerImage(s.Spec.Template.Spec.Containers),
		ReplicasDesired: desired,
		ReplicasReady:   s.Status.ReadyReplicas,
	}
}

// firstContainerImage returns the image of the first container in a pod
// spec, or "" if no containers are defined. We deliberately do NOT
// concatenate every image — multi-container pods would explode the
// orphan row width and the operator only needs the primary image for
// rollback decisions.
func firstContainerImage(containers []corev1.Container) string {
	if len(containers) == 0 {
		return ""
	}
	return containers[0].Image
}
