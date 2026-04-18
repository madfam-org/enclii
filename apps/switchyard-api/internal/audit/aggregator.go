package audit

import (
	"context"
	"sort"
	"sync"

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
	sources []Source
	logger  logrus.FieldLogger
}

// NewAggregator wires a fixed set of sources. Callers typically build
// this once at startup and share the pointer across requests.
func NewAggregator(logger logrus.FieldLogger, sources ...Source) *Aggregator {
	return &Aggregator{
		sources: sources,
		logger:  logger,
	}
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
