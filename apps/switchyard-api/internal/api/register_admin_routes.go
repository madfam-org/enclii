package api

// Admin Control Plane (superuser-only) route registration.
// Extracted from handlers.go to keep that file under the 800-line ceiling.

import (
	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// registerAdminRoutes wires every /v1/admin/* route under the protected
// router group, gating the whole subtree on the `admin` role.
func (h *Handler) registerAdminRoutes(protected *gin.RouterGroup) {
	admin := protected.Group("/admin")
	admin.Use(h.auth.RequireRole(string(types.RoleAdmin)))

	// Fleet Management (Bare Metal Hosts)
	admin.GET("/fleet", h.ListBareMetalHosts)
	admin.GET("/fleet/:id", h.GetBareMetalHost)
	admin.POST("/fleet", h.RegisterBareMetalHost)
	admin.PUT("/fleet/:id/firmware", h.UpdateFirmware)
	admin.PUT("/fleet/:id/partition", h.ConfigurePartition)
	admin.POST("/fleet/:id/wipe", h.SecureWipe)
	admin.PUT("/fleet/:id/power", h.SetPowerState)

	// Cluster Management
	admin.GET("/clusters", h.ListAdminClusters)
	admin.GET("/clusters/:id", h.GetAdminCluster)
	admin.POST("/clusters", h.RegisterCluster)
	admin.PUT("/clusters/:id", h.UpdateCluster)
	admin.DELETE("/clusters/:id", h.DeregisterCluster)

	// Infrastructure Composition (Crossplane)
	admin.GET("/resources", h.ListManagedResources)
	admin.GET("/resources/:id", h.GetManagedResource)
	admin.POST("/resources", h.CreateManagedResource)
	admin.PUT("/resources/:id/policy", h.UpdateResourcePolicy)
	admin.DELETE("/resources/:id", h.DeleteManagedResource)

	// Virtual Clusters
	admin.GET("/vclusters", h.ListVirtualClusters)
	admin.GET("/vclusters/:id", h.GetVirtualCluster)
	admin.POST("/vclusters", h.ProvisionVirtualCluster)
	admin.DELETE("/vclusters/:id", h.TeardownVirtualCluster)
	admin.GET("/vclusters/:id/kubeconfig", h.GetVClusterKubeconfig)

	// Propagation Policies
	admin.GET("/propagation", h.ListPropagationPolicies)
	admin.GET("/propagation/:id", h.GetPropagationPolicy)
	admin.POST("/propagation", h.CreatePropagationPolicy)
	admin.DELETE("/propagation/:id", h.DeletePropagationPolicy)

	// Drift & Governance
	admin.GET("/drift", h.ListDriftEvents)
	admin.GET("/drift/:id", h.GetDriftEvent)
	admin.POST("/drift/:id/resolve", h.ResolveDrift)

	// Cost Tracking
	admin.GET("/costs", h.GetCostAllocations)
	admin.GET("/costs/summary", h.GetCostSummary)
	admin.POST("/costs/allocate", h.AllocateCost)

	// XC-2 master-admin tenant switching: list every team, enter/exit
	// an "acting as <tenant>" session, query the active session.
	// See claudedocs/master-admin-tenant-switching.md.
	admin.GET("/tenants", h.ListTenants)
	admin.GET("/tenants/active", h.ActiveTenant)
	admin.POST("/tenants/exit", h.ExitTenant)
	admin.POST("/tenants/:slug/enter", h.EnterTenant)

	// Repo Onboarding (self-service)
	admin.POST("/onboard", h.OnboardRepo)
	admin.POST("/onboard/ensure", h.EnsureOnboarding)
	admin.POST("/onboard/preflight", h.PreflightOnboard)
	admin.GET("/preflight", h.PreflightImageGates) // image gates preflight, see onboarding_image_gates.go
	admin.GET("/onboard", h.ListOnboardings)
	admin.GET("/onboard/:owner/:repo", h.GetOnboarding)

	// Reconcile services from K8s cluster — discovers deployments in a
	// project's namespace and inserts missing services rows (or updates
	// existing rows with NULL k8s_namespace). Recovery + repair path.
	admin.POST("/projects/:slug/reconcile-services", h.ReconcileServicesFromCluster)

	// Database addon discovery + backfill — registers existing
	// shared-postgres logical DBs and standalone Redis deploys as
	// `database_addons` rows so /databases reflects reality. Idempotent.
	admin.POST("/databases/discover", h.DiscoverDatabases)

	// Standalone Provisioning (ad-hoc for already-onboarded projects)
	admin.POST("/provision/postgres", h.ProvisionPostgres)
	admin.POST("/provision/secrets", h.ProvisionSecrets)
	admin.POST("/provision/r2", h.ProvisionR2)

	// Status Page Management
	admin.GET("/status/services", h.ListStatusServices)
	admin.POST("/status/regenerate", h.RegenerateStatusConfig)

	// Topology (admin-level)
	admin.GET("/topology", h.GetAdminTopology)

	// Namespace Discoverer (parity audit gap #2): live workloads found in
	// cluster with no matching service row.
	admin.GET("/discovered-orphans", h.ListDiscoveredOrphans)
}
