// Package sentry provides a thin REST client for Sentry's /api/0/ endpoints.
//
// This package intentionally stays small: it exposes only the read paths used
// by the /v1/observability/sentry admin endpoint (parity audit gap #9). It does
// NOT initialise the Sentry SDK for switchyard-api itself — capturing api
// errors into Sentry is a separate concern handled in a different change.
//
// Configuration is environment-driven:
//
//	SENTRY_AUTH_TOKEN  — operator-provisioned token with at minimum
//	                     org:read + project:read + event:read scopes.
//	SENTRY_ORG_SLUG    — Sentry org slug (default: innovaciones-madfam-sas-de-cv).
//	SENTRY_BASE_URL    — Override base URL (default: https://sentry.io).
//	                     Useful for self-hosted Sentry or for pointing the
//	                     organization sub-domain (e.g.
//	                     https://innovaciones-madfam-sas-de-cv.sentry.io).
//
// IsConfigured() returns true only when both AUTH_TOKEN and ORG_SLUG are set.
// The handler uses this to short-circuit with a structured 503
// "sentry-unconfigured" response so the UI can hide the badge gracefully.
package sentry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the public Sentry SaaS endpoint.
	DefaultBaseURL = "https://sentry.io"

	// DefaultOrgSlug matches the MADFAM Sentry org. Overridable via
	// SENTRY_ORG_SLUG.
	DefaultOrgSlug = "innovaciones-madfam-sas-de-cv"

	// DefaultTimeout caps any single Sentry API call. The handler also
	// applies its own 5s context deadline; whichever fires first wins.
	DefaultTimeout = 5 * time.Second
)

// Sentinel errors. Callers (the handler) inspect via errors.Is so they can
// translate to the right HTTP status without leaking auth-token details.
var (
	// ErrUnconfigured indicates the env vars required to talk to Sentry
	// (SENTRY_AUTH_TOKEN + SENTRY_ORG_SLUG) are missing. The handler
	// surfaces this as 503 with reason="sentry_unconfigured".
	ErrUnconfigured = errors.New("sentry: not configured (SENTRY_AUTH_TOKEN or SENTRY_ORG_SLUG missing)")

	// ErrUnauthorized indicates Sentry rejected the auth token (401).
	// Most often the token was rotated or its scopes were narrowed.
	ErrUnauthorized = errors.New("sentry: unauthorized — check SENTRY_AUTH_TOKEN scopes (org:read + project:read + event:read)")

	// ErrNotFound indicates the project slug does not exist in the org.
	// The handler treats this as a soft "no_sentry_project" hint rather
	// than an error, so the UI keeps showing the badge as "configured".
	ErrNotFound = errors.New("sentry: project not found")

	// ErrRateLimited indicates Sentry returned 429. The handler surfaces
	// this as 502 with a generic message; the cache layer absorbs the
	// next 60s of requests so we don't aggravate the limit.
	ErrRateLimited = errors.New("sentry: rate limited (429)")
)

// Client is the minimal Sentry REST API client used by the observability
// handler. All methods accept a context for cancellation/timeout propagation.
type Client struct {
	baseURL    string
	orgSlug    string
	authToken  string
	httpClient *http.Client
}

// NewClient constructs a Sentry client from environment variables. If either
// SENTRY_AUTH_TOKEN or SENTRY_ORG_SLUG is missing the returned client's
// IsConfigured() will return false and all methods will return ErrUnconfigured;
// this lets the handler wire the client unconditionally and decide per-request
// whether to short-circuit.
func NewClient() *Client {
	return NewClientFromEnv(os.Getenv)
}

// NewClientFromEnv is the testable form of NewClient: pass any env-resolver
// (typically os.Getenv in production, or a map-backed func in tests).
func NewClientFromEnv(getenv func(string) string) *Client {
	baseURL := strings.TrimRight(getenv("SENTRY_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	orgSlug := getenv("SENTRY_ORG_SLUG")
	if orgSlug == "" {
		// Allow empty (IsConfigured() will return false); we deliberately
		// don't auto-fill DefaultOrgSlug here because doing so would silently
		// hide an unconfigured deployment. The operator-provisioned secret
		// must set this explicitly.
		orgSlug = ""
	}
	return &Client{
		baseURL:   baseURL,
		orgSlug:   orgSlug,
		authToken: getenv("SENTRY_AUTH_TOKEN"),
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// NewClientWithConfig builds a client with explicit values. Used by tests to
// point at httptest.NewServer without mutating process env.
func NewClientWithConfig(baseURL, orgSlug, authToken string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		orgSlug:    orgSlug,
		authToken:  authToken,
		httpClient: httpClient,
	}
}

// IsConfigured reports whether the client has both an auth token and an org
// slug. The handler uses this to return a structured 503 before issuing any
// upstream call, so the UI can hide the badge gracefully.
func (c *Client) IsConfigured() bool {
	return c != nil && c.authToken != "" && c.orgSlug != ""
}

// OrgSlug returns the configured Sentry org slug. Useful for the UI tooltip
// link generation in upstream callers.
func (c *Client) OrgSlug() string {
	if c == nil {
		return ""
	}
	return c.orgSlug
}

// projectStatsResponse models the Sentry /projects/{org}/{proj}/stats/ payload.
//
// Sentry returns a JSON array of [unix_timestamp_seconds, count] pairs, e.g.:
//
//	[[1714032000, 4], [1714035600, 7], ...]
//
// We sum the counts for the requested window. Because the response is an
// untyped JSON array we decode into [][]float64 (timestamps are ints, but
// counts have been observed as floats in some org configurations).
type projectStatsResponse [][]float64

// GetProjectIssueCount returns the total received-event count for the project
// over the requested stats period. statsPeriod follows Sentry's relative-time
// notation: "24h", "1h", "7d", "14d", "30d". The "stat=received" series is the
// one Sentry recommends for "did anything blow up".
//
// Returns ErrUnconfigured / ErrUnauthorized / ErrNotFound / ErrRateLimited
// per the wrapper sentinels. All other 4xx/5xx surface as a generic error
// with the status code embedded (no token leak — Sentry never echoes the
// token in error bodies).
func (c *Client) GetProjectIssueCount(ctx context.Context, projectSlug, statsPeriod string) (int, error) {
	if !c.IsConfigured() {
		return 0, ErrUnconfigured
	}
	if projectSlug == "" {
		return 0, errors.New("sentry: projectSlug is required")
	}
	if statsPeriod == "" {
		statsPeriod = "24h"
	}

	// Sentry expects raw seconds offsets. We translate "24h"/"1h"/"7d" here
	// rather than passing statsPeriod through, because the /stats/ endpoint
	// accepts since/until pairs more reliably than statsPeriod.
	since, until, err := resolveStatsWindow(statsPeriod)
	if err != nil {
		return 0, err
	}

	q := url.Values{}
	q.Set("stat", "received")
	q.Set("resolution", "1h")
	q.Set("since", fmt.Sprintf("%d", since.Unix()))
	q.Set("until", fmt.Sprintf("%d", until.Unix()))

	endpoint := fmt.Sprintf("%s/api/0/projects/%s/%s/stats/?%s",
		c.baseURL,
		url.PathEscape(c.orgSlug),
		url.PathEscape(projectSlug),
		q.Encode(),
	)

	body, err := c.do(ctx, http.MethodGet, endpoint)
	if err != nil {
		return 0, err
	}

	var stats projectStatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return 0, fmt.Errorf("sentry: decode stats response: %w", err)
	}

	total := 0
	for _, point := range stats {
		if len(point) < 2 {
			continue
		}
		// point[0] = timestamp seconds, point[1] = count
		total += int(point[1])
	}
	return total, nil
}

// GetProjectErrorRate is a convenience derivative: error events / received
// events for the same window. Both numerator and denominator come from the
// same /stats/ endpoint with different `stat=` values.
//
// Returns 0.0 when the denominator is zero (no traffic) — callers should not
// alarm on a zero rate unless they also check the issue count.
func (c *Client) GetProjectErrorRate(ctx context.Context, projectSlug, statsPeriod string) (float64, error) {
	if !c.IsConfigured() {
		return 0, ErrUnconfigured
	}
	received, err := c.GetProjectIssueCount(ctx, projectSlug, statsPeriod)
	if err != nil {
		return 0, err
	}
	if received == 0 {
		return 0, nil
	}
	rejected, err := c.getProjectStatSum(ctx, projectSlug, statsPeriod, "rejected")
	if err != nil {
		// Soft-fail on rejected — return 0 rate rather than fail the call.
		return 0, nil
	}
	if rejected >= received {
		return 1.0, nil
	}
	return float64(rejected) / float64(received), nil
}

// getProjectStatSum is the shared internal that GetProjectIssueCount and the
// error-rate derivative both delegate to. Kept private because the public
// surface is intentionally minimal.
func (c *Client) getProjectStatSum(ctx context.Context, projectSlug, statsPeriod, stat string) (int, error) {
	since, until, err := resolveStatsWindow(statsPeriod)
	if err != nil {
		return 0, err
	}
	q := url.Values{}
	q.Set("stat", stat)
	q.Set("resolution", "1h")
	q.Set("since", fmt.Sprintf("%d", since.Unix()))
	q.Set("until", fmt.Sprintf("%d", until.Unix()))

	endpoint := fmt.Sprintf("%s/api/0/projects/%s/%s/stats/?%s",
		c.baseURL,
		url.PathEscape(c.orgSlug),
		url.PathEscape(projectSlug),
		q.Encode(),
	)
	body, err := c.do(ctx, http.MethodGet, endpoint)
	if err != nil {
		return 0, err
	}
	var stats projectStatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return 0, fmt.Errorf("sentry: decode stats response: %w", err)
	}
	total := 0
	for _, point := range stats {
		if len(point) < 2 {
			continue
		}
		total += int(point[1])
	}
	return total, nil
}

// do performs a single GET against Sentry, normalising common error paths
// into our sentinel errors. The auth token is masked from any error message
// — Sentry never echoes the token, but we belt-and-braces here.
func (c *Client) do(ctx context.Context, method, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("sentry: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.authToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "enclii-switchyard-api/1.0 (+sentry-observability)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sentry: http error: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("sentry: read body: %w", err)
		}
		return body, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		// Drain to free the connection but never bubble the body — it
		// often echoes header fragments we don't want in operator logs.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, ErrUnauthorized
	case http.StatusNotFound:
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, ErrNotFound
	case http.StatusTooManyRequests:
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, ErrRateLimited
	default:
		// Sentry's 5xx bodies are usually safe (no token), but we still
		// truncate to avoid log spam. Status code is the actionable bit.
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("sentry: unexpected status %d (body: %s)", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
}

// resolveStatsWindow translates Sentry's relative-time strings into explicit
// since/until timestamps. We accept "Nh" and "Nd" forms only — anything else
// is a programmer error in the calling handler and surfaces as an error.
func resolveStatsWindow(statsPeriod string) (since, until time.Time, err error) {
	until = time.Now().UTC()
	dur, err := parseStatsPeriod(statsPeriod)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	since = until.Add(-dur)
	return since, until, nil
}

// parseStatsPeriod accepts the subset of Sentry stats-period strings the
// handler actually uses. Keeping this private and minimal avoids leaking a
// half-implemented Sentry parser into the rest of the codebase.
func parseStatsPeriod(statsPeriod string) (time.Duration, error) {
	if statsPeriod == "" {
		return 24 * time.Hour, nil
	}
	last := statsPeriod[len(statsPeriod)-1]
	num := statsPeriod[:len(statsPeriod)-1]
	if num == "" {
		return 0, fmt.Errorf("sentry: invalid statsPeriod %q", statsPeriod)
	}
	var unit time.Duration
	switch last {
	case 'h', 'H':
		unit = time.Hour
	case 'd', 'D':
		unit = 24 * time.Hour
	case 'm', 'M':
		unit = time.Minute
	default:
		return 0, fmt.Errorf("sentry: invalid statsPeriod unit %q (expected h/d/m)", string(last))
	}
	n := 0
	for _, ch := range num {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("sentry: invalid statsPeriod number %q", num)
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("sentry: statsPeriod must be positive (got %q)", statsPeriod)
	}
	return time.Duration(n) * unit, nil
}
