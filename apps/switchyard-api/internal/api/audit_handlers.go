package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AuditList proxies to the consolidated audit handler wired via
// SetAuditHandler. See internal/audit/handler.go for the logic. We keep
// the thin shim here so the route table in SetupRoutes is uniform
// (“protected.GET("/audit", h.AuditList)“) with the rest of the file.
//
// Returns 503 if the audit handler wasn't configured — local-dev setups
// without Nexus/Janua wiring land here.
func (h *Handler) AuditList(c *gin.Context) {
	if h.auditHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "consolidated audit surface not configured on this deployment",
		})
		return
	}
	h.auditHandler.List(c)
}

// AuditExport proxies to the admin-only CSV exporter. See internal/audit/
// handler.go for the streaming implementation.
func (h *Handler) AuditExport(c *gin.Context) {
	if h.auditHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "consolidated audit surface not configured on this deployment",
		})
		return
	}
	h.auditHandler.Export(c)
}
