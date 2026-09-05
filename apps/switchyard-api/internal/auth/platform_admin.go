package auth

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// ReconcilePlatformAdmins makes users.is_platform_admin match the operator
// allow-list, and is called once at API startup, after migrations and before
// the server accepts traffic.
//
// This is the "migration or startup mapping" half of ADR-003's role split, and
// it is a startup mapping rather than a data migration on purpose. A migration
// would have to name the estate's operators in a SQL file in a PUBLIC
// repository; a startup reconcile reads them from the deployment's own
// environment, where they already live, and re-asserts them on every boot so
// the allow-list stays the single source of truth.
//
// Existing `admin` principals are NOT promoted. They keep users.role = 'admin',
// which the API now reads as tenant_admin — ADR-003 is explicit that a
// migration promoting every current admin would re-create the defect the split
// exists to remove.
//
// An empty allow-list is not an error, but it is logged at WARN: it means no
// principal can perform a cross-tenant operation, which is a safe state and a
// surprising one.
func ReconcilePlatformAdmins(ctx context.Context, repo *db.TenantScopeRepository) error {
	if repo == nil {
		return nil
	}

	allowList := PlatformAdminAllowList()
	if len(allowList) == 0 {
		logrus.WithField("adr", "ADR-003").Warn(
			"No platform admins configured (ENCLII_PLATFORM_ADMIN_EMAILS / ENCLII_ADMIN_EMAILS are empty): " +
				"no principal has cross-tenant reach")
	}

	granted, revoked, err := repo.SetPlatformAdmins(ctx, allowList)
	if err != nil {
		return err
	}

	// Count, never contents: the addresses are operator identities and the
	// log stream is not the place to enumerate them on every boot.
	logrus.WithFields(logrus.Fields{
		"adr":              "ADR-003",
		"allow_list_size":  len(allowList),
		"granted_this_run": granted,
		"revoked_this_run": revoked,
	}).Info("Reconciled platform-admin rank from operator allow-list")

	return nil
}
