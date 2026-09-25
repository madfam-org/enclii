package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newInternalOnlyRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/metrics", InternalOnly(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return router
}

func TestInternalOnly_RejectsPublicHost(t *testing.T) {
	router := newInternalOnlyRouter()
	for _, host := range []string{
		"api.enclii.dev",
		"api.enclii.dev:443",
		"API.ENCLII.DEV",
		"example.com",
		"switchyard-api.enclii", // namespace-qualified but not *.svc
		"",
	} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.Host = host
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "host %q", host)
		assert.Empty(t, w.Body.String(), "host %q", host)
	}
}

func TestInternalOnly_RejectsCloudflareEdgeHeaders(t *testing.T) {
	router := newInternalOnlyRouter()
	for _, header := range []string{"cf-connecting-ip", "CF-Ray", "cdn-loop"} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.Host = "10.42.1.2:4200" // otherwise-internal Host
		req.Header.Set(header, "cloudflare")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "header %q", header)
	}
}

func TestInternalOnly_AdmitsInClusterHosts(t *testing.T) {
	router := newInternalOnlyRouter()
	for _, host := range []string{
		"10.42.1.2:4200",
		"10.42.1.2",
		"[fd00::1]:4200",
		"[::1]",
		"localhost:4200",
		"localhost",
		"switchyard-api",
		"switchyard-api:4200",
		"switchyard-api.enclii.svc:4200",
		"switchyard-api.enclii.svc.cluster.local:4200",
		"switchyard-api.enclii.svc.cluster.local.",
	} {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.Host = host
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "host %q", host)
		assert.Equal(t, "ok", w.Body.String(), "host %q", host)
	}
}

// The guard must compose with the auth middleware's /metrics bypass: an
// in-cluster scrape with no Authorization header still gets through.
func TestInternalOnly_WithAuthBypass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	auth := NewAuthMiddleware(nil)
	router.Use(auth.Middleware())
	router.GET("/metrics", InternalOnly(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Host = "10.42.1.2:4200"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Host = "api.enclii.dev"
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
