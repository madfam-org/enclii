package api

// ADR-003 / ruling R21, PR 2 — docs/architecture/ADR_003_TENANT_ADMIN_SCOPE.md.
//
// PR #499 fixed the guard. It did not reach the routes that never called the
// guard in the first place: 23 tenant-owned verbs enumerated, with reasons, in
// tenantScopeUnguardedBacklog. This file is the seam those 23 routes are
// switched onto.
//
// WHY A SEPARATE ENTRY POINT AND NOT A BARE CALL TO THE GUARD
// ===========================================================
// The rollout is staged, and production is sitting in stage 1 with
// ENCLII_TENANT_SCOPE_ENFORCE=false. The whole point of that stage is that the
// deployed build behaves EXACTLY as pre-ADR-003 main, so an operator can run
// the dry-run report against real data before anything starts refusing.
//
// For the routes PR #499 touched, "exactly as main" and "the guard with its
// rank bypass restored" are the same thing, because those routes already
// called the guard: main's behaviour WAS the guard minus the tenant
// comparison. That is not true here. These 23 routes performed no target-side
// check at all on main, so a guard that merely restored the rank bypass would
// still be a new refusal for every caller below the admin rank — a developer
// with no project_access grant reaches GET /v1/services/:id/networking on main
// today and would stop reaching it the moment this merges, with the flag off,
// with nobody having decided to turn anything on.
//
// So the gates added by this change are inert while the flag is off, and the
// flag remains the single lever for the whole of ADR-003:
//
//	flag off (stage 1, shipped)  -> byte-for-byte main, on every one of the 23
//	flag on  (stage 3)           -> the tenant comparison at the target
//
// The cost of that choice is stated plainly rather than hidden: until stage 3
// these routes remain cross-tenant reachable, which is exactly what ADR-003's
// "not yet enforced" section says about the backlog. Emptying the backlog
// makes stage 3 sufficient; it does not make stage 1 safe. Tenant #2 is gated
// on stage 3, not on this merge.
//
// A skipped gate is logged at WARN for a caller that holds the tenant-admin
// rank — the same signal, on the same lever, that access.go emits for the
// bypasses it grants. It is deliberately NOT logged for anonymous or
// lesser-ranked callers: several of these routes are high-volume reads and one
// of them is unauthenticated, and a WARN per request would bury the line that
// matters.

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	apperrors "github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
)

// stagedGateSkipped reports whether a gate introduced by R21 PR 2 must stand
// down for this request, and records the fact when the caller is one the gate
// would otherwise have been about.
func (h *Handler) stagedGateSkipped(c *gin.Context) bool {
	if auth.TenantScopeEnforced() {
		return false
	}
	if callerHasTenantAdminRank(c) && h.logger != nil {
		h.logger.Warn(c.Request.Context(),
			"ADR-003 tenant-scope enforcement is DISABLED: a route guarded by R21 PR 2 answered without the tenant comparison",
			logging.String("path", c.FullPath()),
			logging.String("user_id", c.GetString("user_id")))
	}
	return true
}

// enforceStagedProjectAccess is the guard, behind the rollout flag, for a
// route that resolved its resource to a project. Returns false when a response
// has already been written.
func (h *Handler) enforceStagedProjectAccess(c *gin.Context, projectID uuid.UUID) bool {
	if h.stagedGateSkipped(c) {
		return true
	}
	return h.enforceUserProjectAccess(c, projectID)
}

// enforceStagedServiceAccess is the same thing for a route addressed by a
// service id. It takes an ALREADY-PARSED id rather than parsing :id itself
// (which is what mustServiceAccess does) so that the handler keeps its own
// malformed-id response: with the flag off, nothing about these routes may
// change, and the shape of a 400 is behaviour too.
func (h *Handler) enforceStagedServiceAccess(c *gin.Context, serviceID uuid.UUID) bool {
	if h.stagedGateSkipped(c) {
		return true
	}
	if h.repos == nil || h.repos.Services == nil {
		// A guard that cannot resolve the resource cannot prove ownership, so
		// it refuses. Same reasoning as the guard's own "authorization
		// unavailable" branch: failing open here would restore exactly the
		// cross-tenant reach this change exists to remove.
		respondAppError(c, apperrors.ErrInternal.WithDetails(map[string]string{"reason": "authorization unavailable"}))
		return false
	}
	return h.enforceServiceAccess(c, serviceID)
}

// enforceStagedDomainAccess resolves a custom domain to its service and
// applies the staged gate. Used by the three domain verbs that loaded the
// domain by id and acted on it without ever asking who owns it.
func (h *Handler) enforceStagedDomainAccess(c *gin.Context, domainID string) bool {
	if h.stagedGateSkipped(c) {
		return true
	}
	domain, err := h.repos.CustomDomains.GetByID(c.Request.Context(), domainID)
	if err != nil || domain == nil {
		// The caller may not be told the difference between "no such domain"
		// and "not yours" — this repository's 404 convention, and the reason
		// the guard refuses with 404 rather than 403.
		c.JSON(http.StatusNotFound, gin.H{"error": "custom domain not found"})
		return false
	}
	return h.enforceServiceAccess(c, domain.ServiceID)
}

// callerMayReachProject answers the guard's question WITHOUT writing a
// response, for the one route that must FILTER rather than refuse:
// GET /v1/builds/:commit_sha/status returns a row per service that built a
// commit, so the cross-tenant rows have to be dropped from a 200, not turned
// into a 404.
//
// It is deliberately a sibling of enforceUserProjectAccess rather than a
// re-implementation: every decision it makes is delegated to the same helper
// the guard uses, in the same order. TestStagedGate_ReadOnlyPredicateAgrees
// WithTheGuard drives both over the same matrix so the two cannot drift.
func (h *Handler) callerMayReachProject(c *gin.Context, projectID uuid.UUID) bool {
	if projectID == uuid.Nil {
		return false
	}

	if actingTeamID, isActing := middleware.ActingTeamID(c); isActing {
		teamID, err := h.repos.Projects.GetTeamID(c.Request.Context(), projectID)
		return err == nil && teamID == actingTeamID
	}

	if !auth.TenantScopeEnforced() && callerHasTenantAdminRank(c) {
		return true
	}

	if flag, known := platformAdminFromContext(c); known && flag {
		return true
	}

	userID, ok := authenticatedUserID(c)
	if !ok {
		// No principal at all. The unauthenticated caller of the public
		// build-status route lands here, and reaches nothing.
		return false
	}
	if h.repos == nil || h.repos.ProjectAccess == nil {
		return false
	}

	hasAccess, err := h.repos.ProjectAccess.UserHasAccess(c.Request.Context(), userID, projectID)
	if err != nil {
		if h.logger != nil {
			h.logger.Error(c.Request.Context(), "ADR-003: project access check failed while filtering, dropping the row",
				logging.Error("error", err))
		}
		return false
	}
	if hasAccess {
		return true
	}
	if h.callerReachesProjectTenant(c, userID, projectID) {
		return true
	}
	return h.callerIsPlatformAdmin(c)
}

// callerMayReachService is callerMayReachProject with the service resolved
// first. A service that cannot be resolved is not reachable.
func (h *Handler) callerMayReachService(c *gin.Context, serviceID uuid.UUID) bool {
	if h.repos == nil || h.repos.Services == nil {
		return false
	}
	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil || svc == nil {
		return false
	}
	return h.callerMayReachProject(c, svc.ProjectID)
}
