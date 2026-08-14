package api

// Object-storage (Cloudflare R2) route registration.
// Extracted from handlers.go to keep that file under the 800-line ceiling,
// mirroring register_admin_routes.go.

import (
	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// registerStorageRoutes wires the day-2 bucket lifecycle for an existing
// project.
//
// These routes are deliberately independent of onboarding: creating a bucket
// must not touch ArgoCD registration, namespaces, or domains, so they are safe
// to call against a live service. Destroy requires admin because it revokes a
// credential a running workload is holding.
func (h *Handler) registerStorageRoutes(protected *gin.RouterGroup) {
	protected.POST("/projects/:slug/storage/buckets",
		h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateStorageBucket)
	protected.GET("/projects/:slug/storage/buckets", h.ListStorageBuckets)
	protected.DELETE("/projects/:slug/storage/buckets/:bucket",
		h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteStorageBucket)
}
