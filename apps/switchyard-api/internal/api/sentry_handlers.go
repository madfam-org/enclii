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
//   - configured=false: env vars missing. UI must hide the badge.
//   - configured=true + reason=no_sentry_project: token works but the project
//     slug doesn't exist in the org. UI shows a neutral chip, not red.
//   - configured=true + error_count!=nil: live data, render normally.
//
// Cached for 60s per (service_id, stats_period) pair to absorb dashboard
// polling. The cache is best-effort — a single-replica control plane
// doesn't need a distributed cache.
type SentryStatsResponse struct {
	Configured        bool      `json:"configured"`
	Reason            string    `json:"reason,omitempty"`
	ServiceID         string    `json:"service_id,omitempty"`
	SentryProjectSlug string    `json:"sentry_project_slug,omitempty"`
	StatsPeriod       string    `json:"stats_period,omitempty"`
	ErrorCount        *int      `json:"error_count"`
	IssueCount        *int      `json:"issue_count,omitempty"`
	OrgSlug           string    `json:"org_slug,omitempty"`
	FetchedAt         time.Time `json:"fetched_at"`
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

	// Short-circuit on configuration. We do this BEFORE touching the DB
	// so an unconfigured deployment doesn't pay an extra query per poll.
	client := h.sentryClientOrDefault()
	if !client.IsConfigured() {
		c.JSON(http.StatusOK, SentryStatsResponse{
			Configured:  false,
			Reason:      "sentry_unconfigured",
			ServiceID:   serviceUUID.String(),
			StatsPeriod: statsPeriod,
			FetchedAt:   time.Now().UTC(),
		})
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
	projectSlug, serviceName, err := h.resolveSentryProjectSlug(ctx, serviceUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "service not found",
			})
			return
		}
		h.logger.Error(ctx, "sentry: resolve project slug failed",
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to resolve service",
		})
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
			ServiceID:         serviceUUID.String(),
			SentryProjectSlug: projectSlug,
			StatsPeriod:       statsPeriod,
			ErrorCount:        &errCount,
			IssueCount:        &errCount, // received-event count doubles as issue count for now
			OrgSlug:           client.OrgSlug(),
			FetchedAt:         time.Now().UTC(),
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
			Reason:            "no_sentry_project",
			ServiceID:         serviceUUID.String(),
			SentryProjectSlug: projectSlug,
			StatsPeriod:       statsPeriod,
			ErrorCount:        nil,
			OrgSlug:           client.OrgSlug(),
			FetchedAt:         time.Now().UTC(),
		}
		storeSentryCache(cacheKey, resp)
		h.logger.Info(ctx, "sentry: no project for service",
			logging.String("service_id", serviceUUID.String()),
			logging.String("attempted_slug", projectSlug),
			logging.String("service_name", serviceName))
		c.JSON(http.StatusOK, resp)
		return

	case errors.Is(err, sentry.ErrUnauthorized):
		h.logger.Error(ctx, "sentry: unauthorized — token rotated or scopes too narrow")
		// 502 with masked details — never echo the token / never echo
		// the upstream body (which could contain header fragments).
		c.JSON(http.StatusBadGateway, gin.H{
			"error":  "sentry upstream rejected our auth",
			"reason": "sentry_unauthorized",
		})
		return

	case errors.Is(err, sentry.ErrRateLimited):
		h.logger.Error(ctx, "sentry: rate limited")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":  "sentry rate limited",
			"reason": "sentry_rate_limited",
		})
		return

	case errors.Is(err, sentry.ErrUnconfigured):
		// Shouldn't happen — we checked IsConfigured() above. Defence in depth.
		c.JSON(http.StatusOK, SentryStatsResponse{
			Configured: false,
			Reason:     "sentry_unconfigured",
			ServiceID:  serviceUUID.String(),
			FetchedAt:  time.Now().UTC(),
		})
		return

	default:
		h.logger.Error(ctx, "sentry: upstream error",
			logging.Error("error", err))
		c.JSON(http.StatusBadGateway, gin.H{
			"error":  "sentry upstream error",
			"reason": "sentry_upstream_error",
		})
		return
	}
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
