package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
)

// ListDiscoveredOrphans returns the orphan workloads currently tracked by
// the namespace discoverer (parity audit gap #2).
//
// Request:
//   - Method: GET /v1/admin/discovered-orphans
//   - Authorization: Bearer <token> with admin role
//
// Response:
//   - 200 OK: {"orphans": [...]} — possibly empty array
//   - 503 Service Unavailable: orphans repository not configured
//   - 500 Internal Server Error: query failed
//
// Security: route is registered under the admin group in handlers.go,
// which already enforces RequireRole(types.RoleAdmin) at the gin.Group
// level. We deliberately do NOT include arbitrary K8s labels in the
// response (only the namespace, name, kind, image, replicas) — labels
// can leak secret-by-name signals (e.g. "vault-token-mounted=true") and
// the operator does not need them to make rollback decisions.
func (h *Handler) ListDiscoveredOrphans(c *gin.Context) {
	if h.repos == nil || h.repos.DiscoveredOrphans == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "discovered orphans repository not configured"})
		return
	}

	orphans, err := h.repos.DiscoveredOrphans.List(c.Request.Context())
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}

	// Always return an array (never null) so UI clients can iterate
	// without a nil check.
	if orphans == nil {
		orphans = []*db.DiscoveredOrphan{}
	}
	c.JSON(http.StatusOK, gin.H{"orphans": orphans})
}
