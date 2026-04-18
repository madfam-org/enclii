package logstream

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// repoResolver adapts db.Repositories into the ServiceResolver
// interface. It lives in the logs package (rather than api/) so tests
// in this package can easily fake the dependency, while still keeping
// the wiring colocated with the thing it wires.
type repoResolver struct {
	repos *db.Repositories
}

// NewRepoResolver — constructor kept exported so main.go can pass a
// *db.Repositories directly. Returns ServiceResolver (the interface)
// so callers can't poke at internals.
func NewRepoResolver(repos *db.Repositories) ServiceResolver {
	return &repoResolver{repos: repos}
}

// Resolve implements ServiceResolver. It mirrors the lookup chain in
// internal/api/logs_handlers.go but returns a flat struct instead of
// three separate values — easier to consume and easier to mock.
func (r *repoResolver) Resolve(ctx context.Context, serviceID uuid.UUID, envName string) (*ServiceCoords, error) {
	service, err := r.repos.Services.GetByID(serviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("service lookup: %w", err)
	}
	if service == nil {
		return nil, nil
	}

	project, err := r.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("project lookup: %w", err)
	}
	if project == nil {
		return nil, nil
	}

	return &ServiceCoords{
		ServiceID:   service.ID,
		ServiceName: service.Name,
		ProjectID:   project.ID,
		ProjectSlug: project.Slug,
		Environment: envName,
		Namespace:   fmt.Sprintf("enclii-%s-%s", project.Slug, envName),
	}, nil
}

// ginAuthz is the production Authz. It relies on route-level middleware
// (auth.RequireRole) to gate access at a role granularity — by the time
// requests reach the handler the caller is guaranteed to have Developer
// role or higher. Our responsibilities inside the handler are:
//
//  1. Read the caller sub for rate-limit keying.
//  2. Refuse when no sub is present (defense in depth — the middleware
//     should've 401'd already, but be explicit).
//
// We deliberately don't re-check project membership here because the
// existing log endpoints don't either (see internal/api/logs_handlers.go).
// Changing that model is out of scope for P2.1; if/when per-env RBAC
// (P1.6) ships, this is where the hook goes.
type ginAuthz struct{}

// NewGinAuthz constructs the production Authz.
func NewGinAuthz() Authz { return &ginAuthz{} }

// CallerSub extracts the caller's user identifier from the gin context
// embedded in ctx. Matches audit.ginAuthzChecker semantics.
func (g *ginAuthz) CallerSub(ctx context.Context) string {
	c := ginFromContext(ctx)
	if c == nil {
		return ""
	}
	v, ok := c.Get("user_id")
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case interface{ String() string }:
		return s.String()
	}
	return ""
}

// CanReadService — see type comment. Returns true for any authenticated
// caller; the handler still enforces CallerSub != "" for 401.
func (g *ginAuthz) CanReadService(ctx context.Context, callerSub string, coords *ServiceCoords) (bool, error) {
	return callerSub != "", nil
}

// ginFromContext extracts the *gin.Context embedded in a request
// context. gin-gonic exposes c.Request.Context() which is itself the
// gin.Context (it implements context.Context). So we attempt the
// direct assertion first.
func ginFromContext(ctx context.Context) *gin.Context {
	if c, ok := ctx.(*gin.Context); ok {
		return c
	}
	if v := ctx.Value(ginContextKey{}); v != nil {
		if c, ok := v.(*gin.Context); ok {
			return c
		}
	}
	return nil
}

// ginContextKey is the unexported key used when we attach a gin.Context
// to a context.Context for downstream code. Used by the handler entry
// points so in-handler helpers can read user_id via Authz without
// having to plumb *gin.Context everywhere.
type ginContextKey struct{}

// WithGinContext attaches c to ctx so downstream helpers can retrieve
// it via ginFromContext. Exported for the handler wrapper in handler.go
// to use at each entry point.
func WithGinContext(ctx context.Context, c *gin.Context) context.Context {
	return context.WithValue(ctx, ginContextKey{}, c)
}
