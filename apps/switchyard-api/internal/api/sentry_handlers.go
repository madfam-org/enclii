package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/integrations/sentry"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// SentryStatsResponse is the JSON shape of GET /v1/observability/sentry.
//
// Field semantics — UI relies on these distinctions:
//   - configured=false: env vars missing OR caller lacks admin scope OR
//     upstream is unreachable. UI hides the badge / renders "no errors".
//   - configured=true + reason=no_sentry_project: token works but the project
//     slug doesn't exist in the org. UI shows a neutral chip, not red.
//   - configured=true + error_count!=nil: live data, render normally.
//
// Reason taxonomy (stable contract — frontend may switch on these):
//   - "sentry_unconfigured"   — env vars not set
//   - "no_sentry_project"     — project slug not found in org (soft 404)
//   - "forbidden"             — caller authenticated but not admin
//   - "upstream_unavailable"  — Sentry rate-limited, unauthorised, network err
//   - "service_lookup_failed" — DB error resolving the service row
//
// Truthfulness contract (audit 2026-05-04): every code path here returns
// HTTP 200 (or 400 on malformed query) — NEVER 5xx. 5xx on per-service
// polling generates N console errors per page load and drowns out real
// signal. 503 is explicitly reserved for "Sentry's API is down" — a real
// outage we'd surface via /v1/observability/health, not this endpoint.
//
// Mirror fields (Enabled/Errors/Stats) are an alias surface for newer
// clients that prefer "enabled" semantics over the legacy "configured"
// flag. Both are populated; the UI today reads `configured`+`error_count`.
//
// Cached for 60s per (service_id, stats_period) pair to absorb dashboard
// polling. The cache is best-effort — a single-replica control plane
// doesn't need a distributed cache.
type SentryStatsResponse struct {
	Configured        bool      `json:"configured"`
	Enabled           bool      `json:"enabled"`
	Reason            string    `json:"reason,omitempty"`
	ServiceID         string    `json:"service_id,omitempty"`
	SentryProjectSlug string    `json:"sentry_project_slug,omitempty"`
	StatsPeriod       string    `json:"stats_period,omitempty"`
	ErrorCount        *int      `json:"error_count"`
	IssueCount        *int      `json:"issue_count,omitempty"`
	OrgSlug           string    `json:"org_slug,omitempty"`
	FetchedAt         time.Time `json:"fetched_at"`

	// Errors is reserved for future per-issue listings. We always emit a
	// non-nil empty slice so JSON consumers don't have to distinguish
	// `null` from `[]` — matches the disabled-payload contract documented
	// in the parity audit ("render no errors").
	Errors []SentryErrorEntry `json:"errors"`

	// Stats is a free-form bag for additional metrics (event counts by
	// level, user-impact buckets, etc.) introduced in later passes.
	// Empty {} on the disabled paths.
	Stats map[string]interface{} `json:"stats"`
}

// SentryErrorEntry is a placeholder for per-issue rows when we surface them
// in the dashboard. Currently always empty — the badge only needs counts.
type SentryErrorEntry struct {
	Title       string    `json:"title"`
	Level       string    `json:"level"`
	Count       int       `json:"count"`
	LastSeen    time.Time `json:"last_seen"`
	PermalinkID string    `json:"permalink_id,omitempty"`
}

// disabledSentryResponse builds the canonical "no Sentry data, render
// gracefully" payload. Centralised so every short-circuit path emits the
// same shape — frontend doesn't have to special-case reasons, just renders
// the absence of errors.
func disabledSentryResponse(serviceID, statsPeriod, reason string) SentryStatsResponse {
	return SentryStatsResponse{
		Configured:  false,
		Enabled:     false,
		Reason:      reason,
		ServiceID:   serviceID,
		StatsPeriod: statsPeriod,
		ErrorCount:  nil,
		FetchedAt:   time.Now().UTC(),
		Errors:      []SentryErrorEntry{},
		Stats:       map[string]interface{}{},
	}
}

// sentryCacheTTL is intentionally short. Dashboards poll at 60s (POLLING_IDLE)
// so any value in this range is fine; 60s gives one cache hit per natural
// poll interval.
const sentryCacheTTL = 60 * time.Second

// sentryUpstreamTimeout caps any upstream Sentry call. The client itself has a
// 5s timeout but we layer one here so the handler returns a clean 504-style
// 502 even if a future client change relaxed its own deadline.
const sentryUpstreamTimeout = 5 * time.Second

// sentryCacheEntry is what we store per (service, period) tuple. We cache
// the full response (including reason fields) so a soft no_sentry_project
// stays sticky for 60s, exactly like a real result would.
type sentryCacheEntry struct {
	resp     SentryStatsResponse
	expireAt time.Time
}

// sentryStatsCache is initialised lazily on first request. We don't bother
// with eviction — entries are keyed on service UUID, of which there are
// O(100) on this control plane, so even a multi-day uptime stays under
// a few KB. Not worth the complexity.
var (
	sentryStatsCache   = map[string]sentryCacheEntry{}
	sentryStatsCacheMu sync.Mutex
)

// SetSentryClient wires the optional Sentry REST client. If nil, the
// /v1/observability/sentry endpoint will instantiate a client from env on
// first call. Bootstrap can call this with a pre-built client for tests.
func (h *Handler) SetSentryClient(c *sentry.Client) {
	h.sentryClient = c
}

// sentryClientOrDefault returns the configured client, falling back to an
// env-derived one. This matches the pattern used by the existing
// observability handlers (which also lazy-init from env).
func (h *Handler) sentryClientOrDefault() *sentry.Client {
	if h.sentryClient != nil {
		return h.sentryClient
	}
	// Lazy-construct on first use. The constructor is cheap and the
	// resulting client is safe to share concurrently (no per-call state).
	h.sentryClient = sentry.NewClient()
	return h.sentryClient
}

// GetSentryServiceStats returns Sentry error/issue counts for a single
// service. Admin-only (parity with the other observability endpoints).
//
// @Summary Get Sentry error stats for a service
// @Description Proxies Sentry's project stats API. Returns 200 OK with
// @Description configured=false when SENTRY_AUTH_TOKEN/ORG_SLUG
// @Description are unset; UI uses this to hide the badge gracefully.
// @Tags observability
// @Produce json
// @Param service query string true "Service UUID"
// @Param hours query int false "Lookback window in hours (default 24)"
// @Success 200 {object} SentryStatsResponse "Returns stats or unconfigured status"
// @Failure 400 {object} map[string]string "missing or invalid service param"
// @Failure 502 {object} map[string]string "upstream Sentry error"
// @Router /v1/observability/sentry [get]
func (h *Handler) GetSentryServiceStats(c *gin.Context) {
	ctx := c.Request.Context()

	// Param: ?service=<uuid> (required).
	rawServiceID := strings.TrimSpace(c.Query("service"))
	if rawServiceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "missing required query param: service",
		})
		return
	}
	serviceUUID, err := uuid.Parse(rawServiceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "service must be a valid UUID",
		})
		return
	}

	// Param: ?hours=<int> (default 24, capped at 30 days).
	hours, err := strconv.Atoi(c.DefaultQuery("hours", "24"))
	if err != nil || hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}
	statsPeriod := strconv.Itoa(hours) + "h"

	// Admin gate (in-handler, not middleware): the route is mounted under
	// `protected.Use(AuthMiddleware)`, so by the time we get here the
	// caller is authenticated — but they may not be admin. Returning 403
	// would break the dashboard's per-service polling for every non-admin
	// user; instead we emit a 200 OK with reason="forbidden" so the badge
	// renders "no errors" exactly like the unconfigured case.
	if !callerIsAdmin(c) {
		c.JSON(http.StatusOK, disabledSentryResponse(
			serviceUUID.String(), statsPeriod, "forbidden",
		))
		return
	}

	// Short-circuit on configuration. We do this BEFORE touching the DB
	// so an unconfigured deployment doesn't pay an extra query per poll.
	client := h.sentryClientOrDefault()
	if !client.IsConfigured() {
		c.JSON(http.StatusOK, disabledSentryResponse(
			serviceUUID.String(), statsPeriod, "sentry_unconfigured",
		))
		return
	}

	// Cache hit?
	cacheKey := serviceUUID.String() + "|" + statsPeriod
	if cached, ok := lookupSentryCache(cacheKey); ok {
		c.JSON(http.StatusOK, cached)
		return
	}

	// Resolve Sentry project slug. We pull (name, sentry_project_slug)
	// directly so we don't need to extend ServiceRepository's existing
	// scan plumbing for one optional column.
	//
	// Truthfulness: we used to return 404 / 500 here on missing-row /
	// DB errors. The dashboard polls per service per 60s — a single
	// stale UUID or a transient DB hiccup would pollute the console
	// with N error log lines per page. Same graceful-degradation policy
	// as the rest of this handler: 200 OK with an explanatory reason so
	// the UI hides the badge silently while operators see the real cause
	// in server logs.
	projectSlug, serviceName, err := h.resolveSentryProjectSlug(ctx, serviceUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Stale serviceId from the dashboard (deleted service, or a
			// new UUID not yet cached). Return the same disabled payload
			// shape as unconfigured — UI renders nothing for this card.
			c.JSON(http.StatusOK, disabledSentryResponse(
				serviceUUID.String(), statsPeriod, "service_lookup_failed",
			))
			return
		}
		// DB unavailable / scan error / etc. Log loudly — operators need
		// to see this — but don't bubble 5xx to the dashboard polling loop.
		h.logger.Error(ctx, "sentry: resolve project slug failed",
			logging.Error("error", err))
		c.JSON(http.StatusOK, disabledSentryResponse(
			serviceUUID.String(), statsPeriod, "service_lookup_failed",
		))
		return
	}

	// Per-call upstream timeout so a wedged Sentry doesn't block the poll.
	upstreamCtx, cancel := context.WithTimeout(ctx, sentryUpstreamTimeout)
	defer cancel()

	count, err := client.GetProjectIssueCount(upstreamCtx, projectSlug, statsPeriod)
	switch {
	case err == nil:
		// Live data — happy path.
		errCount := count
		resp := SentryStatsResponse{
			Configured:        true,
			Enabled:           true,
			ServiceID:         serviceUUID.String(),
			SentryProjectSlug: projectSlug,
			StatsPeriod:       statsPeriod,
			ErrorCount:        &errCount,
			IssueCount:        &errCount, // received-event count doubles as issue count for now
			OrgSlug:           client.OrgSlug(),
			FetchedAt:         time.Now().UTC(),
			Errors:            []SentryErrorEntry{},
			Stats:             map[string]interface{}{"error_count": errCount},
		}
		storeSentryCache(cacheKey, resp)
		c.JSON(http.StatusOK, resp)
		return

	case errors.Is(err, sentry.ErrNotFound):
		// Soft case: token works, but the project slug doesn't match
		// anything in the org. UI renders a neutral "no Sentry project"
		// chip rather than alarming red. We still cache to suppress
		// repeated 404s during the 60s window.
		resp := SentryStatsResponse{
			Configured:        true,
			Enabled:           true,
			Reason:            "no_sentry_project",
			ServiceID:         serviceUUID.String(),
			SentryProjectSlug: projectSlug,
			StatsPeriod:       statsPeriod,
			ErrorCount:        nil,
			OrgSlug:           client.OrgSlug(),
			FetchedAt:         time.Now().UTC(),
			Errors:            []SentryErrorEntry{},
			Stats:             map[string]interface{}{},
		}
		storeSentryCache(cacheKey, resp)
		h.logger.Info(ctx, "sentry: no project for service",
			logging.String("service_id", serviceUUID.String()),
			logging.String("attempted_slug", projectSlug),
			logging.String("service_name", serviceName))
		c.JSON(http.StatusOK, resp)
		return

	case errors.Is(err, sentry.ErrUnconfigured):
		// Shouldn't happen — we checked IsConfigured() above. Defence in depth.
		c.JSON(http.StatusOK, disabledSentryResponse(
			serviceUUID.String(), statsPeriod, "sentry_unconfigured",
		))
		return

	case errors.Is(err, sentry.ErrUnauthorized),
		errors.Is(err, sentry.ErrRateLimited):
		// Sentry SaaS rejected us (token rotated, scopes too narrow, or
		// rate-limited). Per the parity audit, the dashboard must not
		// surface 5xx for these — the badge renders "no errors" instead.
		// We log at error so operators still see token problems.
		h.logger.Error(ctx, "sentry: upstream unavailable",
			logging.Error("error", err))
		c.JSON(http.StatusOK, disabledSentryResponse(
			serviceUUID.String(), statsPeriod, "upstream_unavailable",
		))
		return

	default:
		// Network errors, timeouts, unexpected status codes. Same
		// graceful-degradation policy: 200 OK with upstream_unavailable
		// so polling consumers don't generate console noise.
		h.logger.Error(ctx, "sentry: upstream error",
			logging.Error("error", err))
		c.JSON(http.StatusOK, disabledSentryResponse(
			serviceUUID.String(), statsPeriod, "upstream_unavailable",
		))
		return
	}
}

// callerIsAdmin reports whether the authenticated caller carries the admin
// (or superadmin) role. Mirrors the hierarchy in JWTManager.RequireRole so
// the in-handler check stays consistent with route-level guards elsewhere
// in the API. A missing user_role context value returns false — we treat
// "no role attached" as no privilege rather than panicking.
func callerIsAdmin(c *gin.Context) bool {
	v, ok := c.Get("user_role")
	if !ok {
		return false
	}
	role, ok := v.(string)
	if !ok {
		return false
	}
	return role == "admin" || role == "superadmin"
}

// resolveSentryProjectSlug returns the Sentry project slug for a service,
// preferring the explicit override column when set and falling back to the
// service name otherwise.
func (h *Handler) resolveSentryProjectSlug(ctx context.Context, serviceID uuid.UUID) (slug, name string, err error) {
	// The services table has both `name` and the new optional
	// `sentry_project_slug` column (migration 019). We deliberately query
	// directly here instead of going through ServiceRepository.GetByID
	// because that scan path doesn't include the override column.
	var override sql.NullString
	row := h.repos.DB().QueryRowContext(ctx,
		`SELECT name, sentry_project_slug FROM services WHERE id = $1`,
		serviceID,
	)
	if err := row.Scan(&name, &override); err != nil {
		return "", "", err
	}
	if override.Valid && strings.TrimSpace(override.String) != "" {
		return strings.TrimSpace(override.String), name, nil
	}
	return name, name, nil
}

// lookupSentryCache returns a cached entry if it's still fresh, else (zero, false).
func lookupSentryCache(key string) (SentryStatsResponse, bool) {
	sentryStatsCacheMu.Lock()
	defer sentryStatsCacheMu.Unlock()
	entry, ok := sentryStatsCache[key]
	if !ok {
		return SentryStatsResponse{}, false
	}
	if time.Now().After(entry.expireAt) {
		delete(sentryStatsCache, key)
		return SentryStatsResponse{}, false
	}
	return entry.resp, true
}

// storeSentryCache records a fresh response with a 60s expiry.
func storeSentryCache(key string, resp SentryStatsResponse) {
	sentryStatsCacheMu.Lock()
	defer sentryStatsCacheMu.Unlock()
	sentryStatsCache[key] = sentryCacheEntry{
		resp:     resp,
		expireAt: time.Now().Add(sentryCacheTTL),
	}
}

// clearSentryCacheForTest is exposed for tests so they can run sequentially
// without state leaking between cases. It's intentionally not exported in
// the public surface (lowercase first letter on the helper, but accessible
// from sentry_handlers_test.go in the same package).
func clearSentryCacheForTest() {
	sentryStatsCacheMu.Lock()
	defer sentryStatsCacheMu.Unlock()
	sentryStatsCache = map[string]sentryCacheEntry{}
}
