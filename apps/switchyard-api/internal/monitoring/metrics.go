package monitoring

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus metrics
var (
	// HTTP metrics
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status_code"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "enclii_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{0.001, 0.01, 0.1, 0.5, 1.0, 2.5, 5.0, 10.0},
		},
		[]string{"method", "endpoint"},
	)

	// Database metrics
	dbConnectionsOpen = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "enclii_db_connections_open",
			Help: "Number of open database connections",
		},
		[]string{"database"},
	)

	dbConnectionsInUse = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "enclii_db_connections_in_use",
			Help: "Number of database connections in use",
		},
		[]string{"database"},
	)

	dbQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "enclii_db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0},
		},
		[]string{"query_type"},
	)

	dbQueryErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_db_query_errors_total",
			Help: "Total number of database query errors",
		},
		[]string{"query_type", "error_type"},
	)

	// Cache metrics
	cacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_cache_hits_total",
			Help: "Total number of cache hits",
		},
		[]string{"cache_name"},
	)

	cacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_cache_misses_total",
			Help: "Total number of cache misses",
		},
		[]string{"cache_name"},
	)

	cacheOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "enclii_cache_operation_duration_seconds",
			Help:    "Cache operation duration in seconds",
			Buckets: []float64{0.0001, 0.001, 0.01, 0.1, 1.0},
		},
		[]string{"operation", "cache_name"},
	)

	// Build metrics
	buildsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_builds_total",
			Help: "Total number of builds",
		},
		[]string{"status", "build_type"},
	)

	buildDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "enclii_build_duration_seconds",
			Help:    "Build duration in seconds",
			Buckets: []float64{10, 30, 60, 120, 300, 600, 1200, 1800}, // 10s to 30m
		},
		[]string{"build_type"},
	)

	// Deployment metrics
	deploymentsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_deployments_total",
			Help: "Total number of deployments",
		},
		[]string{"environment", "status"},
	)

	deploymentDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "enclii_deployment_duration_seconds",
			Help:    "Deployment duration in seconds",
			Buckets: []float64{5, 15, 30, 60, 120, 300, 600}, // 5s to 10m
		},
		[]string{"environment"},
	)

	activeDeployments = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "enclii_active_deployments",
			Help: "Number of active deployments",
		},
		[]string{"environment", "status"},
	)

	// Kubernetes metrics
	k8sOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "enclii_k8s_operation_duration_seconds",
			Help:    "Kubernetes operation duration in seconds",
			Buckets: []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
		},
		[]string{"operation", "resource_type"},
	)

	k8sOperationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "enclii_k8s_operation_errors_total",
			Help: "Total number of Kubernetes operation errors",
		},
		[]string{"operation", "resource_type", "error_type"},
	)

	// Business metrics
	activeProjects = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "enclii_active_projects",
			Help: "Number of active projects",
		},
	)

	activeServices = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "enclii_active_services",
			Help: "Number of active services",
		},
		[]string{"project"},
	)

	// System metrics
	goGoroutines = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "enclii_go_goroutines",
			Help: "Number of goroutines",
		},
	)
)

// MetricsCollector handles metrics collection and registration
type MetricsCollector struct {
	registry *prometheus.Registry
}

func NewMetricsCollector() *MetricsCollector {
	registry := prometheus.NewRegistry()

	// Register all metrics
	metrics := []prometheus.Collector{
		httpRequestsTotal,
		httpRequestDuration,
		dbConnectionsOpen,
		dbConnectionsInUse,
		dbQueryDuration,
		dbQueryErrors,
		cacheHits,
		cacheMisses,
		cacheOperationDuration,
		buildsTotal,
		buildDuration,
		deploymentsTotal,
		deploymentDuration,
		activeDeployments,
		k8sOperationDuration,
		k8sOperationErrors,
		activeProjects,
		activeServices,
		goGoroutines,
	}

	for _, metric := range metrics {
		registry.MustRegister(metric)
	}

	// Add Go runtime metrics
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	collector := &MetricsCollector{
		registry: registry,
	}

	// Start background metrics collection
	go collector.collectSystemMetrics()

	return collector
}

func (mc *MetricsCollector) Handler() http.Handler {
	return promhttp.HandlerFor(mc.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// HTTP Middleware
func (mc *MetricsCollector) HTTPMetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start)
		status := strconv.Itoa(c.Writer.Status())

		httpRequestsTotal.WithLabelValues(c.Request.Method, c.FullPath(), status).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, c.FullPath()).Observe(duration.Seconds())
	}
}

// Metric recording functions
func RecordHTTPRequest(method, endpoint, statusCode string, duration time.Duration) {
	httpRequestsTotal.WithLabelValues(method, endpoint, statusCode).Inc()
	httpRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

func RecordDBConnections(database string, open, inUse int) {
	dbConnectionsOpen.WithLabelValues(database).Set(float64(open))
	dbConnectionsInUse.WithLabelValues(database).Set(float64(inUse))
}

func RecordDBQuery(queryType string, duration time.Duration) {
	dbQueryDuration.WithLabelValues(queryType).Observe(duration.Seconds())
}

func RecordDBError(queryType, errorType string) {
	dbQueryErrors.WithLabelValues(queryType, errorType).Inc()
}

func RecordCacheHit(cacheName string) {
	cacheHits.WithLabelValues(cacheName).Inc()
}

func RecordCacheMiss(cacheName string) {
	cacheMisses.WithLabelValues(cacheName).Inc()
}

func RecordCacheOperation(operation, cacheName string, duration time.Duration) {
	cacheOperationDuration.WithLabelValues(operation, cacheName).Observe(duration.Seconds())
}

func RecordBuild(status, buildType string, duration time.Duration) {
	buildsTotal.WithLabelValues(status, buildType).Inc()
	if duration > 0 {
		buildDuration.WithLabelValues(buildType).Observe(duration.Seconds())
	}
}

func RecordDeployment(environment, status string, duration time.Duration) {
	deploymentsTotal.WithLabelValues(environment, status).Inc()
	if duration > 0 {
		deploymentDuration.WithLabelValues(environment).Observe(duration.Seconds())
	}
}

func SetActiveDeployments(environment, status string, count int) {
	activeDeployments.WithLabelValues(environment, status).Set(float64(count))
}

func RecordK8sOperation(operation, resourceType string, duration time.Duration) {
	k8sOperationDuration.WithLabelValues(operation, resourceType).Observe(duration.Seconds())
}

func RecordK8sError(operation, resourceType, errorType string) {
	k8sOperationErrors.WithLabelValues(operation, resourceType, errorType).Inc()
}

func SetActiveProjects(count int) {
	activeProjects.Set(float64(count))
}

func SetActiveServices(project string, count int) {
	activeServices.WithLabelValues(project).Set(float64(count))
}

// RecordProjectCreated increments the project creation counter
func RecordProjectCreated() {
	deploymentsTotal.WithLabelValues("created", "project").Inc()
}

// RecordServiceDeployed increments the service deployment counter
func RecordServiceDeployed(project, environment string) {
	deploymentsTotal.WithLabelValues("deployed", "service").Inc()
}

// Background system metrics collection
func (mc *MetricsCollector) collectSystemMetrics() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Update goroutine count
		// Note: This is handled by the Go collector, but keeping as example
		// goGoroutines.Set(float64(runtime.NumGoroutine()))
	}
}

// Health check metrics
func RecordHealthCheck(component string, success bool, duration time.Duration) {
	statusCode := "200"
	if !success {
		statusCode = "503"
	}

	httpRequestsTotal.WithLabelValues("GET", "/health/"+component, statusCode).Inc()
	httpRequestDuration.WithLabelValues("GET", "/health/"+component).Observe(duration.Seconds())
}

// Custom metrics for business logic
type BusinessMetrics struct {
	UsersActive      prometheus.Gauge
	ProjectsCreated  prometheus.Counter
	ServicesDeployed *prometheus.CounterVec
	ErrorRate        *prometheus.GaugeVec
}

func NewBusinessMetrics() *BusinessMetrics {
	return &BusinessMetrics{
		UsersActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "enclii_users_active",
			Help: "Number of active users",
		}),
		ProjectsCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "enclii_projects_created_total",
			Help: "Total number of projects created",
		}),
		ServicesDeployed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "enclii_services_deployed_total",
			Help: "Total number of services deployed",
		}, []string{"project", "environment"}),
		ErrorRate: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "enclii_error_rate",
			Help: "Error rate percentage",
		}, []string{"service", "endpoint"}),
	}
}

// Alerting thresholds (for use with Prometheus AlertManager)
const (
	HighErrorRateThreshold   = 0.05 // 5%
	HighLatencyThreshold     = 2.0  // 2 seconds
	LowCacheHitRateThreshold = 0.8  // 80%
	HighDBConnUsageThreshold = 0.8  // 80% of max connections
	LongBuildTimeThreshold   = 600  // 10 minutes
	LongDeployTimeThreshold  = 300  // 5 minutes
)

// Metrics export for external monitoring systems
type MetricsSnapshot struct {
	Timestamp    time.Time         `json:"timestamp"`
	HTTPMetrics  HTTPMetrics       `json:"http"`
	DBMetrics    DatabaseMetrics   `json:"database"`
	CacheMetrics CacheMetrics      `json:"cache"`
	BuildMetrics BuildMetrics      `json:"builds"`
	K8sMetrics   KubernetesMetrics `json:"kubernetes"`
}

type HTTPMetrics struct {
	RequestsPerSecond float64 `json:"requests_per_second"`
	AverageLatency    float64 `json:"average_latency"`
	ErrorRate         float64 `json:"error_rate"`
}

type DatabaseMetrics struct {
	ConnectionsOpen  int     `json:"connections_open"`
	ConnectionsInUse int     `json:"connections_in_use"`
	AverageQueryTime float64 `json:"average_query_time"`
	ErrorRate        float64 `json:"error_rate"`
}

type CacheMetrics struct {
	HitRate          float64 `json:"hit_rate"`
	AverageLatency   float64 `json:"average_latency"`
	OperationsPerSec float64 `json:"operations_per_second"`
}

type BuildMetrics struct {
	SuccessRate     float64 `json:"success_rate"`
	AverageDuration float64 `json:"average_duration"`
	QueueLength     int     `json:"queue_length"`
}

type KubernetesMetrics struct {
	OperationLatency float64 `json:"operation_latency"`
	ErrorRate        float64 `json:"error_rate"`
	ActivePods       int     `json:"active_pods"`
}

func (mc *MetricsCollector) GetSnapshot() (*MetricsSnapshot, error) {
	// Gather metrics from the registry
	metricFamilies, err := mc.registry.Gather()
	if err != nil {
		return nil, err
	}

	snapshot := &MetricsSnapshot{
		Timestamp: time.Now(),
		HTTPMetrics: HTTPMetrics{
			RequestsPerSecond: 0,
			AverageLatency:    0,
			ErrorRate:         0,
		},
		DBMetrics: DatabaseMetrics{
			ConnectionsOpen:  0,
			ConnectionsInUse: 0,
			AverageQueryTime: 0,
			ErrorRate:        0,
		},
		CacheMetrics: CacheMetrics{
			HitRate:          0,
			AverageLatency:   0,
			OperationsPerSec: 0,
		},
		BuildMetrics: BuildMetrics{
			SuccessRate:     0,
			AverageDuration: 0,
			QueueLength:     0,
		},
		K8sMetrics: KubernetesMetrics{
			OperationLatency: 0,
			ErrorRate:        0,
			ActivePods:       0,
		},
	}

	// Parse metric families to populate snapshot
	var totalRequests, errorRequests float64
	var totalLatency float64
	var latencyCount int
	var cacheHitsTotal, cacheMissesTotal float64
	var buildsSuccess, buildsTotal float64

	for _, mf := range metricFamilies {
		name := mf.GetName()
		metrics := mf.GetMetric()

		switch name {
		case "enclii_http_requests_total":
			for _, m := range metrics {
				val := m.GetCounter().GetValue()
				totalRequests += val
				for _, label := range m.GetLabel() {
					if label.GetName() == "status_code" && label.GetValue()[0] >= '4' {
						errorRequests += val
					}
				}
			}
		case "enclii_http_request_duration_seconds":
			for _, m := range metrics {
				h := m.GetHistogram()
				if h != nil {
					totalLatency += h.GetSampleSum()
					latencyCount += int(h.GetSampleCount())
				}
			}
		case "enclii_db_connections_open":
			for _, m := range metrics {
				snapshot.DBMetrics.ConnectionsOpen = int(m.GetGauge().GetValue())
			}
		case "enclii_db_connections_in_use":
			for _, m := range metrics {
				snapshot.DBMetrics.ConnectionsInUse = int(m.GetGauge().GetValue())
			}
		case "enclii_cache_hits_total":
			for _, m := range metrics {
				cacheHitsTotal += m.GetCounter().GetValue()
			}
		case "enclii_cache_misses_total":
			for _, m := range metrics {
				cacheMissesTotal += m.GetCounter().GetValue()
			}
		case "enclii_builds_total":
			for _, m := range metrics {
				val := m.GetCounter().GetValue()
				buildsTotal += val
				for _, label := range m.GetLabel() {
					if label.GetName() == "status" && label.GetValue() == "success" {
						buildsSuccess += val
					}
				}
			}
		case "enclii_build_duration_seconds":
			for _, m := range metrics {
				h := m.GetHistogram()
				if h != nil && h.GetSampleCount() > 0 {
					snapshot.BuildMetrics.AverageDuration = h.GetSampleSum() / float64(h.GetSampleCount())
				}
			}
		case "enclii_k8s_operation_duration_seconds":
			for _, m := range metrics {
				h := m.GetHistogram()
				if h != nil && h.GetSampleCount() > 0 {
					snapshot.K8sMetrics.OperationLatency = h.GetSampleSum() / float64(h.GetSampleCount())
				}
			}
		}
	}

	// Calculate derived metrics
	if totalRequests > 0 {
		snapshot.HTTPMetrics.ErrorRate = errorRequests / totalRequests
	}
	if latencyCount > 0 {
		snapshot.HTTPMetrics.AverageLatency = totalLatency / float64(latencyCount)
	}
	cacheTotal := cacheHitsTotal + cacheMissesTotal
	if cacheTotal > 0 {
		snapshot.CacheMetrics.HitRate = cacheHitsTotal / cacheTotal
	}
	if buildsTotal > 0 {
		snapshot.BuildMetrics.SuccessRate = buildsSuccess / buildsTotal
	}

	return snapshot, nil
}

// MetricsHistory stores time-series data for metrics
type MetricsHistory struct {
	TimeRange  string             `json:"time_range"`
	Resolution string             `json:"resolution"`
	DataPoints []MetricsDataPoint `json:"data_points"`
}

type MetricsDataPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	RequestsPerSec   float64   `json:"requests_per_sec"`
	AverageLatencyMs float64   `json:"average_latency_ms"`
	ErrorRate        float64   `json:"error_rate"`
	CPUUsage         float64   `json:"cpu_usage"`
	MemoryUsage      float64   `json:"memory_usage"`
}

// In-memory metrics history buffer (ring buffer)
type metricsHistoryBuffer struct {
	dataPoints []MetricsDataPoint
	maxSize    int
	mu         sync.RWMutex
}

var historyBuffer = &metricsHistoryBuffer{
	dataPoints: make([]MetricsDataPoint, 0, 1440), // 24h at 1-minute resolution
	maxSize:    1440,
}

func init() {
	// Start background collector for history
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			collectHistoryDataPoint()
		}
	}()
}

func collectHistoryDataPoint() {
	historyBuffer.mu.Lock()
	defer historyBuffer.mu.Unlock()

	// Collect current values from Prometheus metrics
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return
	}

	dp := MetricsDataPoint{
		Timestamp: time.Now(),
	}

	var totalRequests, errorRequests float64
	var totalLatency float64
	var latencyCount uint64

	for _, mf := range metricFamilies {
		name := mf.GetName()
		metrics := mf.GetMetric()

		switch name {
		case "enclii_http_requests_total":
			for _, m := range metrics {
				totalRequests += m.GetCounter().GetValue()
				for _, label := range m.GetLabel() {
					if label.GetName() == "status_code" && len(label.GetValue()) > 0 && label.GetValue()[0] >= '4' {
						errorRequests += m.GetCounter().GetValue()
					}
				}
			}
		case "enclii_http_request_duration_seconds":
			for _, m := range metrics {
				h := m.GetHistogram()
				if h != nil {
					totalLatency += h.GetSampleSum()
					latencyCount += h.GetSampleCount()
				}
			}
		case "process_cpu_seconds_total":
			for _, m := range metrics {
				dp.CPUUsage = m.GetCounter().GetValue()
			}
		case "process_resident_memory_bytes":
			for _, m := range metrics {
				dp.MemoryUsage = m.GetGauge().GetValue() / (1024 * 1024) // Convert to MB
			}
		}
	}

	if totalRequests > 0 {
		dp.ErrorRate = errorRequests / totalRequests
	}
	if latencyCount > 0 {
		dp.AverageLatencyMs = (totalLatency / float64(latencyCount)) * 1000
	}

	// Add to ring buffer
	if len(historyBuffer.dataPoints) >= historyBuffer.maxSize {
		historyBuffer.dataPoints = historyBuffer.dataPoints[1:]
	}
	historyBuffer.dataPoints = append(historyBuffer.dataPoints, dp)
}

// GetHistory returns metrics history for a given time range
func (mc *MetricsCollector) GetHistory(timeRange string) (*MetricsHistory, error) {
	historyBuffer.mu.RLock()
	defer historyBuffer.mu.RUnlock()

	now := time.Now()
	var since time.Time
	var resolution string

	switch timeRange {
	case "1h":
		since = now.Add(-1 * time.Hour)
		resolution = "1m"
	case "6h":
		since = now.Add(-6 * time.Hour)
		resolution = "5m"
	case "24h":
		since = now.Add(-24 * time.Hour)
		resolution = "15m"
	case "7d":
		since = now.Add(-7 * 24 * time.Hour)
		resolution = "1h"
	default:
		since = now.Add(-1 * time.Hour)
		resolution = "1m"
		timeRange = "1h"
	}

	// Filter data points by time range
	var filteredPoints []MetricsDataPoint
	for _, dp := range historyBuffer.dataPoints {
		if dp.Timestamp.After(since) {
			filteredPoints = append(filteredPoints, dp)
		}
	}

	return &MetricsHistory{
		TimeRange:  timeRange,
		Resolution: resolution,
		DataPoints: filteredPoints,
	}, nil
}
