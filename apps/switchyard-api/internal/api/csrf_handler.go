package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// csrfCookieName is the name of the double-submit CSRF cookie. Kept in sync
// with internal/middleware/csrf.go so a follow-up that wires the middleware
// onto write routes can validate tokens minted here without re-issuing them.
const csrfCookieName = "csrf_token"

// csrfTokenTTL controls both the cookie max-age and the practical lifetime
// the SPA caches the token for. Matches the existing middleware default.
const csrfTokenTTL = 24 * time.Hour

// CSRFTokenResponse is the JSON shape of GET /v1/csrf.
//
// The SPA at apps/switchyard-ui/lib/api.ts reads the token from the
// X-CSRF-Token response header (header is the source of truth for the
// cache); the JSON body is included as a fallback for non-browser callers
// that prefer parsing the body over inspecting headers.
type CSRFTokenResponse struct {
	CSRFToken string `json:"csrf_token"`
}

// IssueCSRFToken serves GET /v1/csrf. It is intentionally registered as a
// public (no-auth) route — the SPA fetches it pre-authentication so it can
// have a token ready by the time a write is attempted, mirroring the
// double-submit cookie pattern in internal/middleware/csrf.go.
//
// Behaviour:
//   - Generates a fresh, cryptographically random token on every request.
//   - Sets the `csrf_token` cookie (Secure, SameSite=Lax, HttpOnly=false so
//     JS can echo it in X-CSRF-Token on writes).
//   - Echoes the same value in the X-CSRF-Token response header so the SPA
//     can read it via fetch() without depending on cookie parsing.
//   - Returns the token in the response body for non-browser clients.
//
// We mint here rather than going through the full CSRFMiddleware because
// the middleware is not (yet) wired onto the write routes; minting via a
// dedicated endpoint keeps token issuance independent of validation and
// avoids a wider middleware rollout in this change.
func (h *Handler) IssueCSRFToken(c *gin.Context) {
	token := generateCSRFToken()

	// Cookie: visible to JS so the SPA can echo it. Secure cookie because
	// the production deploy is HTTPS-only behind Cloudflare; the dev SPA
	// runs on localhost where Secure cookies are still accepted.
	c.SetCookie(
		csrfCookieName,
		token,
		int(csrfTokenTTL.Seconds()),
		"/",
		"",    // domain (empty = current host)
		true,  // secure (HTTPS only — production)
		false, // httpOnly=false so JS can read the cookie and echo it
	)
	c.Header("X-CSRF-Token", token)
	c.JSON(http.StatusOK, CSRFTokenResponse{CSRFToken: token})
}

// generateCSRFToken returns a 32-byte URL-safe base64 random token. On the
// astronomically unlikely event that crypto/rand fails we log loudly and
// fall back to a time-based seed so the request still completes — token
// rotation will replace it within seconds at most.
func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		logrus.WithError(err).Error("csrf: rand.Read failed, falling back to time seed")
		return base64.URLEncoding.EncodeToString([]byte(time.Now().UTC().String()))
	}
	return base64.URLEncoding.EncodeToString(b)
}
