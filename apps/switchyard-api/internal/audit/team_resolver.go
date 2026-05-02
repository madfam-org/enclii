package audit

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// TeamResolver maps a project UUID to its owning team UUID. It exists so the
// aggregator's team post-filter (XC-2 Round 6) can run without dragging the
// concrete db.ProjectRepository into this package.
//
// Implementations should:
//   - return uuid.Nil + nil when the project is known but team_id IS NULL
//     (personal-account projects that pre-date team adoption)
//   - return any non-nil error when the project is unknown OR the lookup
//     itself failed; the aggregator treats both as "drop the row" under
//     the team filter, which is the safe default for tenant isolation
//
// The implementation is allowed to cache aggressively — project→team
// mapping is effectively static (project rebinding is rare, manual, and
// would invalidate caches via deploy/restart anyway).
type TeamResolver interface {
	GetTeamID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error)
}

// cachingTeamResolver wraps any TeamResolver with a per-request mutex-guarded
// map cache. The aggregator instantiates one of these per Fetch call so a
// page that touches 1000 events from 50 distinct projects does 50 lookups,
// not 1000.
//
// Process-wide caching is deliberately NOT done here — project→team mapping
// CAN change (rare, but legitimate when ops re-parents a project), and the
// blast radius of a stale entry is tenant data leakage. Per-request scope
// keeps the win without the risk.
type cachingTeamResolver struct {
	inner TeamResolver
	mu    sync.Mutex
	hits  map[uuid.UUID]teamLookup
}

type teamLookup struct {
	teamID uuid.UUID
	err    error
}

func newCachingTeamResolver(inner TeamResolver) *cachingTeamResolver {
	return &cachingTeamResolver{
		inner: inner,
		hits:  make(map[uuid.UUID]teamLookup),
	}
}

// GetTeamID returns the cached team for projectID, or fetches it once and
// memoises the result (including any error). A zero project UUID short-
// circuits to (uuid.Nil, nil) without hitting the inner resolver — that's
// "row has no project linkage", which the post-filter handles separately.
func (c *cachingTeamResolver) GetTeamID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	if projectID == uuid.Nil {
		return uuid.Nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if hit, ok := c.hits[projectID]; ok {
		return hit.teamID, hit.err
	}
	teamID, err := c.inner.GetTeamID(ctx, projectID)
	c.hits[projectID] = teamLookup{teamID: teamID, err: err}
	return teamID, err
}

// fixedTeamResolver is a test helper that returns the configured map without
// touching any DB. Exported lower-case so it lives in this package only —
// tests in audit_test.go and aggregator_test.go can both reuse it.
type fixedTeamResolver struct {
	mapping map[uuid.UUID]uuid.UUID
	// missing reports a sentinel error for project ids absent from mapping;
	// when nil, those ids resolve to (uuid.Nil, nil) — which the post-filter
	// also drops, but for the "unknown project" reason rather than "lookup
	// failed". Keeping both branches lets us assert each on its own.
	missing error
}

func (f *fixedTeamResolver) GetTeamID(_ context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	if t, ok := f.mapping[projectID]; ok {
		return t, nil
	}
	if f.missing != nil {
		return uuid.Nil, f.missing
	}
	return uuid.Nil, nil
}
