package monitoring

import (
	"net/http"
	"os"
	"strconv"
	"strings"
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

	// history holds this replica's own one-minute points, read from THIS
	// registry. Until 2026-11-04 (RFC 0049 Phase 1) a package init() read
	// prometheus.DefaultGatherer instead, where none of the enclii_* metrics
	// are registered, so every /observability chart was flat zero.
	history   *metricsHistoryBuffer
	scope     string
	startedAt time.Time
	prev      *historyCounters // guarded by history.mu
}

// historyCounters are the cumulative values at one sample; a history point is
// the difference between two of them.
type historyCounters struct {
	at           time.Time
	requests     float64
	serverErrors float64
	latencySum   float64
	latencyCount float64
	cpuSeconds   float64
	residentMB   float64
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
	metrics = append(metrics, ReconcilerMetricsCollectors()...)
	metrics = append(metrics, PgbouncerDriftMetricsCollectors()...)

	for _, metric := range metrics {
		registry.MustRegister(metric)
	}

	// Add Go runtime metrics
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	collector := &MetricsCollector{
		registry:  registry,
		history:   newMetricsHistoryBuffer(historyCapacity),
		scope:     replicaScope(),
		startedAt: time.Now(),
	}

	// Start background metrics collection
	go collector.collectSystemMetrics()
	go collector.collectHistory()

	return collector
}

// replicaScope names what the snapshot and history describe: one
// switchyard-api replica's own traffic and process, not the platform.
func replicaScope() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "switchyard-api replica"
	}
	return "switchyard-api replica " + host
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

// AutoRollbackErrorThreshold is the 5xx error rate threshold that triggers
// automatic deployment rollback. Deployments with error rates above this
// threshold are considered unhealthy.
const AutoRollbackErrorThreshold = 0.02 // 2% 5xx error rate

// CheckDeploymentHealth checks if a deployment is healthy based on HTTP error rate.
// Returns true if healthy (error rate <= threshold), false if unhealthy.
// The threshold is the AutoRollbackErrorThreshold (2%).
// Also returns the calculated error rate.
func CheckDeploymentHealth(registry *prometheus.Registry, namespace, service string) (bool, float64) {
	metricFamilies, err := registry.Gather()
	if err != nil {
		// If we cannot gather metrics, assume healthy (fail open)
		return true, 0
	}

	var totalRequests, serverErrors float64

	for _, mf := range metricFamilies {
		if mf.GetName() != "enclii_http_requests_total" {
			continue
		}

		for _, m := range mf.GetMetric() {
			val := m.GetCounter().GetValue()
			totalRequests += val

			for _, label := range m.GetLabel() {
				if label.GetName() == "status_code" && strings.HasPrefix(label.GetValue(), "5") {
					serverErrors += val
				}
			}
		}
	}

	if totalRequests == 0 {
		return true, 0
	}

	errorRate := serverErrors / totalRequests
	return errorRate <= AutoRollbackErrorThreshold, errorRate
}

// Metrics export for external monitoring systems
type MetricsSnapshot struct {
	Timestamp    time.Time         `json:"timestamp"`
	HTTPMetrics  HTTPMetrics       `json:"http"`
	DBMetrics    DatabaseMetrics   `json:"database"`
	CacheMetrics CacheMetrics      `json:"cache"`
	BuildMetrics BuildMetrics      `json:"builds"`
	K8sMetrics   KubernetesMetrics `json:"kubernetes"`
	// Scope and Since say what the numbers are: counters of ONE
	// switchyard-api replica, accumulated since it started. Requests go to
	// whichever replica the load balancer picks, so two reads can differ.
	Scope string     `json:"scope,omitempty"`
	Since *time.Time `json:"since,omitempty"`
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
	var dbQuerySum, dbQueryCount, cacheOpSum, cacheOpCount float64

	for _, mf := range metricFamilies {
		name := mf.GetName()
		metrics := mf.GetMetric()

		switch name {
		case "enclii_http_requests_total":
			for _, m := range metrics {
				val := m.GetCounter().GetValue()
				totalRequests += val
				if isServerErrorLabels(m.GetLabel()) {
					errorRequests += val
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
		case "enclii_db_query_duration_seconds":
			for _, m := range metrics {
				if h := m.GetHistogram(); h != nil {
					dbQuerySum += h.GetSampleSum()
					dbQueryCount += float64(h.GetSampleCount())
				}
			}
		case "enclii_cache_operation_duration_seconds":
			for _, m := range metrics {
				if h := m.GetHistogram(); h != nil {
					cacheOpSum += h.GetSampleSum()
					cacheOpCount += float64(h.GetSampleCount())
				}
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
	if dbQueryCount > 0 {
		snapshot.DBMetrics.AverageQueryTime = dbQuerySum / dbQueryCount
	}
	if cacheOpCount > 0 {
		snapshot.CacheMetrics.AverageLatency = cacheOpSum / cacheOpCount
	}
	if mc.history != nil {
		mc.history.mu.RLock()
		if n := len(mc.history.dataPoints); n > 0 {
			snapshot.HTTPMetrics.RequestsPerSecond = mc.history.dataPoints[n-1].RequestsPerSec
		}
		mc.history.mu.RUnlock()
	}
	snapshot.Scope = mc.scope
	if !mc.startedAt.IsZero() {
		since := mc.startedAt
		snapshot.Since = &since
	}

	return snapshot, nil
}

// labelPair is the accessor pair of a gathered metric's label.
type labelPair interface {
	GetName() string
	GetValue() string
}

// isServerErrorLabels reports a 5xx status_code label. 4xx are the caller's
// problem (expired sessions, missing objects) and are not counted as errors.
func isServerErrorLabels[L labelPair](labels []L) bool {
	for _, label := range labels {
		if label.GetName() == "status_code" {
			v := label.GetValue()
			return len(v) > 0 && v[0] == '5'
		}
	}
	return false
}

// MetricsHistory stores time-series data for metrics
type MetricsHistory struct {
	TimeRange  string             `json:"time_range"`
	Resolution string             `json:"resolution"`
	DataPoints []MetricsDataPoint `json:"data_points"`
	// Scope: these are ONE switchyard-api replica's own requests and
	// process, kept in memory for at most historyCapacity minutes and lost on
	// restart. They are not platform-wide and not other replicas'.
	Scope         string     `json:"scope"`
	RetainedSince *time.Time `json:"retained_since,omitempty"`
	Note          string     `json:"note,omitempty"`
}

// MetricsDataPoint is one minute of one replica. Every field is a rate or
// level over that minute, not a lifetime counter. Latency and error rate are
// null for a minute with no requests: there was nothing to measure, which is
// not the same as 0 ms or 0%.
type MetricsDataPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	RequestsPerSec   float64   `json:"requests_per_sec"`
	AverageLatencyMs *float64  `json:"average_latency_ms"`
	ErrorRate        *float64  `json:"error_rate"`   // share of 5xx responses
	CPUUsage         float64   `json:"cpu_usage"`    // cores, averaged over the minute
	MemoryUsage      float64   `json:"memory_usage"` // resident MB at the sample
}

// historyCapacity is 24 hours at one-minute resolution.
const historyCapacity = 1440

// In-memory metrics history buffer (ring buffer)
type metricsHistoryBuffer struct {
	dataPoints []MetricsDataPoint
	maxSize    int
	mu         sync.RWMutex
}

func newMetricsHistoryBuffer(size int) *metricsHistoryBuffer {
	return &metricsHistoryBuffer{dataPoints: make([]MetricsDataPoint, 0, size), maxSize: size}
}

func (mc *MetricsCollector) collectHistory() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	mc.recordHistoryPoint(time.Now())
	for now := range ticker.C {
		mc.recordHistoryPoint(now)
	}
}

// readCounters gathers this collector's registry into cumulative values.
func (mc *MetricsCollector) readCounters(now time.Time) (*historyCounters, error) {
	families, err := mc.registry.Gather()
	if err != nil {
		return nil, err
	}
	c := &historyCounters{at: now}
	for _, mf := range families {
		switch mf.GetName() {
		case "enclii_http_requests_total":
			for _, m := range mf.GetMetric() {
				v := m.GetCounter().GetValue()
				c.requests += v
				if isServerErrorLabels(m.GetLabel()) {
					c.serverErrors += v
				}
			}
		case "enclii_http_request_duration_seconds":
			for _, m := range mf.GetMetric() {
				if h := m.GetHistogram(); h != nil {
					c.latencySum += h.GetSampleSum()
					c.latencyCount += float64(h.GetSampleCount())
				}
			}
		case "process_cpu_seconds_total":
			for _, m := range mf.GetMetric() {
				c.cpuSeconds = m.GetCounter().GetValue()
			}
		case "process_resident_memory_bytes":
			for _, m := range mf.GetMetric() {
				c.residentMB = m.GetGauge().GetValue() / (1024 * 1024)
			}
		}
	}
	return c, nil
}

// recordHistoryPoint appends the minute since the previous sample. The first
// sample only sets the baseline: a point needs two readings to be a rate.
func (mc *MetricsCollector) recordHistoryPoint(now time.Time) {
	cur, err := mc.readCounters(now)
	if err != nil {
		return
	}
	mc.history.mu.Lock()
	defer mc.history.mu.Unlock()
	prev := mc.prev
	mc.prev = cur
	if prev == nil {
		return
	}
	dp, ok := historyPoint(prev, cur)
	if !ok {
		return
	}
	if len(mc.history.dataPoints) >= mc.history.maxSize {
		mc.history.dataPoints = mc.history.dataPoints[1:]
	}
	mc.history.dataPoints = append(mc.history.dataPoints, dp)
}

// historyPoint turns two cumulative readings into one interval point.
func historyPoint(prev, cur *historyCounters) (MetricsDataPoint, bool) {
	dt := cur.at.Sub(prev.at).Seconds()
	if dt <= 0 {
		return MetricsDataPoint{}, false
	}
	dReq := cur.requests - prev.requests
	if dReq < 0 {
		// A counter went backwards: not one process. Skip, never invent.
		return MetricsDataPoint{}, false
	}
	dp := MetricsDataPoint{
		Timestamp:      cur.at,
		RequestsPerSec: dReq / dt,
		CPUUsage:       (cur.cpuSeconds - prev.cpuSeconds) / dt,
		MemoryUsage:    cur.residentMB,
	}
	if dReq > 0 {
		errRate := (cur.serverErrors - prev.serverErrors) / dReq
		dp.ErrorRate = &errRate
	}
	if dCount := cur.latencyCount - prev.latencyCount; dCount > 0 {
		latency := (cur.latencySum - prev.latencySum) / dCount * 1000
		dp.AverageLatencyMs = &latency
	}
	return dp, true
}

// GetHistory returns metrics history for a given time range
func (mc *MetricsCollector) GetHistory(timeRange string) (*MetricsHistory, error) {
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

	history := &MetricsHistory{
		TimeRange:  timeRange,
		Resolution: resolution,
		Scope:      "switchyard-api replica",
	}
	if mc.history == nil {
		history.Note = "No history is kept by this replica."
		return history, nil
	}
	history.Scope = mc.scope

	mc.history.mu.RLock()
	defer mc.history.mu.RUnlock()

	for _, dp := range mc.history.dataPoints {
		if dp.Timestamp.After(since) {
			history.DataPoints = append(history.DataPoints, dp)
		}
	}
	retained := mc.startedAt
	if len(mc.history.dataPoints) > 0 {
		retained = mc.history.dataPoints[0].Timestamp
	}
	if !retained.IsZero() {
		history.RetainedSince = &retained
		if since.Before(retained) {
			history.Note = "This replica keeps at most 24 hours in memory, from its start; there are no points before " +
				retained.UTC().Format(time.RFC3339) + "."
		}
	}
	return history, nil
}
