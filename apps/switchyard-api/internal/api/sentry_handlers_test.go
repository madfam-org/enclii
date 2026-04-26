package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/integrations/sentry"
)

// TestGetSentryServiceStats_NotConfigured verifies the structured 503 path
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
	engine.GET("/v1/observability/sentry", h.GetSentryServiceStats)

	req, _ := http.NewRequest(http.MethodGet,
		"/v1/observability/sentry?service=11111111-1111-1111-1111-111111111111", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when unconfigured, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp SentryStatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response json: %v", err)
	}
	if resp.Configured {
		t.Errorf("expected configured=false when env vars unset")
	}
	if resp.Reason != "sentry_unconfigured" {
		t.Errorf("expected reason=sentry_unconfigured, got %q", resp.Reason)
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
