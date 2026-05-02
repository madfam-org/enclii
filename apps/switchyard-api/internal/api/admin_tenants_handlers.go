package api

// Master-admin tenant switching (XC-2). Lets a user with the global `admin`
// role list every tenant (team) on the platform and "act as" one for a
// bounded session. Subsequent requests carrying the `ax_acting_as` cookie are
// then filtered to that tenant by the auth middleware.
//
// See claudedocs/master-admin-tenant-switching.md for the full design.

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
)

// actingAsCookieName is the cookie key the SPA reads/writes to keep the
// "currently acting as <tenant>" selection bound to the browser session. The
// cookie is HttpOnly so the SPA does NOT round-trip the value itself; it just
// trusts whatever the server tells it via /v1/admin/tenants/active.
const actingAsCookieName = "ax_acting_as"

// Default acting-as session length. Operators usually want a few hours; we
// cap at 24h server-side to keep a stale cookie from indefinitely shadowing
// the admin's own data view.
const (
	defaultActingSessionTTL = 4 * time.Hour
	maxActingSessionTTL     = 24 * time.Hour
)

// TenantListResponse is one row in GET /v1/admin/tenants. Shape is intentionally
// stable for the SPA — fields are added, not renamed.
type TenantListResponse struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Description  *string `json:"description,omitempty"`
	AvatarURL    *string `json:"avatar_url,omitempty"`
	BillingEmail *string `json:"billing_email,omitempty"`
	MemberCount  int     `json:"member_count"`
	ProjectCount int     `json:"project_count"`
	LastDeployAt *string `json:"last_deploy_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// EnterTenantRequest is the body for POST /v1/admin/tenants/:slug/enter. The
// reason is optional but encouraged — it lands in the audit log alongside the
// session row.
type EnterTenantRequest struct {
	Reason          string `json:"reason"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
}

// ActiveActingSessionResponse is what the SPA polls on each route change to
// know whether to render the "Acting as <tenant>" banner.
type ActiveActingSessionResponse struct {
	Active    bool                `json:"active"`
	Tenant    *TenantListResponse `json:"tenant,omitempty"`
	StartedAt *string             `json:"started_at,omitempty"`
	ExpiresAt *string             `json:"expires_at,omitempty"`
	Reason    *string             `json:"reason,omitempty"`
}

// ListTenants implements GET /v1/admin/tenants — every team on the platform,
// with aggregate counts. Admin-only; the route group already enforces this.
func (h *Handler) ListTenants(c *gin.Context) {
	if h.repos == nil || h.repos.Teams == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "team repository not initialized"})
		return
	}

	teams, err := h.repos.Teams.ListAll(c.Request.Context())
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}

	// We pre-resolve project counts in a single query rather than N+1 per
	// team — the dashboard reaches for this on every admin login.
	projectCounts, err := h.repos.Teams.CountProjectsByTeam(c.Request.Context())
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}

	out := make([]TenantListResponse, 0, len(teams))
	for _, t := range teams {
		out = append(out, TenantListResponse{
			ID:           t.ID.String(),
			Name:         t.Name,
			Slug:         t.Slug,
			Description:  t.Description,
			AvatarURL:    t.AvatarURL,
			BillingEmail: t.BillingEmail,
			ProjectCount: projectCounts[t.ID],
			CreatedAt:    t.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"tenants": out})
}

// EnterTenant implements POST /v1/admin/tenants/:slug/enter — opens an
// acting-as session bound to the calling admin and sets the ax_acting_as
// cookie. Returns the session metadata so the SPA can render the banner
// without a follow-up call.
func (h *Handler) EnterTenant(c *gin.Context) {
	slug := strings.TrimSpace(c.Param("slug"))
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "tenant slug is required"})
		return
	}

	adminID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing admin user_id in context"})
		return
	}

	team, err := h.repos.Teams.GetBySlug(c.Request.Context(), slug)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
		return
	}

	var req EnterTenantRequest
	_ = c.ShouldBindJSON(&req) // body is optional

	ttl := defaultActingSessionTTL
	if req.DurationSeconds > 0 {
		requested := time.Duration(req.DurationSeconds) * time.Second
		if requested > maxActingSessionTTL {
			requested = maxActingSessionTTL
		}
		ttl = requested
	}

	session, err := h.repos.AdminActingSessions.Start(
		c.Request.Context(),
		adminID,
		team.ID,
		time.Now().Add(ttl),
		strings.TrimSpace(req.Reason),
		c.ClientIP(),
		c.Request.UserAgent(),
	)
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}

	// HttpOnly so the SPA can't be tricked into reading or forwarding the
	// active tenant slug. Path is /v1 so it accompanies API calls but not
	// (e.g.) static assets.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		actingAsCookieName,
		team.Slug,
		int(ttl.Seconds()),
		"/v1",
		"",
		c.Request.TLS != nil, // Secure on TLS connections
		true,                 // HttpOnly
	)

	startedAt := session.StartedAt.Format(time.RFC3339)
	expiresAt := session.ExpiresAt.Format(time.RFC3339)
	c.JSON(http.StatusOK, ActiveActingSessionResponse{
		Active:    true,
		Tenant:    &TenantListResponse{ID: team.ID.String(), Name: team.Name, Slug: team.Slug, Description: team.Description, AvatarURL: team.AvatarURL, BillingEmail: team.BillingEmail, CreatedAt: team.CreatedAt.Format(time.RFC3339)},
		StartedAt: &startedAt,
		ExpiresAt: &expiresAt,
		Reason:    session.Reason,
	})
}

// ExitTenant implements POST /v1/admin/tenants/exit — closes every open
// acting-as session for the calling admin and clears the cookie.
func (h *Handler) ExitTenant(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing admin user_id in context"})
		return
	}
	if _, err := h.repos.AdminActingSessions.EndAll(c.Request.Context(), adminID); err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(actingAsCookieName, "", -1, "/v1", "", c.Request.TLS != nil, true)
	c.JSON(http.StatusOK, ActiveActingSessionResponse{Active: false})
}

// ActiveTenant implements GET /v1/admin/tenants/active — returns the calling
// admin's open acting-as session if any. The SPA polls this on first paint
// and on focus-change to render the banner. Always 200 (no session is not an
// error condition).
func (h *Handler) ActiveTenant(c *gin.Context) {
	adminID, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing admin user_id in context"})
		return
	}
	session, err := h.repos.AdminActingSessions.GetActive(c.Request.Context(), adminID)
	if err != nil {
		if errors.Is(err, db.ErrNoActiveActingSession) {
			c.JSON(http.StatusOK, ActiveActingSessionResponse{Active: false})
			return
		}
		middleware.AbortInternal(c, err)
		return
	}

	team, err := h.repos.Teams.GetByID(c.Request.Context(), session.TenantTeamID)
	if err != nil {
		// Session exists but the team was deleted under us — close it and
		// surface "no active session" rather than confuse the SPA with a
		// half-state.
		_, _ = h.repos.AdminActingSessions.EndAll(c.Request.Context(), adminID)
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(actingAsCookieName, "", -1, "/v1", "", c.Request.TLS != nil, true)
		c.JSON(http.StatusOK, ActiveActingSessionResponse{Active: false})
		return
	}

	startedAt := session.StartedAt.Format(time.RFC3339)
	expiresAt := session.ExpiresAt.Format(time.RFC3339)
	c.JSON(http.StatusOK, ActiveActingSessionResponse{
		Active:    true,
		Tenant:    &TenantListResponse{ID: team.ID.String(), Name: team.Name, Slug: team.Slug, Description: team.Description, AvatarURL: team.AvatarURL, BillingEmail: team.BillingEmail, CreatedAt: team.CreatedAt.Format(time.RFC3339)},
		StartedAt: &startedAt,
		ExpiresAt: &expiresAt,
		Reason:    session.Reason,
	})
}
