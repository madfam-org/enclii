package reconciler

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ----------------------------------------------------------------------
// Test doubles
// ----------------------------------------------------------------------

// fakeWorkloadLister returns a canned list of workloadRef values, useful
// for driving the discoverer through deterministic scenarios.
type fakeWorkloadLister struct {
	workloads []workloadRef
	err       error
}

func (f *fakeWorkloadLister) ListWorkloads(_ context.Context) ([]workloadRef, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.workloads, nil
}

// fakeServicesView records calls so tests can assert on them. It carries
// a snapshot list of services to return from ListAll.
type fakeServicesView struct {
	mu             sync.Mutex
	services       []*types.Service
	healthyCalls   []healthyCall
	zombieCalls    []uuid.UUID
	listAllErr     error
	markHealthyErr error
	markZombieErr  error
}

type healthyCall struct {
	ID              uuid.UUID
	ReplicasDesired int32
	ReplicasReady   int32
}

func (f *fakeServicesView) ListAll(_ context.Context) ([]*types.Service, error) {
	if f.listAllErr != nil {
		return nil, f.listAllErr
	}
	return f.services, nil
}

func (f *fakeServicesView) MarkReconciledHealthy(_ context.Context, id uuid.UUID, desired, ready int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healthyCalls = append(f.healthyCalls, healthyCall{ID: id, ReplicasDesired: desired, ReplicasReady: ready})
	return f.markHealthyErr
}

func (f *fakeServicesView) MarkReconciledZombie(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.zombieCalls = append(f.zombieCalls, id)
	return f.markZombieErr
}

// fakeOrphansView records Upsert and DeleteStale calls.
type fakeOrphansView struct {
	mu                sync.Mutex
	upserts           []*db.DiscoveredOrphan
	deleteStaleCutoff *time.Time
	upsertErr         error
	deleteStaleErr    error
	deleteStaleCount  int64
}

func (f *fakeOrphansView) Upsert(_ context.Context, o *db.DiscoveredOrphan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Defensive copy so tests can mutate the passed-in struct without
	// affecting the recorded value.
	cp := *o
	f.upserts = append(f.upserts, &cp)
	return f.upsertErr
}

func (f *fakeOrphansView) DeleteStale(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteStaleCutoff = &cutoff
	return f.deleteStaleCount, f.deleteStaleErr
}

// silentLogger returns a logrus.Logger writing to io.Discard. We do NOT
// want test runs to pollute stdout with reconciler INFO traffic.
func silentLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

// newDiscovererForTest constructs a NamespaceDiscoverer with stub state
// suitable for exercising reconcileWith directly.
func newDiscovererForTest() *NamespaceDiscoverer {
	return &NamespaceDiscoverer{
		logger:          silentLogger(),
		stopCh:          make(chan struct{}),
		lastZombieState: make(map[uuid.UUID]bool),
		knownOrphanKeys: make(map[string]struct{}),
	}
}

func ptrString(s string) *string { return &s }

// ----------------------------------------------------------------------
// Classification tests
// ----------------------------------------------------------------------

func TestNamespaceDiscoverer_HealthyMatch(t *testing.T) {
	svcID := uuid.New()
	svcs := &fakeServicesView{
		services: []*types.Service{{
			ID:           svcID,
			Name:         "phyndcrm-web",
			K8sNamespace: ptrString("phyndcrm"),
		}},
	}
	orphans := &fakeOrphansView{}

	lister := &fakeWorkloadLister{workloads: []workloadRef{{
		Namespace:       "phyndcrm",
		Name:            "phyndcrm-web",
		Kind:            kindDeployment,
		ServiceLabel:    "phyndcrm-web",
		Image:           "ghcr.io/madfam-org/phyndcrm-web@sha256:abc",
		ReplicasDesired: 2,
		ReplicasReady:   2,
	}}}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	assert.Len(t, svcs.healthyCalls, 1, "healthy match should mark service")
	assert.Equal(t, svcID, svcs.healthyCalls[0].ID)
	assert.Equal(t, int32(2), svcs.healthyCalls[0].ReplicasDesired)
	assert.Equal(t, int32(2), svcs.healthyCalls[0].ReplicasReady)
	assert.Empty(t, svcs.zombieCalls, "matched service must not be marked zombie")
	assert.Empty(t, orphans.upserts, "matched workload must not become an orphan")
}

func TestNamespaceDiscoverer_OrphanWorkload(t *testing.T) {
	svcs := &fakeServicesView{services: []*types.Service{}}
	orphans := &fakeOrphansView{}

	lister := &fakeWorkloadLister{workloads: []workloadRef{{
		Namespace:       "rondelio",
		Name:            "rondelio-api",
		Kind:            kindDeployment,
		ServiceLabel:    "rondelio-api", // labelled, but no service row
		Image:           "ghcr.io/madfam-org/rondelio-api@sha256:def",
		ReplicasDesired: 1,
		ReplicasReady:   1,
	}}}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	assert.Len(t, orphans.upserts, 1, "orphan workload must be upserted")
	assert.Equal(t, "rondelio", orphans.upserts[0].Namespace)
	assert.Equal(t, "rondelio-api", orphans.upserts[0].Name)
	assert.Equal(t, kindDeployment, orphans.upserts[0].Kind)
	assert.Empty(t, svcs.healthyCalls, "no service to mark healthy")
}

func TestNamespaceDiscoverer_UnlabeledWorkloadIgnored(t *testing.T) {
	svcs := &fakeServicesView{services: []*types.Service{}}
	orphans := &fakeOrphansView{}

	// A system DaemonSet with no enclii.dev/service label — out of scope.
	lister := &fakeWorkloadLister{workloads: []workloadRef{{
		Namespace:    "kube-system",
		Name:         "coredns",
		Kind:         kindDeployment,
		ServiceLabel: "", // critical: no label
		Image:        "registry.k8s.io/coredns/coredns:v1.11.1",
	}}}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	assert.Empty(t, orphans.upserts, "unlabeled workloads must NOT be tracked as orphans")
	assert.Empty(t, svcs.healthyCalls)
	assert.Empty(t, svcs.zombieCalls)
}

func TestNamespaceDiscoverer_ZombieService(t *testing.T) {
	svcID := uuid.New()
	svcs := &fakeServicesView{
		services: []*types.Service{{
			ID:           svcID,
			Name:         "ghosted-service",
			K8sNamespace: ptrString("ghosted"),
		}},
	}
	orphans := &fakeOrphansView{}

	// No workloads in cluster.
	lister := &fakeWorkloadLister{workloads: nil}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	assert.Len(t, svcs.zombieCalls, 1, "service with k8s_namespace and no workload must be zombie")
	assert.Equal(t, svcID, svcs.zombieCalls[0])
	assert.Empty(t, svcs.healthyCalls)
}

func TestNamespaceDiscoverer_UnreleasedServiceNotZombie(t *testing.T) {
	// A service with no k8s_namespace pinned (never deployed) should NOT
	// be classified as zombie — it's just unreleased.
	svcs := &fakeServicesView{
		services: []*types.Service{{
			ID:           uuid.New(),
			Name:         "never-deployed",
			K8sNamespace: nil, // critical: no namespace
		}},
	}
	orphans := &fakeOrphansView{}

	lister := &fakeWorkloadLister{workloads: nil}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	assert.Empty(t, svcs.zombieCalls, "unreleased service must not be zombie")
}

func TestNamespaceDiscoverer_Idempotent(t *testing.T) {
	// Running the reconciler twice in immediate succession must be a
	// no-op for the second run (assuming cluster state unchanged): the
	// fake repos accumulate calls but each call is idempotent at the
	// SQL layer (UPSERT, COALESCE).
	svcID := uuid.New()
	svcs := &fakeServicesView{
		services: []*types.Service{{
			ID:           svcID,
			Name:         "phyndcrm-web",
			K8sNamespace: ptrString("phyndcrm"),
		}},
	}
	orphans := &fakeOrphansView{}

	lister := &fakeWorkloadLister{workloads: []workloadRef{{
		Namespace:       "phyndcrm",
		Name:            "phyndcrm-web",
		Kind:            kindDeployment,
		ServiceLabel:    "phyndcrm-web",
		ReplicasDesired: 2,
		ReplicasReady:   2,
	}}}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	// Two passes → MarkReconciledHealthy called twice with identical args.
	// Both calls are observable; semantic idempotence is at the SQL UPDATE
	// layer (the second sets the same values). What we assert here is that
	// running again does NOT erroneously promote the service to zombie or
	// add stale orphans.
	assert.Len(t, svcs.healthyCalls, 2)
	assert.Empty(t, svcs.zombieCalls)
	assert.Empty(t, orphans.upserts)
}

func TestNamespaceDiscoverer_ZombieRecovery(t *testing.T) {
	// First pass: no workload → service marked zombie.
	// Second pass: workload reappears → service marked healthy.
	svcID := uuid.New()
	svc := &types.Service{
		ID:           svcID,
		Name:         "phyndcrm-web",
		K8sNamespace: ptrString("phyndcrm"),
	}
	svcs := &fakeServicesView{services: []*types.Service{svc}}
	orphans := &fakeOrphansView{}

	d := newDiscovererForTest()

	// Pass 1: no workloads
	d.reconcileWith(context.Background(), &fakeWorkloadLister{workloads: nil}, svcs, orphans)
	assert.Len(t, svcs.zombieCalls, 1)
	assert.Empty(t, svcs.healthyCalls)

	// Pass 2: workload appears
	lister := &fakeWorkloadLister{workloads: []workloadRef{{
		Namespace:    "phyndcrm",
		Name:         "phyndcrm-web",
		Kind:         kindDeployment,
		ServiceLabel: "phyndcrm-web",
	}}}
	d.reconcileWith(context.Background(), lister, svcs, orphans)
	assert.Len(t, svcs.healthyCalls, 1, "recovered service marked healthy")
	assert.Len(t, svcs.zombieCalls, 1, "no new zombie marking on recovery pass")
}

func TestNamespaceDiscoverer_StaleOrphansReaped(t *testing.T) {
	svcs := &fakeServicesView{services: nil}
	orphans := &fakeOrphansView{deleteStaleCount: 3}

	lister := &fakeWorkloadLister{workloads: nil}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	if assert.NotNil(t, orphans.deleteStaleCutoff, "DeleteStale must be called") {
		// Cutoff should be ~24h ago.
		expected := time.Now().Add(-orphanRetention)
		delta := orphans.deleteStaleCutoff.Sub(expected)
		assert.True(t, delta > -2*time.Second && delta < 2*time.Second,
			"DeleteStale cutoff must be ~24h ago, got delta %v", delta)
	}
}

func TestNamespaceDiscoverer_ListerError(t *testing.T) {
	// A lister failure must NOT mark services zombie or write orphans —
	// the discoverer just logs and bails out for this pass.
	svcs := &fakeServicesView{
		services: []*types.Service{{
			ID:           uuid.New(),
			Name:         "phyndcrm-web",
			K8sNamespace: ptrString("phyndcrm"),
		}},
	}
	orphans := &fakeOrphansView{}

	lister := &fakeWorkloadLister{err: errors.New("k8s api unavailable")}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	assert.Empty(t, svcs.zombieCalls, "lister failure must not promote zombies")
	assert.Empty(t, svcs.healthyCalls)
	assert.Empty(t, orphans.upserts)
}

func TestNamespaceDiscoverer_MultipleServicesSharedName(t *testing.T) {
	// Two services share name "api" across two projects. The discoverer
	// must pick the one whose k8s_namespace matches the workload's namespace.
	wantedID := uuid.New()
	svcs := &fakeServicesView{
		services: []*types.Service{
			{ID: uuid.New(), Name: "api", K8sNamespace: ptrString("project-a")},
			{ID: wantedID, Name: "api", K8sNamespace: ptrString("project-b")},
		},
	}
	orphans := &fakeOrphansView{}

	lister := &fakeWorkloadLister{workloads: []workloadRef{{
		Namespace:    "project-b",
		Name:         "api",
		Kind:         kindDeployment,
		ServiceLabel: "api",
	}}}

	d := newDiscovererForTest()
	d.reconcileWith(context.Background(), lister, svcs, orphans)

	if assert.Len(t, svcs.healthyCalls, 1) {
		assert.Equal(t, wantedID, svcs.healthyCalls[0].ID,
			"expected service in matching namespace to win")
	}
}

// ----------------------------------------------------------------------
// pickServiceForWorkload (helper)
// ----------------------------------------------------------------------

func TestPickServiceForWorkload(t *testing.T) {
	t.Run("empty candidates returns nil", func(t *testing.T) {
		assert.Nil(t, pickServiceForWorkload(nil, "ns"))
	})

	t.Run("namespace match wins", func(t *testing.T) {
		a := &types.Service{ID: uuid.New(), K8sNamespace: ptrString("ns-a")}
		b := &types.Service{ID: uuid.New(), K8sNamespace: ptrString("ns-b")}
		picked := pickServiceForWorkload([]*types.Service{a, b}, "ns-b")
		assert.Equal(t, b, picked)
	})

	t.Run("falls back to first when no namespace matches", func(t *testing.T) {
		a := &types.Service{ID: uuid.New(), K8sNamespace: ptrString("ns-a")}
		b := &types.Service{ID: uuid.New(), K8sNamespace: ptrString("ns-b")}
		picked := pickServiceForWorkload([]*types.Service{a, b}, "ns-z")
		assert.Equal(t, a, picked)
	})

	t.Run("nil k8s_namespace candidates handled", func(t *testing.T) {
		a := &types.Service{ID: uuid.New(), K8sNamespace: nil}
		picked := pickServiceForWorkload([]*types.Service{a}, "ns")
		assert.Equal(t, a, picked)
	})
}

func TestOrphanKey(t *testing.T) {
	assert.Equal(t, "ns/name/Deployment", orphanKey("ns", "name", "Deployment"))
}

// ----------------------------------------------------------------------
// NewNamespaceDiscoverer (env config)
// ----------------------------------------------------------------------

func TestNewNamespaceDiscoverer_DefaultInterval(t *testing.T) {
	t.Setenv(envDiscoveryInterval, "")
	d := NewNamespaceDiscoverer(nil, nil, silentLogger())
	assert.Equal(t, defaultNamespaceDiscoveryInterval, d.interval)
}

func TestNewNamespaceDiscoverer_OverrideInterval(t *testing.T) {
	t.Setenv(envDiscoveryInterval, "30s")
	d := NewNamespaceDiscoverer(nil, nil, silentLogger())
	assert.Equal(t, 30*time.Second, d.interval)
}

func TestNewNamespaceDiscoverer_InvalidInterval(t *testing.T) {
	t.Setenv(envDiscoveryInterval, "not-a-duration")
	d := NewNamespaceDiscoverer(nil, nil, silentLogger())
	assert.Equal(t, defaultNamespaceDiscoveryInterval, d.interval,
		"invalid duration should fall back to default")
}
