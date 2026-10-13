package api

import (
	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Central custody has no tenant parent; register its read/write/status routes
// together so tests exercise the exact production authority chain. The additive
// operator guard is unconditional: tenant-scope rollback never opens Vault
// custody targets, writes or status to tenant administrators.
func (h *Handler) registerSecretIntakeRoutes(protected *gin.RouterGroup) {
	protected.GET("/secrets/intake/targets", h.auth.RequireRole(string(types.RoleAdmin)), h.RequirePlatformAdmin(), h.RequirePlatformOperator(), h.ListSecretIntakeTargets)
	protected.POST("/secrets/intake", h.auth.RequireRole(string(types.RoleAdmin)), h.RequirePlatformAdmin(), h.RequirePlatformOperator(), h.SubmitSecretIntake)
	protected.GET("/secrets/intake/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.RequirePlatformAdmin(), h.RequirePlatformOperator(), h.GetSecretIntakeStatus)
}
