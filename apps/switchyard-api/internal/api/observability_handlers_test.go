package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// resetHealthCache clears the package-global cache + singleflight group so
// tests don't leak state between runs. Required because GetServiceHealth and
// computeServiceHealth use a process-global cache (intentionally — it's a
// per-replica cache for a fan-out result that's the same for all callers).
func resetHealthCache() {
	healthCacheMu.Lock()
	healthCache = nil
	healthCacheMu.Unlock()
	healthSF = singleflight.Group{}
}

// setupObservabilityTestHandler wires a Handler with sqlmock-backed Services,
// Projects, and Deployments repositories. k8sClient is left nil so the
// per-service K8s probe is skipped — mocking the K8s API is out of scope for
// these tests; we cover that surface in the k8s package directly.
func setupObservabilityTestHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	database, mock, err := sqlmock.New()
	require.NoError(t, err)

	h := &Handler{
		repos: &db.Repositories{
			Services:    db.NewServiceRepository(database),
			Projects:    db.NewProjectRepository(database),
			Deployments: db.NewDeploymentRepository(database),
		},
		logger: testLogger(t),
	}
	return h, mock, func() {
		_ = database.Close()
		resetHealthCache()
	}
}

// servicesListAllColumns matches ServiceRepository.ListAll's SELECT shape.
// k8s_namespace is included (the column was restored in the 2026-04-29 audit).
var servicesListAllColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config",
	"auto_deploy", "auto_deploy_branch", "auto_deploy_env", "k8s_namespace",
	"created_at", "updated_at", "jobs", "type", "region",
}

// TestGetServiceHealth_CacheHitSkipsRecompute pre-populates the package cache
// with a fresh entry and asserts the handler returns it directly without
// touching the database. This protects the fast-path that keeps the dashboard
// poll sub-millisecond on warm replicas.
func TestGetServiceHealth_CacheHitSkipsRecompute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupObservabilityTestHandler(t)
	defer cleanup()

	// Pre-populate cache with a known response. No DB expectations set, so
	// any DB call would fail the test via sqlmock's strict-by-default mode.
	resetHealthCache()
	healthCacheMu.Lock()
	healthCache = &healthCacheEntry{
		resp: ServiceHealthResponse{
			Services:    []ServiceHealth{{ServiceID: "cached-svc", Status: "healthy"}},
			HealthySvcs: 1,
			Timestamp:   time.Now(),
		},
		expiresAt: time.Now().Add(15 * time.Second),
	}
	healthCacheMu.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/v1/observability/health", nil)

	h.GetServiceHealth(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp ServiceHealthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.HealthySvcs)
	require.Len(t, resp.Services, 1)
	assert.Equal(t, "cached-svc", resp.Services[0].ServiceID)

	// No DB expectations were registered — if computeServiceHealth ran,
	// sqlmock would have logged unexpected query usage.
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetServiceHealth_ServiceIDFiltersCachedFleetResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupObservabilityTestHandler(t)
	defer cleanup()

	targetID := uuid.New().String()
	otherID := uuid.New().String()
	resetHealthCache()
	healthCacheMu.Lock()
	healthCache = &healthCacheEntry{
		resp: ServiceHealthResponse{
			Services: []ServiceHealth{
				{ServiceID: targetID, Status: "unhealthy"},
				{ServiceID: otherID, Status: "healthy"},
			},
			HealthySvcs:   1,
			UnhealthySvcs: 1,
			Timestamp:     time.Now(),
		},
		expiresAt: time.Now().Add(15 * time.Second),
	}
	healthCacheMu.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/v1/observability/health?service_id="+targetID, nil)

	h.GetServiceHealth(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp ServiceHealthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Services, 1)
	assert.Equal(t, targetID, resp.Services[0].ServiceID)
	assert.Equal(t, 0, resp.HealthySvcs)
	assert.Equal(t, 1, resp.UnhealthySvcs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetServiceHealth_RejectsInvalidServiceID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := setupObservabilityTestHandler(t)
	defer cleanup()
	resetHealthCache()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/v1/observability/health?service_id=not-a-uuid", nil)

	h.GetServiceHealth(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid service_id")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestComputeServiceHealth_HappyPath_NoK8s exercises the extracted recompute
// against a single service with no K8s client wired. It validates:
//
//  1. ListAll services row is correctly scanned
//  2. Projects.List is collapsed into a single round-trip (one ExpectQuery)
//  3. Deployments.GetLatestByService is invoked per service
//  4. Status is seeded from the latest deployment row (running → healthy)
//  5. Cache is populated on success
//  6. partial=false on a clean computation
func TestComputeServiceHealth_HappyPath_NoK8s(t *testing.T) {
	h, mock, cleanup := setupObservabilityTestHandler(t)
	defer cleanup()
	resetHealthCache()

	serviceID := uuid.New()
	projectID := uuid.New()
	releaseID := uuid.New()
	deploymentID := uuid.New()
	envID := uuid.New()
	now := time.Now()

	// 1. ServiceRepository.ListAll
	mock.ExpectQuery(`SELECT id, project_id, name, git_repo,.+FROM services ORDER BY created_at DESC`).
		WillReturnRows(sqlmock.NewRows(servicesListAllColumns).AddRow(
			serviceID, projectID, "api", "https://github.com/madfam-org/enclii", "",
			[]byte(`{"type":"dockerfile"}`),
			true, "main", "production", "default",
			now, now, []byte(`[]`), "web", "default",
		))

	// 2. ProjectRepository.List — single round-trip even with N services.
	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, "API", "api", "shared", now, now))

	// 3. Deployments.GetLatestByService — running status → healthy.
	mock.ExpectQuery(`SELECT d.id, d.release_id, d.environment_id, d.replicas, d.status, d.health.+FROM deployments d`).
		WithArgs(serviceID.String()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "release_id", "environment_id", "replicas", "status", "health", "error_message",
			"service_id", "version_number", "created_at", "updated_at",
		}).AddRow(
			deploymentID, releaseID, envID, 2, "running", "healthy", nil,
			serviceID, 1, now, now,
		))

	// 4. computeUptime → Deployments.GetByServiceSince. Status=healthy so
	// the uptime branch fires; we return zero rows (uptime 0% is fine —
	// the assertion below doesn't depend on the value).
	mock.ExpectQuery(`(?s)FROM deployments d\s+JOIN releases r ON d.release_id = r.id\s+WHERE r.service_id = \$1 AND d.created_at >= \$2`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "release_id", "environment_id", "replicas", "status", "health", "error_message",
			"service_id", "version_number", "created_at", "updated_at",
		}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, partial, err := h.computeServiceHealth(ctx)
	require.NoError(t, err)
	assert.False(t, partial, "no budget exceedance expected on happy path")
	require.Len(t, resp.Services, 1)

	got := resp.Services[0]
	assert.Equal(t, serviceID.String(), got.ServiceID)
	assert.Equal(t, "api", got.ServiceName)
	assert.Equal(t, "api", got.ProjectSlug, "project slug must come from the bulk-fetched projects map")
	assert.Equal(t, "healthy", got.Status, "running deployment must seed status=healthy")
	assert.Equal(t, 1, resp.HealthySvcs)

	// Cache must be populated on success.
	healthCacheMu.Lock()
	cached := healthCache
	healthCacheMu.Unlock()
	require.NotNil(t, cached, "cache must be populated after a successful recompute")
	assert.True(t, cached.expiresAt.After(time.Now()), "cache expiry must be in the future")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestComputeServiceHealthSkipsBuildOnlyServices(t *testing.T) {
	h, mock, cleanup := setupObservabilityTestHandler(t)
	defer cleanup()
	resetHealthCache()

	serviceID := uuid.New()
	projectID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, project_id, name, git_repo,.+FROM services ORDER BY created_at DESC`).
		WillReturnRows(sqlmock.NewRows(servicesListAllColumns).AddRow(
			serviceID, projectID, "api", "https://github.com/madfam-org/blueprint-harvester", "",
			[]byte(`{"type":"dockerfile","build_only":true}`),
			true, "main", "production", nil,
			now, now, []byte(`[]`), "web", "default",
		))

	mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).
			AddRow(projectID, "Blueprint Harvester", "blueprint-harvester", "shared", now, now))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, partial, err := h.computeServiceHealth(ctx)
	require.NoError(t, err)
	assert.False(t, partial)
	assert.Empty(t, resp.Services)
	assert.Equal(t, 0, resp.HealthySvcs)
	assert.Equal(t, 0, resp.DegradedSvcs)
	assert.Equal(t, 0, resp.UnhealthySvcs)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSingleflightGroup_CollapsesConcurrentCalls is a focused proof that
// singleflight.Group with a constant key collapses N concurrent callers into
// 1 underlying invocation. This is the same Group + key pattern
// GetServiceHealth uses to dedupe cache-miss recomputes; we test the
// primitive directly to keep the fixture tight (no DB, no K8s mocks) while
// still proving the contract the handler relies on.
//
// Models: 10 concurrent callers at the same cache-miss tick observe 1 fan-out.
func TestSingleflightGroup_CollapsesConcurrentCalls(t *testing.T) {
	var (
		sf      singleflight.Group
		invokes atomic.Int32
	)

	// Underlying work: simulates the 1.5–2s K8s fan-out we're protecting.
	// We sleep before incrementing so all racing goroutines have time to
	// queue behind the leader — without the sleep singleflight could
	// legitimately run the function twice if the first invocation finished
	// before the second goroutine even joined.
	work := func() (interface{}, error) {
		time.Sleep(50 * time.Millisecond)
		invokes.Add(1)
		return "done", nil
	}

	const callers = 10
	var wg sync.WaitGroup
	wg.Add(callers)
	results := make([]string, callers)

	for i := 0; i < callers; i++ {
		i := i
		go func() {
			defer wg.Done()
			v, err, _ := sf.Do("health", work)
			require.NoError(t, err)
			results[i] = v.(string)
		}()
	}
	wg.Wait()

	assert.EqualValues(t, 1, invokes.Load(), "10 concurrent callers must collapse to exactly 1 underlying invocation")
	for i, r := range results {
		assert.Equal(t, "done", r, "caller %d must observe the leader's result", i)
	}
}

// TestSingleflightGroup_ReruncesAfterCompletion verifies the corollary:
// once the in-flight call resolves, a subsequent caller triggers a fresh
// invocation. This is the property that lets the cache-TTL eviction work —
// we don't want singleflight to memoize forever, only to dedupe concurrent
// in-flight calls.
func TestSingleflightGroup_ReruncesAfterCompletion(t *testing.T) {
	var (
		sf      singleflight.Group
		invokes atomic.Int32
	)
	work := func() (interface{}, error) {
		invokes.Add(1)
		return invokes.Load(), nil
	}

	v1, _, _ := sf.Do("health", work)
	v2, _, _ := sf.Do("health", work)

	assert.EqualValues(t, 1, v1)
	assert.EqualValues(t, 2, v2)
	assert.EqualValues(t, 2, invokes.Load(), "sequential calls past completion must re-invoke")
}
