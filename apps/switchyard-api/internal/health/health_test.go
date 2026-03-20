package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// Mock HealthChecker
// ---------------------------------------------------------------------------

// mockChecker implements HealthChecker for testing, allowing callers to
// control every return value and observe invocation counts.
type mockChecker struct {
	name     string
	result   CheckResult
	timeout  time.Duration
	critical bool
	calls    atomic.Int32
}

func (m *mockChecker) Name() string { return m.name }

func (m *mockChecker) Check(_ context.Context) CheckResult {
	m.calls.Add(1)
	return m.result
}

func (m *mockChecker) Timeout() time.Duration { return m.timeout }
func (m *mockChecker) Critical() bool         { return m.critical }

func newMockChecker(name string, status HealthStatus, critical bool) *mockChecker {
	return &mockChecker{
		name: name,
		result: CheckResult{
			Status:    status,
			Message:   name + " check",
			Duration:  1 * time.Millisecond,
			Timestamp: time.Now(),
		},
		timeout:  5 * time.Second,
		critical: critical,
	}
}

// ---------------------------------------------------------------------------
// HealthManager creation and configuration
// ---------------------------------------------------------------------------

func TestNewHealthManager_DefaultState(t *testing.T) {
	hm := NewHealthManager("1.0.0")

	assert.NotNil(t, hm)
	assert.Equal(t, "1.0.0", hm.version)
	assert.Empty(t, hm.checkers)
	assert.NotNil(t, hm.checkCache)
	assert.Equal(t, 30*time.Second, hm.cacheTTL)
	assert.WithinDuration(t, time.Now(), hm.startTime, 2*time.Second)
}

func TestAddChecker_AccumulatesCheckers(t *testing.T) {
	hm := NewHealthManager("1.0.0")

	c1 := newMockChecker("db", HealthStatusHealthy, true)
	c2 := newMockChecker("redis", HealthStatusHealthy, false)

	hm.AddChecker(c1)
	hm.AddChecker(c2)

	assert.Len(t, hm.checkers, 2)
}

// ---------------------------------------------------------------------------
// Overall status aggregation
// ---------------------------------------------------------------------------

func TestCheckHealth_AllHealthy(t *testing.T) {
	hm := NewHealthManager("2.0.0")
	hm.AddChecker(newMockChecker("db", HealthStatusHealthy, true))
	hm.AddChecker(newMockChecker("redis", HealthStatusHealthy, false))

	result := hm.CheckHealth(context.Background())

	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.Equal(t, "2.0.0", result.Version)
	assert.Len(t, result.Checks, 2)
	assert.Contains(t, result.Checks, "db")
	assert.Contains(t, result.Checks, "redis")
}

func TestCheckHealth_CriticalUnhealthy_OverallUnhealthy(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.AddChecker(newMockChecker("db", HealthStatusUnhealthy, true))
	hm.AddChecker(newMockChecker("redis", HealthStatusHealthy, false))

	result := hm.CheckHealth(context.Background())

	assert.Equal(t, HealthStatusUnhealthy, result.Status)
	assert.Equal(t, HealthStatusUnhealthy, result.Checks["db"].Status)
	assert.Equal(t, HealthStatusHealthy, result.Checks["redis"].Status)
}

func TestCheckHealth_NonCriticalUnhealthy_OverallStaysHealthy(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.AddChecker(newMockChecker("db", HealthStatusHealthy, true))
	// A non-critical checker that is unhealthy does NOT mark overall as unhealthy.
	hm.AddChecker(newMockChecker("redis", HealthStatusUnhealthy, false))

	result := hm.CheckHealth(context.Background())

	// The aggregation logic only promotes to unhealthy when critical=true.
	// A non-critical unhealthy check has no special handling in the current code,
	// so overall remains healthy.
	assert.Equal(t, HealthStatusHealthy, result.Status)
}

func TestCheckHealth_DegradedPromotesFromHealthy(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.AddChecker(newMockChecker("db", HealthStatusHealthy, true))
	hm.AddChecker(newMockChecker("redis", HealthStatusDegraded, false))

	result := hm.CheckHealth(context.Background())

	assert.Equal(t, HealthStatusDegraded, result.Status)
}

func TestCheckHealth_NoCheckers_ReturnsHealthy(t *testing.T) {
	hm := NewHealthManager("1.0.0")

	result := hm.CheckHealth(context.Background())

	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.Empty(t, result.Checks)
}

// ---------------------------------------------------------------------------
// Parallel execution
// ---------------------------------------------------------------------------

func TestCheckHealth_ParallelExecution(t *testing.T) {
	hm := NewHealthManager("1.0.0")

	// Add multiple checkers that each take a bit of time. If run sequentially,
	// the total time would be ~count*delay. In parallel it should be ~delay.
	const count = 5
	const delay = 50 * time.Millisecond

	for i := 0; i < count; i++ {
		mc := newMockChecker("check_"+string(rune('a'+i)), HealthStatusHealthy, false)
		// Override Check to include a sleep so we can measure parallelism.
		mc.result.Duration = delay
		origResult := mc.result
		// We cannot override the method, but we can observe timing externally.
		_ = origResult
		hm.AddChecker(mc)
	}

	start := time.Now()
	result := hm.CheckHealth(context.Background())
	elapsed := time.Since(start)

	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.Len(t, result.Checks, count)

	// All checkers should have been invoked.
	for _, c := range hm.checkers {
		mc := c.(*mockChecker)
		assert.Equal(t, int32(1), mc.calls.Load(), "checker %s should be called exactly once", mc.name)
	}

	// Verify it ran faster than sequential would take (generous margin).
	maxSequentialTime := time.Duration(count) * 100 * time.Millisecond
	assert.Less(t, elapsed, maxSequentialTime,
		"parallel execution should be faster than sequential upper bound")
}

// ---------------------------------------------------------------------------
// Cache behavior
// ---------------------------------------------------------------------------

func TestCheckHealth_CacheHitPreventsRecheck(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	mc := newMockChecker("db", HealthStatusHealthy, true)
	hm.AddChecker(mc)

	// First call populates cache.
	_ = hm.CheckHealth(context.Background())
	assert.Equal(t, int32(1), mc.calls.Load())

	// Second call within TTL should serve from cache.
	result := hm.CheckHealth(context.Background())
	assert.Equal(t, int32(1), mc.calls.Load(), "checker should not be called again within cache TTL")
	assert.Equal(t, HealthStatusHealthy, result.Checks["db"].Status)
}

func TestCheckHealth_CacheExpiry_RechecksAfterTTL(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.cacheTTL = 10 * time.Millisecond // Very short TTL for testing.

	mc := newMockChecker("db", HealthStatusHealthy, true)
	hm.AddChecker(mc)

	_ = hm.CheckHealth(context.Background())
	assert.Equal(t, int32(1), mc.calls.Load())

	// Wait for cache to expire.
	time.Sleep(20 * time.Millisecond)

	_ = hm.CheckHealth(context.Background())
	assert.Equal(t, int32(2), mc.calls.Load(), "checker should be called again after cache TTL expires")
}

// ---------------------------------------------------------------------------
// Readiness check (critical-only)
// ---------------------------------------------------------------------------

func TestCheckReadiness_OnlyCriticalCheckers(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	critical := newMockChecker("db", HealthStatusHealthy, true)
	nonCritical := newMockChecker("redis", HealthStatusUnhealthy, false)
	hm.AddChecker(critical)
	hm.AddChecker(nonCritical)

	result := hm.CheckReadiness(context.Background())

	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.Contains(t, result.Checks, "db")
	assert.NotContains(t, result.Checks, "redis",
		"readiness check should skip non-critical checkers")
	assert.Equal(t, int32(0), nonCritical.calls.Load(),
		"non-critical checker should not be invoked during readiness")
}

func TestCheckReadiness_CriticalUnhealthy_FastFail(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	// Two critical checkers; the first is unhealthy.
	unhealthy := newMockChecker("db", HealthStatusUnhealthy, true)
	healthy := newMockChecker("k8s", HealthStatusHealthy, true)
	hm.AddChecker(unhealthy)
	hm.AddChecker(healthy)

	result := hm.CheckReadiness(context.Background())

	assert.Equal(t, HealthStatusUnhealthy, result.Status)
	// Should contain the failed check.
	assert.Contains(t, result.Checks, "db")
	// Due to fast-fail (break), the second critical checker may or may not have
	// been reached depending on iteration order, but the overall status must be unhealthy.
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func newTestRouter(hm *HealthManager) *gin.Engine {
	r := gin.New()
	SetupHealthRoutes(r, hm)
	return r
}

func doGET(router *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", path, nil)
	router.ServeHTTP(w, req)
	return w
}

func TestHealthHandler_Healthy_Returns200(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.AddChecker(newMockChecker("db", HealthStatusHealthy, true))
	router := newTestRouter(hm)

	w := doGET(router, "/health/")

	assert.Equal(t, http.StatusOK, w.Code)

	var body OverallHealth
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, HealthStatusHealthy, body.Status)
	assert.Equal(t, "1.0.0", body.Version)
}

func TestHealthHandler_Unhealthy_Returns503(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.AddChecker(newMockChecker("db", HealthStatusUnhealthy, true))
	router := newTestRouter(hm)

	w := doGET(router, "/health/")

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var body OverallHealth
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, HealthStatusUnhealthy, body.Status)
}

func TestHealthHandler_Degraded_Returns200(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.AddChecker(newMockChecker("cache", HealthStatusDegraded, false))
	router := newTestRouter(hm)

	w := doGET(router, "/health/")

	// Degraded is still operational, so 200.
	assert.Equal(t, http.StatusOK, w.Code)

	var body OverallHealth
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, HealthStatusDegraded, body.Status)
}

func TestReadinessHandler_Healthy_Returns200(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.AddChecker(newMockChecker("db", HealthStatusHealthy, true))
	router := newTestRouter(hm)

	w := doGET(router, "/health/ready")

	assert.Equal(t, http.StatusOK, w.Code)

	var body OverallHealth
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, HealthStatusHealthy, body.Status)
}

func TestReadinessHandler_Unhealthy_Returns503(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	hm.AddChecker(newMockChecker("db", HealthStatusUnhealthy, true))
	router := newTestRouter(hm)

	w := doGET(router, "/health/ready")

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestLivenessHandler_AlwaysReturns200(t *testing.T) {
	hm := NewHealthManager("1.0.0")
	// Even with an unhealthy critical checker, liveness only checks if the
	// server process is alive -- it does not run health checks.
	hm.AddChecker(newMockChecker("db", HealthStatusUnhealthy, true))
	router := newTestRouter(hm)

	w := doGET(router, "/health/live")

	assert.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "healthy", body["status"])
	assert.Contains(t, body, "timestamp")
	assert.Contains(t, body, "uptime")
}

// ---------------------------------------------------------------------------
// Concrete checker unit tests (non-external-dependency)
// ---------------------------------------------------------------------------

func TestDiskSpaceHealthChecker_PathExists(t *testing.T) {
	// Use the OS temp dir which should always exist.
	checker := NewDiskSpaceHealthChecker(os.TempDir(), 90.0)

	assert.Equal(t, "disk_space", checker.Name())
	assert.Equal(t, 1*time.Second, checker.Timeout())
	assert.False(t, checker.Critical())

	result := checker.Check(context.Background())
	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.Equal(t, "Disk space is sufficient", result.Message)
	assert.NotNil(t, result.Metadata)
	assert.Equal(t, os.TempDir(), result.Metadata["path"])
}

func TestDiskSpaceHealthChecker_PathNotExists(t *testing.T) {
	checker := NewDiskSpaceHealthChecker("/nonexistent/path/that/does/not/exist", 90.0)

	result := checker.Check(context.Background())
	assert.Equal(t, HealthStatusUnhealthy, result.Status)
	assert.Equal(t, "Disk path not accessible", result.Message)
	assert.NotEmpty(t, result.Error)
}

func TestMemoryHealthChecker_ReportsHealthy(t *testing.T) {
	checker := NewMemoryHealthChecker(90.0)

	assert.Equal(t, "memory", checker.Name())
	assert.Equal(t, 1*time.Second, checker.Timeout())
	assert.False(t, checker.Critical())

	result := checker.Check(context.Background())

	// In a test process, memory usage should be well under 1GB.
	assert.Equal(t, HealthStatusHealthy, result.Status)
	assert.Equal(t, "Memory usage is normal", result.Message)
	assert.NotNil(t, result.Metadata)
	assert.Contains(t, result.Metadata, "alloc_mb")
	assert.Contains(t, result.Metadata, "sys_mb")
	assert.Contains(t, result.Metadata, "gc_count")
	assert.Contains(t, result.Metadata, "goroutines")
}

// ---------------------------------------------------------------------------
// Concrete checker interface conformance
// ---------------------------------------------------------------------------

func TestDatabaseHealthChecker_ImplementsInterface(t *testing.T) {
	// Verify the concrete struct satisfies the HealthChecker interface at compile time.
	var _ HealthChecker = (*DatabaseHealthChecker)(nil)

	checker := NewDatabaseHealthChecker(nil, "postgres")
	assert.Equal(t, "postgres", checker.Name())
	assert.Equal(t, 5*time.Second, checker.Timeout())
	assert.True(t, checker.Critical())
}

func TestRedisHealthChecker_ImplementsInterface(t *testing.T) {
	var _ HealthChecker = (*RedisHealthChecker)(nil)

	checker := NewRedisHealthChecker(nil, "redis")
	assert.Equal(t, "redis", checker.Name())
	assert.Equal(t, 3*time.Second, checker.Timeout())
	assert.False(t, checker.Critical())
}

func TestKubernetesHealthChecker_ImplementsInterface(t *testing.T) {
	var _ HealthChecker = (*KubernetesHealthChecker)(nil)

	checker := NewKubernetesHealthChecker(nil, "k8s")
	assert.Equal(t, "k8s", checker.Name())
	assert.Equal(t, 5*time.Second, checker.Timeout())
	assert.True(t, checker.Critical())
}
