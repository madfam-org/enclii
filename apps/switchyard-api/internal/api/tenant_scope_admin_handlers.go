package api

// ADR-003 operator surface: the pre-deploy dry-run report, and the middleware
// that restricts a route to the platform rank.
//
// The report exists because the enforcement is not a behaviour an operator can
// safely discover in production. Turning the tenant comparison on removes
// standing reach from principals that have it today, and the only honest way
// to know WHOSE reach disappears is to compute it against the live data before
// the deploy. The operator runs this; nobody else can.

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// RequirePlatformAdmin refuses any caller that does not hold the ADR-003
// platform rank.
//
// It is applied to the routes whose whole purpose is cross-tenant — the
// master-admin tenant switcher and this report. Those routes were gated on the
// `admin` role, which post-ADR-003 means tenant_admin, i.e. they were reachable
// by any customer administrator. The remaining /v1/admin/* subtree is gated the
// same way and is switched in the follow-up change; it is called out in the PR
// so it is not mistaken for having been reviewed and left alone.
func (h *Handler) RequirePlatformAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.callerIsPlatformAdmin(c) {
			// 403 rather than this repository's usual 404: the existence of
			// the platform control plane is not a tenant's secret, and a
			// tenant admin who lands here needs to be told the rank is the
			// problem rather than hunt a missing route.
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "Forbidden",
				"message": "platform_admin rank required (ADR-003); tenant administrators are scoped to their own tenant",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// TenantScopeDryRunResponse is the operator's pre-deploy answer to "who loses
// what?".
type TenantScopeDryRunResponse struct {
	// EnforcementActive reports whether the guard is currently refusing
	// cross-tenant calls in THIS process. Running the report against a
	// process that already enforces is still useful (it verifies the
	// post-deploy state), but it is no longer a dry run, so the field is
	// first in the payload.
	EnforcementActive bool `json:"enforcement_active"`

	// AllowListSize is how many addresses the platform-admin allow-list
	// carries. Zero means no principal will have cross-tenant reach after
	// deploy — usually a misconfiguration, and the loudest thing this report
	// can tell an operator.
	AllowListSize int `json:"platform_admin_allow_list_size"`

	// PlatformAdmins is how many principals actually carry the rank in the
	// database. It should equal AllowListSize; a shortfall means an
	// allow-listed address has no user row yet (the operator has never logged
	// in) and that principal will be refused after deploy.
	PlatformAdmins int `json:"platform_admins_in_database"`

	// PrincipalsLosingReach is the count of rows below with projects_lost > 0.
	PrincipalsLosingReach int `json:"principals_losing_reach"`

	// Principals is every admin-ranked principal, whether or not it loses
	// reach, so the operator reads one table instead of inferring a
	// complement.
	Principals []db.PrincipalReach `json:"principals"`

	// Warnings are conditions the operator should resolve BEFORE deploying.
	Warnings []string `json:"warnings"`
}

// TenantScopeDryRun implements GET /v1/admin/tenant-scope/dry-run.
//
// It is read-only. It changes no rank, writes no row and does not depend on
// the enforcement being on, so it is safe to run against production before the
// enforcing build is deployed.
func (h *Handler) TenantScopeDryRun(c *gin.Context) {
	ctx := c.Request.Context()

	if h.repos == nil || h.repos.TenantScope == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "tenant-scope repository not initialized"})
		return
	}

	principals, err := h.repos.TenantScope.ReportCrossTenantReachLoss(ctx)
	if err != nil {
		if h.logger != nil {
			h.logger.Error(ctx, "ADR-003 dry-run report failed", logging.Error("error", err))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to compute tenant-scope dry-run report"})
		return
	}

	resp := TenantScopeDryRunResponse{
		EnforcementActive: auth.TenantScopeEnforced(),
		AllowListSize:     len(auth.PlatformAdminAllowList()),
		Principals:        principals,
		Warnings:          []string{},
	}

	for _, p := range principals {
		if p.IsPlatformAdmin {
			resp.PlatformAdmins++
		}
		if p.ProjectsLost > 0 {
			resp.PrincipalsLosingReach++
		}
	}

	if resp.AllowListSize == 0 {
		resp.Warnings = append(resp.Warnings,
			"platform-admin allow-list is empty: after deploy no principal will have cross-tenant reach. "+
				"Set ENCLII_PLATFORM_ADMIN_EMAILS before deploying.")
	}
	if resp.PlatformAdmins < resp.AllowListSize {
		resp.Warnings = append(resp.Warnings,
			"fewer principals carry the platform rank than the allow-list names: an allow-listed address has no "+
				"user row yet, or holds no admin role. That principal will be refused cross-tenant calls after deploy.")
	}
	if resp.PrincipalsLosingReach > 0 {
		resp.Warnings = append(resp.Warnings,
			"one or more admin-ranked principals lose reach. Confirm each is a TENANT administrator that should "+
				"not have had cross-tenant access, and not an operator missing from the allow-list.")
	}

	c.JSON(http.StatusOK, resp)
}
