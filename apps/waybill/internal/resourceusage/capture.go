package resourceusage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
)

var tracer = otel.Tracer("waybill/resourceusage")

// Source is the provenance written on every row.
const Source = "prometheus"

// DayStore is the part of Store the capturer needs.
type DayStore interface {
	CapturedDates(ctx context.Context, cluster, from, to string) (map[string]bool, error)
	Save(ctx context.Context, c Capture) error
}

// Capturer reads complete days from Prometheus and saves them.
type Capturer struct {
	q       Querier
	store   DayStore
	cfg     Config
	queries Queries
	logger  *zap.Logger
	now     func() time.Time
	mu      sync.Mutex
}

// NewCapturer returns a capturer. cfg.Location must be set (LoadConfig does).
func NewCapturer(q Querier, store DayStore, cfg Config, logger *zap.Logger) *Capturer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Capturer{
		q: q, store: store, cfg: cfg, logger: logger, now: time.Now,
		queries: Queries{CadvisorJob: cfg.CadvisorJob, KSMJob: cfg.KSMJob},
	}
}

// RunResult summarizes one RunMissing call.
type RunResult struct {
	Checked  int      // complete days in the lookback window
	Captured []string // days written this run
	Empty    []string // days Prometheus held nothing for (not recorded)
	Failed   []string // days that errored (retried on the next run)
	Skipped  bool     // another run was in progress
}

// ErrDayFailed is returned when at least one day failed; the others were
// still attempted.
var ErrDayFailed = errors.New("resource usage capture failed for at least one day")

// RunMissing captures every complete day in the lookback window that has no
// capture record yet, oldest first: the oldest is the next one Prometheus
// will drop. It is the startup backfill and the scheduled run alike; with
// nothing missing it costs one database read. Concurrent calls do not
// overlap: a call made while another runs returns Skipped.
func (c *Capturer) RunMissing(ctx context.Context) (RunResult, error) {
	if !c.mu.TryLock() {
		return RunResult{Skipped: true}, nil
	}
	defer c.mu.Unlock()

	days := CompleteDays(c.now(), c.cfg.Location, c.cfg.LookbackDays, c.cfg.Settle)
	res := RunResult{Checked: len(days)}
	if len(days) == 0 {
		return res, nil
	}
	captured, err := c.store.CapturedDates(ctx, c.cfg.Cluster, days[0].Date, days[len(days)-1].Date)
	if err != nil {
		return res, err
	}

	for _, d := range days {
		if captured[d.Date] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return res, err
		}
		capture, err := c.CaptureDay(ctx, d)
		if err == nil && !capture.Empty() {
			err = c.store.Save(ctx, capture)
		}
		switch {
		case err != nil:
			res.Failed = append(res.Failed, d.Date)
			c.logger.Error("resource usage capture failed; retried on the next run",
				zap.String("day", d.Date), zap.Error(err))
		case capture.Empty():
			res.Empty = append(res.Empty, d.Date)
			c.logger.Info("Prometheus holds no samples for this day; nothing recorded",
				zap.String("day", d.Date))
		default:
			res.Captured = append(res.Captured, d.Date)
			c.logger.Info("resource usage day captured",
				zap.String("day", d.Date),
				zap.String("cluster", capture.Cluster),
				zap.Int("workload_rows", len(capture.Workloads)),
				zap.Int("node_rows", len(capture.Nodes)),
				zap.Float64("usage_coverage", capture.UsageCoverage),
				zap.Float64("requests_coverage", capture.RequestsCoverage),
			)
		}
	}
	if len(res.Failed) > 0 {
		return res, ErrDayFailed
	}
	return res, nil
}

// CaptureDay reads one civil day from Prometheus. It writes nothing.
func (c *Capturer) CaptureDay(ctx context.Context, d Day) (Capture, error) {
	ctx, span := tracer.Start(ctx, "resourceusage.capture_day")
	defer span.End()
	span.SetAttributes(attribute.String("resourceusage.day", d.Date))

	capture, err := c.captureDay(ctx, d)
	if err != nil {
		span.SetStatus(codes.Error, "capture day")
		span.RecordError(err)
		return Capture{}, err
	}
	span.SetAttributes(
		attribute.Int("resourceusage.workload_rows", len(capture.Workloads)),
		attribute.Float64("resourceusage.usage_coverage", capture.UsageCoverage),
	)
	return capture, nil
}

func (c *Capturer) captureDay(ctx context.Context, d Day) (Capture, error) {
	empty := Capture{Date: d.Date, Cluster: c.cfg.Cluster, Source: Source}

	probe, err := c.q.Query(ctx, c.queries.Probe(d), d.End)
	if err != nil {
		return Capture{}, fmt.Errorf("probe %s: %w", d.Date, err)
	}
	if !anyPositive(probe) {
		return empty, nil
	}

	var owners [3][]Series
	for i, query := range []string{c.queries.PodOwners(d), c.queries.ReplicaSetOwners(d), c.queries.JobOwners(d)} {
		if owners[i], err = c.q.Query(ctx, query, d.End); err != nil {
			return Capture{}, fmt.Errorf("owners %s: %w", d.Date, err)
		}
	}

	acc := newAccumulator(d)
	for _, ch := range chunks(d, c.cfg.ChunkHours) {
		for m := Measure(0); m < numContainerMeasures; m++ {
			res, err := c.q.QueryRange(ctx, c.queries.Container(m), ch.start, ch.end, Step)
			if err != nil {
				return Capture{}, fmt.Errorf("container measure %d %s..%s: %w", m, ch.start.Format(time.RFC3339), ch.end.Format(time.RFC3339), err)
			}
			acc.addContainer(m, res)
		}
		for m := Measure(0); m < numNodeMeasures; m++ {
			res, err := c.q.QueryRange(ctx, c.queries.Node(m), ch.start, ch.end, Step)
			if err != nil {
				return Capture{}, fmt.Errorf("node measure %d %s..%s: %w", m, ch.start.Format(time.RFC3339), ch.end.Format(time.RFC3339), err)
			}
			acc.addNode(m, res)
		}
	}
	return acc.build(c.cfg.Cluster, Source, NewOwners(owners[0], owners[1], owners[2], c.cfg.RunnerNamespaces)), nil
}

type window struct{ start, end time.Time }

// chunks splits the day's sample timestamps (Start+Step ... End) into
// windows of at most hours each, without overlap: range queries include both
// ends, so each window starts one step after the previous one ended.
func chunks(d Day, hours int) []window {
	size := time.Duration(hours) * time.Hour
	var out []window
	for s := d.Start; s.Before(d.End); s = s.Add(size) {
		e := s.Add(size)
		if e.After(d.End) {
			e = d.End
		}
		out = append(out, window{start: s.Add(Step), end: e})
	}
	return out
}

func anyPositive(series []Series) bool {
	for _, s := range series {
		for _, p := range s.Points {
			if p.V > 0 {
				return true
			}
		}
	}
	return false
}
