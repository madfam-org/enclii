package reconciler

import (
	"context"
	"errors"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// BackupConverger wires backups onto one live managed-Postgres cluster.
// Satisfied by *addons.AddonService.ConvergeAddonBackups; narrow for the same
// reason as NetworkConverger.
type BackupConverger interface {
	ConvergeAddonBackups(ctx context.Context, addon *types.DatabaseAddon) (addons.BackupConvergence, error)
}

// SetBackupConverger wires the converger used by the backup sweep. Safe to
// leave unset: the sweep no-ops then.
func (r *AddonReconciler) SetBackupConverger(c BackupConverger) {
	r.backupConverger = c
}

// backupSweepResult tallies one sweep. Exported to the log, and to tests.
type backupSweepResult struct {
	Live, Wired, Skipped, Unprotected, Drifted, Failed int
	// Unwedged counts walArchivingFailing Backups deleted so their schedule
	// runs again.
	Unwedged          int
	StoreUnconfigured bool
}

// sweepAddonBackups converges backups for every live managed-Postgres addon.
// It is the backfill for clusters provisioned while backups were not wired
// (every addon before 2026-10-26) and the repair for a cluster whose wiring
// was lost. It runs on the same cadence as the NetworkPolicy sweep.
//
// FAIL VISIBLE. While the store or its credential is missing, every live
// cluster is unprotected; the sweep says so at ERROR with the count, once per
// sweep, instead of once per addon or not at all. AddonBackupNeverCompleted is
// the page; this log line is the reason next to it.
func (r *AddonReconciler) sweepAddonBackups(ctx context.Context) backupSweepResult {
	var res backupSweepResult
	if r.backupConverger == nil || r.repos == nil || r.repos.DatabaseAddons == nil {
		return res
	}
	live, err := r.repos.DatabaseAddons.ListLivePostgres(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Failed to list live PostgreSQL addons for the backup sweep")
		return res
	}
	res.Live = len(live)
	var credentialErr error
	for _, addon := range live {
		out, err := r.backupConverger.ConvergeAddonBackups(ctx, addon)
		fields := logrus.Fields{"addon_id": addon.ID, "namespace": addon.K8sNamespace, "resource": addon.K8sResourceName}
		switch {
		case err == nil:
			if out.ScheduleResumed {
				r.logger.WithFields(fields).Info("Addon backup schedule resumed: cluster woke from hibernation")
			}
			if out.StanzaPatched || out.ScheduledBackupCreated {
				res.Wired++
				r.logger.WithFields(fields).WithFields(logrus.Fields{"stanza_patched": out.StanzaPatched, "scheduled_backup_created": out.ScheduledBackupCreated}).Info("Addon backups wired")
			}
			if out.CredentialReloaded {
				r.logger.WithFields(fields).Info("Addon backup credential version annotated: instances reload the replicated credential")
			}
			if len(out.WedgedBackupsDeleted) > 0 {
				res.Unwedged += len(out.WedgedBackupsDeleted)
				r.logger.WithFields(fields).WithField("backups", out.WedgedBackupsDeleted).Warn("Addon backup schedule was wedged: deleted its walArchivingFailing Backup now that WAL archiving is healthy; the schedule catches up")
			}
		case errors.Is(err, addons.ErrBackupConvergenceNotApplicable):
			res.Skipped++
			if out.ScheduleSuspended {
				r.logger.WithFields(fields).Info("Addon backup schedule suspended: cluster is hibernated")
			}
		case errors.Is(err, addons.ErrBackupStoreNotConfigured):
			res.StoreUnconfigured = true
			res.Unprotected++
		case errors.Is(err, addons.ErrBackupCredentialsMissing):
			credentialErr = err
			res.Unprotected++
		case errors.Is(err, addons.ErrBackupStanzaDrift):
			res.Drifted++
			r.logger.WithError(err).WithFields(fields).Error("Addon backup stanza differs from Enclii's; left untouched for an operator")
		default:
			res.Failed++
			r.logger.WithError(err).WithFields(fields).Warn("Addon backups not converged; next sweep retries")
		}
	}
	summary := r.logger.WithFields(logrus.Fields{"live": res.Live, "wired": res.Wired, "skipped": res.Skipped, "unprotected": res.Unprotected, "drifted": res.Drifted, "failed": res.Failed, "unwedged": res.Unwedged})
	switch {
	case res.StoreUnconfigured:
		summary.Error("Addon backups are NOT configured (ENCLII_ADDON_BACKUP_DESTINATION_BASE unset): live managed-Postgres addons have no backups")
	case credentialErr != nil:
		summary.WithError(credentialErr).Error("Addon backup credential missing: live managed-Postgres addons have no backups")
	default:
		summary.Debug("Addon backup sweep done")
	}
	return res
}
