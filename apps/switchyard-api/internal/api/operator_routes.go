package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// RequirePlatformOperator gates estate operations on the server-resolved
// platform rank. Unlike the staged tenant-access guard, this gate cannot be
// disabled by ENCLII_TENANT_SCOPE_ENFORCE: operator/provider actions address
// shared infrastructure, and are not needed to run the tenant dry-run report.
func (h *Handler) RequirePlatformOperator() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.callerIsPlatformAdmin(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "Forbidden",
				"message": "platform_admin rank required for platform operations (ADR-003)",
			})
			return
		}
		c.Next()
	}
}

// During the additive inventory rollout, existing operator routes retain
// their original admin role guard. Inventory is new and requires platform rank
// immediately. Both this guard and adapter dispatch use the same domain param;
// an operation name in the body cannot select or authorize another adapter.
func registerOperatorRoutes(protected *gin.RouterGroup, h *Handler) {
	legacyAdmin := h.auth.RequireRole(string(types.RoleAdmin))
	platformOperator := h.RequirePlatformOperator()
	ops := protected.Group("/ops")
	ops.GET("/capabilities", legacyAdmin, h.GetOpsCapabilities)
	ops.POST("/:domain/:action", func(c *gin.Context) {
		if c.Param("domain") == "inventory" {
			platformOperator(c)
			return
		}
		legacyAdmin(c)
	}, h.HandleOpsOperation)

	providers := protected.Group("/providers", legacyAdmin)
	providers.GET("/capabilities", h.GetProviderCapabilities)
	providers.POST("/:provider/:action", h.HandleProviderOperation)
}
