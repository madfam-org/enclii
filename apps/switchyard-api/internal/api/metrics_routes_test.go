package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
)

// Regression guard: https://api.enclii.dev/metrics answered 200 with the full
// Prometheus exposition to anyone on the internet. /metrics must stay
// reachable for the in-cluster scrapers (Host "<podIP>:4200", no Cloudflare
// headers) and return 404 for anything that came through the tunnel.
func newMetricsRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerMetricsRoutes(router, monitoring.NewMetricsCollector().Handler())
	return router
}

func TestMetricsRoute_PublicRequestsGet404(t *testing.T) {
	router := newMetricsRouter(t)

	cases := []struct {
		name   string
		host   string
		header string
	}{
		{name: "public host", host: "api.enclii.dev"},
		{name: "cf-connecting-ip", host: "10.42.1.2:4200", header: "cf-connecting-ip"},
		{name: "cf-ray", host: "10.42.1.2:4200", header: "cf-ray"},
		{name: "cdn-loop", host: "10.42.1.2:4200", header: "cdn-loop"},
	}
	for _, path := range []string{"/metrics", "/metrics/"} {
		for _, tc := range cases {
			t.Run(tc.name+" "+path, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Host = tc.host
				if tc.header != "" {
					req.Header.Set(tc.header, "203.0.113.7")
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				assert.Equal(t, http.StatusNotFound, w.Code)
				assert.NotContains(t, w.Body.String(), "# TYPE")
			})
		}
	}
}

func TestMetricsRoute_ScraperRequestsGetExposition(t *testing.T) {
	router := newMetricsRouter(t)

	for _, path := range []string{"/metrics", "/metrics/"} {
		for _, host := range []string{
			"10.42.1.2:4200",
			"switchyard-api.enclii.svc:4200",
			"localhost:4200",
			"switchyard-api",
		} {
			t.Run(host+" "+path, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Host = host
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code)
				assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
				assert.Contains(t, w.Body.String(), "# TYPE ")
			})
		}
	}
}
