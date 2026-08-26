package reconciler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
)

// ----------------------------------------------------------------------
// Test doubles
// ----------------------------------------------------------------------

// fakeUserlistDetector returns a canned drift/error, useful for driving the
// checker through deterministic scenarios without a live k8s clientset or
// Postgres connection. Guarded by a mutex because TestPgbouncerDriftChecker_
// StartStop exercises it from the checker's background goroutine while the
// test goroutine polls call state -- CI runs `go test -race`.
type fakeUserlistDetector struct {
	mu       sync.Mutex
	drift    provisioning.UserlistDrift
	err      error
	adminURL string // records the adminURL the checker passed through
	calls    int
}

func (f *fakeUserlistDetector) ReconcileUserlist(_ context.Context, adminURL string) (provisioning.UserlistDrift, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.adminURL = adminURL
	if f.err != nil {
		return provisioning.UserlistDrift{}, f.err
	}
	return f.drift, nil
}

func (f *fakeUserlistDetector) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeUserlistDetector) lastAdminURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.adminURL
}

// newCheckerForTest constructs a PgbouncerDriftChecker with stub state
// suitable for exercising check() directly, mirroring
// newDiscovererForTest in namespace_discoverer_test.go. interval is set to a
// short duration (rather than left at the zero value) because
// TestPgbouncerDriftChecker_StartStop drives the real Start() ticker loop;
// time.NewTicker panics on a non-positive duration, and check()-only tests
// never consult this field at all.
func newCheckerForTest(detector userlistDetector, adminURL string) *PgbouncerDriftChecker {
	return &PgbouncerDriftChecker{
		detector: detector,
		adminURL: adminURL,
		logger:   silentLogger(),
		interval: time.Millisecond,
		stopCh:   make(chan struct{}),
	}
}

// resetPgbouncerDriftMetrics zeroes the package-level collectors between
// tests so assertions on absolute gauge/counter values do not leak state
// across test functions (the collectors are process-global singletons).
func resetPgbouncerDriftMetrics() {
	monitoring.RecordPgbouncerUserlistCheckSuccess(0)
	// There is no public reset for the error counter (Prometheus counters are
	// monotonic by design) -- tests that assert on it use a before/after
	// delta instead, the same convention reconciler_metrics_test.go uses for
	// reconcilerWorkScheduledTotal.
}

// The pgbouncerUserlist* gauges/counter live as unexported vars in the
// monitoring package (same convention as reconciler_metrics.go), so this
// test file -- living in package reconciler -- cannot name them directly.
// monitoring.PgbouncerDriftMetricsCollectors() is the exported, order-stable
// seam this checker already uses to register them with the real Prometheus
// registry (see metrics.go); these three helpers reuse that same seam to
// read values back out via testutil.ToFloat64, exactly as the task asks
// ("Metric assertions via prometheus/client_golang's testutil"). The index
// order MUST stay in lockstep with PgbouncerDriftMetricsCollectors'
// declaration order in pgbouncer_drift_metrics.go: [0]=missing users gauge,
// [1]=check errors counter, [2]=last-check timestamp gauge.
func missingUsersGaugeValue() float64 {
	return testutil.ToFloat64(monitoring.PgbouncerDriftMetricsCollectors()[0])
}

func checkErrorsGaugeValue() float64 {
	return testutil.ToFloat64(monitoring.PgbouncerDriftMetricsCollectors()[1])
}

func lastCheckTimestampValue() float64 {
	return testutil.ToFloat64(monitoring.PgbouncerDriftMetricsCollectors()[2])
}

// ----------------------------------------------------------------------
// NewPgbouncerDriftChecker
// ----------------------------------------------------------------------

func TestNewPgbouncerDriftChecker(t *testing.T) {
	updater := provisioning.NewPgBouncerUpdater(nil, nil)
	c := NewPgbouncerDriftChecker(updater, "postgres://admin@host/db", silentLogger())
	assert.NotNil(t, c)
	assert.Equal(t, defaultPgbouncerDriftCheckInterval, c.interval)
	assert.NotNil(t, c.stopCh)
	assert.Equal(t, "postgres://admin@host/db", c.adminURL)
}

// TestNewPgbouncerDriftChecker_NilUpdaterIsTrueNilInterface guards against
// Go's typed-nil-in-interface gotcha: main.go passes a nil
// *provisioning.PgBouncerUpdater when the k8s client was unavailable at
// startup. Assigning that nil pointer directly into the userlistDetector
// interface field would make `c.detector == nil` evaluate to false (the
// interface holds a non-nil type descriptor around a nil value), so check()'s
// "detector not configured" guard would never fire and the first tick would
// instead panic on a nil-receiver call. This test fails loudly if that
// conversion in NewPgbouncerDriftChecker is ever removed.
func TestNewPgbouncerDriftChecker_NilUpdaterIsTrueNilInterface(t *testing.T) {
	var nilUpdater *provisioning.PgBouncerUpdater // explicitly nil, not omitted
	c := NewPgbouncerDriftChecker(nilUpdater, "postgres://admin@host/db", silentLogger())

	assert.Nil(t, c.detector, "a nil *PgBouncerUpdater must become a true nil interface, not a typed-nil one")

	resetPgbouncerDriftMetrics()
	errCountBefore := checkErrorsGaugeValue()

	// check() must hit the "detector not configured" branch and count an
	// error, NOT panic on a nil-receiver ReconcileUserlist call.
	assert.NotPanics(t, func() {
		c.check(context.Background())
	})
	assert.Equal(t, errCountBefore+1, checkErrorsGaugeValue())
}

// ----------------------------------------------------------------------
// check() — the testable core
// ----------------------------------------------------------------------

func TestPgbouncerDriftChecker_NoMissing(t *testing.T) {
	resetPgbouncerDriftMetrics()
	detector := &fakeUserlistDetector{drift: provisioning.UserlistDrift{}}
	c := newCheckerForTest(detector, "postgres://admin@host/db")

	c.check(context.Background())

	assert.Equal(t, 1, detector.callCount())
	assert.Equal(t, "postgres://admin@host/db", detector.lastAdminURL(), "checker must pass its configured adminURL through to the detector")
	assert.Equal(t, 0.0, missingUsersGaugeValue())
	assert.Greater(t, lastCheckTimestampValue(), 0.0, "timestamp gauge must update on a successful check")
}

func TestPgbouncerDriftChecker_MissingUsers(t *testing.T) {
	resetPgbouncerDriftMetrics()
	detector := &fakeUserlistDetector{
		drift: provisioning.UserlistDrift{
			MissingFromUserlist: []string{"fortuna", "bloom", "ceq", "autoswarm"},
		},
	}
	c := newCheckerForTest(detector, "postgres://admin@host/db")

	c.check(context.Background())

	assert.Equal(t, 4.0, missingUsersGaugeValue(), "gauge must report the exact count of missing login roles")
	assert.Greater(t, lastCheckTimestampValue(), 0.0, "a completed check (even with drift found) is still a successful check -- timestamp must update")
}

func TestPgbouncerDriftChecker_DetectorError(t *testing.T) {
	resetPgbouncerDriftMetrics()
	// Seed a known-good timestamp, then verify an erroring check does NOT
	// advance it -- the dead-man gauge must only move forward on success.
	monitoring.RecordPgbouncerUserlistCheckSuccess(0)
	before := lastCheckTimestampValue()
	errCountBefore := checkErrorsGaugeValue()

	detector := &fakeUserlistDetector{err: errors.New("connect to admin postgres: dial tcp: connection refused")}
	c := newCheckerForTest(detector, "postgres://admin@host/db")

	c.check(context.Background())

	assert.Equal(t, errCountBefore+1, checkErrorsGaugeValue(), "error counter must increment on detector failure")
	assert.Equal(t, before, lastCheckTimestampValue(), "timestamp gauge must NOT update on a failed check -- that is the dead-man property")
}

func TestPgbouncerDriftChecker_NilDetector(t *testing.T) {
	resetPgbouncerDriftMetrics()
	errCountBefore := checkErrorsGaugeValue()

	c := newCheckerForTest(nil, "postgres://admin@host/db")
	c.check(context.Background())

	assert.Equal(t, errCountBefore+1, checkErrorsGaugeValue(), "an unconfigured detector must count as an error, not silently no-op")
}

func TestPgbouncerDriftChecker_EmptyAdminURL(t *testing.T) {
	resetPgbouncerDriftMetrics()
	errCountBefore := checkErrorsGaugeValue()

	detector := &fakeUserlistDetector{}
	c := newCheckerForTest(detector, "")
	c.check(context.Background())

	assert.Equal(t, errCountBefore+1, checkErrorsGaugeValue(), "a missing admin URL must count as an error")
	assert.Equal(t, 0, detector.callCount(), "the detector must not be invoked when adminURL is empty -- it would only fail its own connection attempt")
}

// ----------------------------------------------------------------------
// safeCheck() — panic guard
// ----------------------------------------------------------------------

// panickingDetector always panics, to verify safeCheck's recover() keeps the
// checker loop alive rather than crashing the process -- the same property
// TimetableReconciler.safeReconcile guards.
type panickingDetector struct{}

func (panickingDetector) ReconcileUserlist(_ context.Context, _ string) (provisioning.UserlistDrift, error) {
	panic("simulated detector panic")
}

func TestPgbouncerDriftChecker_SafeCheckRecoversPanic(t *testing.T) {
	resetPgbouncerDriftMetrics()
	errCountBefore := checkErrorsGaugeValue()

	c := newCheckerForTest(panickingDetector{}, "postgres://admin@host/db")

	assert.NotPanics(t, func() {
		c.safeCheck(context.Background())
	}, "a panic inside check() must not escape safeCheck")

	assert.Equal(t, errCountBefore+1, checkErrorsGaugeValue(), "a recovered panic must still count as a check error")
}

// ----------------------------------------------------------------------
// Start/Stop lifecycle
// ----------------------------------------------------------------------

func TestPgbouncerDriftChecker_StartStop(t *testing.T) {
	resetPgbouncerDriftMetrics()
	detector := &fakeUserlistDetector{drift: provisioning.UserlistDrift{}}
	c := newCheckerForTest(detector, "postgres://admin@host/db")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx)
	// Start's immediate on-boot pass runs in the spawned goroutine, so give
	// it a moment to land before asserting -- avoids a fixed sleep by polling
	// the fake's call counter, which is safe here because fakeUserlistDetector
	// has no concurrent-write races beyond the single background goroutine.
	waitForCondition(t, func() bool { return detector.callCount() >= 1 })

	c.Stop()
	// Stop must be safe to call more than once.
	assert.NotPanics(t, func() { c.Stop() })
}

// waitForCondition polls cond every millisecond up to a short bound, failing
// the test if it never becomes true. Used instead of a fixed time.Sleep so
// the test is not flaky under load yet still terminates promptly.
func waitForCondition(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
