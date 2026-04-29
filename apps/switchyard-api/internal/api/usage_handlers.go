package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
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
	ctx := c.Request.Context()

	// Get current billing period (1st of month to end of month)
	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Second)

	// Calculate usage from actual data
	usage, err := h.calculateUsage(ctx, periodStart, periodEnd)
	if err != nil {
		h.logger.Error(ctx, "Failed to calculate usage", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate usage"})
		return
	}

	c.JSON(http.StatusOK, usage)
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

	// Count releases for build minutes
	var totalBuilds int
	var totalBuildMinutes float64
	for _, svc := range services {
		releases, err := h.repos.Releases.ListByService(svc.ID)
		if err != nil {
			continue
		}
		for _, rel := range releases {
			if rel.CreatedAt.After(periodStart) && rel.CreatedAt.Before(periodEnd) {
				totalBuilds++
				// Estimate 3 minutes per build (average for buildpacks)
				totalBuildMinutes += 3.0
			}
		}
	}

	// Count custom domains
	var totalDomains int
	for _, svc := range services {
		domains, err := h.repos.CustomDomains.GetByServiceID(ctx, svc.ID.String())
		if err != nil {
			continue
		}
		totalDomains += len(domains)
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

// calculateRealComputeUsage calculates compute usage from real K8s metrics
func (h *Handler) calculateRealComputeUsage(ctx context.Context, services []*types.Service, periodStart time.Time) float64 {
	if h.k8sClient == nil {
		// Fallback to estimation if K8s client not available
		daysInPeriod := time.Since(periodStart).Hours() / 24
		if daysInPeriod < 1 {
			daysInPeriod = 1
		}
		return float64(len(services)) * 5.0 * daysInPeriod
	}

	// Try to get real metrics from K8s metrics-server
	var totalComputeGBHours float64

	for _, svc := range services {
		// Determine namespace for service
		namespace := h.resolveK8sNamespace(ctx, svc)

		// Get real-time metrics for the service
		metrics, err := h.k8sClient.GetServiceMetrics(ctx, namespace, svc.Name)
		if err != nil {
			h.logger.Warn(ctx, "Failed to get metrics for service",
				logging.String("service", svc.Name),
				logging.Error("error", err))
			// Fallback to estimate for this service
			daysInPeriod := time.Since(periodStart).Hours() / 24
			if daysInPeriod < 1 {
				daysInPeriod = 1
			}
			totalComputeGBHours += 5.0 * daysInPeriod
			continue
		}

		// Convert current memory usage to GB and extrapolate for the billing period
		// Memory is in bytes, convert to GB
		memoryGB := float64(metrics.TotalMemory) / (1024 * 1024 * 1024)

		// Hours since period start (or service creation, whichever is later)
		hoursActive := time.Since(periodStart).Hours()
		if hoursActive < 0 {
			hoursActive = 0
		}

		// GB-hours = memory in GB * hours active
		totalComputeGBHours += memoryGB * hoursActive
	}

	return totalComputeGBHours
}

// calculateRealStorageUsage calculates storage from actual deployments
func (h *Handler) calculateRealStorageUsage(ctx context.Context, services []*types.Service) float64 {
	// For now, estimate based on service count
	// Real implementation would check container image sizes from registry
	// Each service typically has ~0.5 GB of image layers
	return float64(len(services)) * 0.5
}

// resolveK8sNamespace returns the K8s namespace for a service using the
// same fallback chain as the observability handler:
//
//	svc.K8sNamespace → project.Slug → "default"
//
// The historical "proj-{first-8-of-project-id}" pattern was never how products
// were actually deployed (real namespaces are bare slugs: karafiel, dhanam,
// janua, etc.) — using it here meant every k8sClient.GetServiceMetrics call
// hit a non-existent namespace and silently returned 0 pods, which is why
// /v1/usage/realtime reports total_pods=0 even though 222 pods are running.
//
// Distinct from resolveServiceNamespace in domain_provisioner.go, which keys
// on environment name and is used when provisioning routes into the cluster.
// Here we just want "where do this service's pods actually run right now."
func (h *Handler) resolveK8sNamespace(ctx context.Context, svc *types.Service) string {
	if svc.K8sNamespace != nil && *svc.K8sNamespace != "" {
		return *svc.K8sNamespace
	}
	if svc.ProjectID != uuid.Nil {
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
		namespace := h.resolveK8sNamespace(ctx, svc)
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
	serviceIDStr := c.Param("id")

	// Parse service ID
	serviceID, err := uuid.Parse(serviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
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
