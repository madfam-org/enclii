package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// usageCacheTTL is how long the /v1/usage response stays cached per
// process. The dashboard's UsageOverview component does not poll on its
// own (only on full refresh), so a 60s window is generous and
// dramatically reduces the cost of the inevitable refresh-storm when an
// operator opens 5 tabs.
const usageCacheTTL = 60 * time.Second

// usageHandlerBudget caps server-side time spent computing usage so a
// slow K8s metrics-server can't run past Cloudflare's 100s edge timeout.
// Same rationale as healthHandlerBudget.
const usageHandlerBudget = 25 * time.Second

// usageFanoutConcurrency caps concurrent in-flight DB queries (Releases
// per service) and K8s metrics calls during usage computation.
const usageFanoutConcurrency = 15

type usageCacheEntry struct {
	resp      UsageSummary
	expiresAt time.Time
}

var (
	usageCacheMu sync.Mutex
	usageCache   *usageCacheEntry
)

// UsageMetric represents a single usage metric
type UsageMetric struct {
	Type     string  `json:"type"`
	Label    string  `json:"label"`
	Used     float64 `json:"used"`
	Included float64 `json:"included"` // -1 for unlimited
	Unit     string  `json:"unit"`
	Cost     float64 `json:"cost"`

	// Unavailable reports that the meter behind this number could not be
	// read. `Used` is then 0 and MEANS NOTHING — it is not a claim that no
	// usage occurred. Omitted from the JSON when false, so the shape is
	// unchanged for every metric that is actually measured.
	//
	// This field exists because the alternative is worse in a specific way:
	// a failed read that reports 0.0 is indistinguishable from a quiet month
	// on every dashboard and in every invoice review, and it is the one wrong
	// answer nobody goes looking for.
	Unavailable bool `json:"unavailable,omitempty"`
	// Note explains an Unavailable reading in one non-sensitive line.
	Note string `json:"note,omitempty"`
	// Source names where the number came from, for the metrics that read a
	// meter rather than counting rows locally.
	Source string `json:"source,omitempty"`
}

// UsageSummary represents infrastructure usage metering data.
// Customer billing is handled by Dhanam.
type UsageSummary struct {
	PeriodStart string        `json:"period_start"`
	PeriodEnd   string        `json:"period_end"`
	Metrics     []UsageMetric `json:"metrics"`
	// TotalCost is always 0 and Priced always false: Enclii has no price list
	// to multiply usage by (see usagePricingNote). Kept for response shape.
	TotalCost   float64 `json:"total_cost"`
	Priced      bool    `json:"priced"`
	PricingNote string  `json:"pricing_note,omitempty"`
}

// CostCategory represents a cost breakdown category
type CostCategory struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Color string  `json:"color"`
}

// CostBreakdown represents infrastructure cost breakdown.
// Customer billing is handled by Dhanam.
type CostBreakdown struct {
	PeriodStart string         `json:"period_start"`
	PeriodEnd   string         `json:"period_end"`
	Categories  []CostCategory `json:"categories"`
	TotalUsage  float64        `json:"total_usage"`
	Priced      bool           `json:"priced"`
	PricingNote string         `json:"pricing_note,omitempty"`
}

// THERE IS NO PRICE LIST AND NO PLAN ALLOWANCE HERE (RFC 0049 Phase 1).
//
// This file used to multiply usage by $0.05/GB-hour, $0.01/minute,
// $0.25/GB and $0.10/GB over "included" allowances of 500 GB-hours, 500
// minutes, 10 GB and 500 GB. None of those numbers came from a contract, an
// invoice or a plan: they were literals, and the compute and storage figures
// they priced were themselves guesses (5 GB-hours per service per day, 0.5 GB
// per service, 10 GB of bandwidth per service per month). The page showed
// the product as "Infrastructure Cost", and the alerts tab raised "over plan
// limit" pages from it. Every metric now reports Included = -1 (no allowance
// applies) and Cost = 0, and a metric nobody measures says so.
const usagePricingNote = "Not priced. Enclii has no price list for platform usage; customer billing is handled by Dhanam, and infrastructure cost comes from the provider's invoice."

// Notes for the metrics that have no meter yet. Month-to-date figures need a
// history Enclii does not keep; the Waybill daily ledger (RFC 0049 Phase 2)
// is where they will come from.
const (
	computeNotMeasuredNote   = "Month-to-date GB-hours are not measured yet. Live CPU and memory are in /v1/usage/realtime; daily history arrives with the Waybill ledger."
	storageNotMeasuredNote   = "Not measured yet: there is no per-project volume or image-size meter."
	bandwidthNotMeasuredNote = "Not measured yet: there is no ingress or egress meter."
	domainsScopeNote         = "Domains recorded in Enclii. Domains routed outside Enclii are not counted."
)

// GetUsageSummary returns the current usage metrics for billing
// GET /v1/usage
func (h *Handler) GetUsageSummary(c *gin.Context) {
	usageCacheMu.Lock()
	if usageCache != nil && time.Now().Before(usageCache.expiresAt) {
		cached := usageCache.resp
		usageCacheMu.Unlock()
		c.JSON(http.StatusOK, cached)
		return
	}
	usageCacheMu.Unlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), usageHandlerBudget)
	defer cancel()

	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Second)

	usage, err := h.calculateUsage(ctx, periodStart, periodEnd)
	if err != nil {
		h.logger.Error(ctx, "Failed to calculate usage", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate usage"})
		return
	}

	usageCacheMu.Lock()
	usageCache = &usageCacheEntry{
		resp:      *usage,
		expiresAt: time.Now().Add(usageCacheTTL),
	}
	usageCacheMu.Unlock()

	if ctx.Err() == context.DeadlineExceeded {
		c.Header("X-Enclii-Partial-Response", "true")
	}
	c.JSON(http.StatusOK, *usage)
}

// GetCostBreakdown returns the cost breakdown for billing
// GET /v1/usage/costs
func (h *Handler) GetCostBreakdown(c *gin.Context) {
	ctx := c.Request.Context()

	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Second)

	breakdown, err := h.calculateCostBreakdown(ctx, periodStart, periodEnd)
	if err != nil {
		h.logger.Error(ctx, "Failed to calculate costs", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate costs"})
		return
	}

	c.JSON(http.StatusOK, breakdown)
}

// calculateUsage computes usage metrics from actual K8s data
func (h *Handler) calculateUsage(ctx context.Context, periodStart, periodEnd time.Time) (*UsageSummary, error) {
	// Count services
	services, err := h.repos.Services.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	// Count releases in the period — fan out per-service queries with a
	// concurrency cap. Sequential N+1 was producing ~N×DB-RTT latency on the
	// dashboard's poll path; this collapses it to ~ceil(N/cap).
	//
	// THIS LOOP NO LONGER PRODUCES BUILD MINUTES. It used to credit
	// `localMinutes += 3.0` per release and bill overage against the total at
	// a per-minute rate. Three was a literal — not an average, not an
	// estimate, not anything anybody had measured — and every runner-SKU and
	// per-product cost figure downstream inherited it. Minutes now come from
	// Waybill, which has aggregated real `build_minutes` from
	// `build.completed` events since it was written and was never asked.
	// The release COUNT stays: it is a genuine local fact and it is what the
	// Waybill total gets cross-checked against.
	var (
		buildMu     sync.Mutex
		totalBuilds int
	)
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(usageFanoutConcurrency)
	for _, svc := range services {
		svc := svc
		g.Go(func() error {
			if gCtx.Err() != nil {
				return nil
			}
			releases, err := h.repos.Releases.ListByService(svc.ID)
			if err != nil {
				return nil
			}
			var localBuilds int
			for _, rel := range releases {
				if rel.CreatedAt.After(periodStart) && rel.CreatedAt.Before(periodEnd) {
					localBuilds++
				}
			}
			buildMu.Lock()
			totalBuilds += localBuilds
			buildMu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	_ = totalBuilds // tracked but not currently surfaced

	// Read the meter. Same fan-out shape, one request per PROJECT rather than
	// per service, because Waybill aggregates per project.
	projectIDs := make([]uuid.UUID, 0, len(services))
	for _, svc := range services {
		projectIDs = append(projectIDs, svc.ProjectID)
	}
	buildMinutes := h.fetchBuildMinutes(ctx, distinctProjectIDs(projectIDs))
	if !buildMinutes.Known {
		h.logger.Warn(ctx, "build minutes unavailable; reporting unknown rather than zero",
			logging.String("reason", buildMinutes.Reason),
		)
	}

	// Count custom domains via a single COUNT(*) on the platform table.
	// Per-service iteration was missing orphan rows whose service_id no
	// longer joins (audit 2026-04-29). Caveat: this counts only domains
	// recorded in `custom_domains` — domains provisioned out-of-band via
	// direct Cloudflare API (~40 in current state) aren't here. Fixing
	// that requires a reconcile pass from cloudflare_tunnels routes back
	// into custom_domains, tracked as a separate data-integrity workstream.
	totalDomains := 0
	domainsRead := false
	if _, count, err := h.repos.CustomDomains.ListAll(ctx, nil, 1, 0); err == nil {
		totalDomains = count
		domainsRead = true
	}

	// Compute, storage and bandwidth have no month-to-date meter. They used to
	// be estimates priced at invented rates (see usagePricingNote); now they
	// say what is missing instead of showing a number.
	buildMetric := buildMinutesMetric(buildMinutes)

	domainsMetric := UsageMetric{
		Type:     "domains",
		Label:    "Custom Domains",
		Used:     float64(totalDomains),
		Included: -1,
		Unit:     "domains",
		Note:     domainsScopeNote,
		Source:   "enclii",
	}
	if !domainsRead {
		// A failed count is not zero domains.
		domainsMetric = notMeasuredMetric("domains", "Custom Domains", "domains", "The domain count could not be read.")
	}

	metrics := []UsageMetric{
		notMeasuredMetric("compute", "Compute", "GB-hours", computeNotMeasuredNote),
		buildMetric,
		notMeasuredMetric("storage", "Storage", "GB", storageNotMeasuredNote),
		notMeasuredMetric("bandwidth", "Bandwidth", "GB", bandwidthNotMeasuredNote),
		domainsMetric,
	}

	return &UsageSummary{
		PeriodStart: periodStart.Format("2006-01-02"),
		PeriodEnd:   periodEnd.Format("2006-01-02"),
		Metrics:     metrics,
		TotalCost:   0,
		Priced:      false,
		PricingNote: usagePricingNote,
	}, nil
}

// notMeasuredMetric is a metric with no meter behind it: Used 0 that MEANS
// NOTHING, flagged Unavailable with the reason, never priced, no allowance.
func notMeasuredMetric(metricType, label, unit, note string) UsageMetric {
	return UsageMetric{
		Type:        metricType,
		Label:       label,
		Included:    -1,
		Unit:        unit,
		Unavailable: true,
		Note:        note,
	}
}

// resolveK8sNamespace determines the actual K8s namespace for a service.
//
// Fallback chain mirrors the reconciler's behaviour so usage metrics line
// up with where the deployment actually runs:
//
//  1. svc.K8sNamespace if set on the row (the explicit, modern value)
//  2. project.Slug looked up by ProjectID (the convention since R2 fix)
//  3. "default" as a last-resort fallback
//
// The legacy `proj-{id8}` heuristic returned a namespace that didn't
// exist for ~all services in the truthful state, which made every
// metrics-server probe return 404 and cost us a full RTT per service.
func (h *Handler) resolveK8sNamespace(ctx context.Context, svc *types.Service) string {
	if svc.K8sNamespace != nil && *svc.K8sNamespace != "" {
		return *svc.K8sNamespace
	}
	if svc.ProjectID != uuid.Nil && h.repos != nil && h.repos.Projects != nil {
		if project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID); err == nil && project != nil && project.Slug != "" {
			return project.Slug
		}
	}
	return "default"
}

// ServiceMetrics represents real-time metrics for a service
type ServiceMetrics struct {
	ServiceID   string  `json:"service_id"`
	ServiceName string  `json:"service_name"`
	Namespace   string  `json:"namespace"`
	PodCount    int     `json:"pod_count"`
	CPUUsage    float64 `json:"cpu_usage_millicores"` // millicores
	MemoryUsage float64 `json:"memory_usage_mb"`      // MB
	Status      string  `json:"status"`               // "running", "stopped", "error"
}

// ClusterMetricsResponse represents cluster-wide metrics
type ClusterMetricsResponse struct {
	TotalCPU       float64          `json:"total_cpu_millicores"`
	TotalMemory    float64          `json:"total_memory_mb"`
	TotalPods      int              `json:"total_pods"`
	MetricsEnabled bool             `json:"metrics_enabled"`
	Services       []ServiceMetrics `json:"services"`
	CollectedAt    string           `json:"collected_at"`
}

// realtimeBudget bounds /v1/usage/realtime, which the usage page polls every
// 30 seconds.
const realtimeBudget = 20 * time.Second

// GetRealTimeMetrics returns current resource usage from metrics-server.
// GET /v1/usage/realtime
//
// The totals are the whole cluster: every pod metrics-server reports, in every
// namespace, so they match `kubectl top pods -A` summed. The per-service rows
// are the subset that belongs to Enclii services, attributed by namespace and
// the `app=<service>` pod label. Cost: one metrics read plus one pod list per
// namespace that holds a service, rather than two API calls per service.
func (h *Handler) GetRealTimeMetrics(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), realtimeBudget)
	defer cancel()

	if h.k8sClient == nil {
		c.JSON(http.StatusOK, ClusterMetricsResponse{
			MetricsEnabled: false,
			Services:       []ServiceMetrics{},
			CollectedAt:    time.Now().Format(time.RFC3339),
		})
		return
	}

	services, err := h.repos.Services.ListAll(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to list services", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list services"})
		return
	}

	clusterMetrics, err := h.k8sClient.GetClusterMetrics(ctx)
	if err != nil {
		h.logger.Warn(ctx, "Failed to get cluster metrics", logging.Error("error", err))
	}

	response := ClusterMetricsResponse{
		MetricsEnabled: clusterMetrics != nil && clusterMetrics.MetricsEnabled,
		Services:       make([]ServiceMetrics, 0, len(services)),
		CollectedAt:    time.Now().Format(time.RFC3339),
	}
	if clusterMetrics != nil {
		response.TotalCPU = float64(clusterMetrics.TotalCPU)
		response.TotalMemory = float64(clusterMetrics.TotalMemory) / (1024 * 1024)
		response.TotalPods = clusterMetrics.TotalPods
	}

	namespaceOf := make(map[uuid.UUID]string, len(services))
	for _, svc := range services {
		namespaceOf[svc.ID] = h.resolveK8sNamespace(ctx, svc)
	}

	podApps := map[string]map[string]string{}
	if response.MetricsEnabled {
		podApps = h.podAppLabels(ctx, namespaceOf)
	}
	response.Services = attributeServiceMetrics(services, namespaceOf, podApps, clusterMetrics)

	c.JSON(http.StatusOK, response)
}

// podAppLabels lists the pods of each namespace once and maps pod name to its
// `app` label. A namespace whose list fails is absent from the map, and its
// services read as "unknown" rather than as zero usage.
func (h *Handler) podAppLabels(ctx context.Context, namespaceOf map[uuid.UUID]string) map[string]map[string]string {
	namespaces := map[string]struct{}{}
	for _, ns := range namespaceOf {
		namespaces[ns] = struct{}{}
	}
	var mu sync.Mutex
	out := make(map[string]map[string]string, len(namespaces))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(usageFanoutConcurrency)
	for ns := range namespaces {
		ns := ns
		g.Go(func() error {
			pods, err := h.k8sClient.ListPods(gCtx, ns, "")
			if err != nil || pods == nil {
				return nil
			}
			apps := make(map[string]string, len(pods.Items))
			for _, pod := range pods.Items {
				apps[pod.Name] = pod.Labels["app"]
			}
			mu.Lock()
			out[ns] = apps
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return out
}

// attributeServiceMetrics builds the per-service rows from one cluster-wide
// metrics read. A service is "running" with its pods' summed usage, "stopped"
// when its namespace was listed and no pod carries its app label, and
// "unknown" when metrics or the pod list could not be read.
func attributeServiceMetrics(
	services []*types.Service,
	namespaceOf map[uuid.UUID]string,
	podApps map[string]map[string]string,
	cluster *k8s.ClusterMetrics,
) []ServiceMetrics {
	out := make([]ServiceMetrics, 0, len(services))
	for _, svc := range services {
		ns := namespaceOf[svc.ID]
		sm := ServiceMetrics{
			ServiceID:   svc.ID.String(),
			ServiceName: svc.Name,
			Namespace:   ns,
			Status:      "unknown",
		}
		apps, listed := podApps[ns]
		if cluster == nil || !cluster.MetricsEnabled || !listed {
			out = append(out, sm)
			continue
		}
		if nsMetrics, ok := cluster.Namespaces[ns]; ok {
			for _, pm := range nsMetrics.Pods {
				if apps[pm.PodName] != svc.Name {
					continue
				}
				sm.PodCount++
				sm.CPUUsage += float64(pm.TotalCPU)
				sm.MemoryUsage += float64(pm.TotalMemory) / (1024 * 1024)
			}
		}
		if sm.PodCount > 0 {
			sm.Status = "running"
		} else {
			sm.Status = "stopped"
		}
		out = append(out, sm)
	}
	return out
}

// GetServiceResourceMetrics returns metrics for a specific service
// GET /v1/services/:id/metrics
func (h *Handler) GetServiceResourceMetrics(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, ok := h.mustServiceAccess(c)
	if !ok {
		return
	}

	// Get service
	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	namespace := h.resolveK8sNamespace(ctx, svc)
	response := ServiceMetrics{
		ServiceID:   svc.ID.String(),
		ServiceName: svc.Name,
		Namespace:   namespace,
		Status:      "running",
	}

	// Get metrics if K8s client is available
	if h.k8sClient != nil {
		metrics, err := h.k8sClient.GetServiceMetrics(ctx, namespace, svc.Name)
		if err == nil && metrics != nil {
			response.PodCount = metrics.PodCount
			response.CPUUsage = float64(metrics.TotalCPU)
			response.MemoryUsage = float64(metrics.TotalMemory) / (1024 * 1024) // Convert to MB
		} else {
			response.Status = "unknown"
			h.logger.Warn(ctx, "Failed to get service metrics",
				logging.String("service_id", serviceID.String()),
				logging.Error("error", err))
		}
	} else {
		response.Status = "metrics_unavailable"
	}

	c.JSON(http.StatusOK, response)
}

// calculateCostBreakdown reports that nothing is priced. It used to split
// the invented overage costs into a pie; see usagePricingNote.
func (h *Handler) calculateCostBreakdown(_ context.Context, periodStart, periodEnd time.Time) (*CostBreakdown, error) {
	return &CostBreakdown{
		PeriodStart: periodStart.Format("2006-01-02"),
		PeriodEnd:   periodEnd.Format("2006-01-02"),
		Categories:  []CostCategory{},
		TotalUsage:  0,
		Priced:      false,
		PricingNote: usagePricingNote,
	}, nil
}

// roundToTwoDecimals rounds a float to 2 decimal places
func roundToTwoDecimals(val float64) float64 {
	return float64(int(val*100)) / 100
}
