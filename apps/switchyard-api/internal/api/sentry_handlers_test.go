package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/integrations/sentry"
)

// asAdmin is a tiny test-only middleware that pretends the caller is admin
// by populating the user_role context key the handler reads via
// callerIsAdmin(). The production routes mount this via JWTManager's
// AuthMiddleware → RequireRole chain; tests stub it out here so they can
// exercise the handler in isolation without spinning up a JWT signer.
func asAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_role", "admin")
		c.Next()
	}
}

// TestGetSentryServiceStats_NotConfigured verifies the structured 200 OK path
// — this is the contract the UI relies on to hide the badge gracefully.
func TestGetSentryServiceStats_NotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clearSentryCacheForTest()

	// Empty client → IsConfigured()=false.
	h := &Handler{
		sentryClient: sentry.NewClientWithConfig("https://sentry.io", "", "", nil),
	}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/observability/sentry", asAdmin(), h.GetSentryServiceStats)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/observability/sentry?service=11111111-1111-1111-1111-111111111111", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when unconfigured, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp SentryStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response json: %v", err)
	}
	if resp.Configured {
		t.Errorf("expected configured=false when env vars unset")
	}
	if resp.Enabled {
		t.Errorf("expected enabled=false when env vars unset")
	}
	if resp.Reason != "sentry_unconfigured" {
		t.Errorf("expected reason=sentry_unconfigured, got %q", resp.Reason)
	}
	if resp.Errors == nil {
		t.Errorf("expected errors=[] (non-nil) on disabled path, got nil")
	}
	if resp.Stats == nil {
		t.Errorf("expected stats={} (non-nil) on disabled path, got nil")
	}
}

// TestGetSentryServiceStats_NonAdminGetsForbiddenReason verifies the
// in-handler role gate: an authenticated but non-admin caller must not
// receive 403 (which would surface as console errors on every dashboard
// poll). They get 200 OK with reason=forbidden so the badge renders
// "no errors", same as the unconfigured path.
func TestGetSentryServiceStats_NonAdminGetsForbiddenReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clearSentryCacheForTest()

	// Fully configured client to ensure we're not short-circuiting on
	// IsConfigured(). The forbidden gate must trip before that check.
	h := &Handler{
		sentryClient: sentry.NewClientWithConfig("https://sentry.io", "org", "tok", nil),
	}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	// Note: no asAdmin() wrapper. We attach a developer role so
	// callerIsAdmin() returns false.
	engine.GET("/v1/observability/sentry", func(c *gin.Context) {
		c.Set("user_role", "developer")
		c.Next()
	}, h.GetSentryServiceStats)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/observability/sentry?service=11111111-1111-1111-1111-111111111111", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for non-admin (graceful degradation), got %d (body: %s)",
			w.Code, w.Body.String())
	}
	var resp SentryStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response json: %v", err)
	}
	if resp.Configured || resp.Enabled {
		t.Errorf("expected configured=false enabled=false for non-admin, got configured=%v enabled=%v",
			resp.Configured, resp.Enabled)
	}
	if resp.Reason != "forbidden" {
		t.Errorf("expected reason=forbidden, got %q", resp.Reason)
	}
	if resp.ErrorCount != nil {
		t.Errorf("expected error_count=nil for forbidden reason, got %v", *resp.ErrorCount)
	}
	if resp.Errors == nil || len(resp.Errors) != 0 {
		t.Errorf("expected errors=[], got %#v", resp.Errors)
	}
}

// TestGetSentryServiceStats_MissingServiceParam verifies 400 on bad input.
func TestGetSentryServiceStats_MissingServiceParam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clearSentryCacheForTest()

	h := &Handler{
		sentryClient: sentry.NewClientWithConfig("https://sentry.io", "org", "tok", nil),
	}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/observability/sentry", h.GetSentryServiceStats)

	req, _ := http.NewRequest(http.MethodGet, "/v1/observability/sentry", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestGetSentryServiceStats_InvalidUUID verifies the UUID parser rejects
// junk input before we touch the DB.
func TestGetSentryServiceStats_InvalidUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clearSentryCacheForTest()

	h := &Handler{
		sentryClient: sentry.NewClientWithConfig("https://sentry.io", "org", "tok", nil),
	}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/observability/sentry", h.GetSentryServiceStats)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/observability/sentry?service=not-a-uuid", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid UUID, got %d", w.Code)
	}
}

// TestGetSentryServiceStats_NeverReturns5xx is the truthfulness contract
// regression test from the 2026-05-04 dashboard audit. The handler must
// never return any 5xx status — every short-circuit path is required to
// emit 200 OK with a `reason` so the per-service polling loop on the
// dashboard does not generate console errors. 503 here would specifically
// be a category error: 503 is reserved for "Sentry's API is down", which
// this handler does not check directly. Unconfigured / forbidden /
// no_sentry_project / upstream_unavailable are all "no error to surface"
// states.
//
// The cases covered:
//   - Unconfigured client (no env vars)            → 200, reason=sentry_unconfigured
//   - Authenticated non-admin caller               → 200, reason=forbidden
//   - Configured client but bogus serviceID format → 400, JSON error (input bug, not a poll error)
//
// We deliberately do not exercise the live-Sentry path here — that's
// covered by the existing happy-path / not-found tests with a fake server.
func TestGetSentryServiceStats_NeverReturns5xx(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name       string
		client     *sentry.Client
		role       string
		query      string
		wantStatus int
		wantReason string // empty = don't assert
	}{
		{
			name:       "unconfigured returns 200",
			client:     sentry.NewClientWithConfig("https://sentry.io", "", "", nil),
			role:       "admin",
			query:      "service=11111111-1111-1111-1111-111111111111",
			wantStatus: http.StatusOK,
			wantReason: "sentry_unconfigured",
		},
		{
			name:       "non-admin returns 200 with forbidden reason",
			client:     sentry.NewClientWithConfig("https://sentry.io", "org", "tok", nil),
			role:       "developer",
			query:      "service=11111111-1111-1111-1111-111111111111",
			wantStatus: http.StatusOK,
			wantReason: "forbidden",
		},
		{
			// Bad input is the one allowed non-200: a 400 is the right
			// status for a malformed query string and frontend code
			// shouldn't ever produce one. This is fundamentally
			// different from a per-service polling 5xx.
			name:       "bad UUID returns 400 (not 5xx)",
			client:     sentry.NewClientWithConfig("https://sentry.io", "", "", nil),
			role:       "admin",
			query:      "service=not-a-uuid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearSentryCacheForTest()
			h := &Handler{sentryClient: tc.client}

			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.GET("/v1/observability/sentry", func(c *gin.Context) {
				c.Set("user_role", tc.role)
				c.Next()
			}, h.GetSentryServiceStats)

			req, _ := http.NewRequest(http.MethodGet,
				"/v1/observability/sentry?"+tc.query, nil)
			engine.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("got status %d, want %d (body: %s)",
					w.Code, tc.wantStatus, w.Body.String())
			}
			if w.Code >= 500 {
				t.Fatalf("handler returned 5xx (%d) — truthfulness contract violated",
					w.Code)
			}
			if tc.wantReason != "" {
				var resp SentryStatsResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("invalid JSON: %v (body: %s)", err, w.Body.String())
				}
				if resp.Reason != tc.wantReason {
					t.Errorf("got reason=%q, want %q", resp.Reason, tc.wantReason)
				}
				if resp.Configured {
					t.Errorf("expected configured=false on disabled path")
				}
			}
		})
	}
}

// TestDisabledSentryResponse_IncludesServiceLookupFailedReason verifies the
// new reason taxonomy entry. The frontend reads `reason` to decide whether
// to render a neutral chip or hide entirely; service_lookup_failed should
// behave like sentry_unconfigured (hide).
func TestDisabledSentryResponse_IncludesServiceLookupFailedReason(t *testing.T) {
	resp := disabledSentryResponse("svc-uuid", "24h", "service_lookup_failed")

	if resp.Configured || resp.Enabled {
		t.Errorf("disabled response must have configured=false enabled=false")
	}
	if resp.Reason != "service_lookup_failed" {
		t.Errorf("expected reason=service_lookup_failed, got %q", resp.Reason)
	}
	if resp.ErrorCount != nil {
		t.Errorf("expected error_count=nil on disabled path, got %v", *resp.ErrorCount)
	}
	if resp.Errors == nil {
		t.Errorf("expected errors=[], got nil")
	}
	if resp.Stats == nil {
		t.Errorf("expected stats={}, got nil")
	}
}

// TestSentryCache_RoundTrip verifies the in-memory 60s cache stores and
// retrieves entries by composite key.
func TestSentryCache_RoundTrip(t *testing.T) {
	clearSentryCacheForTest()
	defer clearSentryCacheForTest()

	count := 7
	want := SentryStatsResponse{
		Configured:        true,
		ServiceID:         "abc",
		SentryProjectSlug: "switchyard-api",
		StatsPeriod:       "24h",
		ErrorCount:        &count,
	}
	storeSentryCache("abc|24h", want)

	got, ok := lookupSentryCache("abc|24h")
	if !ok {
		t.Fatalf("expected cache hit immediately after store")
	}
	if got.ErrorCount == nil || *got.ErrorCount != 7 {
		t.Errorf("expected error count 7, got %v", got.ErrorCount)
	}

	// Different key → miss.
	if _, ok := lookupSentryCache("abc|1h"); ok {
		t.Errorf("expected cache miss for different key")
	}
}
