package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// healthCacheTTL is how long the GetServiceHealth response stays cached
// per process. The dashboard polls this on POLLING_IDLE (~30s) — caching
// for 20s means each replica returns instantly to the second poll without
// re-fanning out to the K8s API. Reads of the cache are racey-but-safe:
// two concurrent dashboard tabs may both miss and trigger a recompute,
// which is fine.
const healthCacheTTL = 20 * time.Second

// healthFanoutConcurrency is the cap on concurrent in-flight K8s
// GetDeploymentStatusInfo calls during a single GetServiceHealth fan-out.
// Each call hits the apiserver; too many in parallel and we DOS the
// control plane. 15 is empirically the sweet spot — fast enough that
// the full ~88-service sweep finishes in under 2s, low enough that the
// apiserver doesn't notice.
const healthFanoutConcurrency = 15

// healthHandlerBudget caps server-side time spent computing the
// response. Anything past this returns a 504 with whatever partial
// data we collected, rather than letting Cloudflare's edge timeout
// (≈100s) kill the request and hang the browser indefinitely.
const healthHandlerBudget = 25 * time.Second

type healthCacheEntry struct {
	resp      ServiceHealthResponse
	expiresAt time.Time
}

var (
	healthCacheMu sync.Mutex
	healthCache   *healthCacheEntry
	// healthSF collapses concurrent cache-miss recomputes into a single
	// underlying computation. Two dashboard tabs polling /v1/observability/health
	// at the same 30s tick used to each fan out 88-service K8s probes against
	// a shared client-go limiter (5 QPS / burst 10), starving the throttle
	// queue and pushing both requests past the 25s server budget into a 30s
	// client timeout. With singleflight, the second caller observes the first
	// caller's freshly-computed result instead of duplicating the fan-out.
	healthSF singleflight.Group
)

// healthSFKey is the single-flight key for /v1/observability/health. We use a
// constant string because the response is process-global (not per-tenant /
// per-user) — the entire endpoint serves the same payload to all callers within
// a 20s TTL window.
const healthSFKey = "service-health"

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
	serviceID := strings.TrimSpace(c.Query("service_id"))
	if serviceID != "" {
		if _, err := uuid.Parse(serviceID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id"})
			return
		}
	}

	// Cache hit: a previous fan-out completed inside the TTL. Return
	// immediately so the dashboard's polling tick is sub-millisecond.
	// We keep the lock scope tight so concurrent requests don't queue.
	healthCacheMu.Lock()
	if healthCache != nil && time.Now().Before(healthCache.expiresAt) {
		cached := filterServiceHealthResponse(healthCache.resp, serviceID)
		healthCacheMu.Unlock()
		c.JSON(http.StatusOK, cached)
		return
	}
	healthCacheMu.Unlock()

	// Wrap the underlying request context with a hard server-side budget
	// so a slow K8s control plane can't push us past Cloudflare's 100s
	// edge timeout (which manifests in the browser as a stuck spinner —
	// the failure mode this handler was hanging the dashboard with).
	ctx, cancel := context.WithTimeout(c.Request.Context(), healthHandlerBudget)
	defer cancel()

	// singleflight collapses concurrent cache-miss recomputes. Without it,
	// two dashboard tabs hitting the same 30s polling tick both miss the
	// cache and both fan out 88-service K8s probes — twice the apiserver
	// pressure for identical results. The second-arriving caller blocks
	// on the first's computation and shares its response, then both
	// callers populate the cache once.
	//
	// Note: singleflight.Do blocks on the in-flight key. If the leader's
	// computation honours its own context budget we're bounded; we still
	// guard the wait against the caller's context separately so a
	// disconnected client (request context cancelled) doesn't hang here.
	type sfResult struct {
		resp    ServiceHealthResponse
		partial bool
	}
	resultCh := healthSF.DoChan(healthSFKey, func() (interface{}, error) {
		resp, partial, err := h.computeServiceHealth(ctx)
		if err != nil {
			return nil, err
		}
		return sfResult{resp: resp, partial: partial}, nil
	})

	var (
		response ServiceHealthResponse
		partial  bool
	)
	select {
	case res := <-resultCh:
		if res.Err != nil {
			h.logger.Error(ctx, "Failed to compute service health", logging.Error("error", res.Err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve services"})
			return
		}
		out := res.Val.(sfResult)
		response = filterServiceHealthResponse(out.resp, serviceID)
		partial = out.partial
	case <-c.Request.Context().Done():
		// Caller went away (browser tab closed, intermediary timed out).
		// Don't emit a body — the singleflight leader will still finish
		// and populate the cache for subsequent callers.
		return
	}

	if partial {
		// We hit our own budget. Send 200 with the partial response so
		// the dashboard renders something — but flag in headers so SRE
		// can spot it. Operators get truthful counts; nobody hangs.
		c.Header("X-Enclii-Partial-Response", "true")
	}
	c.JSON(http.StatusOK, response)
}

// computeServiceHealth performs the actual fan-out: ListAll services + per-
// service K8s probe + per-service latest-deployment lookup, with a bounded
// concurrency cap. Extracted from GetServiceHealth so the singleflight
// wrapper has a clean callee and the recompute is unit-testable in
// isolation. The boolean return is true when the handler-budget context
// expired before all goroutines finished — caller flips that into the
// X-Enclii-Partial-Response header.
func (h *Handler) computeServiceHealth(ctx context.Context) (ServiceHealthResponse, bool, error) {
	services, err := h.repos.Services.ListAll(ctx)
	if err != nil {
		return ServiceHealthResponse{}, false, err
	}

	// One DB call to get all projects, then map by ID — replaces the
	// previous N-deep `Projects.GetByID` loop. With ~88 services that's
	// 88 round-trips collapsed into 1.
	projectByID := map[uuid.UUID]*types.Project{}
	if projects, err := h.repos.Projects.List(); err == nil {
		for _, p := range projects {
			projectByID[p.ID] = p
		}
	}

	results := make([]ServiceHealth, len(services))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(healthFanoutConcurrency)

	for i, svc := range services {
		i, svc := i, svc
		g.Go(func() error {
			health := ServiceHealth{
				ServiceID:   svc.ID.String(),
				ServiceName: svc.Name,
				LastChecked: time.Now(),
				Status:      "unknown",
			}

			var projectSlug string
			if svc.ProjectID != uuid.Nil {
				if project, ok := projectByID[svc.ProjectID]; ok && project != nil {
					health.ProjectSlug = project.Slug
					projectSlug = project.Slug
				}
			}

			// Latest deployment seeds the status; the K8s probe below
			// overrides if the pod-level reality disagrees (catches stale
			// "running" deployment rows masking crashloops).
			if latestDep, err := h.repos.Deployments.GetLatestByService(gCtx, svc.ID.String()); err == nil && latestDep != nil {
				switch latestDep.Status {
				case types.DeploymentStatusRunning:
					health.Status = "healthy"
				case types.DeploymentStatusPending:
					health.Status = "degraded"
				case types.DeploymentStatusFailed:
					health.Status = "unhealthy"
				}
			}

			if h.k8sClient != nil && svc.Name != "" {
				ns := "default"
				if svc.K8sNamespace != nil && *svc.K8sNamespace != "" {
					ns = *svc.K8sNamespace
				} else if projectSlug != "" {
					ns = projectSlug
				}
				status, err := h.k8sClient.GetDeploymentStatusInfo(gCtx, ns, svc.Name)
				if err == nil && status != nil {
					health.PodCount = int(status.Replicas)
					health.ReadyPods = int(status.ReadyReplicas)
					switch {
					case status.Replicas == 0, status.ReadyReplicas == 0:
						health.Status = "unhealthy"
					case status.ReadyReplicas < status.Replicas:
						health.Status = "degraded"
					default:
						health.Status = "healthy"
					}
				}
			}

			// Uptime is an additional DB query. We compute it for the
			// happy path only — the rollup doesn't need it, and skipping
			// it for unknown/missing services saves another N round-trips.
			if health.Status == "healthy" || health.Status == "degraded" || health.Status == "unhealthy" {
				health.Uptime = computeUptime(gCtx, h, svc.ID.String())
			}

			results[i] = health
			return nil
		})
	}

	// Wait but tolerate a budget exceedance — partial results are still
	// useful to the dashboard; we'd rather paint a slightly-stale set of
	// counts than spin forever.
	if err := g.Wait(); err != nil && ctx.Err() == nil {
		h.logger.Warn(ctx, "Health fan-out reported error", logging.Error("error", err))
	}

	response := ServiceHealthResponse{
		Services:  make([]ServiceHealth, 0, len(results)),
		Timestamp: time.Now(),
	}
	for _, health := range results {
		if health.ServiceID == "" {
			continue // slot wasn't populated (e.g., context cancelled)
		}
		switch health.Status {
		case "healthy":
			response.HealthySvcs++
		case "degraded", "unknown":
			response.DegradedSvcs++
		case "unhealthy":
			response.UnhealthySvcs++
		}
		response.Services = append(response.Services, health)
	}

	healthCacheMu.Lock()
	healthCache = &healthCacheEntry{
		resp:      response,
		expiresAt: time.Now().Add(healthCacheTTL),
	}
	healthCacheMu.Unlock()

	partial := ctx.Err() == context.DeadlineExceeded
	return response, partial, nil
}

func filterServiceHealthResponse(response ServiceHealthResponse, serviceID string) ServiceHealthResponse {
	serviceID = strings.TrimSpace(serviceID)
	if serviceID == "" {
		return response
	}

	filtered := response
	filtered.Services = make([]ServiceHealth, 0, 1)
	filtered.HealthySvcs = 0
	filtered.DegradedSvcs = 0
	filtered.UnhealthySvcs = 0
	for _, health := range response.Services {
		if health.ServiceID != serviceID {
			continue
		}
		filtered.Services = append(filtered.Services, health)
		switch health.Status {
		case "healthy":
			filtered.HealthySvcs++
		case "degraded", "unknown":
			filtered.DegradedSvcs++
		case "unhealthy":
			filtered.UnhealthySvcs++
		}
	}
	return filtered
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

	// Check for unhealthy services. The replica/health checks below read
	// directly off the Service row (already on disk after the reconciler
	// sweep) — no extra round-trips. The "deployment failed" alert needs
	// the latest deployment per service though, which is a per-service DB
	// hit; we fan that out in parallel with a small concurrency cap so the
	// alerts handler doesn't sequentialise into a multi-second blocker the
	// way it did before.
	services, _ := h.repos.Services.ListAll(ctx)

	type depResult struct {
		svc    *types.Service
		dep    *types.Deployment
		failed bool
	}
	depCtx, depCancel := context.WithTimeout(ctx, 8*time.Second)
	defer depCancel()
	depResults := make([]depResult, len(services))
	g, gCtx := errgroup.WithContext(depCtx)
	g.SetLimit(healthFanoutConcurrency)
	for i, svc := range services {
		i, svc := i, svc
		// Replica mismatch + Service Unhealthy alerts need no DB; emit
		// outside the fan-out so they're never lost on a budget exceedance.
		if svc.DesiredReplicas > 0 && svc.ReadyReplicas < svc.DesiredReplicas {
			alerts = append(alerts, Alert{
				ID:          "alert-service-replicas-" + svc.ID.String(),
				Name:        "Service Replica Mismatch",
				Severity:    "warning",
				Status:      "firing",
				Message:     fmt.Sprintf("Service %s has %d/%d ready replicas", svc.Name, svc.ReadyReplicas, svc.DesiredReplicas),
				ServiceID:   svc.ID.String(),
				ServiceName: svc.Name,
				Value:       float64(svc.ReadyReplicas),
				Threshold:   float64(svc.DesiredReplicas),
				FiredAt:     now,
			})
		}
		if svc.Health == types.HealthStatusUnhealthy {
			alerts = append(alerts, Alert{
				ID:          "alert-service-unhealthy-" + svc.ID.String(),
				Name:        "Service Unhealthy",
				Severity:    "critical",
				Status:      "firing",
				Message:     "Service " + svc.Name + " is reporting unhealthy",
				ServiceID:   svc.ID.String(),
				ServiceName: svc.Name,
				FiredAt:     now,
			})
		}

		g.Go(func() error {
			latestDep, err := h.repos.Deployments.GetLatestByService(gCtx, svc.ID.String())
			if err != nil || latestDep == nil {
				return nil
			}
			depResults[i] = depResult{svc: svc, dep: latestDep, failed: latestDep.Status == types.DeploymentStatusFailed}
			return nil
		})
	}
	_ = g.Wait()
	for _, r := range depResults {
		if !r.failed || r.svc == nil {
			continue
		}
		alerts = append(alerts, Alert{
			ID:          "alert-service-failed-" + r.svc.ID.String(),
			Name:        "Service Deployment Failed",
			Severity:    "critical",
			Status:      "firing",
			Message:     "Service " + r.svc.Name + " deployment has failed",
			ServiceID:   r.svc.ID.String(),
			ServiceName: r.svc.Name,
			FiredAt:     now,
		})
	}

	// Usage overage alerts — surface billing exposure on the dashboard so the
	// operator can't miss compute/build/storage going materially over plan
	// (audit 2026-04-29 caught a $311.91/mo overage hidden behind clamped
	// usage gauges). 100% threshold is the included limit; we alert at 105%
	// to avoid noise from rounding right at the boundary.
	//
	// `calculateUsage` is the same helper that powers /v1/usage and itself
	// loops over services × releases × K8s metrics. We give it its own short
	// budget so a slow usage compute doesn't push the alerts response past
	// the dashboard's tolerance window — the alerts list still renders, just
	// without overage rows when usage is unavailable.
	usageCtx, usageCancel := context.WithTimeout(ctx, 6*time.Second)
	defer usageCancel()
	if usage, err := h.calculateUsage(usageCtx, now.AddDate(0, 0, -30), now); err == nil && usage != nil {
		for _, m := range usage.Metrics {
			if m.Included <= 0 {
				continue // unlimited
			}
			pct := (m.Used / m.Included) * 100
			if pct < 105 {
				continue
			}
			severity := "warning"
			if pct >= 200 {
				severity = "critical"
			}
			alerts = append(alerts, Alert{
				ID:        "alert-usage-overage-" + m.Type,
				Name:      m.Label + " Over Plan Limit",
				Severity:  severity,
				Status:    "firing",
				Message:   fmt.Sprintf("%s usage is %.0f%% of plan (cost so far: $%.2f)", m.Label, pct, m.Cost),
				Value:     pct,
				Threshold: 100,
				FiredAt:   now,
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
