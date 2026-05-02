package audit

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Source is the interface every upstream satisfies so the aggregator can
// treat direct-DB (SwitchyardSource) and HTTP (JanuaClient, NexusClient)
// uniformly.
//
// A source that receives a Query it cannot serve (wrong category,
// disabled by config) MUST return (nil, nil) rather than erroring —
// the aggregator treats errors as hard failures.
type Source interface {
	Name() string
	Fetch(ctx context.Context, q Query) ([]AuditEvent, error)
}

// Aggregator fans a single Query out to N sources in parallel, then
// merges their results in timestamp-DESC order and trims to limit.
//
// Degradation contract: an individual source error does NOT fail the
// request. We log the failure, drop that source's events from the
// response, and still return a coherent (but partial) page. This is
// correct for a SOC 2 audit surface — we'd rather show "we couldn't
// reach Janua right now" than a 500 that hides the rest.
type Aggregator struct {
	sources      []Source
	logger       logrus.FieldLogger
	teamResolver TeamResolver // optional — when non-nil, enables team post-filter
}

// NewAggregator wires a fixed set of sources. Callers typically build
// this once at startup and share the pointer across requests.
func NewAggregator(logger logrus.FieldLogger, sources ...Source) *Aggregator {
	return &Aggregator{
		sources: sources,
		logger:  logger,
	}
}

// WithTeamResolver enables the XC-2 Round 6 post-filter for sources that
// can't push a team_id filter to their upstream (Janua, Nexus today). The
// switchyard source still scopes via SQL JOIN regardless. Pass nil to disable
// post-filtering — the aggregator will then emit any team-tagged source's
// rows verbatim and drop nothing on the basis of TeamResolver lookups.
func (a *Aggregator) WithTeamResolver(r TeamResolver) *Aggregator {
	a.teamResolver = r
	return a
}

// FetchResult holds the merged page plus per-source diagnostic info.
// SourceErrors is keyed by Source.Name() and populated only for sources
// that errored; a nil or empty map means a clean fetch across the board.
type FetchResult struct {
	Events       []AuditEvent
	NextCursor   *AuditEvent // pointer to the last event (oldest); nil if no more
	SourceErrors map[string]string
}

// Fetch runs each source concurrently, merges their outputs, and trims.
// We request limit+1 per source so that after the merge we can detect
// whether more pages exist past the cutoff.
func (a *Aggregator) Fetch(ctx context.Context, q Query) (*FetchResult, error) {
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}

	// Fan out with bounded concurrency — we have at most 3 sources today,
	// so an unbuffered goroutine-per-source is fine. We carry a private
	// Query to each source with Limit+1 so we can detect "has more".
	sub := q
	sub.Limit = q.Limit + 1

	type result struct {
		name   string
		events []AuditEvent
		err    error
	}
	results := make(chan result, len(a.sources))
	var wg sync.WaitGroup
	for _, s := range a.sources {
		wg.Add(1)
		go func(src Source) {
			defer wg.Done()
			events, err := src.Fetch(ctx, sub)
			results <- result{name: src.Name(), events: events, err: err}
		}(s)
	}
	wg.Wait()
	close(results)

	merged := make([]AuditEvent, 0, len(a.sources)*(q.Limit+1))
	srcErrs := map[string]string{}
	for r := range results {
		if r.err != nil {
			// Log+record, but keep going.
			if a.logger != nil {
				a.logger.WithError(r.err).
					WithField("source", r.name).
					Warn("audit aggregator: source fetch failed — continuing without this source")
			}
			srcErrs[r.name] = r.err.Error()
			continue
		}
		merged = append(merged, r.events...)
	}

	// XC-2 Round 6: team scoping post-filter. Sources that pushed the team
	// filter to their upstream (currently only the switchyard source) emit
	// only matching rows already; this loop is a no-op for them. Sources
	// that emit unscoped (Janua, Nexus today) get filtered here via the
	// per-event projectID + a per-request team lookup cache.
	//
	// Performance note: for each row from a non-team-aware source we do
	// at most one ProjectRepository.GetTeamID lookup, deduped by the
	// caching resolver. At current data volumes (≤2.5k rows/page across
	// all sources) this is at worst a few dozen indexed lookups per
	// request — negligible compared to the per-source HTTP roundtrips
	// we already paid. TODO: revisit if Janua/Nexus rows-per-page grow
	// past ~10k unique projects, at which point pushing a team filter to
	// nexus-api would amortise better than post-filtering.
	if q.TeamID != nil && a.teamResolver != nil {
		merged = a.applyTeamFilter(ctx, merged, *q.TeamID)
	}

	// DESC merge. Go's sort is stable, and we rely on that only for
	// reproducibility (events with equal timestamps will page in the
	// same order across calls).
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Timestamp.After(merged[j].Timestamp)
	})

	// Slice to the caller's limit; decide whether there's a next page.
	var next *AuditEvent
	var page []AuditEvent
	if len(merged) > q.Limit {
		page = merged[:q.Limit]
		// Next cursor = oldest event on this page; next call resumes strictly older.
		nextRef := page[len(page)-1]
		next = &nextRef
	} else {
		page = merged
	}

	return &FetchResult{
		Events:       page,
		NextCursor:   next,
		SourceErrors: srcErrs,
	}, nil
}

// applyTeamFilter walks the merged event slice and keeps only rows that
// belong to teamID. The keep predicate (in order):
//
//  1. ev.ActingTeamID == teamID — switchyard rows emitted while a master
//     admin was acting-as that tenant; trustworthy because the column was
//     written under explicit operator intent.
//  2. ev.projectID resolves (via the cached TeamResolver) to teamID.
//
// Any other state — projectID is uuid.Nil, project unknown, lookup error,
// or team mismatch — drops the row. This is the conservative default for
// tenant isolation: when in doubt, don't show it.
func (a *Aggregator) applyTeamFilter(ctx context.Context, events []AuditEvent, teamID uuid.UUID) []AuditEvent {
	resolver := newCachingTeamResolver(a.teamResolver)
	out := events[:0] // reuse backing array — events is local to Fetch
	for _, ev := range events {
		if ev.ActingTeamID != nil && *ev.ActingTeamID == teamID {
			out = append(out, ev)
			continue
		}
		pid := ev.projectID
		if pid == uuid.Nil {
			// No project linkage on this row → can't prove ownership →
			// drop. Janua login events fall in this bucket today; that's
			// expected and documented under "Round 6 — partial coverage".
			continue
		}
		t, err := resolver.GetTeamID(ctx, pid)
		if err != nil || t == uuid.Nil {
			// Unknown project, lookup error, or NULL team_id (personal
			// account project) — drop under the conservative default.
			continue
		}
		if t == teamID {
			out = append(out, ev)
		}
	}
	return out
}
