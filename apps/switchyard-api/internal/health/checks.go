package health

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cache"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

// HealthStatus represents the health status of a component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// CheckResult represents the result of a health check
type CheckResult struct {
	Status    HealthStatus           `json:"status"`
	Message   string                 `json:"message,omitempty"`
	Duration  time.Duration          `json:"duration"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// OverallHealth represents the overall system health
type OverallHealth struct {
	Status    HealthStatus           `json:"status"`
	Version   string                 `json:"version"`
	Uptime    time.Duration          `json:"uptime"`
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]CheckResult `json:"checks"`
}

// HealthChecker interface for individual health checks
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) CheckResult
	Timeout() time.Duration
	Critical() bool // If true, failure of this check marks overall status as unhealthy
}

// HealthManager manages all health checks
type HealthManager struct {
	checkers   []HealthChecker
	startTime  time.Time
	version    string
	checkCache map[string]CheckResult
	cacheMutex sync.RWMutex
	cacheTTL   time.Duration
}

// NewHealthManager creates a new health manager
func NewHealthManager(version string) *HealthManager {
	return &HealthManager{
		checkers:   make([]HealthChecker, 0),
		startTime:  time.Now(),
		version:    version,
		checkCache: make(map[string]CheckResult),
		cacheTTL:   30 * time.Second, // Cache results for 30 seconds
	}
}

// AddChecker adds a health checker
func (hm *HealthManager) AddChecker(checker HealthChecker) {
	hm.checkers = append(hm.checkers, checker)
}

// CheckHealth performs all health checks
func (hm *HealthManager) CheckHealth(ctx context.Context) OverallHealth {
	results := make(map[string]CheckResult)
	overallStatus := HealthStatusHealthy

	// Run all health checks in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, checker := range hm.checkers {
		wg.Add(1)
		go func(c HealthChecker) {
			defer wg.Done()

			// Check cache first
			if cachedResult := hm.getCachedResult(c.Name()); cachedResult != nil {
				mu.Lock()
				results[c.Name()] = *cachedResult
				mu.Unlock()
				return
			}

			// Create context with timeout
			checkCtx, cancel := context.WithTimeout(ctx, c.Timeout())
			defer cancel()

			result := c.Check(checkCtx)

			// Cache the result
			hm.setCachedResult(c.Name(), result)

			mu.Lock()
			results[c.Name()] = result

			// Update overall status
			if c.Critical() && result.Status == HealthStatusUnhealthy {
				overallStatus = HealthStatusUnhealthy
			} else if result.Status == HealthStatusDegraded && overallStatus == HealthStatusHealthy {
				overallStatus = HealthStatusDegraded
			}
			mu.Unlock()
		}(checker)
	}

	wg.Wait()

	return OverallHealth{
		Status:    overallStatus,
		Version:   hm.version,
		Uptime:    time.Since(hm.startTime),
		Timestamp: time.Now(),
		Checks:    results,
	}
}

// Quick readiness check (only critical components)
func (hm *HealthManager) CheckReadiness(ctx context.Context) OverallHealth {
	results := make(map[string]CheckResult)
	overallStatus := HealthStatusHealthy

	for _, checker := range hm.checkers {
		if !checker.Critical() {
			continue
		}

		checkCtx, cancel := context.WithTimeout(ctx, checker.Timeout())
		result := checker.Check(checkCtx)
		cancel()

		results[checker.Name()] = result

		if result.Status == HealthStatusUnhealthy {
			overallStatus = HealthStatusUnhealthy
			break // Fast fail for readiness
		}
	}

	return OverallHealth{
		Status:    overallStatus,
		Version:   hm.version,
		Uptime:    time.Since(hm.startTime),
		Timestamp: time.Now(),
		Checks:    results,
	}
}

// Cache management
func (hm *HealthManager) getCachedResult(name string) *CheckResult {
	hm.cacheMutex.RLock()
	defer hm.cacheMutex.RUnlock()

	if result, exists := hm.checkCache[name]; exists {
		if time.Since(result.Timestamp) < hm.cacheTTL {
			return &result
		}
	}

	return nil
}

func (hm *HealthManager) setCachedResult(name string, result CheckResult) {
	hm.cacheMutex.Lock()
	defer hm.cacheMutex.Unlock()

	hm.checkCache[name] = result
}

// HTTP handlers
func (hm *HealthManager) HealthHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		health := hm.CheckHealth(ctx)

		statusCode := http.StatusOK
		if health.Status == HealthStatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		} else if health.Status == HealthStatusDegraded {
			statusCode = http.StatusOK // Still operational
		}

		c.JSON(statusCode, health)
	}
}

func (hm *HealthManager) ReadinessHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		readiness := hm.CheckReadiness(ctx)

		statusCode := http.StatusOK
		if readiness.Status == HealthStatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		}

		c.JSON(statusCode, readiness)
	}
}

func (hm *HealthManager) LivenessHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Liveness is just "is the server responding?"
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"timestamp": time.Now(),
			"uptime":    time.Since(hm.startTime).String(),
		})
	}
}

// Specific health checkers
type DatabaseHealthChecker struct {
	db   *sql.DB
	name string
}

func NewDatabaseHealthChecker(db *sql.DB, name string) *DatabaseHealthChecker {
	return &DatabaseHealthChecker{
		db:   db,
		name: name,
	}
}

func (d *DatabaseHealthChecker) Name() string {
	return d.name
}

func (d *DatabaseHealthChecker) Check(ctx context.Context) CheckResult {
	start := time.Now()

	// Test basic connectivity
	if err := d.db.PingContext(ctx); err != nil {
		return CheckResult{
			Status:    HealthStatusUnhealthy,
			Message:   "Database ping failed",
			Duration:  time.Since(start),
			Timestamp: time.Now(),
			Error:     err.Error(),
		}
	}

	// Test with a query
	var version string
	if err := d.db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err != nil {
		return CheckResult{
			Status:    HealthStatusDegraded,
			Message:   "Database query failed",
			Duration:  time.Since(start),
			Timestamp: time.Now(),
			Error:     err.Error(),
		}
	}

	// Check connection pool stats
	stats := d.db.Stats()
	metadata := map[string]interface{}{
		"open_connections": stats.OpenConnections,
		"in_use":           stats.InUse,
		"idle":             stats.Idle,
		"version":          version[:50], // Truncate for brevity
	}

	status := HealthStatusHealthy
	message := "Database is healthy"

	// Check for potential issues
	if stats.WaitCount > 0 {
		status = HealthStatusDegraded
		message = fmt.Sprintf("Database has connection waits: %d", stats.WaitCount)
	}

	if stats.OpenConnections == stats.MaxOpenConnections {
		status = HealthStatusDegraded
		message = "Database connection pool at maximum capacity"
	}

	return CheckResult{
		Status:    status,
		Message:   message,
		Duration:  time.Since(start),
		Timestamp: time.Now(),
		Metadata:  metadata,
	}
}

func (d *DatabaseHealthChecker) Timeout() time.Duration {
	return 5 * time.Second
}

func (d *DatabaseHealthChecker) Critical() bool {
	return true
}

// Redis health checker
type RedisHealthChecker struct {
	cache cache.CacheService
	name  string
}

func NewRedisHealthChecker(cache cache.CacheService, name string) *RedisHealthChecker {
	return &RedisHealthChecker{
		cache: cache,
		name:  name,
	}
}

func (r *RedisHealthChecker) Name() string {
	return r.name
}

func (r *RedisHealthChecker) Check(ctx context.Context) CheckResult {
	start := time.Now()

	if err := r.cache.Ping(ctx); err != nil {
		return CheckResult{
			Status:    HealthStatusUnhealthy,
			Message:   "Redis ping failed",
			Duration:  time.Since(start),
			Timestamp: time.Now(),
			Error:     err.Error(),
		}
	}

	// Test set/get operation
	testKey := "health:check"
	testValue := time.Now().String()

	if err := r.cache.Set(ctx, testKey, testValue, 30*time.Second); err != nil {
		return CheckResult{
			Status:    HealthStatusDegraded,
			Message:   "Redis set operation failed",
			Duration:  time.Since(start),
			Timestamp: time.Now(),
			Error:     err.Error(),
		}
	}

	if _, err := r.cache.Get(ctx, testKey); err != nil {
		return CheckResult{
			Status:    HealthStatusDegraded,
			Message:   "Redis get operation failed",
			Duration:  time.Since(start),
			Timestamp: time.Now(),
			Error:     err.Error(),
		}
	}

	// Clean up test key
	_ = r.cache.Del(ctx, testKey)

	return CheckResult{
		Status:    HealthStatusHealthy,
		Message:   "Redis is healthy",
		Duration:  time.Since(start),
		Timestamp: time.Now(),
	}
}

func (r *RedisHealthChecker) Timeout() time.Duration {
	return 3 * time.Second
}

func (r *RedisHealthChecker) Critical() bool {
	return false // Redis is not critical for core functionality
}

// Kubernetes health checker
type KubernetesHealthChecker struct {
	client *k8s.Client
	name   string
}

func NewKubernetesHealthChecker(client *k8s.Client, name string) *KubernetesHealthChecker {
	return &KubernetesHealthChecker{
		client: client,
		name:   name,
	}
}

func (k *KubernetesHealthChecker) Name() string {
	return k.name
}

func (k *KubernetesHealthChecker) Check(ctx context.Context) CheckResult {
	start := time.Now()

	// Try to list pods in a test namespace
	pods, err := k.client.ListPods(ctx, "enclii-system", "")
	if err != nil {
		return CheckResult{
			Status:    HealthStatusUnhealthy,
			Message:   "Kubernetes API unreachable",
			Duration:  time.Since(start),
			Timestamp: time.Now(),
			Error:     err.Error(),
		}
	}

	metadata := map[string]interface{}{
		"pods_count": len(pods.Items),
	}

	return CheckResult{
		Status:    HealthStatusHealthy,
		Message:   "Kubernetes API is accessible",
		Duration:  time.Since(start),
		Timestamp: time.Now(),
		Metadata:  metadata,
	}
}

func (k *KubernetesHealthChecker) Timeout() time.Duration {
	return 5 * time.Second
}

func (k *KubernetesHealthChecker) Critical() bool {
	return true
}

// Disk space health checker
type DiskSpaceHealthChecker struct {
	path      string
	threshold float64 // percentage
}

func NewDiskSpaceHealthChecker(path string, threshold float64) *DiskSpaceHealthChecker {
	return &DiskSpaceHealthChecker{
		path:      path,
		threshold: threshold,
	}
}

func (d *DiskSpaceHealthChecker) Name() string {
	return "disk_space"
}

func (d *DiskSpaceHealthChecker) Check(ctx context.Context) CheckResult {
	start := time.Now()

	// This is a simplified check - in production you'd use syscalls to get disk stats
	// For now, we'll just check if the path exists
	if _, err := os.Stat(d.path); err != nil {
		return CheckResult{
			Status:    HealthStatusUnhealthy,
			Message:   "Disk path not accessible",
			Duration:  time.Since(start),
			Timestamp: time.Now(),
			Error:     err.Error(),
		}
	}

	// In a real implementation, you would calculate actual disk usage
	// For now, simulate healthy disk space
	return CheckResult{
		Status:    HealthStatusHealthy,
		Message:   "Disk space is sufficient",
		Duration:  time.Since(start),
		Timestamp: time.Now(),
		Metadata: map[string]interface{}{
			"path":      d.path,
			"threshold": d.threshold,
		},
	}
}

func (d *DiskSpaceHealthChecker) Timeout() time.Duration {
	return 1 * time.Second
}

func (d *DiskSpaceHealthChecker) Critical() bool {
	return false
}

// Memory usage health checker
type MemoryHealthChecker struct {
	threshold float64 // percentage
}

func NewMemoryHealthChecker(threshold float64) *MemoryHealthChecker {
	return &MemoryHealthChecker{
		threshold: threshold,
	}
}

func (m *MemoryHealthChecker) Name() string {
	return "memory"
}

func (m *MemoryHealthChecker) Check(ctx context.Context) CheckResult {
	start := time.Now()

	// Get memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Calculate memory usage percentage (simplified)
	usedMB := float64(memStats.Alloc) / 1024 / 1024
	sysMB := float64(memStats.Sys) / 1024 / 1024

	metadata := map[string]interface{}{
		"alloc_mb":   usedMB,
		"sys_mb":     sysMB,
		"gc_count":   memStats.NumGC,
		"goroutines": runtime.NumGoroutine(),
	}

	status := HealthStatusHealthy
	message := "Memory usage is normal"

	// Simple check - in production you'd compare against system memory
	if usedMB > 1000 { // More than 1GB allocated
		status = HealthStatusDegraded
		message = fmt.Sprintf("High memory usage: %.2f MB", usedMB)
	}

	return CheckResult{
		Status:    status,
		Message:   message,
		Duration:  time.Since(start),
		Timestamp: time.Now(),
		Metadata:  metadata,
	}
}

func (m *MemoryHealthChecker) Timeout() time.Duration {
	return 1 * time.Second
}

func (m *MemoryHealthChecker) Critical() bool {
	return false
}

// Setup routes for health checks
func SetupHealthRoutes(router *gin.Engine, manager *HealthManager) {
	health := router.Group("/health")
	{
		health.GET("/", manager.HealthHandler())
		health.GET("/live", manager.LivenessHandler())
		health.GET("/ready", manager.ReadinessHandler())
	}
}
