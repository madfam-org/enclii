package api

import (
	"context"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/argocd"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ArgoCD Application poller (enclii#324).
//
// enclii's only *live* deploy-tracking channel for GitOps-managed services is
// the ArgoCD Notifications → webhook push to POST /v1/callbacks/argocd-sync
// (argocd_callbacks.go). That channel has once-per-revision suppression and
// depends on each sync operation reaching operationState.phase == Succeeded; a
// service whose Application sits OutOfSync (git drift, healthy pods) stops
// emitting on-sync-succeeded and tracking freezes with no self-heal (the
// `tulana` freeze, ADAPTER_GAPS.md 2026-07-12). The K8s-poll reconciler cannot
// back it up because it is label-gated on enclii.dev/managed-by: switchyard,
// which GitOps manifests don't carry.
//
// The poller closes that gap: it periodically LISTS ArgoCD Application resources
// (read-only) and reconciles release/deployment/activity records directly from
// status.sync.revision + status.summary.images + status.health, independent of
// the notifications webhook. It reuses the webhook's exact record-creation logic
// (Handler.processArgocdSyncRequest) so behavior is identical, and pre-filters
// observations for idempotency so a steady-state Application produces no writes.
//
// It ships dark — main.go only constructs it when ENCLII_ARGOCD_POLLER_ENABLED
// is set. The only writes are enclii DB records; the cluster is never mutated.

const (
	// DefaultArgocdPollInterval is the cadence used when
	// ENCLII_ARGOCD_POLL_INTERVAL is unset or unparseable.
	DefaultArgocdPollInterval = 3 * time.Minute

	// MinArgocdPollInterval floors the configured interval so a typo can't
	// hammer the ArgoCD API server.
	MinArgocdPollInterval = 30 * time.Second
)

// ParseArgocdPollInterval parses the ENCLII_ARGOCD_POLL_INTERVAL value (a Go
// duration such as "2m" or "5m"). Invalid or non-positive values fall back to
// DefaultArgocdPollInterval; values below MinArgocdPollInterval are clamped up.
func ParseArgocdPollInterval(raw string) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d <= 0 {
		return DefaultArgocdPollInterval
	}
	if d < MinArgocdPollInterval {
		return MinArgocdPollInterval
	}
	return d
}

// argoAppLister lists ArgoCD Application resources. Backed by the dynamic
// client in production; faked in tests.
type argoAppLister interface {
	ListApplications(ctx context.Context) ([]unstructured.Unstructured, error)
}

// dynamicArgoAppLister lists Applications through the shared dynamic client,
// read-only (List only — no create/update/delete).
type dynamicArgoAppLister struct {
	client    dynamic.Interface
	namespace string
}

func (l dynamicArgoAppLister) ListApplications(ctx context.Context) ([]unstructured.Unstructured, error) {
	if l.client == nil {
		return nil, nil
	}
	list, err := l.client.Resource(argoApplicationGVR).Namespace(l.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// ArgocdPoller periodically reconciles GitOps deploy tracking from live ArgoCD
// Application state. The repo/apply dependencies are function fields so the
// reconcile path can be unit-tested with fakes instead of a real DB or cluster.
type ArgocdPoller struct {
	lister argoAppLister

	// resolveService maps a container image URI to a registered enclii service
	// (nil when unknown). Backed by Handler.argocdServiceForImage.
	resolveService func(ctx context.Context, imageURI string) *types.Service

	// latestTracked returns the service's most recent deployment and the
	// release it points at (nil when none). Backed by Handler.argocdLatestTracked.
	latestTracked func(ctx context.Context, serviceID string) (*types.Deployment, *types.Release)

	// apply performs the shared, webhook-identical record creation for an
	// observation. Backed by Handler.processArgocdSyncRequest.
	apply func(ctx context.Context, req ArgocdSyncRequest, source string) int

	interval time.Duration
	logger   logging.Logger

	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewArgocdPoller wires an ArgocdPoller against a Handler. The lister reads
// Applications from the given namespace (defaults to the ArgoCD namespace), and
// the record-creation path reuses the exact webhook logic on the Handler.
func NewArgocdPoller(h *Handler, interval time.Duration, namespace string) *ArgocdPoller {
	if strings.TrimSpace(namespace) == "" {
		namespace = argocd.DefaultNamespace
	}
	var client dynamic.Interface
	if h.k8sClient != nil {
		client = h.k8sClient.DynamicClient
	}
	return &ArgocdPoller{
		lister: dynamicArgoAppLister{client: client, namespace: namespace},
		resolveService: func(ctx context.Context, imageURI string) *types.Service {
			svc, _ := h.argocdServiceForImage(ctx, imageURI)
			return svc
		},
		latestTracked: h.argocdLatestTracked,
		apply:         h.processArgocdSyncRequest,
		interval:      interval,
		logger:        h.logger,
		stopCh:        make(chan struct{}),
	}
}

// Start launches the poll loop in a goroutine and returns immediately. The
// first pass runs after one full interval to give the API time to finish boot
// and to avoid a thundering herd against the ArgoCD API when pods restart
// together (mirrors NamespaceDiscoverer).
func (p *ArgocdPoller) Start(ctx context.Context) {
	p.logger.Info(ctx, "Starting ArgoCD Application poller (GitOps deploy-tracking fallback, enclii#324)",
		logging.Duration("interval", p.interval))

	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.reconcile(ctx)
			case <-p.stopCh:
				p.logger.Info(ctx, "ArgoCD Application poller stopped")
				return
			case <-ctx.Done():
				p.logger.Info(ctx, "ArgoCD Application poller context cancelled")
				return
			}
		}
	}()
}

// Stop gracefully shuts down the poller. Safe to call multiple times.
func (p *ArgocdPoller) Stop() {
	p.stopOnce.Do(func() { close(p.stopCh) })
}

// reconcile lists every Application and reconciles each one. Read-only against
// the cluster; the only writes are DB records via the shared apply path.
func (p *ArgocdPoller) reconcile(ctx context.Context) {
	apps, err := p.lister.ListApplications(ctx)
	if err != nil {
		p.logger.Warn(ctx, "ArgoCD poller: failed to list Applications",
			logging.Error("error", err))
		return
	}

	records := 0
	for i := range apps {
		records += p.reconcileApplication(ctx, apps[i])
	}
	if records > 0 {
		p.logger.Info(ctx, "ArgoCD poller reconciled GitOps deploy records",
			logging.Int("records", records),
			logging.Int("applications", len(apps)))
	}
}

// reconcileApplication turns one Application's live status into tracking records
// for the images that changed since they were last tracked. Returns the number
// of deployment records created/updated (0 when nothing changed).
func (p *ArgocdPoller) reconcileApplication(ctx context.Context, app unstructured.Unstructured) int {
	obs, ok := argocdObservationFromApplication(app)
	if !ok {
		return 0
	}

	resolveService := func(imageURI string) *types.Service { return p.resolveService(ctx, imageURI) }
	latestTracked := func(serviceID string) (*types.Deployment, *types.Release) {
		return p.latestTracked(ctx, serviceID)
	}

	changed := argocdPollDecision(obs, resolveService, latestTracked)
	if len(changed) == 0 {
		return 0
	}

	p.logger.Info(ctx, "ArgoCD poller detected untracked GitOps deploy",
		logging.String("app_name", obs.AppName),
		logging.String("revision", obs.Revision),
		logging.String("health_status", obs.HealthStatus),
		logging.Int("changed_images", len(changed)))

	req := ArgocdSyncRequest{
		AppName:      obs.AppName,
		Trigger:      obs.Trigger,
		SyncStatus:   obs.SyncStatus,
		HealthStatus: obs.HealthStatus,
		Revision:     obs.Revision,
		Images:       changed,
	}
	return p.apply(ctx, req, argocdSyncSourcePoller)
}

// argocdObservation is the projection of an ArgoCD Application's live status
// that the poller needs. It is derived purely from the Application object.
type argocdObservation struct {
	AppName      string
	Revision     string
	Images       []string
	SyncStatus   string
	HealthStatus string
	Trigger      string // mapped to the webhook's trigger vocabulary
}

// argocdObservationFromApplication extracts a poll observation from a live
// Application. It returns ok=false (skip this tick) unless the Application is in
// a settled, terminal health state with a concrete revision and image set:
//
//   - Healthy           → trigger "sync-succeeded" (record a running deploy)
//   - Degraded, Missing → trigger "sync-failed"    (record a failed deploy)
//   - Progressing / Suspended / Unknown / empty → skip (transient; re-check next
//     tick so we never record a half-rolled-out revision or flap)
func argocdObservationFromApplication(app unstructured.Unstructured) (argocdObservation, bool) {
	obs := argocdObservation{AppName: app.GetName()}
	obs.SyncStatus, _, _ = unstructured.NestedString(app.Object, "status", "sync", "status")
	obs.HealthStatus, _, _ = unstructured.NestedString(app.Object, "status", "health", "status")
	obs.Revision, _, _ = unstructured.NestedString(app.Object, "status", "sync", "revision")
	obs.Images, _, _ = unstructured.NestedStringSlice(app.Object, "status", "summary", "images")

	switch obs.HealthStatus {
	case "Healthy":
		obs.Trigger = "sync-succeeded"
	case "Degraded", "Missing":
		obs.Trigger = "sync-failed"
	default:
		return obs, false
	}

	if obs.Revision == "" || len(obs.Images) == 0 {
		return obs, false
	}
	return obs, true
}

// argocdPollDecision returns the subset of an observation's images that are NOT
// yet tracked for their service and therefore must be fed to the shared
// record-creation path. It is pure: service resolution and tracking lookups are
// injected, so it is fully unit-testable without a DB or cluster.
//
// Dedup key: (service, git revision). Unknown images and images whose service
// already tracks the observed revision are dropped, which keeps the poller
// idempotent — polling a steady-state Application yields an empty slice and
// hence no writes.
func argocdPollDecision(
	obs argocdObservation,
	resolveService func(imageURI string) *types.Service,
	latestTracked func(serviceID string) (*types.Deployment, *types.Release),
) []string {
	if obs.Revision == "" || len(obs.Images) == 0 {
		return nil
	}

	var changed []string
	seen := make(map[string]struct{}, len(obs.Images))
	for _, imageURI := range obs.Images {
		if _, dup := seen[imageURI]; dup {
			continue
		}
		seen[imageURI] = struct{}{}

		service := resolveService(imageURI)
		if service == nil {
			continue // unknown app/image → skip
		}

		dep, rel := latestTracked(service.ID.String())
		if argocdObservationAlreadyTracked(dep, rel, obs.Revision, imageURI) {
			continue // already recorded → idempotent no-op
		}
		changed = append(changed, imageURI)
	}
	return changed
}

// argocdObservationAlreadyTracked reports whether a service's latest tracked
// deployment already reflects the observed ArgoCD revision/image.
//
// A GitOps revision deterministically pins the image digest, so a matching git
// revision means the running image is already recorded — the authoritative
// dedup key is (service, git revision). When a revision is unavailable on
// either side (legacy releases with an empty git_sha), it falls back to
// image-digest identity. Keying on revision (never re-deriving "changed" from a
// reused release's stale image field) guarantees the poller cannot churn
// duplicate records for a steady-state Application.
func argocdObservationAlreadyTracked(dep *types.Deployment, rel *types.Release, revision, imageURI string) bool {
	if dep == nil || rel == nil {
		return false
	}
	if rel.GitSHA != "" && revision != "" {
		return rel.GitSHA == revision
	}
	// Revision unavailable — fall back to image-digest identity when both sides
	// carry a digest.
	storedDigest := imageDigest(rel.ImageURI)
	observedDigest := imageDigest(imageURI)
	return storedDigest != "" && observedDigest != "" && storedDigest == observedDigest
}

// imageDigest returns the "sha256:…" digest portion of a digest-pinned image
// URI (the part after "@"), or "" when the image is tag-based only.
func imageDigest(imageURI string) string {
	if idx := strings.LastIndex(imageURI, "@"); idx != -1 {
		return imageURI[idx+1:]
	}
	return ""
}
