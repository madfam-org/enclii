package main

import (
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/api"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/lockbox"
)

func wireVaultWriter(cfg *config.Config, apiHandler *api.Handler) {
	if cfg.SecretRotationEnabled && cfg.VaultAddress != "" && cfg.VaultToken != "" {
		apiHandler.SetVaultClient(lockbox.NewVaultClient(&lockbox.VaultConfig{
			Address:      cfg.VaultAddress,
			Token:        cfg.VaultToken,
			Namespace:    cfg.VaultNamespace,
			PollInterval: time.Duration(cfg.VaultPollInterval) * time.Second,
			Enabled:      true,
		}))
		logrus.Info("✓ Vault writer client wired to API handler")
		return
	}
	logrus.Warn("⚠ Vault writer client disabled (ENCLII_SECRET_ROTATION_ENABLED, ENCLII_VAULT_ADDRESS, and ENCLII_VAULT_TOKEN required)")
}
