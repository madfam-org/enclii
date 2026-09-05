package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	apperrors "github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
)

// ADR-003 / ruling R21 — docs/architecture/ADR_003_TENANT_ADMIN_SCOPE.md.
//
// This file is the target-side tenant-scope guard. Every handler that reads or
// mutates a tenant-owned resource resolves that resource to a project and
// calls enforceUserProjectAccess (directly, or through one of the
// loadXWithAccess helpers in access_resource.go). The tenant comparison
// therefore happens where the resource is loaded, on every call — not once at
// the edge, and not in the UI.
//
// WHAT CHANGED, AND WHY IT IS THE WHOLE DEFECT
// ============================================
// This function used to open with, in effect:
//
//	if role == "admin" || role == "superadmin" { return true }
//
// A rank comparison answers a question about SENIORITY and says nothing about
// WHICH TENANT the caller is senior in, so every `admin` principal — including
// the one minted for a customer when their tenant is provisioned — reached
// every project on the platform. Removing that short-circuit, and replacing it
// with a comparison between the caller's tenants and the resource's tenant, is
// the enforcement ADR-003 defers.

// ctxKeyPlatformAdmin memoizes the platform-rank lookup for the life of one
// request. The guard runs several times per request on handlers that touch
// more than one resource (topology, deployment groups), and the rank cannot
// change mid-request.
const ctxKeyPlatformAdmin = "adr003_platform_admin_resolved"

// callerRoleStrings returns every role string the auth layer attached to this
// request. Both keys are load-bearing: `user_role` (singular) is set by the
// local/OIDC session paths in internal/auth, `user_roles` (plural) by the
// external-JWT and API-token paths in internal/middleware/auth.go.
func callerRoleStrings(c *gin.Context) []string {
	roles := c.GetStringSlice("user_roles")
	if single := c.GetString("user_role"); single != "" {
		roles = append(roles, single)
	}
	return roles
}

// callerHasTenantAdminRank reports whether the caller administers its own
// tenant. Post-ADR-003 this is what the legacy strings `admin` and
// `superadmin` mean; see auth.NormalizeRole for why they are mapped down
// rather than up.
func callerHasTenantAdminRank(c *gin.Context) bool {
	return auth.AnyTenantAdminRole(callerRoleStrings(c))
}

// callerIsPlatformAdmin resolves the ADR-003 platform rank — the ONLY
// cross-tenant principal.
//
// The rank is never taken from a role string. internal/middleware/auth.go
// copies an API token's `scopes` list verbatim into user_roles, so any string
// a caller can put in a token they mint for themselves is a string they can
// assert; the authority is therefore users.is_platform_admin (migration 039),
// which is reconciled at startup from an operator allow-list no tenant can
// write to.
//
// Two resolution paths, in order:
//
//  1. An auth layer that already loaded the principal may set the
//     `is_platform_admin` context key; the guard trusts that and issues no
//     query.
//  2. Otherwise the guard reads the column — but only for a caller that
//     already carries an administrative role. A principal holding no admin
//     role has nothing for the platform rank to elevate, and skipping the
//     lookup keeps the guard's cost unchanged for the overwhelming majority of
//     requests (developers and viewers), which is what makes it affordable to
//     run on every call. The dry-run report
//     (GET /v1/admin/tenant-scope/dry-run) lists any allow-listed principal
//     that does not hold an admin role, precisely so this narrowing is visible
//     to the operator BEFORE deploy rather than as a surprise 404 after it.
func (h *Handler) callerIsPlatformAdmin(c *gin.Context) bool {
	if flag, known := platformAdminFromContext(c); known {
		return flag
	}

	resolved := false
	defer func() { c.Set(ctxKeyPlatformAdmin, resolved) }()

	if !callerHasTenantAdminRank(c) {
		return false
	}
	if h.repos == nil || h.repos.TenantScope == nil {
		return false
	}
	userID, ok := authenticatedUserID(c)
	if !ok {
		return false
	}
	flag, err := h.repos.TenantScope.IsPlatformAdmin(c.Request.Context(), userID)
	if err != nil {
		// A rank we cannot prove is a rank the caller does not have. Failing
		// closed here costs a platform admin one 404 during a database
		// incident; failing open would restore the cross-tenant write.
		if h.logger != nil {
			h.logger.Error(c.Request.Context(), "ADR-003: platform rank lookup failed, treating caller as tenant-scoped",
				logging.Error("error", err))
		}
		return false
	}
	resolved = flag
	return flag
}

// platformAdminFromContext returns the platform rank when it is already known
// for this request — either resolved earlier in the same request, or set by an
// auth layer that had the principal record in hand. The second return reports
// whether the answer is known at all; an unknown rank costs a query, which is
// why the guard consults this first and the querying resolver last.
func platformAdminFromContext(c *gin.Context) (flag bool, known bool) {
	if v, ok := c.Get(ctxKeyPlatformAdmin); ok {
		if resolved, ok := v.(bool); ok {
			return resolved, true
		}
	}
	if v, ok := c.Get("is_platform_admin"); ok {
		if resolved, ok := v.(bool); ok {
			c.Set(ctxKeyPlatformAdmin, resolved)
			return resolved, true
		}
	}
	return false, false
}

func authenticatedUserID(c *gin.Context) (uuid.UUID, bool) {
	raw := c.GetString("user_id")
	if raw == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// callerReachesProjectTenant is the tenant comparison itself: does the tenant
// that OWNS this project appear among the tenants the caller belongs to?
//
// It applies only to a caller holding the tenant-admin rank. That is what
// "administers this tenant" buys and all it buys: every project inside the
// caller's own tenant, nothing outside it. A developer or viewer still needs
// an explicit project_access grant, exactly as before — ADR-003 narrows the
// admin rank, it does not widen the others.
//
// A project with no team (`team_id IS NULL`, the "personal" projects that
// predate tenanting) has no owning tenant to compare against, so no rank
// reaches it and only an explicit grant does. That is a real reach loss for
// today's admins and it is deliberate: an untenanted project is precisely the
// case where "which tenant is the caller senior in?" has no answer. The
// dry-run report counts these before deploy.
func (h *Handler) callerReachesProjectTenant(c *gin.Context, userID, projectID uuid.UUID) bool {
	if !callerHasTenantAdminRank(c) {
		return false
	}
	if h.repos == nil || h.repos.TenantScope == nil || h.repos.Projects == nil {
		return false
	}

	ctx := c.Request.Context()
	ownerTeamID, err := h.repos.Projects.GetTeamID(ctx, projectID)
	if err != nil || ownerTeamID == uuid.Nil {
		return false
	}

	callerTeamIDs, err := h.repos.TenantScope.TeamIDsForUser(ctx, userID)
	if err != nil {
		if h.logger != nil {
			h.logger.Error(ctx, "ADR-003: tenant membership lookup failed, refusing tenant-rank reach",
				logging.Error("error", err))
		}
		return false
	}
	for _, teamID := range callerTeamIDs {
		if teamID == ownerTeamID {
			return true
		}
	}
	return false
}

// callerTenantIDs returns the tenants whose resources the caller may reach by
// rank alone — i.e. the teams a tenant_admin belongs to. It is empty for every
// other principal, and empty for a platform admin (whose reach is not a
// tenant list but "all of them", handled by callerIsPlatformAdmin).
//
// List endpoints use this to FILTER. A list that returned another tenant's
// rows would be a cross-tenant read even though no per-resource guard was
// bypassed, so "filter the list" and "guard the target" are two halves of the
// same rule, not alternatives.
func (h *Handler) callerTenantIDs(c *gin.Context) []uuid.UUID {
	if !callerHasTenantAdminRank(c) {
		return nil
	}
	if h.repos == nil || h.repos.TenantScope == nil {
		return nil
	}
	userID, ok := authenticatedUserID(c)
	if !ok {
		return nil
	}
	teamIDs, err := h.repos.TenantScope.TeamIDsForUser(c.Request.Context(), userID)
	if err != nil {
		if h.logger != nil {
			h.logger.Error(c.Request.Context(), "ADR-003: tenant membership lookup failed, listing without tenant reach",
				logging.Error("error", err))
		}
		return nil
	}
	return teamIDs
}

// enforceUserProjectAccess ensures the caller may access projectID. Returns
// false when a response has already been written.
//
// Order of the checks, and why:
//
//  1. Acting-as (XC-2) — a platform admin who has entered a tenant is scoped
//     to that tenant for the duration, so the acting-team comparison replaces
//     the rank entirely.
//  2. The rollback lever — see auth.TenantScopeEnforced.
//  3. An ALREADY-RESOLVED platform rank — free, and the only cross-tenant
//     answer.
//  4. An explicit project_access grant. Cheapest query and by far the most
//     common pass, and it is per-project, so it is tenant-safe by
//     construction: a consultant genuinely granted a project in two tenants
//     keeps both.
//  5. The tenant comparison — the caller's tenant-admin rank inside its own
//     tenant.
//  6. The platform rank read from the database, last, so an unresolved rank
//     costs a query only on the path that would otherwise refuse.
//
// The refusal is 404, not 403. That is this repository's existing convention
// for a resource the caller may not see — enforceActingTeamForProject
// (internal/api/services_handlers.go) and every loadXWithAccess helper already
// answer ErrNotFound — and it is the right one here: 403 on
// /v1/projects/<other-tenant-slug> would confirm that the other tenant's
// project exists, which is itself a cross-tenant read.
func (h *Handler) enforceUserProjectAccess(c *gin.Context, projectID uuid.UUID) bool {
	if projectID == uuid.Nil {
		respondAppError(c, apperrors.ErrNotFound)
		return false
	}

	if _, isActing := middleware.ActingTeamID(c); isActing {
		return h.enforceActingTeamForProject(c, projectID)
	}

	// Rollback lever ONLY. With enforcement off the pre-ADR-003 rank bypass is
	// restored verbatim, including its defect: every tenant admin reaches
	// every tenant. Each use is logged at WARN with the project it waved
	// through so the window is auditable after the fact.
	if !auth.TenantScopeEnforced() && callerHasTenantAdminRank(c) {
		if h.logger != nil {
			h.logger.Warn(c.Request.Context(), "ADR-003 tenant-scope enforcement is DISABLED: admin rank bypassed the tenant comparison",
				logging.String("project_id", projectID.String()),
				logging.String("user_id", c.GetString("user_id")))
		}
		return true
	}

	// A platform rank the auth layer already resolved short-circuits here, at
	// zero query cost, exactly where the old unconditional rank bypass used to
	// sit. A rank that would have to be READ from the database is checked
	// last instead (below), so the common path — developers and tenant admins
	// inside their own tenant — never pays for it.
	if flag, known := platformAdminFromContext(c); known && flag {
		return true
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		respondAppError(c, apperrors.ErrUnauthorized)
		return false
	}

	if h.repos == nil || h.repos.ProjectAccess == nil {
		respondAppError(c, apperrors.ErrInternal.WithDetails(map[string]string{"reason": "authorization unavailable"}))
		return false
	}

	hasAccess, err := h.repos.ProjectAccess.UserHasAccess(c.Request.Context(), userID, projectID)
	if err != nil {
		h.logger.Error(c.Request.Context(), "Failed to verify project access", logging.Error("error", err))
		respondAppError(c, apperrors.ErrInternal.WithError(err))
		return false
	}
	if hasAccess {
		return true
	}

	if h.callerReachesProjectTenant(c, userID, projectID) {
		return true
	}

	if h.callerIsPlatformAdmin(c) {
		return true
	}

	respondAppError(c, apperrors.ErrNotFound)
	return false
}

// RequireProjectAccessBySlug is middleware for routes carrying :slug. Resolves the project,
// enforces membership (or admin / acting-as rules), and stores project_id in context.
//
// This is an affordance, not the enforcement point: it saves the handler a
// lookup on the common shape. Handlers under :slug that load a second
// tenant-owned resource still call the guard on THAT resource — see
// loadDeploymentGroupWithSlugAccess in access_resource.go, which checks both.
func (h *Handler) RequireProjectAccessBySlug() gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")
		if slug == "" {
			c.Next()
			return
		}

		project, err := h.repos.Projects.GetBySlug(slug)
		if err != nil {
			respondAppError(c, apperrors.ErrProjectNotFound)
			c.Abort()
			return
		}

		if !h.enforceUserProjectAccess(c, project.ID) {
			c.Abort()
			return
		}

		c.Set("project_id", project.ID)
		c.Next()
	}
}

// enforceServiceAccess loads a service and applies project-level access control.
func (h *Handler) enforceServiceAccess(c *gin.Context, serviceID uuid.UUID) bool {
	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil || svc == nil {
		respondAppError(c, apperrors.ErrServiceNotFound)
		return false
	}
	return h.enforceUserProjectAccess(c, svc.ProjectID)
}
