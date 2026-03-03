package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// CSRFMiddleware provides Cross-Site Request Forgery protection using the
// stateless double-submit cookie pattern. The client must echo the cookie
// value in the X-CSRF-Token header — no server-side token storage is needed,
// so this works correctly across multiple API replicas.
type CSRFMiddleware struct {
	cookieName string
	headerName string
	tokenTTL   time.Duration
}

// NewCSRFMiddleware creates a new CSRF protection middleware
func NewCSRFMiddleware() *CSRFMiddleware {
	return &CSRFMiddleware{
		cookieName: "csrf_token",
		headerName: "X-CSRF-Token",
		tokenTTL:   24 * time.Hour,
	}
}

// Middleware returns a Gin middleware function for CSRF protection
func (c *CSRFMiddleware) Middleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// Skip CSRF check for safe methods
		if ctx.Request.Method == "GET" || ctx.Request.Method == "HEAD" || ctx.Request.Method == "OPTIONS" {
			// Generate and set CSRF token for safe methods
			token := c.generateToken()
			ctx.SetCookie(
				c.cookieName,
				token,
				int(c.tokenTTL.Seconds()),
				"/",
				"",    // domain (empty = current domain)
				true,  // secure (HTTPS only)
				false, // httpOnly must be false so JS can read and echo it
			)
			ctx.Header(c.headerName, token)
			ctx.Next()
			return
		}

		// For unsafe methods (POST, PUT, DELETE, PATCH), validate CSRF token
		cookieToken, err := ctx.Cookie(c.cookieName)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"path":   ctx.Request.URL.Path,
				"method": ctx.Request.Method,
				"ip":     ctx.ClientIP(),
			}).Warn("CSRF token missing from cookie")

			ctx.JSON(http.StatusForbidden, gin.H{
				"error": "CSRF token missing",
			})
			ctx.Abort()
			return
		}

		// Check token in header
		headerToken := ctx.GetHeader(c.headerName)
		if headerToken == "" {
			logrus.WithFields(logrus.Fields{
				"path":   ctx.Request.URL.Path,
				"method": ctx.Request.Method,
				"ip":     ctx.ClientIP(),
			}).Warn("CSRF token missing from header")

			ctx.JSON(http.StatusForbidden, gin.H{
				"error":  "CSRF token required in header",
				"header": c.headerName,
			})
			ctx.Abort()
			return
		}

		// Double-submit cookie pattern: cookie value must match header value.
		// An attacker on a different origin cannot read the cookie to echo it.
		if cookieToken != headerToken {
			logrus.WithFields(logrus.Fields{
				"path":   ctx.Request.URL.Path,
				"method": ctx.Request.Method,
				"ip":     ctx.ClientIP(),
			}).Warn("CSRF token mismatch")

			ctx.JSON(http.StatusForbidden, gin.H{
				"error": "CSRF token mismatch",
			})
			ctx.Abort()
			return
		}

		ctx.Next()
	}
}

// generateToken creates a new CSRF token
func (c *CSRFMiddleware) generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		logrus.WithError(err).Error("Failed to generate CSRF token")
		return base64.URLEncoding.EncodeToString([]byte(time.Now().String()))
	}
	return base64.URLEncoding.EncodeToString(b)
}
