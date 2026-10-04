package api

import (
	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// registerProjectServiceRoutes registers routes on the existing authenticated group.
// Keep their order and route-specific middleware intact.
func (h *Handler) registerProjectServiceRoutes(protected *gin.RouterGroup) {
	// Projects
	protected.POST("/projects", h.auth.RequireRole(string(types.RoleAdmin)), middleware.RequireTierForProject(h.repos), h.CreateProject)
	protected.GET("/projects", h.ListProjects)
	protected.GET("/projects/cards", h.ListProjectCards)
	protected.GET("/project-processes/summary", h.GetProjectProcessSummaries)
	protected.GET("/project-processes/stream", h.StreamProjectProcessSummaries)
	protected.GET("/projects/:slug", h.GetProject)
	protected.GET("/projects/:slug/processes", h.GetProjectProcesses)
	protected.GET("/projects/:slug/processes/stream", h.StreamProjectProcesses)
	protected.DELETE("/projects/:slug", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteProject)
	// Re-parent a project to a team (its tenant), or un-parent it.
	// Admin-gated: reparenting changes which tenant owns the project.
	protected.PUT("/projects/:slug/team", h.auth.RequireRole(string(types.RoleAdmin)), h.SetProjectTeam)

	// CI Runner Configuration
	protected.GET("/projects/:slug/ci-runner-config", h.GetCIRunnerConfig)
	protected.PUT("/projects/:slug/ci-runner-config", h.auth.RequireRole(string(types.RoleDeveloper)), h.UpdateCIRunnerConfig)

	// Environments
	protected.POST("/projects/:slug/environments", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateEnvironment)
	protected.GET("/projects/:slug/environments", h.ListEnvironments)
	protected.GET("/projects/:slug/environments/:env_name", h.GetEnvironment)

	// Services
	protected.POST("/projects/:slug/services", h.auth.RequireRole(string(types.RoleDeveloper)), middleware.RequireTierForService(h.repos), h.CreateService)
	protected.POST("/projects/:slug/services/bulk", h.auth.RequireRole(string(types.RoleDeveloper)), h.BulkCreateServices)
	protected.GET("/projects/:slug/services", h.ListServices)
	protected.GET("/services/:id", h.GetService)
	protected.GET("/services/:id/settings", h.GetServiceSettings)
	protected.PATCH("/services/:id", h.auth.RequireRole(string(types.RoleDeveloper)), h.UpdateService)
	protected.DELETE("/services/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteService)

	// Build & Deploy
	protected.POST("/services/:id/build", h.auth.RequireRole(string(types.RoleDeveloper)), h.BuildService)
	protected.GET("/services/:id/releases", h.ListReleases)
	protected.POST("/services/:id/deploy", h.auth.RequireRole(string(types.RoleDeveloper)), middleware.RequireTierForDeploy(h.repos), h.DeployService)

	// Status & Deployments
	protected.GET("/services/:id/status", h.GetServiceStatus)
	protected.GET("/services/:id/metrics", h.GetServiceResourceMetrics)
	protected.GET("/services/:id/deployments", h.ListServiceDeployments)
	protected.GET("/services/:id/deployments/latest", h.GetLatestDeployment)
	// P2.6: lookup by Heroku-style v-number. Route accepts either
	// the bare integer ("42") or the prefixed form ("v42") — the
	// handler normalizes. We use a separate `/versions/:v` segment
	// because gin can't register `:version` next to the static
	// `latest` above (httprouter rejects the mix at boot).
	protected.GET("/services/:id/versions/:version", h.GetDeploymentByVersion)
	protected.GET("/deployments", h.ListAllDeployments)
	protected.GET("/deployments/:id", h.GetDeployment)
	protected.GET("/deployments/:id/logs", h.GetLogs)
	protected.POST("/deployments/:id/rollback", h.auth.RequireRole(string(types.RoleDeveloper)), h.RollbackDeployment)
	// Instant rollback via Service-selector flip (P0.5). Traffic flips in <30s
	// for still-running targets, <90s for scale-back-up. Coexists with the
	// deployments/:id/rollback endpoint above — ArgoCD path is the fallback.
	protected.POST("/services/:id/rollback", h.auth.RequireRole(string(types.RoleDeveloper)), h.InstantRollback)

	// Canary releases (P2.7). Replica-proportion traffic splitting with
	// auto-promote after a validation window. See internal/reconciler/canary.go.
	protected.POST("/services/:id/canary", h.auth.RequireRole(string(types.RoleDeveloper)), h.StartCanary)
	protected.GET("/services/:id/canary", h.ListServiceCanaries)
	protected.GET("/services/:id/canary/:rollout_id", h.GetCanary)
	protected.POST("/services/:id/canary/:rollout_id/promote", h.auth.RequireRole(string(types.RoleDeveloper)), h.PromoteCanary)
	protected.POST("/services/:id/canary/:rollout_id/rollback", h.auth.RequireRole(string(types.RoleDeveloper)), h.RollbackCanary)

	// Real-time Logs (WebSocket streaming)
	protected.GET("/services/:id/logs/stream", h.StreamServiceLogsWS)
	protected.GET("/services/:id/logs/history", h.GetLogsHistory)
	protected.POST("/services/:id/logs/search", h.SearchLogs)
	protected.GET("/deployments/:id/logs/stream", h.StreamLogsWS)

	// P2.1 — Loki-backed log tail for app.enclii.dev UI.
	// /logs     returns a windowed, paginated historical slice.
	// /logs/tail is a WebSocket that pushes entries as they land
	// in Loki (typically <2s from ingest).
	protected.GET("/services/:id/logs", h.loggedLogsQuery)
	protected.GET("/services/:id/logs/tail", h.loggedLogsTail)
	protected.GET("/services/:id/builds/:build_id/logs", h.GetBuildLogs)
	protected.GET("/services/:id/builds/:build_id/logs/stream", h.StreamBuildLogsWS)

	// Build Status (Unified CI + Build + Deploy status)
	// Note: :build_id here can be either a release UUID or commit SHA
	protected.GET("/services/:id/builds/:build_id/status", h.GetUnifiedBuildStatus)
}
