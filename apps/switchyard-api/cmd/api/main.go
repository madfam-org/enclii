package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/XSAM/otelsql"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/api"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/audit"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/builder"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/clients"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/compliance"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	logstream "github.com/madfam-org/enclii/apps/switchyard-api/internal/logstream"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/monitoring"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/notifications"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provenance"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/realtime"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/reconciler"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/topology"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/validation"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/webhooks"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logrus.Fatal("Failed to load configuration:", err)
	}

	// Set global logrus level based on config
	// This ensures components using logrus.StandardLogger() also respect the log level
	logrusLevel, err := logrus.ParseLevel(cfg.LogLevel.String())
	if err != nil {
		logrusLevel = logrus.InfoLevel
	}
	logrus.SetLevel(logrusLevel)
	logrus.Infof("Log level set to: %s", logrusLevel.String())

	// Setup logging
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{
		Level:       cfg.LogLevel.String(),
		Format:      "json",
		Output:      "stdout",
		ServiceName: "switchyard-api",
		Environment: cfg.Environment,
	})
	if err != nil {
		logrus.Fatal("Failed to initialize logger:", err)
	}

	// -------------------------------------------------------------------
	// P2.5: OpenTelemetry distributed tracing.
	//
	// Wired via packages/otel-go so every MADFAM Go service shares one
	// SDK bootstrap path. The shared package:
	//   - Exports spans to Tempo (tempo.observability.svc.cluster.local:4317)
	//   - Applies parent-based sampling (default 0.1 prod / 1.0 elsewhere)
	//   - Drops attributes whose keys look like credentials (password,
	//     token, authorization, etc.) before they leave the process
	//   - Installs a logrus hook so every log line with an active span
	//     context carries trace_id + span_id (for Loki<->Tempo pivots)
	//
	// The returned shutdown is bounded to 5s — don't let a stuck exporter
	// hold SIGTERM past the pod termination grace period.
	//
	// This replaces the earlier half-wired OTel in internal/logging, which
	// never received an OTLP endpoint at boot and so silently dropped
	// every span.
	// -------------------------------------------------------------------
	otelShutdown := logging.Setup(context.Background(), cfg.Environment)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Warn("OpenTelemetry shutdown returned error")
		}
	}()

	// Connect to database with connection pooling.
	//
	// otelsql.Open wraps lib/pq so every query emits a child span with
	// db.system=postgresql, db.statement sanitized. Queries inherit the
	// trace context from the caller (e.g., the inbound HTTP request).
	database, err := otelsql.Open("postgres", cfg.DatabaseURL,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			OmitConnPrepare: true, // reduces noise; prepared statements are per-conn
			OmitRows:        true, // Rows iteration spans are rarely useful
		}),
	)
	if err != nil {
		logrus.Fatal("Failed to connect to database:", err)
	}
	defer func() { _ = database.Close() }()

	// Register DB pool metrics with OTel (optional but cheap). otelsql v0.43
	// returns a metric.Registration for callers that want to Unregister; we
	// keep metrics for the process lifetime, so it is deliberately discarded.
	if _, err := otelsql.RegisterDBStatsMetrics(database,
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
	); err != nil {
		logrus.WithError(err).Warn("Failed to register DB stats metrics (non-fatal)")
	}

	// Configure connection pool. Budget vs the SHARED postgres.data.svc:
	// max_connections=100 for the ENTIRE MADFAM cluster, ~90 in use at steady
	// state, cluster-wide exhaustion incident 2026-07-22. The previous
	// hardcoded 25 let 2 replicas claim 50 of the 100 slots; measured steady
	// state for the whole enclii database (this service plus roundhouse-api)
	// is ~10. Budget: 2 replicas x 10 = 20 absolute max. Raise per-env via
	// DB_MAX_OPEN_CONNS / DB_MAX_IDLE_CONNS, not by editing code. (Same
	// budgeting pattern as fortuna, janua#483, ceq#70, bloom-scroll#112.)
	maxOpenConns := envInt("DB_MAX_OPEN_CONNS", 10)
	maxIdleConns := envInt("DB_MAX_IDLE_CONNS", 4)
	database.SetMaxOpenConns(maxOpenConns)
	database.SetMaxIdleConns(maxIdleConns)
	database.SetConnMaxLifetime(30 * time.Minute)
	database.SetConnMaxIdleTime(5 * time.Minute)

	// Verify database connection
	if err := database.Ping(); err != nil {
		logrus.Fatal("Failed to ping database:", err)
	}
	logrus.WithFields(logrus.Fields{
		"max_open_conns":    maxOpenConns,
		"max_idle_conns":    maxIdleConns,
		"conn_max_lifetime": "30m",
	}).Info("✓ Database connection pool configured")

	// Run database migrations
	if err := db.Migrate(database, cfg.DatabaseURL); err != nil {
		logrus.Fatal("Failed to run database migrations:", err)
	}

	// Initialize repositories
	repos := db.NewRepositories(database)

	// Initialize cache service with retry (handles K8s startup timing)
	// Supports both standalone Redis and Redis Sentinel (HA mode)
	// Uses exponential backoff to wait for Redis to become available
	// Returns nil if all retries fail (cache disabled, API continues working)
	cacheService := initRedisWithRetry(cfg)
	if cacheService == nil {
		logrus.Warn("Running without Redis cache (session revocation disabled)")
	}

	// Initialize authentication manager
	// This will create either JWTManager (local mode) or OIDCManager (OIDC mode)
	// based on the ENCLII_AUTH_MODE configuration
	ctx := context.Background()
	authManager, err := auth.NewAuthManager(
		ctx,
		cfg,
		repos,
		cacheService, // Cache for session revocation (can be nil)
	)
	if err != nil {
		logrus.Fatal("Failed to initialize auth manager:", err)
	}

	// Log which authentication mode is active
	logrus.WithField("auth_mode", cfg.AuthMode).Info("✓ Authentication manager initialized")

	// Wire up API token validator for CLI/CI/CD authentication
	// This enables "enclii_xxx" tokens in addition to JWT/OIDC tokens
	switch am := authManager.(type) {
	case *auth.JWTManager:
		am.SetAPITokenValidator(repos.APITokens)
		logrus.Info("✓ API token authentication enabled (JWT mode)")
	case *auth.OIDCManager:
		am.SetAPITokenValidator(repos.APITokens)
		logrus.Info("✓ API token authentication enabled (OIDC mode)")
	default:
		logrus.Warn("⚠ API token authentication not configured (unknown auth manager type)")
	}

	// Initialize Kubernetes client
	k8sClient, err := k8s.NewClient(cfg.KubeConfig, cfg.KubeContext)
	if err != nil {
		logrus.Fatal("Failed to initialize Kubernetes client:", err)
	}

	// Initialize builder service
	builderService := builder.NewService(&builder.Config{
		WorkDir:          cfg.BuildWorkDir,
		Registry:         cfg.Registry,
		RegistryUsername: cfg.RegistryUsername,
		RegistryPassword: cfg.RegistryPassword,
		CacheDir:         cfg.BuildCacheDir,
		Timeout:          time.Duration(cfg.BuildTimeout) * time.Second,
		GenerateSBOM:     true, // Enable SBOM generation with Syft
		SignImages:       true, // Enable image signing with Cosign
	}, logrus.StandardLogger())

	// Ensure build directories exist
	if err := os.MkdirAll(cfg.BuildWorkDir, 0755); err != nil {
		logrus.Fatal("Failed to create build work directory:", err)
	}
	if err := os.MkdirAll(cfg.BuildCacheDir, 0755); err != nil {
		logrus.Warnf("Failed to create build cache directory (non-fatal): %v", err)
	}

	// Initialize reconciler
	reconcilerController := reconciler.NewController(database, repos, k8sClient, logrus.StandardLogger())

	// Start reconciliation controller (processes pending deployments from database)
	if err := reconcilerController.Start(ctx); err != nil {
		logrus.Fatal("Failed to start reconciler controller:", err)
	}
	logrus.Info("✓ Reconciliation controller started (processing pending deployments)")

	// Initialize service reconciler (also used directly by API handlers)
	serviceReconciler := reconciler.NewServiceReconciler(k8sClient, logrus.StandardLogger())

	// Initialize metrics collector
	metricsCollector := monitoring.NewMetricsCollector()

	// Initialize validator
	validatorInstance := validation.NewValidator()

	// Initialize provenance checker (PR approval verification)
	var provenanceChecker *provenance.Checker
	if cfg.GitHubToken != "" {
		provenanceChecker = provenance.NewChecker(cfg.GitHubToken, nil) // nil = use default policy
		logrus.Info("✓ PR approval checking enabled")
	} else {
		logrus.Warn("⚠ GitHub token not configured - PR approval checking disabled")
		logrus.Warn("   Set ENCLII_GITHUB_TOKEN to enable deployment approval verification")
	}

	// Initialize compliance exporter (Vanta/Drata webhooks)
	complianceExporter := compliance.NewExporter(&compliance.Config{
		Enabled:      cfg.ComplianceWebhooksEnabled,
		VantaWebhook: cfg.VantaWebhookURL,
		DrataWebhook: cfg.DrataWebhookURL,
		MaxRetries:   3,
		RetryDelay:   2 * time.Second,
	}, logrus.StandardLogger())

	if cfg.ComplianceWebhooksEnabled {
		logrus.Info("✓ Compliance webhooks enabled")
		if cfg.VantaWebhookURL != "" {
			logrus.Info("  → Vanta webhook configured")
		}
		if cfg.DrataWebhookURL != "" {
			logrus.Info("  → Drata webhook configured")
		}
	} else {
		logrus.Info("ℹ Compliance webhooks disabled")
		logrus.Info("   Set ENCLII_COMPLIANCE_WEBHOOKS_ENABLED=true to enable")
	}

	// Initialize topology builder
	topologyBuilder := topology.NewGraphBuilder(repos, k8sClient, logrus.StandardLogger())
	logrus.Info("✓ Topology graph builder initialized")

	// Initialize authentication provider (supports both JWT and OIDC modes)
	authProvider, err := auth.NewAuthProvider(ctx, cfg, repos, cacheService, authManager, logrus.StandardLogger())
	if err != nil {
		logrus.Fatal("Failed to initialize authentication provider:", err)
	}
	logrus.WithField("mode", authProvider.Mode()).Info("✓ Authentication provider initialized")

	// Initialize service layer (business logic)
	authService := services.NewAuthService(
		repos,
		authProvider,
		logrus.StandardLogger(),
	)
	logrus.Info("✓ AuthService initialized")

	projectService := services.NewProjectService(
		repos,
		logrus.StandardLogger(),
	)
	logrus.Info("✓ ProjectService initialized")

	deploymentService := services.NewDeploymentService(
		repos,
		logrus.StandardLogger(),
	)
	logrus.Info("✓ DeploymentService initialized")

	deploymentGroupService := services.NewDeploymentGroupService(
		repos,
		deploymentService,
		logrus.StandardLogger(),
	)
	logrus.Info("✓ DeploymentGroupService initialized")

	// Initialize addon service (database add-ons: PostgreSQL, Redis, MySQL)
	addonService := addons.NewAddonService(repos, k8sClient, logrus.StandardLogger())
	logrus.Info("✓ AddonService initialized (PostgreSQL, Redis, MySQL add-ons)")

	// Initialize and start addon reconciler (syncs database addon status from K8s)
	addonReconciler := reconciler.NewAddonReconciler(repos, k8sClient, logrus.StandardLogger())
	// Wire the retention finalizer so the reconciler can tear down retention-hold
	// addons once their grace window elapses (2026-08-17 audit #10).
	addonReconciler.SetRetentionFinalizer(addonService)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("Addon reconciler panicked: %v", r)
			}
		}()
		addonReconciler.Start(ctx)
	}()
	logrus.Info("✓ Addon reconciler started (syncing database addon status)")

	// Initialize and start function reconciler (scale-to-zero serverless functions)
	functionReconciler := reconciler.NewFunctionReconciler(repos, k8sClient, logrus.StandardLogger(), cfg.FunctionBaseDomain)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("Function reconciler panicked: %v", r)
			}
		}()
		functionReconciler.Start(ctx)
	}()
	logrus.Info("✓ Function reconciler started (serverless functions with KEDA scale-to-zero)")

	// Initialize and start namespace discoverer (parity audit gap #2):
	// detects orphan workloads (in cluster, not in DB) and zombie services
	// (DB record, no live workload) every RECONCILER_NAMESPACE_DISCOVERY_INTERVAL
	// (default 5m). Read-only against the cluster.
	namespaceDiscoverer := reconciler.NewNamespaceDiscoverer(repos, k8sClient, logrus.StandardLogger())
	namespaceDiscoverer.Start(ctx)
	logrus.Info("✓ Namespace discoverer started (orphan/zombie detection)")

	// Initialize Roundhouse client (for async builds)
	var roundhouseClient *clients.RoundhouseClient
	if cfg.BuildMode == "roundhouse" {
		roundhouseClient = clients.NewRoundhouseClient(cfg.RoundhouseURL, cfg.RoundhouseAPIKey)
		logrus.WithFields(logrus.Fields{
			"build_mode":     "roundhouse",
			"roundhouse_url": cfg.RoundhouseURL,
		}).Info("✓ Build mode: roundhouse (async builds via worker)")
	} else {
		logrus.WithField("build_mode", "in-process").Info("✓ Build mode: in-process (sync builds in API)")
	}

	// Initialize Cloudflare client (for domain status sync)
	var cfClient *cloudflare.Client
	var domainSyncService *services.DomainSyncService
	if cfg.CloudflareAPIToken != "" && cfg.CloudflareAccountID != "" && cfg.CloudflareZoneID != "" {
		var cfErr error
		cfClient, cfErr = cloudflare.NewClient(&cloudflare.Config{
			APIToken:  cfg.CloudflareAPIToken,
			AccountID: cfg.CloudflareAccountID,
			ZoneID:    cfg.CloudflareZoneID,
			TunnelID:  cfg.CloudflareTunnelID,
		})
		if cfErr != nil {
			logrus.WithError(cfErr).Warn("⚠ Failed to initialize Cloudflare client")
		} else {
			// Verify token works
			if verifyErr := cfClient.VerifyToken(ctx); verifyErr != nil {
				logrus.WithError(verifyErr).Warn("⚠ Cloudflare API token verification failed")
				cfClient = nil
			} else {
				logrus.Info("✓ Cloudflare client initialized")

				// Create domain sync service
				domainSyncService = services.NewDomainSyncService(cfClient, repos, logrus.StandardLogger())
				logrus.Info("✓ Domain sync service initialized")
			}
		}
	} else {
		logrus.Info("ℹ Cloudflare integration not configured")
		logrus.Info("   Set ENCLII_CLOUDFLARE_API_TOKEN, ENCLII_CLOUDFLARE_ACCOUNT_ID, ENCLII_CLOUDFLARE_ZONE_ID to enable")
	}

	// Initialize tunnel routes service (Cloudflare API-based for remotely-managed tunnels)
	var tunnelRoutesService services.TunnelRoutesManager
	if cfClient != nil && cfg.CloudflareTunnelID != "" {
		tunnelRoutesService = services.NewTunnelRoutesServiceCloudflare(cfClient, logrus.StandardLogger())
		logrus.WithField("tunnel_id", cfg.CloudflareTunnelID).Info("✓ Tunnel routes service initialized (Cloudflare API)")
	} else if cfClient == nil {
		logrus.Info("ℹ Tunnel routes service not configured (Cloudflare client required)")
	} else {
		logrus.Info("ℹ Tunnel routes service not configured (ENCLII_CLOUDFLARE_TUNNEL_ID required)")
	}

	// Setup HTTP server
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Logger())
	// Use custom recovery middleware that logs panics with full stack trace
	// and returns proper JSON error responses instead of empty body
	router.Use(middleware.RecoveryMiddleware(logger))
	// OTel HTTP tracing — creates spans for every request when tracing is enabled
	router.Use(otelgin.Middleware("switchyard-api"))

	// Initialize security middleware with CORS support
	securityMiddleware := middleware.NewSecurityMiddleware(nil) // Uses default config with CORS
	router.Use(securityMiddleware.CORSMiddleware())
	logrus.Info("✓ CORS middleware enabled")

	// Global IP-based rate limiting (complements per-endpoint auth rate limits)
	router.Use(securityMiddleware.RateLimitMiddleware())
	logrus.Info("✓ Global rate limiter enabled")

	// Setup API routes with all dependencies
	apiHandler := api.NewHandler(
		repos,
		cfg,
		authManager,
		cacheService,
		builderService,
		k8sClient,
		reconcilerController,
		serviceReconciler,
		metricsCollector,
		logger,
		validatorInstance,
		provenanceChecker,
		complianceExporter,
		topologyBuilder,
		// Service layer
		authService,
		projectService,
		deploymentService,
		deploymentGroupService,
		// Optional clients
		roundhouseClient,
	)

	if cfg.WaybillBaseURL != "" {
		apiHandler.SetBillingProxy(&api.BillingProxyConfig{
			WaybillBaseURL: cfg.WaybillBaseURL,
			InternalAPIKey: cfg.WaybillInternalAPIKey,
		})
		logrus.WithField("waybill_base_url", cfg.WaybillBaseURL).Info("Billing proxy wired to Waybill")
	} else {
		logrus.Warn("Billing proxy disabled; ENCLII_WAYBILL_BASE_URL is unset")
	}

	wireDhanamCheckoutRelay(cfg, apiHandler)
	wireVaultWriter(cfg, apiHandler)

	// Wire up optional domain sync service (if Cloudflare is configured)
	if domainSyncService != nil {
		apiHandler.SetDomainSyncService(domainSyncService)
		logrus.Info("✓ Domain sync service wired to API handler")
	}

	// Wire up tunnel routes service (Cloudflare API-based route management)
	if tunnelRoutesService != nil {
		apiHandler.SetTunnelRoutesService(tunnelRoutesService)
		logrus.Info("✓ Tunnel routes service wired to API handler (automatic route management enabled)")
	}

	// Wire up addon service (database add-ons)
	apiHandler.SetAddonService(addonService)
	logrus.Info("✓ Addon service wired to API handler")

	// Initialize notification service (Slack/Discord/Telegram webhooks)
	notificationService := notifications.NewService(repos.Webhooks, logrus.StandardLogger())
	apiHandler.SetNotificationService(notificationService)
	reconcilerController.SetNotificationService(notificationService)
	logrus.Info("✓ Notification service wired to API handler and reconciler (Slack/Discord/Telegram)")

	// Initialize email service (team invitations, transactional emails)
	emailService := notifications.NewEmailService(notifications.EmailConfig{
		APIKey:    cfg.EmailAPIKey,
		FromEmail: cfg.EmailFromAddress,
		FromName:  cfg.EmailFromName,
		BaseURL:   cfg.AppBaseURL,
	}, logrus.StandardLogger())
	apiHandler.SetEmailService(emailService)
	if emailService.IsEnabled() {
		logrus.Info("✓ Email service wired to API handler (Resend API)")
	} else {
		logrus.Warn("⚠ Email service not configured - invitation emails will be logged only")
	}

	// Wire up Admin Control Plane services (fleet, clusters, infrastructure, multi-tenancy, governance, costs)
	bareMetalService := services.NewBareMetalService(repos, k8sClient, logrus.StandardLogger())
	apiHandler.SetBareMetalService(bareMetalService)
	clusterAdminService := services.NewClusterAdminService(repos, logrus.StandardLogger())
	apiHandler.SetClusterAdminService(clusterAdminService)
	infrastructureService := services.NewInfrastructureService(repos, k8sClient, logrus.StandardLogger())
	apiHandler.SetInfrastructureService(infrastructureService)
	vclusterService := services.NewVClusterService(repos, k8sClient, logrus.StandardLogger())
	apiHandler.SetVClusterService(vclusterService)
	placementService := services.NewPlacementService(repos, logrus.StandardLogger())
	apiHandler.SetPlacementService(placementService)
	driftService := services.NewDriftService(repos, logrus.StandardLogger())
	apiHandler.SetDriftService(driftService)
	costTrackingService := services.NewCostTrackingService(repos, logrus.StandardLogger())
	apiHandler.SetCostTrackingService(costTrackingService)
	logrus.Info("✓ Admin control plane services wired (fleet, clusters, infrastructure, vclusters, placement, drift, costs)")

	// Wire up provisioning services (for onboarding pipeline)
	{
		var pgProv *provisioning.PostgresProvisioner
		var pgbUpdater *provisioning.PgBouncerUpdater
		var secProv *provisioning.SecretsProvisioner
		var r2Prov *provisioning.R2Provisioner

		if cfg.PostgresAdminURL != "" {
			pgProv = provisioning.NewPostgresProvisioner(cfg.PostgresAdminURL, logger)
			logrus.Info("✓ Postgres provisioner configured")
		} else {
			logrus.Warn("⚠ Postgres provisioner not configured (ENCLII_POSTGRES_ADMIN_URL not set)")
		}

		if k8sClient != nil && k8sClient.IsValid() {
			pgbUpdater = provisioning.NewPgBouncerUpdater(k8sClient.Clientset, logger)
			secProv = provisioning.NewSecretsProvisioner(k8sClient.Clientset, logger)
			logrus.Info("✓ PgBouncer updater and secrets provisioner configured")
		}

		if cfg.CloudflareAPIToken != "" && cfg.CloudflareAccountID != "" {
			r2Prov = provisioning.NewR2Provisioner(cfg.CloudflareAPIToken, cfg.CloudflareAccountID, logger)
			logrus.Info("✓ R2 provisioner configured")
		}

		apiHandler.SetProvisioners(pgProv, pgbUpdater, secProv, r2Prov)
	}

	// P3.6: Tenant data export wiring (see tenant_export_wiring.go).
	// Disabled when TENANT_EXPORT_R2_* env vars aren't set — endpoints
	// return 503 in that case. Kept out of this file to keep main.go
	// under the 800-line budget.
	wireTenantExport(cfg, repos, apiHandler, emailService, k8sClient)

	// P3.2 Sprint 1 — self-serve signup. Disabled by default; operator
	// flips ENCLII_SIGNUP_ENABLED=true once the Janua companion changes
	// and Resend templates are in place. When disabled, /v1/signup
	// endpoints return 404 (not 503 — we don't advertise the surface).
	wireSignup(cfg, repos, apiHandler, k8sClient, emailService)

	// Start admin reconciler (syncs cluster status, fleet, ArgoCD drift, costs every 60s)
	adminReconciler := reconciler.NewAdminReconciler(repos, k8sClient, logrus.StandardLogger())
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("Admin reconciler panicked: %v", r)
			}
		}()
		adminReconciler.Start(ctx)
	}()
	logrus.Info("✓ Admin reconciler started (cluster status, fleet, ArgoCD drift, cost tracking)")

	// Start domain background sync (existing code, never activated)
	if domainSyncService != nil {
		domainSyncService.StartBackgroundSync(5 * time.Minute)
		logrus.Info("✓ Domain background sync started (every 5 minutes)")
	}

	// Wire P1.5 consolidated audit surface. The aggregator is enabled if
	// the local DB is reachable (always true here); Janua and Nexus
	// sources self-disable when their tokens/URLs are empty, letting
	// dev deployments run without external wiring.
	{
		auditSources := []audit.Source{
			audit.NewSwitchyardSource(database), // direct-DB to this service's own audit tables
		}
		if cfg.JanuaAPIURL != "" && cfg.JanuaAdminToken != "" {
			auditSources = append(auditSources, audit.NewJanuaClient(cfg.JanuaAPIURL, cfg.JanuaAdminToken))
			logrus.Info("✓ Audit aggregator: Janua session source enabled")
		} else {
			logrus.Warn("⚠ Audit aggregator: Janua session source DISABLED (JANUA_ADMIN_TOKEN not set)")
		}
		if cfg.NexusAPIURL != "" && cfg.NexusAPIToken != "" {
			auditSources = append(auditSources, audit.NewNexusClient(cfg.NexusAPIURL, cfg.NexusAPIToken))
			logrus.Info("✓ Audit aggregator: Nexus (Selva RFCs 0005-0008) source enabled")
		} else {
			logrus.Warn("⚠ Audit aggregator: Nexus source DISABLED (NEXUS_API_URL/TOKEN not set)")
		}
		// XC-2 Round 6: aggregator gets a TeamResolver so non-team-aware
		// sources (Janua, Nexus today) can be post-filtered when a master
		// admin is acting-as a tenant. Switchyard source still pushes the
		// filter to SQL — this is just the safety net for the others.
		auditAgg := audit.NewAggregator(logrus.StandardLogger(), auditSources...).
			WithTeamResolver(repos.Projects)
		auditH := audit.NewHandler(auditAgg, audit.NewGinAuthz(), logrus.StandardLogger())
		auditH.SetActingReader(audit.GinActingTeamReader{})
		apiHandler.SetAuditHandler(auditH)
		logrus.Infof("✓ Consolidated audit surface wired at /v1/audit (sources=%d)", len(auditSources))
	}

	// Wire P2.1 in-UI log tail. The feature self-disables cleanly when
	// LOKI_URL is empty (endpoints 503 rather than 500). In production
	// LOKI_URL defaults to the in-cluster DNS name, which works out of
	// the box with the existing Fluent Bit → Loki deployment.
	{
		if cfg.LokiURL != "" {
			lokiClient := logstream.NewLokiClient(cfg.LokiURL)
			lokiLimiter := logstream.NewLimiter(
				cfg.LokiQueryBudgetPerMinute,
				cfg.LokiQueryBudgetBurst,
			)
			lokiResolver := logstream.NewRepoResolver(repos)
			lokiAuthz := logstream.NewGinAuthz()
			logsH := logstream.NewHandler(
				lokiClient,
				lokiResolver,
				lokiAuthz,
				lokiLimiter,
				cfg.WebSocketAllowedOrigins,
				logrus.StandardLogger(),
			)
			apiHandler.SetLogsHandler(logsH)
			logrus.Infof("✓ Loki log tail wired at /v1/services/:id/logs (loki=%s, budget=%d/min)",
				cfg.LokiURL, cfg.LokiQueryBudgetPerMinute)
		} else {
			logrus.Warn("⚠ Loki log tail DISABLED (ENCLII_LOKI_URL not set); /v1/services/:id/logs returns 503")
		}
	}

	// Wire the C2 realtime DB-change subscriptions (Supabase Realtime
	// equivalent). The hub holds one LISTEN connection per addon and fans row
	// changes out to WS subscribers; the manager installs the opt-in NOTIFY
	// triggers on tenant tables. Both dial the addon's own database on demand
	// via the addon service, so there is no global dependency to gate on — the
	// feature is always wired when the addon service is present. WebSocket
	// origins reuse the same allow-list as the Loki tail. See
	// docs/architecture/ADR_002_REALTIME_DB_SUBSCRIPTIONS.md.
	{
		realtimeHub := realtime.NewHub(realtime.NewPQDialer(logrus.StandardLogger()), logrus.StandardLogger())
		realtimeManager := realtime.NewManager(realtime.NewPQConnector(), logrus.StandardLogger())
		apiHandler.SetRealtime(realtimeHub, realtimeManager, cfg.WebSocketAllowedOrigins, logrus.StandardLogger())
		// Ensure listeners are torn down on shutdown alongside other resources.
		defer realtimeHub.Shutdown()
		logrus.Info("✓ Realtime DB subscriptions wired at /v1/projects/:slug/addons/:id/realtime (LISTEN/NOTIFY)")
	}

	// -------------------------------------------------------------------
	// P2.3: Outbound lifecycle webhooks — dispatcher + worker
	// -------------------------------------------------------------------
	// The master key is optional in dev (signing-secret storage disabled
	// when absent — CRUD endpoints return 503 and emitLifecycleEvent
	// skips the fan-out branch). In prod the secret must be mounted
	// from the K8s Secret owned by the vault team.
	if cfg.WebhookMasterKeyB64 != "" {
		masterKey, err := webhooks.LoadMasterKey(cfg.WebhookMasterKeyB64)
		if err != nil {
			logrus.Fatalf("Invalid ENCLII_WEBHOOK_MASTER_KEY: %v", err)
		}
		encryptor, err := webhooks.NewEncryptor(masterKey)
		if err != nil {
			logrus.Fatalf("Init webhook encryptor: %v", err)
		}
		webhookLog := webhooks.NewLoggingAdapter(logger)
		dispatcher := webhooks.NewDispatcher(repos, webhookLog)
		apiHandler.SetWebhookDispatcher(dispatcher, encryptor)
		workerCfg := webhooks.WorkerConfig{
			PoolSize:       cfg.WebhookWorkerPool,
			HTTPTimeout:    10 * time.Second,
			ClaimBatchSize: 20,
		}
		worker := webhooks.NewWorker(workerCfg, repos, encryptor, webhookLog)
		workerCtx, cancelWorker := context.WithCancel(context.Background())
		go worker.Run(workerCtx)
		// Ensure graceful shutdown stops the worker loop.
		defer cancelWorker()
		logrus.Infof("✓ Outbound lifecycle webhooks enabled (pool=%d)", workerCfg.PoolSize)
	} else {
		logrus.Warn("⚠ Outbound lifecycle webhooks DISABLED (ENCLII_WEBHOOK_MASTER_KEY unset)")
	}

	defer wireArgocdPoller(ctx, cfg, apiHandler)() // ArgoCD poller — ships dark; see argocd_poller_wiring.go
	api.SetupRoutes(router, apiHandler)

	server := &http.Server{
		Addr:           ":" + cfg.Port,
		Handler:        router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   0,       // Streaming endpoints (SSE/logs/audit export) own their own heartbeat/budget.
		MaxHeaderBytes: 1 << 20, // 1 MB
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		logrus.Infof("🚂 Switchyard API starting on port %s", cfg.Port)
		logrus.Infof("   Environment: %s", cfg.Environment)
		logrus.Infof("   Registry: %s", cfg.Registry)
		logrus.Infof("   Build work dir: %s", cfg.BuildWorkDir)
		logrus.Infof("   Build cache dir: %s", cfg.BuildCacheDir)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for interrupt signal or server error
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		logrus.Errorf("Server failed to start: %v", err)
	case <-quit:
		logrus.Info("Received shutdown signal")
	}

	logrus.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logrus.Fatal("Server forced to shutdown:", err)
	}

	// Clean up resources
	// Stop domain sync service if running
	if domainSyncService != nil {
		domainSyncService.StopBackgroundSync()
		logrus.Info("Domain sync service stopped")
	}

	// Stop admin reconciler
	adminReconciler.Stop()
	logrus.Info("Admin reconciler stopped")

	// Stop reconciler controller gracefully
	reconcilerController.Stop()
	logrus.Info("Reconciler controller stopped")

	// Stop addon reconciler
	addonReconciler.Stop()
	logrus.Info("Addon reconciler stopped")

	// Stop function reconciler
	functionReconciler.Stop()
	logrus.Info("Function reconciler stopped")

	// Stop namespace discoverer
	namespaceDiscoverer.Stop()
	logrus.Info("Namespace discoverer stopped")

	// Stop security middleware cleanup goroutine
	securityMiddleware.Stop()
	logrus.Info("Security middleware stopped")

	if cacheService != nil {
		if err := cacheService.Close(); err != nil {
			logrus.Warnf("Error closing cache connection: %v", err)
		} else {
			logrus.Info("Cache connection closed")
		}
	}

	logrus.Info("Server exiting")
}
