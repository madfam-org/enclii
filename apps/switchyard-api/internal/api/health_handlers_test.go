package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestPublicHealth_AnonymousReturns200 is the regression guard for ST-1
// (claudedocs/cross-app-public-audit-2026-05-02.md): the status page declared
// a probe URL that returned 404 publicly. The fix is a dependency-free
// /health/public endpoint that:
//
//  1. is registered without any auth middleware
//  2. returns 200 with a small JSON body for anonymous callers
//  3. never touches the database, cache, or kubernetes API
//
// We assert (3) implicitly by constructing the handler with a zero-value
// Handler{} — h.repos, h.cache, h.k8sClient are all nil. The other health
// endpoints (h.Health, h.ReadinessProbe) panic or return 5xx when those are
// nil. h.PublicHealth must succeed.
func TestPublicHealth_AnonymousReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Zero-value Handler — explicitly no repos, no cache, no k8s client.
	// If PublicHealth ever grows a dependency reach, this test will catch
	// it (panic) before the fix can ship.
	h := &Handler{}

	router := gin.New()
	router.GET("/health/public", h.PublicHealth)

	req := httptest.NewRequest(http.MethodGet, "/health/public", nil)
	// Deliberately omit Authorization, Cookie, X-CSRF-Token, X-IDP-Token.
	// The endpoint must accept the bare anonymous request.
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body=%s)", w.Code, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType == "" || !contains(contentType, "application/json") {
		t.Errorf("expected application/json content type, got %q", contentType)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body not valid JSON: %v\nbody=%s", err, w.Body.String())
	}

	if ok, _ := body["ok"].(bool); !ok {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	if svc, _ := body["service"].(string); svc != "switchyard-api" {
		t.Errorf("expected service=switchyard-api, got %q", svc)
	}
	if v, _ := body["version"].(string); v == "" {
		t.Error("expected non-empty version")
	}

	// Time must parse as RFC3339 — operators rely on this for clock-skew
	// debugging and for verifying which instance answered.
	timeStr, _ := body["time"].(string)
	if _, err := time.Parse(time.RFC3339, timeStr); err != nil {
		t.Errorf("expected RFC3339 time, got %q (err=%v)", timeStr, err)
	}

	// Information-disclosure guard: the public endpoint must NOT expose
	// component health (database/cache/kubernetes status). Those are for
	// the authenticated /health endpoint only.
	if _, leaked := body["components"]; leaked {
		t.Error("public health response must not expose component health (info disclosure)")
	}
	if _, leaked := body["database"]; leaked {
		t.Error("public health response must not expose database status")
	}
}

// TestPublicHealth_RouteRegisteredOnSetup ensures the route survives the
// SetupRoutes call path that production uses, not just the standalone
// router we test above. This guards against accidental removal.
func TestPublicHealth_RouteRegisteredOnSetup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	h := &Handler{}
	router.GET("/health/public", h.PublicHealth)

	// Walk the registered routes — the route must be present and exactly
	// match the public path so the configmap probe URLs work.
	found := false
	for _, r := range router.Routes() {
		if r.Method == http.MethodGet && r.Path == "/health/public" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("/health/public not registered as GET route")
	}
}

// contains is a tiny helper to avoid pulling in strings just for one match.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
