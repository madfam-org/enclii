package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestIssueCSRFToken_ReturnsTokenViaHeaderCookieAndBody verifies the SPA
// contract: GET /v1/csrf must surface the token in three places (header,
// cookie, body) so callers can pick whichever fits their stack. The SPA at
// apps/switchyard-ui/lib/api.ts reads X-CSRF-Token from the response header.
func TestIssueCSRFToken_ReturnsTokenViaHeaderCookieAndBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/csrf", h.IssueCSRFToken)

	req, _ := http.NewRequest(http.MethodGet, "/v1/csrf", nil)
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", w.Code, w.Body.String())
	}

	headerToken := w.Header().Get("X-CSRF-Token")
	if headerToken == "" {
		t.Fatalf("expected X-CSRF-Token response header to be set")
	}

	var body CSRFTokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v (body: %s)", err, w.Body.String())
	}
	if body.CSRFToken == "" {
		t.Fatalf("expected csrf_token in body, got empty string")
	}
	if body.CSRFToken != headerToken {
		t.Errorf("expected body csrf_token to match X-CSRF-Token header; got body=%q header=%q",
			body.CSRFToken, headerToken)
	}

	// Cookie must be present, not HttpOnly (so JS can echo it), and the
	// value must match the header/body token.
	var found bool
	for _, ck := range w.Result().Cookies() {
		if ck.Name != csrfCookieName {
			continue
		}
		found = true
		if ck.HttpOnly {
			t.Errorf("csrf_token cookie must NOT be HttpOnly so JS can read it")
		}
		// gin URL-encodes cookie values (so `=` becomes `%3D`). Decode
		// before comparing — the SPA's document.cookie reader will see
		// the decoded value, which must match what we put in the header.
		decoded, err := url.QueryUnescape(ck.Value)
		if err != nil {
			t.Fatalf("cookie value not URL-decodable: %v", err)
		}
		if decoded != headerToken {
			t.Errorf("decoded cookie %q does not match header token %q", decoded, headerToken)
		}
	}
	if !found {
		t.Errorf("expected csrf_token cookie to be set")
	}
}

// TestIssueCSRFToken_NoAuthRequired confirms the handler does not look at
// any auth context — it is mounted on the public router branch and must
// work pre-authentication so the SPA can fetch a token before login.
// We simulate "no auth" simply by not attaching any Authorization header
// or auth middleware; if the handler reached for one, the test would fail.
func TestIssueCSRFToken_NoAuthRequired(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{}

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.GET("/v1/csrf", h.IssueCSRFToken)

	req, _ := http.NewRequest(http.MethodGet, "/v1/csrf", nil)
	// Deliberately no Authorization / no cookies attached.
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even without auth, got %d", w.Code)
	}
}

// TestIssueCSRFToken_FreshTokenPerCall verifies the handler does not cache
// or reuse tokens across requests. Two consecutive calls must produce
// different values — important so a stolen cookie can't be replayed
// indefinitely once the SPA fetches a new one.
func TestIssueCSRFToken_FreshTokenPerCall(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{}

	_, engine := gin.CreateTestContext(httptest.NewRecorder())
	engine.GET("/v1/csrf", h.IssueCSRFToken)

	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest(http.MethodGet, "/v1/csrf", nil)
	engine.ServeHTTP(w1, req1)

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/v1/csrf", nil)
	engine.ServeHTTP(w2, req2)

	t1 := w1.Header().Get("X-CSRF-Token")
	t2 := w2.Header().Get("X-CSRF-Token")
	if t1 == "" || t2 == "" {
		t.Fatalf("expected non-empty tokens; t1=%q t2=%q", t1, t2)
	}
	if t1 == t2 {
		t.Errorf("expected fresh token per request, got duplicate %q", t1)
	}
}
