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

	h.registerStorageObjectRoutes(protected)
}

// registerStorageObjectRoutes wires the end-user object API (Supabase-Storage
// -style) over a project's own bucket. See storage_object_handlers.go.
//
// The split from the bucket lifecycle above is intentional: these operate on
// the *contents* of a bucket a project already owns, and the primary
// upload/download path never streams bytes through the API — it mints presigned
// URLs the client uses directly against R2.
//
// AuthZ mirrors the read/write split used elsewhere:
//   - list + presign-download are reads → project access (enforced by the
//     :slug middleware and re-checked in each handler),
//   - presign-upload, direct upload, and delete mutate → developer role.
//
// Every handler additionally scopes to the project's own bucket and namespaces
// object keys under projects/<slug>/, so authorization is never the only thing
// standing between two tenants.
func (h *Handler) registerStorageObjectRoutes(protected *gin.RouterGroup) {
	const base = "/projects/:slug/storage/buckets/:bucket/objects"

	protected.GET(base, h.ListObjects)
	protected.GET(base+"/presign-download", h.PresignDownload)

	protected.POST(base+"/presign-upload",
		h.auth.RequireRole(string(types.RoleDeveloper)), h.PresignUpload)
	protected.POST(base+"/upload",
		h.auth.RequireRole(string(types.RoleDeveloper)), h.UploadObject)
	protected.DELETE(base,
		h.auth.RequireRole(string(types.RoleDeveloper)), h.DeleteObject)
}
