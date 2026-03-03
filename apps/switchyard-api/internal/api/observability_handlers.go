package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ServiceHealth represents the health status of a service
type ServiceHealth struct {
	ServiceID    string    `json:"service_id"`
	ServiceName  string    `json:"service_name"`
	ProjectSlug  string    `json:"project_slug"`
	Status       string    `json:"status"` // healthy, degraded, unhealthy, unknown
	Uptime       float64   `json:"uptime"` // percentage
	ResponseTime float64   `json:"response_time_ms"`
	ErrorRate    float64   `json:"error_rate"`
	LastChecked  time.Time `json:"last_checked"`
	PodCount     int       `json:"pod_count"`
	ReadyPods    int       `json:"ready_pods"`
}

// ServiceHealthResponse contains health status for all services
type ServiceHealthResponse struct {
	Services      []ServiceHealth `json:"services"`
	HealthySvcs   int             `json:"healthy_count"`
	DegradedSvcs  int             `json:"degraded_count"`
	UnhealthySvcs int             `json:"unhealthy_count"`
	Timestamp     time.Time       `json:"timestamp"`
}

// ErrorEntry represents a logged error
type ErrorEntry struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	ServiceID   string    `json:"service_id"`
	ServiceName string    `json:"service_name"`
	Level       string    `json:"level"` // error, warn, fatal
	Message     string    `json:"message"`
	StackTrace  string    `json:"stack_trace,omitempty"`
	Count       int       `json:"count"` // occurrences
	LastSeen    time.Time `json:"last_seen"`
	FirstSeen   time.Time `json:"first_seen"`
	Resolved    bool      `json:"resolved"`
}

// RecentErrorsResponse contains recent errors
type RecentErrorsResponse struct {
	Errors     []ErrorEntry `json:"errors"`
	TotalCount int          `json:"total_count"`
	TimeRange  string       `json:"time_range"`
}

// Alert represents an active alert
type Alert struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Severity    string            `json:"severity"` // critical, warning, info
	Status      string            `json:"status"`   // firing, pending, resolved
	Message     string            `json:"message"`
	ServiceID   string            `json:"service_id,omitempty"`
	ServiceName string            `json:"service_name,omitempty"`
	Value       float64           `json:"value,omitempty"`
	Threshold   float64           `json:"threshold,omitempty"`
	FiredAt     time.Time         `json:"fired_at"`
	ResolvedAt  *time.Time        `json:"resolved_at,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// AlertsResponse contains active alerts
type AlertsResponse struct {
	Alerts        []Alert   `json:"alerts"`
	CriticalCount int       `json:"critical_count"`
	WarningCount  int       `json:"warning_count"`
	InfoCount     int       `json:"info_count"`
	Timestamp     time.Time `json:"timestamp"`
}

// GetMetricsSnapshot returns the current metrics snapshot
// @Summary Get current metrics snapshot
// @Description Returns current values for all system metrics
// @Tags observability
// @Produce json
// @Success 200 {object} monitoring.MetricsSnapshot
// @Router /v1/observability/metrics [get]
func (h *Handler) GetMetricsSnapshot(c *gin.Context) {
	ctx := c.Request.Context()

	snapshot, err := h.metrics.GetSnapshot()
	if err != nil {
		h.logger.Error(ctx, "Failed to get metrics snapshot", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve metrics"})
		return
	}

	c.JSON(http.StatusOK, snapshot)
}

// GetMetricsHistory returns historical metrics data
// @Summary Get metrics history
// @Description Returns time-series metrics data for the specified range
// @Tags observability
// @Produce json
// @Param range query string false "Time range: 1h, 6h, 24h, 7d" default(1h)
// @Success 200 {object} monitoring.MetricsHistory
// @Router /v1/observability/metrics/history [get]
func (h *Handler) GetMetricsHistory(c *gin.Context) {
	ctx := c.Request.Context()
	timeRange := c.DefaultQuery("range", "1h")

	history, err := h.metrics.GetHistory(timeRange)
	if err != nil {
		h.logger.Error(ctx, "Failed to get metrics history", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve metrics history"})
		return
	}

	c.JSON(http.StatusOK, history)
}

// GetServiceHealth returns health status for all services
// @Summary Get service health status
// @Description Returns health status for all deployed services
// @Tags observability
// @Produce json
// @Success 200 {object} ServiceHealthResponse
// @Router /v1/observability/health [get]
func (h *Handler) GetServiceHealth(c *gin.Context) {
	ctx := c.Request.Context()

	// Get all services
	services, err := h.repos.Services.ListAll(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to list services", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve services"})
		return
	}

	response := ServiceHealthResponse{
		Services:  make([]ServiceHealth, 0, len(services)),
		Timestamp: time.Now(),
	}

	for _, svc := range services {
		health := ServiceHealth{
			ServiceID:   svc.ID.String(),
			ServiceName: svc.Name,
			LastChecked: time.Now(),
			Status:      "unknown",
		}

		// Get project for the service (ProjectID is not nil for valid services)
		if svc.ProjectID != uuid.Nil {
			project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID)
			if err == nil && project != nil {
				health.ProjectSlug = project.Slug
			}
		}

		// Get latest deployment for the service
		latestDep, err := h.repos.Deployments.GetLatestByService(ctx, svc.ID.String())
		if err == nil && latestDep != nil {
			switch latestDep.Status {
			case types.DeploymentStatusRunning:
				health.Status = "healthy"
				health.Uptime = computeUptime(ctx, h, svc.ID.String())
				response.HealthySvcs++
			case types.DeploymentStatusPending:
				health.Status = "degraded"
				health.Uptime = computeUptime(ctx, h, svc.ID.String())
				response.DegradedSvcs++
			case types.DeploymentStatusFailed:
				health.Status = "unhealthy"
				health.Uptime = computeUptime(ctx, h, svc.ID.String())
				response.UnhealthySvcs++
			default:
				health.Status = "unknown"
			}
		}

		// Get pod info from K8s if available
		if h.k8sClient != nil && svc.Name != "" {
			status, err := h.k8sClient.GetDeploymentStatusInfo(ctx, "default", svc.Name)
			if err == nil && status != nil {
				health.PodCount = int(status.Replicas)
				health.ReadyPods = int(status.ReadyReplicas)
				if status.Replicas > 0 && status.ReadyReplicas < status.Replicas {
					health.Status = "degraded"
				}
			}
		}

		response.Services = append(response.Services, health)
	}

	c.JSON(http.StatusOK, response)
}

// GetRecentErrors returns recent errors
// @Summary Get recent errors
// @Description Returns recent errors across all services
// @Tags observability
// @Produce json
// @Param limit query int false "Maximum number of errors to return" default(50)
// @Param service_id query string false "Filter by service ID"
// @Param level query string false "Filter by level: error, warn, fatal"
// @Success 200 {object} RecentErrorsResponse
// @Router /v1/observability/errors [get]
func (h *Handler) GetRecentErrors(c *gin.Context) {
	ctx := c.Request.Context()

	limitStr := c.DefaultQuery("limit", "50")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	serviceID := c.Query("service_id")
	level := c.Query("level")

	// Query audit logs for error-level entries
	// Since we don't have a dedicated error log table, we aggregate from audit logs
	// In production, this would query from a proper error tracking system

	errors := make([]ErrorEntry, 0)

	// Build filters for audit log query
	filters := make(map[string]interface{})
	if serviceID != "" {
		filters["resource_id"] = serviceID
	}

	// Get recent audit log entries that indicate errors
	logs, err := h.repos.AuditLogs.Query(ctx, filters, limit*2, 0)
	if err != nil {
		h.logger.Error(ctx, "Failed to get audit logs", logging.Error("error", err))
	} else {
		for _, log := range logs {
			// Filter for error-like entries
			if log.Action == "build_failed" || log.Action == "deploy_failed" || log.Action == "service_error" {
				entry := ErrorEntry{
					ID:        log.ID.String(),
					Timestamp: log.Timestamp,
					Level:     "error",
					Message:   log.Action + ": " + log.ResourceType,
					Count:     1,
					LastSeen:  log.Timestamp,
					FirstSeen: log.Timestamp,
					Resolved:  false,
				}

				if level != "" && entry.Level != level {
					continue
				}

				errors = append(errors, entry)
				if len(errors) >= limit {
					break
				}
			}
		}
	}

	// Sort by timestamp descending
	sort.Slice(errors, func(i, j int) bool {
		return errors[i].Timestamp.After(errors[j].Timestamp)
	})

	response := RecentErrorsResponse{
		Errors:     errors,
		TotalCount: len(errors),
		TimeRange:  "24h",
	}

	c.JSON(http.StatusOK, response)
}

// GetActiveAlerts returns active alerts
// @Summary Get active alerts
// @Description Returns currently firing or pending alerts
// @Tags observability
// @Produce json
// @Param status query string false "Filter by status: firing, pending, resolved"
// @Param severity query string false "Filter by severity: critical, warning, info"
// @Success 200 {object} AlertsResponse
// @Router /v1/observability/alerts [get]
func (h *Handler) GetActiveAlerts(c *gin.Context) {
	ctx := c.Request.Context()

	statusFilter := c.Query("status")
	severityFilter := c.Query("severity")

	// Get current metrics to determine alerts
	snapshot, err := h.metrics.GetSnapshot()
	if err != nil {
		h.logger.Error(ctx, "Failed to get metrics for alerts", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve alerts"})
		return
	}

	alerts := make([]Alert, 0)
	now := time.Now()

	// Check error rate threshold
	if snapshot.HTTPMetrics.ErrorRate > 0.05 { // > 5% error rate
		alerts = append(alerts, Alert{
			ID:        "alert-error-rate-high",
			Name:      "High Error Rate",
			Severity:  "critical",
			Status:    "firing",
			Message:   "HTTP error rate is above 5%",
			Value:     snapshot.HTTPMetrics.ErrorRate * 100,
			Threshold: 5.0,
			FiredAt:   now,
		})
	}

	// Check latency threshold
	if snapshot.HTTPMetrics.AverageLatency > 2.0 { // > 2 seconds
		alerts = append(alerts, Alert{
			ID:        "alert-latency-high",
			Name:      "High Latency",
			Severity:  "warning",
			Status:    "firing",
			Message:   "Average response time is above 2 seconds",
			Value:     snapshot.HTTPMetrics.AverageLatency * 1000, // ms
			Threshold: 2000.0,
			FiredAt:   now,
		})
	}

	// Check cache hit rate
	if snapshot.CacheMetrics.HitRate < 0.8 && snapshot.CacheMetrics.HitRate > 0 { // < 80%
		alerts = append(alerts, Alert{
			ID:        "alert-cache-hit-low",
			Name:      "Low Cache Hit Rate",
			Severity:  "warning",
			Status:    "firing",
			Message:   "Cache hit rate is below 80%",
			Value:     snapshot.CacheMetrics.HitRate * 100,
			Threshold: 80.0,
			FiredAt:   now,
		})
	}

	// Check DB connections
	maxConns := 20 // typical max connections
	connUsage := float64(snapshot.DBMetrics.ConnectionsInUse) / float64(maxConns)
	if connUsage > 0.8 { // > 80% used
		alerts = append(alerts, Alert{
			ID:        "alert-db-conn-high",
			Name:      "High DB Connection Usage",
			Severity:  "warning",
			Status:    "firing",
			Message:   "Database connection pool usage is above 80%",
			Value:     connUsage * 100,
			Threshold: 80.0,
			FiredAt:   now,
		})
	}

	// Check build success rate
	if snapshot.BuildMetrics.SuccessRate < 0.9 && snapshot.BuildMetrics.SuccessRate > 0 { // < 90%
		alerts = append(alerts, Alert{
			ID:        "alert-build-failures",
			Name:      "Build Failure Rate High",
			Severity:  "warning",
			Status:    "firing",
			Message:   "Build success rate is below 90%",
			Value:     snapshot.BuildMetrics.SuccessRate * 100,
			Threshold: 90.0,
			FiredAt:   now,
		})
	}

	// Check for unhealthy services
	services, _ := h.repos.Services.ListAll(ctx)
	for _, svc := range services {
		latestDep, err := h.repos.Deployments.GetLatestByService(ctx, svc.ID.String())
		if err != nil || latestDep == nil {
			continue
		}
		if latestDep.Status == types.DeploymentStatusFailed {
			alerts = append(alerts, Alert{
				ID:          "alert-service-failed-" + svc.ID.String(),
				Name:        "Service Deployment Failed",
				Severity:    "critical",
				Status:      "firing",
				Message:     "Service " + svc.Name + " deployment has failed",
				ServiceID:   svc.ID.String(),
				ServiceName: svc.Name,
				FiredAt:     now,
			})
		}
	}

	// Apply filters
	filteredAlerts := make([]Alert, 0)
	for _, alert := range alerts {
		if statusFilter != "" && alert.Status != statusFilter {
			continue
		}
		if severityFilter != "" && alert.Severity != severityFilter {
			continue
		}
		filteredAlerts = append(filteredAlerts, alert)
	}

	// Count by severity
	response := AlertsResponse{
		Alerts:    filteredAlerts,
		Timestamp: now,
	}
	for _, alert := range filteredAlerts {
		switch alert.Severity {
		case "critical":
			response.CriticalCount++
		case "warning":
			response.WarningCount++
		case "info":
			response.InfoCount++
		}
	}

	c.JSON(http.StatusOK, response)
}

// computeUptime calculates the uptime percentage for a service over the last 30 days
// by summing the time the latest deployment spent in "running" status relative to
// the total observation window. Falls back to 0 on any query error.
func computeUptime(ctx context.Context, h *Handler, serviceID string) float64 {
	window := 30 * 24 * time.Hour
	since := time.Now().Add(-window)

	deployments, err := h.repos.Deployments.GetByServiceSince(ctx, serviceID, since)
	if err != nil || len(deployments) == 0 {
		return 0
	}

	var runningDuration time.Duration
	now := time.Now()
	for i, dep := range deployments {
		if dep.Status != types.DeploymentStatusRunning {
			continue
		}
		start := dep.CreatedAt
		if start.Before(since) {
			start = since
		}
		// The "end" is when the next deployment started, or now if it's the latest
		end := now
		if i+1 < len(deployments) {
			end = deployments[i+1].CreatedAt
		}
		if end.After(now) {
			end = now
		}
		if end.After(start) {
			runningDuration += end.Sub(start)
		}
	}

	uptime := float64(runningDuration) / float64(window) * 100
	if uptime > 100 {
		uptime = 100
	}
	return float64(int(uptime*100)) / 100 // round to 2 decimal places
}
