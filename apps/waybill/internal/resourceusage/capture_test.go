package resourceusage

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

// fakeSeries yields a value per slot of whichever civil day a timestamp
// belongs to, so one scenario serves any day.
type fakeSeries struct {
	labels map[string]string
	value  func(slot int) (float64, bool)
}

type fakeProm struct {
	loc     *time.Location
	ranges  map[string][]fakeSeries
	instant func(q string, at time.Time) ([]Series, error)

	mu       sync.Mutex
	windows  map[window]int
	instants []time.Time
}

func (f *fakeProm) QueryRange(_ context.Context, q string, start, end time.Time, step time.Duration) ([]Series, error) {
	f.mu.Lock()
	if f.windows == nil {
		f.windows = map[window]int{}
	}
	f.windows[window{start, end}]++
	f.mu.Unlock()
	if step != Step {
		return nil, errors.New("unexpected step")
	}
	var out []Series
	for _, fs := range f.ranges[q] {
		s := Series{Labels: fs.labels}
		for ts := start; !ts.After(end); ts = ts.Add(step) {
			d := DayOf(ts.Add(-time.Second), f.loc)
			if v, ok := fs.value(d.SlotIndex(ts)); ok {
				s.Points = append(s.Points, Point{T: ts, V: v})
			}
		}
		if len(s.Points) > 0 {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeProm) Query(_ context.Context, q string, at time.Time) ([]Series, error) {
	f.mu.Lock()
	f.instants = append(f.instants, at)
	f.mu.Unlock()
	return f.instant(q, at)
}

func always(v float64) func(int) (float64, bool) {
	return func(int) (float64, bool) { return v, true }
}

// between is present for slots [from, to).
func between(from, to int, v float64) func(int) (float64, bool) {
	return func(i int) (float64, bool) { return v, i >= from && i < to }
}

func ctr(ns, pod, container, node string) map[string]string {
	return map[string]string{"namespace": ns, "pod": pod, "container": container, "node": node}
}

func nodeLabels(node string) map[string]string { return map[string]string{"node": node} }

const mib = 1 << 20

// scenario: one civil day of a small cluster (see TestCaptureDay_Scenario).
func scenario(t *testing.T) (*fakeProm, Config) {
	t.Helper()
	loc := cdmx(t)
	q := testQueries
	ramp := func(i int) (float64, bool) { return 0.1 * float64(i+1), i >= 0 && i < 12 }
	f := &fakeProm{
		loc: loc,
		ranges: map[string][]fakeSeries{
			q.Container(CPUUsed): {
				{ctr("shop", "api-7d9-a", "app", "node-a"), always(0.5)},
				{ctr("shop", "api-7d9-b", "app", "node-b"), between(0, 144, 0.25)},
				{ctr("shop", "nightly-1-x", "job", "node-a"), ramp},
				{ctr("shop", "debug", "shell", "node-a"), between(100, 110, 0.01)},
				{ctr("arc-runners", "runner-abc", "runner", "node-b"), between(0, 24, 2)},
				{ctr("arc-runners", "runner-def", "runner", "node-b"), between(24, 48, 2)},
			},
			q.Container(MemUsed): {
				{ctr("shop", "api-7d9-a", "app", "node-a"), always(512 * mib)},
				{ctr("shop", "api-7d9-b", "app", "node-b"), between(0, 144, 256*mib)},
			},
			q.Container(CPURequest): {
				{ctr("shop", "api-7d9-a", "app", "node-a"), always(1)},
				{ctr("shop", "api-7d9-b", "app", "node-b"), between(0, 144, 1)},
				{ctr("shop", "nightly-1-x", "job", "node-a"), between(0, 12, 0.5)},
				{ctr("shop", "db-0", "postgres", "node-b"), always(0.2)},
			},
			q.Container(CPULimit): {
				{ctr("shop", "api-7d9-a", "app", "node-a"), always(2)},
			},
			q.Container(MemRequest): {
				{ctr("shop", "api-7d9-a", "app", "node-a"), always(gib)},
			},
			q.Container(MemLimit): {
				{ctr("shop", "api-7d9-a", "app", "node-a"), always(2 * gib)},
			},
			q.Node(NodeCPUUsed): {
				{nodeLabels("node-a"), always(3)},
				{nodeLabels("node-b"), between(0, 276, 2)},
			},
			q.Node(NodeMemUsed): {
				{nodeLabels("node-a"), always(10 * gib)},
			},
			q.Node(NodeCPUAllocatable): {
				{nodeLabels("node-a"), always(8)},
				{nodeLabels("node-b"), always(8)},
			},
			q.Node(NodeMemAllocatable): {
				{nodeLabels("node-a"), always(32 * gib)},
			},
		},
	}
	f.instant = func(query string, at time.Time) ([]Series, error) {
		d := DayOf(at.Add(-time.Second), loc)
		switch query {
		case q.Probe(d):
			return []Series{{Labels: map[string]string{}, Points: []Point{{T: at, V: 2}}}}, nil
		case q.PodOwners(d):
			return []Series{
				owner("shop", "pod", "api-7d9-a", "ReplicaSet", "api-7d9"),
				owner("shop", "pod", "api-7d9-b", "ReplicaSet", "api-7d9"),
				owner("shop", "pod", "nightly-1-x", "Job", "nightly-1"),
				owner("shop", "pod", "db-0", "StatefulSet", "db"),
				owner("shop", "pod", "debug", "<none>", "<none>"),
				owner("arc-runners", "pod", "runner-abc", "EphemeralRunner", "runner-abc"),
				owner("arc-runners", "pod", "runner-def", "EphemeralRunner", "runner-def"),
			}, nil
		case q.ReplicaSetOwners(d):
			return []Series{owner("shop", "replicaset", "api-7d9", "Deployment", "api")}, nil
		case q.JobOwners(d):
			return []Series{owner("shop", "job_name", "nightly-1", "CronJob", "nightly")}, nil
		}
		t.Errorf("unexpected instant query %s", query)
		return nil, nil
	}
	cfg := Config{
		PrometheusURL: "http://prometheus.test:9090", Cluster: "test-cluster", Location: loc,
		LookbackDays: 3, Settle: 15 * time.Minute, QueryTimeout: time.Minute, ChunkHours: 6,
		RunnerNamespaces: []string{"arc-runners"}, CadvisorJob: q.CadvisorJob, KSMJob: q.KSMJob,
	}
	return f, cfg
}

func near(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = NULL, want %v", what, want)
		return
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", what, *got, want)
	}
}

func isNull(t *testing.T, what string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, want NULL (not measured)", what, *got)
	}
}

func findRow(t *testing.T, c Capture, node, ns, kind, name, container string) WorkloadRow {
	t.Helper()
	for _, r := range c.Workloads {
		if r.Node == node && r.Namespace == ns && r.WorkloadKind == kind && r.WorkloadName == name && r.Container == container {
			return r
		}
	}
	t.Fatalf("no row %s %s/%s/%s %s in %d rows", node, ns, kind, name, container, len(c.Workloads))
	return WorkloadRow{}
}

func TestCaptureDay_Scenario(t *testing.T) {
	f, cfg := scenario(t)
	d, _ := ParseDay("2026-11-04", cfg.Location)
	c, err := NewCapturer(f, nil, cfg, nil).CaptureDay(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if c.Date != "2026-11-04" || c.Cluster != "test-cluster" || c.Source != "prometheus" {
		t.Errorf("capture header = %s %s %s", c.Date, c.Cluster, c.Source)
	}
	if c.UsageCoverage != 1 || c.RequestsCoverage != 1 {
		t.Errorf("coverage = %v / %v, want 1 / 1", c.UsageCoverage, c.RequestsCoverage)
	}
	if len(c.Workloads) != 6 {
		t.Errorf("%d workload rows, want 6", len(c.Workloads))
	}

	// A full-day replica: 0.5 cores x 24h, 1 core requested x 24h.
	a := findRow(t, c, "node-a", "shop", "Deployment", "api", "app")
	near(t, "api@a cpu used", a.CPUCoreHoursUsed, 12)
	near(t, "api@a cpu requested", a.CPUCoreHoursRequested, 24)
	near(t, "api@a cpu limit", a.CPULimitCoreHours, 48)
	near(t, "api@a mem used", a.MemGiBHoursUsed, 12)
	near(t, "api@a mem requested", a.MemGiBHoursRequested, 24)
	near(t, "api@a mem limit", a.MemLimitGiBHours, 48)
	near(t, "api@a p95", a.CPUCoresP95, 0.5)
	if a.Coverage != 1 || a.SampleCount != 288 || a.PodCount != 1 {
		t.Errorf("api@a coverage=%v samples=%d pods=%d", a.Coverage, a.SampleCount, a.PodCount)
	}
	if a.MemWorkingSetMaxBytes == nil || *a.MemWorkingSetMaxBytes != 512*mib {
		t.Errorf("api@a mem max = %v", a.MemWorkingSetMaxBytes)
	}

	// The other replica ran half the day on another node: its own row, half
	// coverage, and no memory request series means NULL, not zero.
	b := findRow(t, c, "node-b", "shop", "Deployment", "api", "app")
	near(t, "api@b cpu used", b.CPUCoreHoursUsed, 3)
	near(t, "api@b cpu requested", b.CPUCoreHoursRequested, 12)
	isNull(t, "api@b mem requested", b.MemGiBHoursRequested)
	isNull(t, "api@b cpu limit", b.CPULimitCoreHours)
	if b.Coverage != 0.5 || b.SampleCount != 144 {
		t.Errorf("api@b coverage=%v samples=%d", b.Coverage, b.SampleCount)
	}

	// CronJob pods roll up to the CronJob; p95 interpolates like Prometheus.
	cj := findRow(t, c, "node-a", "shop", "CronJob", "nightly", "job")
	near(t, "nightly cpu used", cj.CPUCoreHoursUsed, 7.8/12)
	near(t, "nightly cpu requested", cj.CPUCoreHoursRequested, 0.5)
	near(t, "nightly p95", cj.CPUCoresP95, 1.145)
	near(t, "nightly max", cj.CPUCoresMax, 1.2)
	isNull(t, "nightly mem used", cj.MemGiBHoursUsed)

	// Requests without a single usage sample: requested is real, used is
	// NULL, coverage 0.
	db := findRow(t, c, "node-b", "shop", "StatefulSet", "db", "postgres")
	near(t, "db cpu requested", db.CPUCoreHoursRequested, 4.8)
	isNull(t, "db cpu used", db.CPUCoreHoursUsed)
	isNull(t, "db p95", db.CPUCoresP95)
	if db.Coverage != 0 || db.SampleCount != 0 || db.PodCount != 1 {
		t.Errorf("db coverage=%v samples=%d pods=%d", db.Coverage, db.SampleCount, db.PodCount)
	}

	findRow(t, c, "node-a", "shop", KindPod, "debug", "shell")

	// CI runners: one namespace row per node and container, not one per runner.
	r := findRow(t, c, "node-b", "arc-runners", KindNamespace, "arc-runners", "runner")
	near(t, "runners cpu used", r.CPUCoreHoursUsed, 8)
	if r.PodCount != 2 || r.SampleCount != 48 || math.Abs(r.Coverage-48.0/288) > 1e-12 {
		t.Errorf("runners pods=%d samples=%d coverage=%v", r.PodCount, r.SampleCount, r.Coverage)
	}

	if len(c.Nodes) != 2 {
		t.Fatalf("%d node rows, want 2", len(c.Nodes))
	}
	na, nb := c.Nodes[0], c.Nodes[1]
	near(t, "node-a used", na.CPUCoreHoursUsed, 72)
	near(t, "node-a allocatable", na.CPUAllocatableCoreHours, 192)
	near(t, "node-a mem used", na.MemGiBHoursUsed, 240)
	near(t, "node-a mem allocatable", na.MemAllocatableGiBHours, 768)
	near(t, "node-a p95", na.CPUCoresP95, 3)
	if na.Coverage != 1 || na.PodCount != 3 {
		t.Errorf("node-a coverage=%v pods=%d", na.Coverage, na.PodCount)
	}
	// node-b's root cgroup stopped reporting for the last hour: coverage says
	// so, and the hour is not zero-filled.
	near(t, "node-b used", nb.CPUCoreHoursUsed, 46)
	isNull(t, "node-b mem used", nb.MemGiBHoursUsed)
	if math.Abs(nb.Coverage-276.0/288) > 1e-12 || nb.SampleCount != 276 || nb.PodCount != 4 {
		t.Errorf("node-b coverage=%v samples=%d pods=%d", nb.Coverage, nb.SampleCount, nb.PodCount)
	}

	// Totals reconcile: a node's requested hours are the sum of its rows.
	for _, n := range c.Nodes {
		var sum float64
		for _, w := range c.Workloads {
			if w.Node == n.Node && w.CPUCoreHoursRequested != nil {
				sum += *w.CPUCoreHoursRequested
			}
		}
		near(t, n.Node+" requested = sum of its rows", n.CPUCoreHoursRequested, sum)
	}
	near(t, "node-a requested", na.CPUCoreHoursRequested, 24.5)
	near(t, "node-b requested", nb.CPUCoreHoursRequested, 16.8)

	// Four 6-hour windows, each read once per measure.
	if len(f.windows) != 4 {
		t.Errorf("%d distinct windows, want 4", len(f.windows))
	}
	for w, n := range f.windows {
		if n != int(numContainerMeasures)+int(numNodeMeasures) {
			t.Errorf("window %s..%s read %d times", w.start.UTC(), w.end.UTC(), n)
		}
	}
}

// Prometheus holding nothing for a day is "nothing measured", not a day of
// zeros: no rows, and the run does not mark the day captured.
func TestCaptureDay_EmptyWhenPrometheusHoldsNothing(t *testing.T) {
	f, cfg := scenario(t)
	f.instant = func(string, time.Time) ([]Series, error) { return nil, nil }
	d, _ := ParseDay("2026-10-20", cfg.Location)
	c, err := NewCapturer(f, nil, cfg, nil).CaptureDay(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Empty() || len(c.Workloads) != 0 || len(c.Nodes) != 0 {
		t.Errorf("capture = %+v, want empty", c)
	}
	if len(f.windows) != 0 {
		t.Error("an empty probe must not run the range queries")
	}
}

func TestCaptureDay_PrometheusErrorFailsTheDay(t *testing.T) {
	f, cfg := scenario(t)
	f.instant = func(string, time.Time) ([]Series, error) { return nil, errors.New("connection refused") }
	d, _ := ParseDay("2026-11-04", cfg.Location)
	if _, err := NewCapturer(f, nil, cfg, nil).CaptureDay(context.Background(), d); err == nil {
		t.Fatal("expected an error")
	}
}

func TestQuantile_MatchesPrometheusInterpolation(t *testing.T) {
	vals := []float64{1, 2, 3, 4}
	for q, want := range map[float64]float64{0: 1, 1: 4, 0.5: 2.5, 0.95: 3.85} {
		if got := quantile(q, vals); math.Abs(got-want) > 1e-12 {
			t.Errorf("quantile(%v) = %v, want %v", q, got, want)
		}
	}
	if !math.IsNaN(quantile(0.95, nil)) {
		t.Error("quantile of nothing must be NaN (not measured)")
	}
}
