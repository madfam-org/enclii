package api

import (
	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/audit"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/builder"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cache"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/clients"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/compliance"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/notifications"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provenance"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/reconciler"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/topology"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/validation"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Handler contains all dependencies for HTTP handlers
type Handler struct {
	// Repositories (legacy - prefer using services)
	repos *db.Repositories

	// Service Layer (business logic)
	authService            *services.AuthService
	projectService         *services.ProjectService
	deploymentService      *services.DeploymentService
	deploymentGroupService *services.DeploymentGroupService
	domainSyncService      *services.DomainSyncService
	tunnelRoutesService    services.TunnelRoutesManager
	addonService           *addons.AddonService
	notificationService    *notifications.Service
	emailService           *notifications.EmailService

	// Infrastructure
	config             *config.Config
	auth               auth.AuthManager // Interface supporting both JWTManager and OIDCManager
	auditMiddleware    *audit.Middleware
	cache              cache.CacheService
	builder            *builder.Service
	k8sClient          *k8s.Client
	reconciler         *reconciler.Controller
	serviceReconciler  *reconciler.ServiceReconciler
	metrics            *monitoring.MetricsCollector
	logger             logging.Logger
	validator          *validation.Validator
	provenanceChecker  *provenance.Checker
	complianceExporter *compliance.Exporter
	topologyBuilder    *topology.GraphBuilder

	// Build concurrency control - semaphore to limit concurrent builds (prevents OOM)
	buildSemaphore chan struct{}

	// Roundhouse client for async builds (optional - only used in "roundhouse" build mode)
	roundhouseClient *clients.RoundhouseClient

	// Provisioning services (optional — set via SetProvisioners)
	postgresProvisioner *provisioning.PostgresProvisioner
	pgbouncerUpdater    *provisioning.PgBouncerUpdater
	secretsProvisioner  *provisioning.SecretsProvisioner
	r2Provisioner       *provisioning.R2Provisioner

	// Pipeline services (optional — set via setters for incremental migration)
	webhookService *services.WebhookService
	buildService   *services.BuildService

	// Admin Control Plane services (optional)
	bareMetalService      *services.BareMetalService
	clusterAdminService   *services.ClusterAdminService
	infrastructureService *services.InfrastructureService
	vclusterService       *services.VClusterService
	placementService      *services.PlacementService
	driftService          *services.DriftService
	costTrackingService   *services.CostTrackingService

	// Consolidated audit surface (P1.5) — serves /v1/audit and /v1/audit/export
	// by merging Janua + Switchyard + Nexus audit streams. Optional; if nil
	// the routes return 503. See internal/audit/.
	auditHandler *audit.Handler
}

// NewHandler creates a new API handler with all dependencies
func NewHandler(
	repos *db.Repositories,
	config *config.Config,
	auth auth.AuthManager, // Can be JWTManager or OIDCManager
	cache cache.CacheService,
	builder *builder.Service,
	k8sClient *k8s.Client,
	reconciler *reconciler.Controller,
	serviceReconciler *reconciler.ServiceReconciler,
	metrics *monitoring.MetricsCollector,
	logger logging.Logger,
	validator *validation.Validator,
	provenanceChecker *provenance.Checker,
	complianceExporter *compliance.Exporter,
	topologyBuilder *topology.GraphBuilder,
	// Service layer
	authService *services.AuthService,
	projectService *services.ProjectService,
	deploymentService *services.DeploymentService,
	deploymentGroupService *services.DeploymentGroupService,
	// Optional clients
	roundhouseClient *clients.RoundhouseClient,
) *Handler {
	// Create build semaphore with capacity 1 to serialize builds
	// This prevents OOM when multiple webhook builds are triggered simultaneously
	buildSem := make(chan struct{}, 1)

	return &Handler{
		// Repositories
		repos: repos,

		// Services
		authService:            authService,
		projectService:         projectService,
		deploymentService:      deploymentService,
		deploymentGroupService: deploymentGroupService,

		// Infrastructure
		config:             config,
		auth:               auth,
		auditMiddleware:    audit.NewMiddleware(repos),
		cache:              cache,
		builder:            builder,
		k8sClient:          k8sClient,
		reconciler:         reconciler,
		serviceReconciler:  serviceReconciler,
		metrics:            metrics,
		logger:             logger,
		validator:          validator,
		provenanceChecker:  provenanceChecker,
		complianceExporter: complianceExporter,
		topologyBuilder:    topologyBuilder,

		// Build concurrency control
		buildSemaphore: buildSem,

		// Roundhouse client (may be nil if in-process mode)
		roundhouseClient: roundhouseClient,
	}
}

// SetDomainSyncService sets the domain sync service for Cloudflare integration
// This is optional - if not set, sync endpoints will return 503 Service Unavailable
func (h *Handler) SetDomainSyncService(svc *services.DomainSyncService) {
	h.domainSyncService = svc
}

// SetAddonService sets the addon service for database addon management
// This is optional - if not set, addon endpoints will return 503 Service Unavailable
func (h *Handler) SetAddonService(svc *addons.AddonService) {
	h.addonService = svc
}

// SetNotificationService sets the notification service for webhook delivery
// This is optional - if not set, notification test endpoints will return 503 Service Unavailable
func (h *Handler) SetNotificationService(svc *notifications.Service) {
	h.notificationService = svc
}

// SetEmailService sets the email service for transactional emails (invitations, etc.)
// This is optional - if not set, emails will be logged instead of sent
func (h *Handler) SetEmailService(svc *notifications.EmailService) {
	h.emailService = svc
}

// SetWebhookService sets the webhook processing service
func (h *Handler) SetWebhookService(svc *services.WebhookService) {
	h.webhookService = svc
}

// SetBuildService sets the build callback processing service
func (h *Handler) SetBuildService(svc *services.BuildService) {
	h.buildService = svc
}

// SetBareMetalService sets the bare metal fleet service
func (h *Handler) SetBareMetalService(svc *services.BareMetalService) {
	h.bareMetalService = svc
}

// SetClusterAdminService sets the cluster admin service
func (h *Handler) SetClusterAdminService(svc *services.ClusterAdminService) {
	h.clusterAdminService = svc
}

// SetInfrastructureService sets the infrastructure composition service
func (h *Handler) SetInfrastructureService(svc *services.InfrastructureService) {
	h.infrastructureService = svc
}

// SetVClusterService sets the virtual cluster service
func (h *Handler) SetVClusterService(svc *services.VClusterService) {
	h.vclusterService = svc
}

// SetPlacementService sets the placement/propagation service
func (h *Handler) SetPlacementService(svc *services.PlacementService) {
	h.placementService = svc
}

// SetDriftService sets the drift detection service
func (h *Handler) SetDriftService(svc *services.DriftService) {
	h.driftService = svc
}

// SetCostTrackingService sets the cost tracking service
func (h *Handler) SetCostTrackingService(svc *services.CostTrackingService) {
	h.costTrackingService = svc
}

// SetAuditHandler wires the consolidated audit surface (P1.5). Pass nil
// to leave the /v1/audit endpoints returning 503 — useful in local-dev
// setups without Janua/Nexus configured.
func (h *Handler) SetAuditHandler(handler *audit.Handler) {
	h.auditHandler = handler
}

// SetTunnelRoutesService sets the tunnel routes service for automatic cloudflared route management
// This is optional - if not set, domain additions will not automatically update tunnel routes
// Accepts either TunnelRoutesService (ConfigMap-based) or TunnelRoutesServiceCloudflare (API-based)
func (h *Handler) SetTunnelRoutesService(svc services.TunnelRoutesManager) {
	h.tunnelRoutesService = svc
}

// SetProvisioners sets all provisioning services for the onboarding pipeline.
// Each is optional — if nil, the corresponding provisioning step is skipped.
func (h *Handler) SetProvisioners(
	pg *provisioning.PostgresProvisioner,
	pgb *provisioning.PgBouncerUpdater,
	sec *provisioning.SecretsProvisioner,
	r2 *provisioning.R2Provisioner,
) {
	h.postgresProvisioner = pg
	h.pgbouncerUpdater = pgb
	h.secretsProvisioner = sec
	h.r2Provisioner = r2
}

// SetupRoutes configures all API routes
// Handler methods are implemented in separate files:
// - auth_handlers.go: Authentication endpoints
// - health_handlers.go: Health check endpoints
// - projects_handlers.go: Project CRUD operations
// - services_handlers.go: Service CRUD operations
// - build_handlers.go: Build and release management
// - deployment_handlers.go: Deployment operations
// - domain_handlers.go: Custom domain management
// - topology_handlers.go: Service dependency graph
// - webhook_handlers.go: GitHub webhook handlers
// - observability_handlers.go: Metrics and monitoring endpoints
func SetupRoutes(router *gin.Engine, h *Handler) {
	// HTTP metrics middleware
	if h.metrics != nil {
		router.Use(h.metrics.HTTPMetricsMiddleware())
	}

	// Prometheus metrics endpoint (for scraping by Prometheus/Grafana)
	if h.metrics != nil {
		router.GET("/metrics", gin.WrapH(h.metrics.Handler()))
	}

	// Health check endpoints (no auth required)
	router.GET("/health", h.Health)
	router.GET("/health/live", h.LivenessProbe)
	router.GET("/health/ready", h.ReadinessProbe)

	// Build status - public endpoint for cross-service commit status lookup
	router.GET("/v1/builds/:commit_sha/status", h.GetBuildStatusByCommit)

	// Dashboard stats (public endpoint for local development)
	router.GET("/v1/dashboard/stats", h.GetDashboardStats)

	// GitHub webhook (no auth required - uses HMAC signature verification)
	// Endpoint for GitHub to send push events for auto-deployments
	router.POST("/v1/webhooks/github", h.GitHubWebhook)

	// Build callbacks (internal - from Roundhouse worker)
	// Uses API key authentication instead of user auth
	router.POST("/v1/callbacks/build-complete", h.BuildCompleteCallback)
	router.POST("/v1/callbacks/function-build-complete", h.FunctionBuildCompleteCallback)
	router.POST("/v1/callbacks/argocd-sync", h.ArgocdSyncCallback)
	router.POST("/v1/callbacks/lifecycle-event", h.LifecycleEventCallback)

	// Internal API endpoints (for Roundhouse webhook integration)
	// GET /v1/services?git_repo=... - Find services by git repository URL
	// Used by Roundhouse to look up services when processing PR webhooks for preview environments
	router.GET("/v1/services", h.ListServicesByGitRepo)

	// Rate limiters for auth endpoints
	authRateLimiter := middleware.NewAuthRateLimiter()             // 10 req/min per IP
	strictAuthRateLimiter := middleware.NewStrictAuthRateLimiter() // 5 req/min per IP

	// API v1 routes
	v1 := router.Group("/v1")
	{
		// Auth routes - Different endpoints based on auth mode
		if h.config.AuthMode == "oidc" {
			// ===== OIDC Mode (Production with Janua) =====
			// Redirect to OIDC provider for login (rate limited)
			v1.GET("/auth/login", authRateLimiter.Middleware(), h.OIDCLogin)

			// OAuth callback from OIDC provider (rate limited)
			v1.GET("/auth/callback", authRateLimiter.Middleware(), h.OIDCCallback)

			// Silent auth check for detecting existing SSO sessions (no rate limit - iframe use)
			v1.GET("/auth/silent-check", h.OIDCSilentCheck)

			// Silent callback for iframe-based auth (no rate limit - iframe use)
			v1.GET("/auth/callback/silent", h.OIDCSilentCallback)

			// Registration is handled by OIDC provider (Janua)
			// POST /auth/register is not available in OIDC mode

		} else {
			// ===== Local Mode (Bootstrap) =====
			// Local user registration (strict rate limit - abuse prevention)
			v1.POST("/auth/register", strictAuthRateLimiter.Middleware(), h.auditMiddleware.AuditMiddleware(), h.Register)

			// Local login with email/password (strict rate limit - brute force prevention)
			v1.POST("/auth/login", strictAuthRateLimiter.Middleware(), h.auditMiddleware.AuditMiddleware(), h.Login)

			// JWKS endpoint for external services to verify our tokens
			v1.GET("/auth/jwks", h.JWKS)
		}

		// Common auth endpoints (both modes) - rate limited
		v1.POST("/auth/refresh", authRateLimiter.Middleware(), h.RefreshToken)
		v1.POST("/auth/logout", authRateLimiter.Middleware(), h.auth.AuthMiddleware(), h.auditMiddleware.AuditMiddleware(), h.Logout)

		// Protected routes (require authentication + audit)
		// These work the same way in both local and OIDC modes
		protected := v1.Group("")
		protected.Use(h.auth.AuthMiddleware())
		protected.Use(h.auditMiddleware.AuditMiddleware())
		{
			// Projects
			protected.POST("/projects", h.auth.RequireRole(string(types.RoleAdmin)), middleware.RequireTierForProject(h.repos), h.CreateProject)
			protected.GET("/projects", h.ListProjects)
			protected.GET("/projects/:slug", h.GetProject)
			protected.DELETE("/projects/:slug", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteProject)

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
			protected.GET("/services/:id/builds/:build_id/logs", h.GetBuildLogs)
			protected.GET("/services/:id/builds/:build_id/logs/stream", h.StreamBuildLogsWS)

			// Build Status (Unified CI + Build + Deploy status)
			// Note: :build_id here can be either a release UUID or commit SHA
			protected.GET("/services/:id/builds/:build_id/status", h.GetUnifiedBuildStatus)

			// Topology
			protected.GET("/topology", h.GetTopology)
			protected.GET("/topology/services/:id/dependencies", h.GetServiceDependencies)
			protected.GET("/topology/services/:id/impact", h.GetServiceImpact)
			protected.GET("/topology/path", h.FindDependencyPath)

			// Networking & Custom Domains
			protected.GET("/services/:id/networking", h.GetServiceNetworking)
			protected.POST("/services/:id/domains", h.auth.RequireRole(string(types.RoleDeveloper)), h.AddServiceDomain)
			protected.GET("/services/:id/domains", h.ListCustomDomains)
			protected.GET("/services/:id/domains/:domain_id", h.GetCustomDomain)
			protected.PATCH("/services/:id/domains/:domain_id", h.auth.RequireRole(string(types.RoleDeveloper)), h.UpdateCustomDomain)
			protected.DELETE("/services/:id/domains/:domain_id", h.auth.RequireRole(string(types.RoleDeveloper)), h.DeleteCustomDomain)
			protected.POST("/services/:id/domains/:domain_id/verify", h.auth.RequireRole(string(types.RoleDeveloper)), h.VerifyCustomDomain)
			protected.PUT("/domains/:domain_id/protection", h.auth.RequireRole(string(types.RoleDeveloper)), h.ToggleZeroTrust)

			// Environments
			protected.GET("/environments", h.GetEnvironments)

			// Integrations (GitHub via Janua OAuth tokens)
			protected.GET("/integrations/github/status", h.GetGitHubStatus)
			protected.GET("/integrations/github/repos", h.ListGitHubRepos)
			protected.POST("/integrations/github/link", h.LinkGitHub)
			protected.GET("/integrations/github/repos/:owner/:repo/branches", h.GetRepositoryBranches)
			protected.POST("/integrations/github/repos/:owner/:repo/analyze", h.AnalyzeRepository)

			// Deployment Groups (coordinated multi-service deployments)
			protected.POST("/projects/:slug/environments/:env_name/deployment-groups", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateDeploymentGroup)
			protected.GET("/projects/:slug/deployment-groups", h.ListDeploymentGroups)
			protected.GET("/projects/:slug/deployment-groups/:group_id", h.GetDeploymentGroup)
			protected.POST("/projects/:slug/deployment-groups/:group_id/execute", h.auth.RequireRole(string(types.RoleDeveloper)), h.ExecuteDeploymentGroup)
			protected.POST("/projects/:slug/deployment-groups/:group_id/rollback", h.auth.RequireRole(string(types.RoleDeveloper)), h.RollbackDeploymentGroup)

			// Service Dependencies
			protected.POST("/services/:id/dependencies", h.auth.RequireRole(string(types.RoleDeveloper)), h.AddServiceDependency)
			protected.GET("/services/:id/dependencies", h.ListServiceDependencies)
			protected.GET("/services/:id/dependents", h.ListServiceDependents)
			protected.DELETE("/services/:id/dependencies/:depends_on_id", h.auth.RequireRole(string(types.RoleDeveloper)), h.RemoveServiceDependency)

			// Environment Variables
			protected.GET("/services/:id/env-vars", h.ListEnvVars)
			protected.POST("/services/:id/env-vars", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateEnvVar)
			protected.GET("/services/:id/env-vars/:var_id", h.GetEnvVar)
			protected.PUT("/services/:id/env-vars/:var_id", h.auth.RequireRole(string(types.RoleDeveloper)), h.UpdateEnvVar)
			protected.DELETE("/services/:id/env-vars/:var_id", h.auth.RequireRole(string(types.RoleDeveloper)), h.DeleteEnvVar)
			protected.POST("/services/:id/env-vars/bulk", h.auth.RequireRole(string(types.RoleDeveloper)), h.BulkUpsertEnvVars)
			protected.POST("/services/:id/env-vars/sync-from-pod", h.auth.RequireRole(string(types.RoleAdmin)), h.SyncEnvVarsFromPod)
			protected.POST("/services/:id/env-vars/:var_id/reveal", h.auth.RequireRole(string(types.RoleDeveloper)), h.RevealEnvVar)

			// Preview Environments (PR-based ephemeral deployments)
			protected.GET("/services/:id/previews", h.ListPreviews)
			protected.GET("/projects/:slug/previews", h.ListProjectPreviews)
			protected.GET("/previews/:id", h.GetPreview)
			protected.POST("/previews", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreatePreview)
			protected.POST("/previews/:id/close", h.auth.RequireRole(string(types.RoleDeveloper)), h.ClosePreview)
			protected.POST("/previews/:id/wake", h.auth.RequireRole(string(types.RoleDeveloper)), h.WakePreview)
			protected.DELETE("/previews/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.DeletePreview)
			protected.POST("/previews/:id/access", h.RecordPreviewAccess)

			// Preview Comments (collaborative feedback)
			protected.GET("/previews/:id/comments", h.ListPreviewComments)
			protected.POST("/previews/:id/comments", h.CreatePreviewComment)
			protected.POST("/previews/:id/comments/:comment_id/resolve", h.ResolvePreviewComment)

			// Teams (platform team management)
			protected.POST("/teams", h.CreateTeam)
			protected.GET("/teams", h.ListTeams)
			protected.GET("/teams/:slug", h.GetTeam)
			protected.PATCH("/teams/:slug", h.UpdateTeam)
			protected.DELETE("/teams/:slug", h.DeleteTeam)

			// Team Members
			protected.GET("/teams/:slug/members", h.ListTeamMembers)
			protected.PATCH("/teams/:slug/members/:member_id", h.UpdateMemberRole)
			protected.DELETE("/teams/:slug/members/:member_id", h.RemoveTeamMember)

			// Team Invitations (team admin operations)
			protected.POST("/teams/:slug/invitations", h.InviteTeamMember)
			protected.GET("/teams/:slug/invitations", h.ListTeamInvitations)
			protected.DELETE("/teams/:slug/invitations/:invitation_id", h.CancelTeamInvitation)

			// User Invitations (personal invitation management)
			protected.GET("/invitations", h.ListMyInvitations)
			protected.GET("/invitations/:token", h.GetInvitationByToken)
			protected.POST("/invitations/:token/accept", h.AcceptInvitation)
			protected.POST("/invitations/:token/decline", h.DeclineInvitation)

			// Usage & Billing
			protected.GET("/usage", h.GetUsageSummary)
			protected.GET("/usage/costs", h.GetCostBreakdown)
			protected.GET("/usage/realtime", h.GetRealTimeMetrics)

			// Global Domains (cross-service domain management)
			protected.GET("/domains", h.GetAllDomains)
			protected.GET("/domains/stats", h.GetDomainStats)
			protected.POST("/domains/sync", h.auth.RequireRole(string(types.RoleAdmin)), h.SyncDomainsFromCloudflare)
			protected.POST("/domains/:domain_id/sync", h.auth.RequireRole(string(types.RoleDeveloper)), h.SyncDomainFromCloudflare)

			// Cloudflare Tunnel Status
			protected.GET("/tunnel/status", h.GetTunnelStatus)

			// Deployment Lifecycle Timeline
			protected.GET("/lifecycle/timeline/:owner/:repo", h.GetLifecycleTimeline)
			protected.GET("/lifecycle/branch/:owner/:repo/:branch", h.GetLifecycleBranch)
			protected.GET("/lifecycle/commit/:sha", h.GetLifecycleCommit)
			protected.GET("/lifecycle/events", h.GetLifecycleEvents)

			// Activity (Audit Logs) — legacy single-source surface
			protected.GET("/activity", h.GetActivity)
			protected.GET("/activity/actions", h.GetActivityActions)
			protected.GET("/activity/resource-types", h.GetActivityResourceTypes)

			// Consolidated audit surface (P1.5) — aggregates Janua sessions,
			// Switchyard lifecycle/audit, and 4 Selva RFC ledgers (via nexus-api).
			// Self-or-admin RBAC; CSV export is admin-only.
			protected.GET("/audit", h.AuditList)
			protected.GET("/audit/export", h.AuditExport)

			// Observability (Metrics & Monitoring)
			protected.GET("/observability/metrics", h.GetMetricsSnapshot)
			protected.GET("/observability/metrics/history", h.GetMetricsHistory)
			protected.GET("/observability/health", h.GetServiceHealth)
			protected.GET("/observability/errors", h.GetRecentErrors)
			protected.GET("/observability/alerts", h.GetActiveAlerts)

			// API Tokens (for CLI/CI/CD access)
			protected.POST("/user/tokens", h.CreateAPIToken)
			protected.GET("/user/tokens", h.ListAPITokens)
			protected.GET("/user/tokens/:token_id", h.GetAPIToken)
			protected.DELETE("/user/tokens/:token_id", h.RevokeAPIToken)

			// Database Add-ons (PostgreSQL, Redis, MySQL)
			// Global addon listing (all addons user has access to)
			protected.GET("/addons", h.ListAllAddons)
			protected.GET("/databases", h.ListAllAddons) // Alias for better UX
			// Project-specific addon operations
			protected.POST("/projects/:slug/addons", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateAddon)
			protected.GET("/projects/:slug/addons", h.ListAddons)
			protected.GET("/addons/:id", h.GetAddon)
			protected.GET("/addons/:id/credentials", h.GetAddonCredentials)
			protected.POST("/addons/:id/refresh", h.RefreshAddonStatus)
			protected.DELETE("/addons/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteAddon)
			protected.POST("/addons/:id/bindings", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateAddonBinding)
			protected.DELETE("/addons/:id/bindings/:service_id", h.auth.RequireRole(string(types.RoleDeveloper)), h.DeleteAddonBinding)
			protected.GET("/services/:id/bindings", h.GetServiceBindings)

			// Infrastructure Operations (exec, restart, scale, migrate, health)
			protected.POST("/services/:id/exec", h.auth.RequireRole(string(types.RoleAdmin)), h.ExecService)
			protected.POST("/services/:id/restart", h.auth.RequireRole(string(types.RoleAdmin)), h.RestartService)
			protected.POST("/services/:id/scale", h.auth.RequireRole(string(types.RoleAdmin)), h.ScaleService)
			protected.POST("/services/:id/migrate", h.auth.RequireRole(string(types.RoleAdmin)), h.MigrateService)
			protected.GET("/services/:id/health/detailed", h.GetDetailedHealth)

			// Serverless Functions (Enclii Functions - Scale-to-Zero)
			// Global function listing (all functions user has access to)
			protected.GET("/functions", h.ListAllFunctions)
			// Project-specific function operations
			protected.POST("/projects/:slug/functions", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateFunction)
			protected.GET("/projects/:slug/functions", h.ListFunctions)
			protected.GET("/functions/:id", h.GetFunction)
			protected.PATCH("/functions/:id", h.auth.RequireRole(string(types.RoleDeveloper)), h.UpdateFunction)
			protected.DELETE("/functions/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteFunction)
			protected.POST("/functions/:id/invoke", h.auth.RequireRole(string(types.RoleDeveloper)), h.InvokeFunction)
			protected.GET("/functions/:id/logs", h.GetFunctionLogs)
			protected.GET("/functions/:id/metrics", h.GetFunctionMetrics)

			// Timetable — Cron Jobs & One-Off Jobs (NOT IMPLEMENTED — stubs return 501)
			protected.POST("/projects/:slug/cron-jobs", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateCronJob)
			protected.GET("/projects/:slug/cron-jobs", h.ListCronJobs)
			protected.GET("/cron-jobs/:id", h.GetCronJob)
			protected.PATCH("/cron-jobs/:id", h.auth.RequireRole(string(types.RoleDeveloper)), h.UpdateCronJob)
			protected.DELETE("/cron-jobs/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteCronJob)
			protected.GET("/cron-jobs/:id/runs", h.ListCronJobRuns)
			protected.POST("/projects/:slug/one-off-jobs", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateOneOffJob)

			// Junctions — Routing & Ingress (NOT IMPLEMENTED — stubs return 501)
			protected.POST("/projects/:slug/junctions", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateJunction)
			protected.GET("/projects/:slug/junctions", h.ListJunctions)
			protected.GET("/junctions/:id", h.GetJunction)
			protected.DELETE("/junctions/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteJunction)

			// Notification Webhooks (Slack/Discord/Telegram/Custom)
			protected.POST("/projects/:slug/webhooks", h.auth.RequireRole(string(types.RoleDeveloper)), h.CreateWebhook)
			protected.GET("/projects/:slug/webhooks", h.ListWebhooks)
			protected.GET("/webhooks/event-types", h.GetWebhookEventTypes)
			protected.GET("/webhooks/:id", h.GetWebhook)
			protected.PATCH("/webhooks/:id", h.auth.RequireRole(string(types.RoleDeveloper)), h.UpdateWebhook)
			protected.DELETE("/webhooks/:id", h.auth.RequireRole(string(types.RoleAdmin)), h.DeleteWebhook)
			protected.POST("/webhooks/:id/test", h.auth.RequireRole(string(types.RoleDeveloper)), h.TestWebhook)
			protected.GET("/webhooks/:id/deliveries", h.ListWebhookDeliveries)
			protected.POST("/webhooks/:id/deliveries/:delivery_id/retry", h.auth.RequireRole(string(types.RoleDeveloper)), h.RetryWebhookDelivery)

			// Templates (Starter templates and marketplace)
			protected.GET("/templates", h.ListTemplates)
			protected.GET("/templates/featured", h.GetFeaturedTemplates)
			protected.GET("/templates/filters", h.GetTemplateFilters)
			protected.GET("/templates/search", h.SearchTemplates)
			protected.GET("/templates/:slug", h.GetTemplate)
			protected.POST("/templates/:slug/deploy", h.auth.RequireRole(string(types.RoleDeveloper)), h.DeployTemplate)
			protected.GET("/templates/deployments/:id", h.GetTemplateDeployment)
			protected.POST("/templates/import", h.auth.RequireRole(string(types.RoleDeveloper)), h.ImportTemplateFromGitHub)

			// ============================================================
			// Admin Control Plane (superuser-only)
			// ============================================================
			admin := protected.Group("/admin")
			admin.Use(h.auth.RequireRole(string(types.RoleAdmin)))
			{
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

				// Repo Onboarding (self-service)
				admin.POST("/onboard", h.OnboardRepo)
				admin.POST("/onboard/preflight", h.PreflightOnboard)
				admin.GET("/onboard", h.ListOnboardings)
				admin.GET("/onboard/:owner/:repo", h.GetOnboarding)

				// Standalone Provisioning (ad-hoc for already-onboarded projects)
				admin.POST("/provision/postgres", h.ProvisionPostgres)
				admin.POST("/provision/secrets", h.ProvisionSecrets)
				admin.POST("/provision/r2", h.ProvisionR2)

				// Status Page Management
				admin.GET("/status/services", h.ListStatusServices)
				admin.POST("/status/regenerate", h.RegenerateStatusConfig)

				// Topology (admin-level)
				admin.GET("/topology", h.GetAdminTopology)
			}
		}
	}
}
