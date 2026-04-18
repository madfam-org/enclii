package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// loggedLogsQuery delegates to the P2.1 Loki-backed handler, with a
// 503 fallback when the feature hasn't been wired (local-dev without
// ENCLII_LOKI_URL). Matches the pattern used by /v1/audit which also
// optionally 503s when the audit aggregator is nil.
func (h *Handler) loggedLogsQuery(c *gin.Context) {
	if h.logsHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  "log_store_not_configured",
			"detail": "Loki integration is not enabled for this deployment.",
		})
		return
	}
	h.logsHandler.Query(c)
}

// loggedLogsTail is the WebSocket counterpart of loggedLogsQuery.
func (h *Handler) loggedLogsTail(c *gin.Context) {
	if h.logsHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  "log_store_not_configured",
			"detail": "Loki integration is not enabled for this deployment.",
		})
		return
	}
	h.logsHandler.Tail(c)
}
