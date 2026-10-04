package api

import (
	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// registerManagedDataRoutes registers routes on the existing authenticated group.
// Keep their order and route-specific middleware intact.
func (h *Handler) registerManagedDataRoutes(protected *gin.RouterGroup) {
	// Database Add-ons (PostgreSQL, Redis, MySQL)
	// Global addon listing (all addons user has access to)
	protected.GET("/addons", h.ListAllAddons)
	protected.GET("/databases", h.ListAllAddons) // Alias for better UX
	// Managed-DB plan catalog (P3.1 Sprint 1)
	protected.GET("/addons/plans", h.ListManagedDBPlans)
	// Project-specific addon operations
	protected.POST("/projects/:slug/addons", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateAddon)
	protected.GET("/projects/:slug/addons", h.ListAddons)
	protected.GET("/addons/:id", h.GetAddon)
	protected.GET("/addons/:id/credentials", h.GetAddonCredentials)
	protected.GET("/addons/:id/events", h.GetAddonEvents) // P3.1 Sprint 1: lifecycle ledger
	protected.POST("/addons/:id/refresh", h.RefreshAddonStatus)
	protected.DELETE("/addons/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteAddon)
	protected.POST("/addons/:id/bindings", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateAddonBinding)
	protected.DELETE("/addons/:id/bindings/:service_id", h.auth.RequireRole(string(types.RoleDeveloper)), h.DeleteAddonBinding)

	// Data API (auto-generated REST over managed Postgres, PostgREST).
	// See docs/architecture/data-api-postgrest.md.
	protected.GET("/addons/:id/data-api", h.GetDataAPI)
	protected.POST("/addons/:id/data-api", h.auth.RequireRole(string(types.RoleDeveloper)), h.EnableDataAPI)
	protected.DELETE("/addons/:id/data-api", h.auth.RequireRole(string(types.RoleAdmin)), h.DisableDataAPI)
	protected.POST("/addons/:id/data-api/token", h.auth.RequireRole(string(types.RoleDeveloper)), h.MintDataAPIToken)
	protected.GET("/services/:id/bindings", h.GetServiceBindings)

	// Realtime DB change subscriptions (parity gap C2). The WS stream
	// sits under :slug so RequireProjectAccessBySlug gates the upgrade;
	// StreamAddonRealtime re-checks the addon→project link as defense
	// in depth. The trigger-management routes are addon-scoped (they
	// self-gate via loadAddonWithAccess) and require Developer role for
	// the mutating enable/disable, matching the other addon mutations.
	// See docs/architecture/ADR_002_REALTIME_DB_SUBSCRIPTIONS.md.
	protected.GET("/projects/:slug/addons/:id/realtime", h.RequireProjectAccessBySlug(), h.StreamAddonRealtime)
	protected.POST("/addons/:id/realtime/tables", h.auth.RequireRole(string(types.RoleDeveloper)), h.EnableAddonRealtimeTable)
	protected.GET("/addons/:id/realtime/tables", h.ListAddonRealtimeTables)
	protected.DELETE("/addons/:id/realtime/tables/:schema/:table", h.auth.RequireRole(string(types.RoleDeveloper)), h.DisableAddonRealtimeTable)

	h.registerStorageRoutes(protected)
}
