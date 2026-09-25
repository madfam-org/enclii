package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
)

// registerMetricsRoutes serves the Prometheus exposition on /metrics and
// /metrics/ for in-cluster callers only.
//
// The auth middleware lets /metrics through without authentication so the
// scrapers (the main Prometheus job "switchyard-api", its rules-eval copy and
// the annotation job "kubernetes-pods") can reach it. They dial the pod IP
// directly, so the InternalOnly guard admits them, while anything arriving
// through the Cloudflare tunnel (Host api.enclii.dev, cf-* headers) gets 404.
//
// "/metrics/" is registered explicitly so gin does not answer it with a 301
// redirect that would confirm the route to public callers.
func registerMetricsRoutes(router gin.IRoutes, metricsHandler http.Handler) {
	guard := middleware.InternalOnly()
	h := gin.WrapH(metricsHandler)
	router.GET("/metrics", guard, h)
	router.GET("/metrics/", guard, h)
}
