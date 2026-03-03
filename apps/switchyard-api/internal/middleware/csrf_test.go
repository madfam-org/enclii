package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCSRFMiddleware_GetRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	csrf := NewCSRFMiddleware()
	router.Use(csrf.Middleware())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-CSRF-Token"))

	// Check cookie is set and NOT httpOnly (JS must read it for double-submit)
	cookies := w.Result().Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == "csrf_token" {
			found = true
			assert.False(t, cookie.HttpOnly, "CSRF cookie must not be httpOnly so JS can read it")
			assert.True(t, cookie.Secure)
			break
		}
	}
	assert.True(t, found, "CSRF cookie should be set")
}

func TestCSRFMiddleware_PostWithoutToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	csrf := NewCSRFMiddleware()
	router.Use(csrf.Middleware())
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest("POST", "/test", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "CSRF token missing")
}

func TestCSRFMiddleware_PostWithValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	csrf := NewCSRFMiddleware()
	router.Use(csrf.Middleware())
	router.GET("/test-get", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Get a CSRF token via GET
	getReq := httptest.NewRequest("GET", "/test-get", nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)

	token := getW.Header().Get("X-CSRF-Token")
	assert.NotEmpty(t, token)

	var cookieValue string
	for _, cookie := range getW.Result().Cookies() {
		if cookie.Name == "csrf_token" {
			cookieValue = cookie.Value
			break
		}
	}

	// POST with matching cookie and header (double-submit pattern)
	postReq := httptest.NewRequest("POST", "/test", strings.NewReader("{}"))
	postReq.Header.Set("X-CSRF-Token", token)
	postReq.AddCookie(&http.Cookie{
		Name:  "csrf_token",
		Value: cookieValue,
	})
	postW := httptest.NewRecorder()

	router.ServeHTTP(postW, postReq)

	assert.Equal(t, http.StatusOK, postW.Code)
}

func TestCSRFMiddleware_PostWithMismatchedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	csrf := NewCSRFMiddleware()
	router.Use(csrf.Middleware())
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest("POST", "/test", strings.NewReader("{}"))
	req.Header.Set("X-CSRF-Token", "token-in-header")
	req.AddCookie(&http.Cookie{
		Name:  "csrf_token",
		Value: "different-token-in-cookie",
	})
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "CSRF token mismatch")
}

func TestCSRFMiddleware_StatelessAcrossInstances(t *testing.T) {
	// Verify that the double-submit pattern works across different middleware
	// instances (simulating multi-replica deployment).
	gin.SetMode(gin.TestMode)

	csrf1 := NewCSRFMiddleware()
	csrf2 := NewCSRFMiddleware()

	router1 := gin.New()
	router1.Use(csrf1.Middleware())
	router1.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	router2 := gin.New()
	router2.Use(csrf2.Middleware())
	router2.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// Get token from instance 1
	getReq := httptest.NewRequest("GET", "/test", nil)
	getW := httptest.NewRecorder()
	router1.ServeHTTP(getW, getReq)

	token := getW.Header().Get("X-CSRF-Token")
	assert.NotEmpty(t, token)

	var cookieValue string
	for _, cookie := range getW.Result().Cookies() {
		if cookie.Name == "csrf_token" {
			cookieValue = cookie.Value
			break
		}
	}

	// Submit to instance 2 with the same token — must succeed
	postReq := httptest.NewRequest("POST", "/test", strings.NewReader("{}"))
	postReq.Header.Set("X-CSRF-Token", token)
	postReq.AddCookie(&http.Cookie{
		Name:  "csrf_token",
		Value: cookieValue,
	})
	postW := httptest.NewRecorder()
	router2.ServeHTTP(postW, postReq)

	assert.Equal(t, http.StatusOK, postW.Code, "double-submit pattern must work across replicas")
}

func TestCSRFMiddleware_HeadAndOptionsSkipped(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	csrf := NewCSRFMiddleware()
	router.Use(csrf.Middleware())
	router.HEAD("/test", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.OPTIONS("/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, method := range []string{"HEAD", "OPTIONS"} {
		req := httptest.NewRequest(method, "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Less(t, w.Code, 400, "%s should not be blocked by CSRF", method)
	}
}

func TestCSRFMiddleware_PostWithHeaderButNoCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	csrf := NewCSRFMiddleware()
	router.Use(csrf.Middleware())
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest("POST", "/test", strings.NewReader("{}"))
	req.Header.Set("X-CSRF-Token", "some-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "CSRF token missing")
}
