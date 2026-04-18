package main

// P3.6 tenant data export wiring.
//
// Split out of main.go for two reasons:
//
//  1. The wiring has a lot of optional branches (R2 credentials may or may
//     not be configured, each provider is optional) and keeping it here
//     lets the main.go bootstrap narrative stay linear.
//
//  2. main.go has a 800-line budget enforced by pre-commit; isolating
//     module-specific wiring into neighbour files preserves that budget
//     as new features land.
//
// When any R2 credential is missing the service is left unwired and
// /v1/exports endpoints return 503. This matches how audit, logs, and
// outbound webhooks already degrade in local-dev setups.

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/api"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/export"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/notifications"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/storage"
)

// wireTenantExport bootstraps the P3.6 service and attaches it to the
// API handler. Safe to call unconditionally — internally gated on
// config presence.
func wireTenantExport(
	cfg *config.Config,
	repos *db.Repositories,
	apiHandler *api.Handler,
	emailService *notifications.EmailService,
) {
	if cfg.TenantExportR2AccountID == "" ||
		cfg.TenantExportR2AccessKeyID == "" ||
		cfg.TenantExportR2AccessKeySecret == "" {
		logrus.Warn("⚠ Tenant export DISABLED (TENANT_EXPORT_R2_* not set); /v1/exports endpoints return 503")
		return
	}

	bucket := cfg.TenantExportR2Bucket
	if bucket == "" {
		bucket = "enclii-backups"
	}
	prefix := cfg.TenantExportR2Prefix
	if prefix == "" {
		prefix = "tenant-exports"
	}

	r2Client, err := storage.NewR2Client(context.Background(), &storage.R2Config{
		AccountID:       cfg.TenantExportR2AccountID,
		AccessKeyID:     cfg.TenantExportR2AccessKeyID,
		AccessKeySecret: cfg.TenantExportR2AccessKeySecret,
		BucketName:      bucket,
	})
	if err != nil {
		logrus.WithError(err).Warn("⚠ Tenant export: R2 client init failed; /v1/exports endpoints will return 503")
		return
	}

	exportSvc, err := export.NewService(export.Config{
		Repo:           repos.TenantExports,
		Projects:       repos.Projects,
		ProjectAccess:  repos.ProjectAccess,
		Storage:        r2Client,
		BundleProvider: export.NewRepoBundleProvider(repos, logrus.StandardLogger()),
		DumpProvider:   export.NewPgDumpProvider(logrus.StandardLogger()),
		BlobProvider:   export.NewR2BlobProvider(r2Client, nil, logrus.StandardLogger()),
		Notifier:       export.NewEmailNotifier(emailService, cfg.AppBaseURL),
		Logger:         logrus.StandardLogger(),
		R2Prefix:       prefix,
		IsProduction:   cfg.Environment == "production",
	})
	if err != nil {
		logrus.WithError(err).Warn("⚠ Tenant export: service init failed")
		return
	}

	apiHandler.SetTenantExportService(exportSvc)
	logrus.Infof("✓ Tenant export wired at /v1/projects/:slug/exports (bucket=%s, prefix=%s, prod=%t)",
		bucket, prefix, cfg.Environment == "production")
}
