package middleware

// XC-2 enforcement: read the ax_acting_as cookie set by the master-admin
// tenant-switcher (apps/switchyard-api/internal/api/admin_tenants_handlers.go),
// validate the open admin_acting_sessions row for the calling admin, and stash
// the acted-on team_id in the gin context. Downstream handlers that filter
// list endpoints by team consult ActingTeamID(c) — see the consumers in
// apps/switchyard-api/internal/api/projects_handlers.go for the pattern.
//
// Non-admin users never reach this middleware path because the cookie is only
// ever set by an admin-only endpoint, but for defense-in-depth we also gate
// activation on the user_roles claim before consulting the cookie.

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

const (
	// actingAsCookieName must match the constant of the same name in
	// apps/switchyard-api/internal/api/admin_tenants_handlers.go. Duplicated
	// here rather than imported to avoid an api↔middleware cycle.
	actingAsCookieName = "ax_acting_as"

	ctxKeyActingTeamID   = "acting_team_id"
	ctxKeyActingTeamSlug = "acting_team_slug"
	ctxKeyIsActingAs     = "is_acting_as"
)

// ActingAsContext is the dependency the middleware needs from the API layer.
// We accept an interface (rather than concrete repos) so the middleware can
// be unit-tested without a database.
type ActingAsContext interface {
	// GetActiveActingSession returns (team_id, true) if the admin has an open,
	// non-expired session and the team still exists, else (uuid.Nil, false).
	// Implementations must be cheap — this runs on every authed request.
	GetActiveActingSession(ctx context.Context, adminUserID uuid.UUID) (teamID uuid.UUID, teamSlug string, ok bool)
}

// ActingAsMiddleware reads the ax_acting_as cookie and resolves the active
// acting-as session. It chains AFTER the auth middleware (which sets
// user_id + user_roles in the gin context). It NEVER aborts the request — at
// worst the request proceeds without a team filter.
func ActingAsMiddleware(resolver ActingAsContext) gin.HandlerFunc {
	if resolver == nil {
		// Test mode / disabled: emit a no-op so the handler chain still works.
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		// Defense-in-depth: only consult the cookie if the caller carries the
		// admin role. Non-admins shouldn't have the cookie at all (the enter
		// endpoint is admin-gated), but if one were planted, ignore it.
		roles := c.GetStringSlice("user_roles")
		if !hasAdminRole(roles) {
			c.Next()
			return
		}

		// The cookie carries a slug; that's debug-friendly but not load-bearing.
		// The actual scope is taken from the session row keyed by admin_user_id.
		if _, err := c.Cookie(actingAsCookieName); err != nil {
			c.Next()
			return
		}

		userIDStr := c.GetString("user_id")
		adminID, err := uuid.Parse(userIDStr)
		if err != nil {
			c.Next()
			return
		}

		teamID, teamSlug, ok := resolver.GetActiveActingSession(c.Request.Context(), adminID)
		if !ok {
			// Cookie present but no active session — auth middleware already
			// passed, so we just skip the filter. The /v1/admin/tenants/active
			// endpoint will surface "not active" on next SPA poll and clear
			// the cookie, so we don't proactively clear it here.
			c.Next()
			return
		}

		c.Set(ctxKeyActingTeamID, teamID)
		c.Set(ctxKeyActingTeamSlug, teamSlug)
		c.Set(ctxKeyIsActingAs, true)
		c.Next()
	}
}

// ActingTeamID returns the team UUID the calling admin is currently acting
// on behalf of, or (uuid.Nil, false) if no session is active. Handlers that
// list resources should consult this and add WHERE team_id = ? when ok.
func ActingTeamID(c *gin.Context) (uuid.UUID, bool) {
	v, exists := c.Get(ctxKeyActingTeamID)
	if !exists {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	if !ok {
		return uuid.Nil, false
	}
	return id, true
}

// ActingTeamSlug returns the slug of the acted-on tenant for logging /
// audit-row enrichment. Empty when no session is active.
func ActingTeamSlug(c *gin.Context) string {
	return c.GetString(ctxKeyActingTeamSlug)
}

// IsActingAs reports whether the caller is currently acting on behalf of a
// tenant. Equivalent to a non-nil ActingTeamID — exposed separately because
// some logging paths only want the boolean.
func IsActingAs(c *gin.Context) bool {
	return c.GetBool(ctxKeyIsActingAs)
}

// repoActingAsResolver is the production implementation of ActingAsContext.
// It hits the AdminActingSessionRepository and the TeamRepository in series;
// both queries are cheap (each is one indexed lookup) and the partial index
// on admin_acting_sessions keeps the GetActive call sub-millisecond.
type repoActingAsResolver struct {
	sessions *db.AdminActingSessionRepository
	teams    *db.TeamRepository
}

// NewRepoActingAsResolver wires the production resolver. Pass nils for either
// repository to disable the middleware (the constructor returns nil and the
// caller's wiring should treat that as "feature off").
func NewRepoActingAsResolver(sessions *db.AdminActingSessionRepository, teams *db.TeamRepository) ActingAsContext {
	if sessions == nil || teams == nil {
		return nil
	}
	return &repoActingAsResolver{sessions: sessions, teams: teams}
}

func (r *repoActingAsResolver) GetActiveActingSession(ctx context.Context, adminUserID uuid.UUID) (uuid.UUID, string, bool) {
	session, err := r.sessions.GetActive(ctx, adminUserID)
	if err != nil {
		return uuid.Nil, "", false
	}
	team, err := r.teams.GetByID(ctx, session.TenantTeamID)
	if err != nil {
		return uuid.Nil, "", false
	}
	return team.ID, team.Slug, true
}

// hasAdminRole returns true if the caller's user_roles slice includes the
// global admin role. Duplicated from auth.go's role-check helper rather than
// imported to keep middleware → middleware/auth dependency-free.
func hasAdminRole(roles []string) bool {
	for _, r := range roles {
		if r == "admin" {
			return true
		}
	}
	return false
}
