package api

// Admin Control Plane (superuser-only) route registration.
// Extracted from handlers.go to keep that file under the 800-line ceiling.

import (
	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// registerAdminRoutes wires every /v1/admin/* route under the protected
// router group.
//
// ADR-003 (R21 PR 2): the subtree is now gated TWICE, and the two gates answer
// different questions.
//
//	admin.Use(RequireRole(admin))   — "does this principal administer anything?"
//	platform.Use(RequirePlatformAdmin) — "is this principal above every tenant?"
//
// The first is the pre-ADR-003 gate and is kept: post-ADR-003 the `admin` role
// string means tenant_admin, so on its own it admits every customer
// administrator. PR #499 moved the four tenant-switcher routes and the
// dry-run report to the platform rank because those routes ARE the
// cross-tenant surface; it left the rest of the subtree explicitly undecided,
// route by route, rather than deciding it inside a diff about role vocabulary.
//
// That judgement is made here. Every route below is one of two things:
//
//   - PLATFORM-ONLY: it addresses the estate rather than a tenant — physical
//     hosts, clusters, virtual clusters, Crossplane compositions, propagation
//     policy, drift, platform cost allocation, repo onboarding, the control
//     plane's own schema, provisioning, the status page, cluster-wide
//     discovery and the admin topology. None of these is a resource a tenant
//     owns, none of them takes a tenant-scoped identifier, and several of them
//     (GET /admin/costs?tenant_id=, GET /admin/topology,
//     GET /admin/discovered-orphans) enumerate ACROSS tenants by
//     construction. A tenant administrator has no business at any of them, and
//     under the old single gate reached all of them.
//
//   - TENANT-VISIBLE: it is addressed by a tenant-owned identifier and belongs
//     to the tenant that owns it. Exactly one route qualifies —
//     POST /admin/projects/:slug/reconcile-services — and it stays on the
//     tenant-scoped guard it already inherits from the protected group
//     (RequireProjectAccessBySlug), because a tenant administrator repairing
//     its OWN project's service rows is a legitimate operation and making it
//     platform-only would move a self-service repair behind an operator.
//
// The per-route judgements are tabulated in the rollout runbook.
//
// The staged flag covers this exactly as it covers everything else:
// RequirePlatformAdmin falls back to the tenant-admin rank while
// ENCLII_TENANT_SCOPE_ENFORCE is off, so with the flag off this subtree gates
// on `admin` — what it gated on before this change.
func (h *Handler) registerAdminRoutes(protected *gin.RouterGroup) {
	admin := protected.Group("/admin")
	admin.Use(h.auth.RequireRole(string(types.RoleAdmin)))

	// Everything under /v1/admin/* is platform-only except the one route
	// re-registered on `admin` at the bottom of this function.
	platform := admin.Group("", h.RequirePlatformAdmin())

	// Fleet Management (Bare Metal Hosts)
	platform.GET("/fleet", h.ListBareMetalHosts)
	platform.GET("/fleet/:id", h.GetBareMetalHost)
	platform.POST("/fleet", h.RegisterBareMetalHost)
	platform.PUT("/fleet/:id/firmware", h.UpdateFirmware)
	platform.PUT("/fleet/:id/partition", h.ConfigurePartition)
	platform.POST("/fleet/:id/wipe", h.SecureWipe)
	platform.PUT("/fleet/:id/power", h.SetPowerState)

	// Cluster Management
	platform.GET("/clusters", h.ListAdminClusters)
	platform.GET("/clusters/:id", h.GetAdminCluster)
	platform.POST("/clusters", h.RegisterCluster)
	platform.PUT("/clusters/:id", h.UpdateCluster)
	platform.DELETE("/clusters/:id", h.DeregisterCluster)

	// Infrastructure Composition (Crossplane)
	platform.GET("/resources", h.ListManagedResources)
	platform.GET("/resources/:id", h.GetManagedResource)
	platform.POST("/resources", h.CreateManagedResource)
	platform.PUT("/resources/:id/policy", h.UpdateResourcePolicy)
	platform.DELETE("/resources/:id", h.DeleteManagedResource)

	// Virtual Clusters
	platform.GET("/vclusters", h.ListVirtualClusters)
	platform.GET("/vclusters/:id", h.GetVirtualCluster)
	platform.POST("/vclusters", h.ProvisionVirtualCluster)
	platform.DELETE("/vclusters/:id", h.TeardownVirtualCluster)
	platform.GET("/vclusters/:id/kubeconfig", h.GetVClusterKubeconfig)

	// Propagation Policies
	platform.GET("/propagation", h.ListPropagationPolicies)
	platform.GET("/propagation/:id", h.GetPropagationPolicy)
	platform.POST("/propagation", h.CreatePropagationPolicy)
	platform.DELETE("/propagation/:id", h.DeletePropagationPolicy)

	// Drift & Governance
	platform.GET("/drift", h.ListDriftEvents)
	platform.GET("/drift/:id", h.GetDriftEvent)
	platform.POST("/drift/:id/resolve", h.ResolveDrift)

	// Cost Tracking
	platform.GET("/costs", h.GetCostAllocations)
	platform.GET("/costs/summary", h.GetCostSummary)
	platform.POST("/costs/allocate", h.AllocateCost)

	// XC-2 master-admin tenant switching: list every team, enter/exit
	// an "acting as <tenant>" session, query the active session.
	// See claudedocs/master-admin-tenant-switching.md.
	//
	// ADR-003: these four routes ARE the cross-tenant surface — listing every
	// tenant and assuming one. PR #499 gave them their own platform-rank
	// group because the surrounding subtree was still gated on `admin`; the
	// whole subtree carries the rank now, so they are ordinary members of it.
	platform.GET("/tenants", h.ListTenants)
	platform.GET("/tenants/active", h.ActiveTenant)
	platform.POST("/tenants/exit", h.ExitTenant)
	platform.POST("/tenants/:slug/enter", h.EnterTenant)

	// ADR-003 pre-deploy dry-run: which principals lose cross-tenant reach
	// when the tenant-scope guard is enforced. Read-only; see
	// docs/runbooks/TENANT_SCOPE_ENFORCEMENT_ROLLOUT.md.
	platform.GET("/tenant-scope/dry-run", h.TenantScopeDryRun)

	// Repo Onboarding (self-service)
	platform.POST("/onboard", h.OnboardRepo)
	platform.POST("/onboard/ensure", h.EnsureOnboarding)
	platform.POST("/onboard/preflight", h.PreflightOnboard)
	platform.GET("/preflight", h.PreflightImageGates) // image gates preflight, see onboarding_image_gates.go
	platform.GET("/onboard", h.ListOnboardings)
	platform.GET("/onboard/:owner/:repo", h.GetOnboarding)

	// Database schema / migration status (GA migration verify)
	platform.GET("/db/schema", h.GetAdminDBSchema)

	// TENANT-VISIBLE — the single exception in this file. Reconcile services
	// from the K8s cluster: discovers deployments in a project's namespace and
	// inserts missing services rows (or updates existing rows with NULL
	// k8s_namespace). Recovery + repair path.
	//
	// It is addressed by :slug, so the protected group's
	// RequireProjectAccessBySlug has already resolved that project and run the
	// ADR-003 comparison against it before this handler is reached. Registered
	// on `admin` rather than `platform` deliberately: a tenant administrator
	// repairing its own project is the intended user, and the tenant guard —
	// not the rank — is what keeps it out of anybody else's project.
	admin.POST("/projects/:slug/reconcile-services", h.ReconcileServicesFromCluster)

	// Database addon discovery + backfill — registers existing
	// shared-postgres logical DBs and standalone Redis deploys as
	// `database_addons` rows so /databases reflects reality. Idempotent.
	platform.POST("/databases/discover", h.DiscoverDatabases)

	// Standalone Provisioning (ad-hoc for already-onboarded projects)
	platform.POST("/provision/postgres", h.ProvisionPostgres)
	platform.POST("/provision/secrets", h.ProvisionSecrets)
	platform.POST("/provision/r2", h.ProvisionR2)

	// Read-only drift check: PgBouncer userlist vs Postgres login roles.
	// 409 when a login role is missing from the userlist (page on it).
	platform.GET("/provision/pgbouncer/reconcile", h.ReconcilePgbouncerUserlist)

	// Status Page Management
	platform.GET("/status/services", h.ListStatusServices)
	platform.POST("/status/regenerate", h.RegenerateStatusConfig)

	// Topology (admin-level)
	platform.GET("/topology", h.GetAdminTopology)

	platform.GET("/providers/catalog", h.GetAdminProvidersCatalog)

	// Namespace Discoverer
	// cluster with no matching service row.
	platform.GET("/discovered-orphans", h.ListDiscoveredOrphans)
}
