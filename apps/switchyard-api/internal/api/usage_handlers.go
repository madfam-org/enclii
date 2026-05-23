package api

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

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
}

// UsageSummary represents infrastructure usage metering data.
// Customer billing is handled by Dhanam.
type UsageSummary struct {
	PeriodStart string        `json:"period_start"`
	PeriodEnd   string        `json:"period_end"`
	Metrics     []UsageMetric `json:"metrics"`
	TotalCost   float64       `json:"total_cost"`
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
}

// Infrastructure cost metering rates. Customer billing handled by Dhanam.
const (
	computePerGBHour = 0.05
	buildPerMinute   = 0.01
	storagePerGB     = 0.25
	bandwidthPerGB   = 0.10
)

// Resource allocation baselines for usage tracking
const (
	includedCompute   = 500.0 // GB-hours
	includedBuild     = 500.0 // minutes
	includedStorage   = 10.0  // GB
	includedBandwidth = 500.0 // GB
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

	// Count releases for build minutes — fan out per-service queries with
	// a concurrency cap. Sequential N+1 was producing ~N×DB-RTT latency
	// on the dashboard's poll path; this collapses it to ~ceil(N/cap).
	var (
		buildMu           sync.Mutex
		totalBuilds       int
		totalBuildMinutes float64
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
			var localMinutes float64
			for _, rel := range releases {
				if rel.CreatedAt.After(periodStart) && rel.CreatedAt.Before(periodEnd) {
					localBuilds++
					localMinutes += 3.0
				}
			}
			buildMu.Lock()
			totalBuilds += localBuilds
			totalBuildMinutes += localMinutes
			buildMu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	_ = totalBuilds // tracked but not currently surfaced

	// Count custom domains via a single COUNT(*) on the platform table.
	// Per-service iteration was missing orphan rows whose service_id no
	// longer joins (audit 2026-04-29). Caveat: this counts only domains
	// recorded in `custom_domains` — domains provisioned out-of-band via
	// direct Cloudflare API (~40 in current state) aren't here. Fixing
	// that requires a reconcile pass from cloudflare_tunnels routes back
	// into custom_domains, tracked as a separate data-integrity workstream.
	totalDomains := 0
	if _, count, err := h.repos.CustomDomains.ListAll(ctx, nil, 1, 0); err == nil {
		totalDomains = count
	}

	// Calculate compute usage from real K8s metrics
	computeUsed := h.calculateRealComputeUsage(ctx, services, periodStart)

	// Calculate storage from actual container images
	storageUsed := h.calculateRealStorageUsage(ctx, services)

	// Bandwidth estimation (would need ingress metrics for real data)
	daysInPeriod := time.Since(periodStart).Hours() / 24
	if daysInPeriod < 1 {
		daysInPeriod = 1
	}
	bandwidthUsed := float64(len(services)) * 10.0 * (daysInPeriod / 30.0)

	// Calculate overage costs
	computeCost := calculateOverage(computeUsed, includedCompute, computePerGBHour)
	buildCost := calculateOverage(totalBuildMinutes, includedBuild, buildPerMinute)
	storageCost := calculateOverage(storageUsed, includedStorage, storagePerGB)
	bandwidthCost := calculateOverage(bandwidthUsed, includedBandwidth, bandwidthPerGB)

	totalCost := computeCost + buildCost + storageCost + bandwidthCost

	metrics := []UsageMetric{
		{
			Type:     "compute",
			Label:    "Compute",
			Used:     roundToTwoDecimals(computeUsed),
			Included: includedCompute,
			Unit:     "GB-hours",
			Cost:     roundToTwoDecimals(computeCost),
		},
		{
			Type:     "build",
			Label:    "Build Minutes",
			Used:     roundToTwoDecimals(totalBuildMinutes),
			Included: includedBuild,
			Unit:     "minutes",
			Cost:     roundToTwoDecimals(buildCost),
		},
		{
			Type:     "storage",
			Label:    "Storage",
			Used:     roundToTwoDecimals(storageUsed),
			Included: includedStorage,
			Unit:     "GB",
			Cost:     roundToTwoDecimals(storageCost),
		},
		{
			Type:     "bandwidth",
			Label:    "Bandwidth",
			Used:     roundToTwoDecimals(bandwidthUsed),
			Included: includedBandwidth,
			Unit:     "GB",
			Cost:     roundToTwoDecimals(bandwidthCost),
		},
		{
			Type:     "domains",
			Label:    "Custom Domains",
			Used:     float64(totalDomains),
			Included: -1, // Unlimited
			Unit:     "domains",
			Cost:     0,
		},
	}

	return &UsageSummary{
		PeriodStart: periodStart.Format("2006-01-02"),
		PeriodEnd:   periodEnd.Format("2006-01-02"),
		Metrics:     metrics,
		TotalCost:   roundToTwoDecimals(totalCost),
	}, nil
}

// calculateRealComputeUsage calculates compute usage from real K8s metrics.
//
// When metrics-server isn't installed in the cluster (current state per
// /v1/usage/realtime returning `metrics_enabled: false`), every per-service
// `GetServiceMetrics` call hits a guaranteed timeout; sequentially looping
// 88 of those is what was hanging the dashboard. Cheap probe-once at the
// top: ask for cluster metrics, and if we can't reach them, fall straight
// to the size-based estimate without N more roundtrips.
//
// When metrics-server IS available, fan out the per-service probes with a
// concurrency cap so the wall-clock cost is bounded.
func (h *Handler) calculateRealComputeUsage(ctx context.Context, services []*types.Service, periodStart time.Time) float64 {
	daysInPeriod := time.Since(periodStart).Hours() / 24
	if daysInPeriod < 1 {
		daysInPeriod = 1
	}
	estimate := float64(len(services)) * 5.0 * daysInPeriod

	if h.k8sClient == nil {
		return estimate
	}

	// Probe metrics-server availability cheaply via the cluster-wide
	// endpoint. If it's not reachable, don't bother fanning out per-service
	// — return the estimate so the handler stays fast.
	probeCtx, probeCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer probeCancel()
	cluster, err := h.k8sClient.GetClusterMetrics(probeCtx)
	if err != nil || cluster == nil || !cluster.MetricsEnabled {
		return estimate
	}

	hoursActive := time.Since(periodStart).Hours()
	if hoursActive < 0 {
		hoursActive = 0
	}

	var (
		mu                  sync.Mutex
		totalComputeGBHours float64
	)
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(usageFanoutConcurrency)
	for _, svc := range services {
		svc := svc
		g.Go(func() error {
			if gCtx.Err() != nil {
				return nil
			}
			ns := h.resolveK8sNamespace(gCtx, svc)
			metrics, err := h.k8sClient.GetServiceMetrics(gCtx, ns, svc.Name)
			if err != nil || metrics == nil {
				mu.Lock()
				totalComputeGBHours += 5.0 * daysInPeriod
				mu.Unlock()
				return nil
			}
			memoryGB := float64(metrics.TotalMemory) / (1024 * 1024 * 1024)
			mu.Lock()
			totalComputeGBHours += memoryGB * hoursActive
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return totalComputeGBHours
}

// calculateRealStorageUsage calculates storage from actual deployments
func (h *Handler) calculateRealStorageUsage(ctx context.Context, services []*types.Service) float64 {
	// For now, estimate based on service count
	// Real implementation would check container image sizes from registry
	// Each service typically has ~0.5 GB of image layers
	return float64(len(services)) * 0.5
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

// getServiceNamespace is the legacy heuristic still consumed by older
// helpers in this file. New code should use h.resolveK8sNamespace.
func getServiceNamespace(svc *types.Service) string {
	if svc.K8sNamespace != nil && *svc.K8sNamespace != "" {
		return *svc.K8sNamespace
	}
	if svc.ProjectID != uuid.Nil {
		return fmt.Sprintf("proj-%s", svc.ProjectID.String()[:8])
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

// GetRealTimeMetrics returns current resource usage from K8s
// GET /v1/usage/realtime
func (h *Handler) GetRealTimeMetrics(c *gin.Context) {
	ctx := c.Request.Context()

	// Check if K8s client is available
	if h.k8sClient == nil {
		c.JSON(http.StatusOK, ClusterMetricsResponse{
			MetricsEnabled: false,
			Services:       []ServiceMetrics{},
			CollectedAt:    time.Now().Format(time.RFC3339),
		})
		return
	}

	// Get all services
	services, err := h.repos.Services.ListAll(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to list services", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list services"})
		return
	}

	// Try to get cluster-wide metrics
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
		response.TotalMemory = float64(clusterMetrics.TotalMemory) / (1024 * 1024) // Convert to MB
		response.TotalPods = clusterMetrics.TotalPods
	}

	// Get metrics for each service
	for _, svc := range services {
		namespace := getServiceNamespace(svc)
		sm := ServiceMetrics{
			ServiceID:   svc.ID.String(),
			ServiceName: svc.Name,
			Namespace:   namespace,
			Status:      "running",
		}

		// Try to get service-specific metrics
		if h.k8sClient != nil && clusterMetrics != nil && clusterMetrics.MetricsEnabled {
			serviceMetrics, err := h.k8sClient.GetServiceMetrics(ctx, namespace, svc.Name)
			if err == nil && serviceMetrics != nil {
				sm.PodCount = serviceMetrics.PodCount
				sm.CPUUsage = float64(serviceMetrics.TotalCPU)
				sm.MemoryUsage = float64(serviceMetrics.TotalMemory) / (1024 * 1024) // Convert to MB
			} else {
				sm.Status = "unknown"
			}
		}

		response.Services = append(response.Services, sm)
	}

	c.JSON(http.StatusOK, response)
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

	namespace := getServiceNamespace(svc)
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

// calculateCostBreakdown computes cost breakdown from actual data
func (h *Handler) calculateCostBreakdown(ctx context.Context, periodStart, periodEnd time.Time) (*CostBreakdown, error) {
	usage, err := h.calculateUsage(ctx, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	categories := []CostCategory{
		{Name: "Compute", Value: usage.Metrics[0].Cost, Color: "#3b82f6"},
		{Name: "Build", Value: usage.Metrics[1].Cost, Color: "#22c55e"},
		{Name: "Storage", Value: usage.Metrics[2].Cost, Color: "#f59e0b"},
		{Name: "Bandwidth", Value: usage.Metrics[3].Cost, Color: "#8b5cf6"},
	}

	return &CostBreakdown{
		PeriodStart: usage.PeriodStart,
		PeriodEnd:   usage.PeriodEnd,
		Categories:  categories,
		TotalUsage:  usage.TotalCost,
	}, nil
}

// calculateOverage calculates overage cost
func calculateOverage(used, included, pricePerUnit float64) float64 {
	if used <= included {
		return 0
	}
	return (used - included) * pricePerUnit
}

// roundToTwoDecimals rounds a float to 2 decimal places
func roundToTwoDecimals(val float64) float64 {
	return float64(int(val*100)) / 100
}
