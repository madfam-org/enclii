package main

import (
	"database/sql"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/api"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/audit"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logstream"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/realtime"
)

// wireLiveSurfaces registers the audit and streaming handlers and returns the
// realtime shutdown hook for the API process lifecycle.
func wireLiveSurfaces(cfg *config.Config, repos *db.Repositories, database *sql.DB, apiHandler *api.Handler) func() {
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
		logrus.Info("✓ Realtime DB subscriptions wired at /v1/projects/:slug/addons/:id/realtime (LISTEN/NOTIFY)")
		return realtimeHub.Shutdown
	}

}
