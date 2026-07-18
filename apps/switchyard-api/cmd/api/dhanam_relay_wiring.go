package main

import (
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/api"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
)

// wireDhanamCheckoutRelay wires the Dhanam checkout relay (billing federation,
// mirrors janua#453) onto the API handler. It is always wired so that
// POST /v1/billing/checkout exists; the endpoint fails closed with 503 at
// runtime until both ENCLII_DHANAM_FEDERATION_URL and FEDERATION_API_TOKEN are
// provisioned. Success/cancel URLs are derived from the app base URL.
func wireDhanamCheckoutRelay(cfg *config.Config, apiHandler *api.Handler) {
	appBaseURL := strings.TrimRight(cfg.AppBaseURL, "/")
	apiHandler.SetDhanamFederation(&api.DhanamFederationConfig{
		FederationURL: cfg.DhanamFederationURL,
		APIToken:      cfg.FederationAPIToken,
		SuccessURL:    appBaseURL + "/settings?upgraded=1",
		CancelURL:     appBaseURL + "/upgrade?canceled=1",
		UpgradeURL:    appBaseURL + "/upgrade",
	})
	if cfg.DhanamFederationURL != "" && cfg.FederationAPIToken != "" {
		logrus.WithField("dhanam_federation_url", cfg.DhanamFederationURL).Info("✓ Dhanam checkout relay wired")
		return
	}
	logrus.Warn("⚠ Dhanam checkout relay disabled (fails closed 503); set ENCLII_DHANAM_FEDERATION_URL and FEDERATION_API_TOKEN")
}
