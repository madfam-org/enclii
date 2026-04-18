package main

// P3.2 Sprint 1 — self-serve signup wiring.
//
// Split out of main.go to keep the bootstrap narrative linear and respect
// the line-count budget. The entire surface is disabled-by-default: set
// ENCLII_SIGNUP_ENABLED=true (plus the companion env vars) to turn it on.
//
// Dependencies:
//   - Janua: RegisterUser + GitHub OAuth on-behalf (companion PR)
//   - K8s client: writes GitHub access tokens into a Secret
//   - Email (Resend): verification + welcome emails
//   - Project service: creates the default project on provision
//
// If any dependency is missing we degrade gracefully — the service logs
// a warning and the /v1/signup routes return 404 (since the service is
// still un-enabled).

import (
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/api"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/notifications"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/signup"
)

// wireSignup attaches the self-serve signup service to the API handler.
// Safe to call unconditionally — internally gated on cfg.SignupEnabled.
func wireSignup(
	cfg *config.Config,
	repos *db.Repositories,
	apiHandler *api.Handler,
	k8sClient *k8s.Client,
	emailService *notifications.EmailService,
) {
	if !cfg.SignupEnabled {
		logrus.Info("✓ Self-serve signup DISABLED (ENCLII_SIGNUP_ENABLED=false); /v1/signup endpoints return 404")
		return
	}

	apiBaseURL := cfg.SelfServiceAPIBaseURL
	if apiBaseURL == "" {
		apiBaseURL = cfg.SelfURL
	}

	januaClient := signup.NewHTTPJanuaClient(cfg.JanuaAPIURL, cfg.JanuaAdminToken)

	var secretWriter signup.SecretWriter
	if k8sClient != nil && k8sClient.IsValid() {
		secretWriter = signup.NewK8sSecretWriter(k8sClient, "", "")
	} else {
		logrus.Warn("⚠ Signup: k8s client not available; GitHub OAuth tokens cannot be persisted — signup will fail at github_linked step")
	}

	emailAdapter := signup.NewEmailAdapter(emailService)
	projectCreator := signup.NewDefaultProjectCreator(repos)

	svc := signup.NewService(signup.Config{
		Repos:         repos,
		Janua:         januaClient,
		Email:         emailAdapter,
		Secrets:       secretWriter,
		Projects:      projectCreator,
		Logger:        logrus.StandardLogger(),
		AppBaseURL:    cfg.AppBaseURL,
		APIBaseURL:    apiBaseURL,
		FeatureFlagOn: true,
	})
	apiHandler.SetSignupService(svc)

	logrus.Infof("✓ Self-serve signup ENABLED at /v1/signup (app_base_url=%s, api_base_url=%s)",
		cfg.AppBaseURL, apiBaseURL)
}
