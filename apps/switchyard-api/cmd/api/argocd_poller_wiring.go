package main

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/api"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
)

// wireArgocdPoller starts the read-only ArgoCD Application poller (GitOps
// deploy-tracking fallback, enclii#324) when ENCLII_ARGOCD_POLLER_ENABLED is
// set, and returns a stop func for graceful shutdown.
//
// It ships dark: when the flag is unset the poller is not constructed and the
// returned func is a no-op. When enabled, the poller lists ArgoCD Applications
// every ENCLII_ARGOCD_POLL_INTERVAL and reconciles release/deployment/activity
// records directly from status.sync.revision + status.summary.images —
// independent of the notifications webhook — so a GitOps service whose
// on-sync-succeeded push goes quiet (OutOfSync-but-healthy) keeps reporting. It
// reuses the webhook's exact record-creation logic and is read-only against the
// cluster.
func wireArgocdPoller(ctx context.Context, cfg *config.Config, apiHandler *api.Handler) func() {
	if !cfg.ArgocdPollerEnabled {
		logrus.Info("ℹ ArgoCD Application poller DISABLED (set ENCLII_ARGOCD_POLLER_ENABLED=true to enable)")
		return func() {}
	}

	interval := api.ParseArgocdPollInterval(cfg.ArgocdPollInterval)
	poller := api.NewArgocdPoller(apiHandler, interval, cfg.ArgocdNamespace)
	poller.Start(ctx)
	logrus.WithField("interval", interval).
		Info("✓ ArgoCD Application poller started (GitOps deploy-tracking fallback, enclii#324)")

	return func() {
		poller.Stop()
		logrus.Info("ArgoCD Application poller stopped")
	}
}
