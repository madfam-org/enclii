package reconciler

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// addon.backup.completed — the one addon usage event that is OBSERVED rather
// than commanded.
//
// The other three (`addon.ready`, `addon.plan.changed`, `addon.destroyed`)
// mirror rows in the managed_db_addon_events ledger, because a human or an API
// call caused them. A backup is different: nothing in this codebase runs it.
// CloudNativePG's ScheduledBackup fires at 04:00 UTC inside the cluster and
// the only trace on this side is `status.lastSuccessfulBackup` changing on the
// Cluster object. So the reconciler that already polls that object every cycle
// is the honest place to notice.
//
// IDEMPOTENCY IS LOAD-BEARING HERE, NOT A NICETY. The reconciler has no
// durable record of what it last saw — `lastSeenBackup` below is a process
// map, so a restart, a rollout or a second replica re-observes a backup that
// was already reported. Each of those would be a duplicate usage event if the
// key were anything but the backup itself. It is: addon id plus the backup's
// own timestamp, which is a property of the artefact, not of the observation.
// Two observers reporting the same backup produce the same key and Waybill
// records it once (migration 040).
//
// This is a record that a backup COMPLETED. It is not a restore, and it is not
// evidence that the backup can be restored — that evidence comes only from an
// actual restore with a verified row count.

// backupObserver remembers the last backup timestamp reported per addon, so a
// steady state does not re-emit every reconcile cycle. Process-local and
// deliberately unpersisted: correctness comes from the idempotency key, and
// this map exists only to keep the common case quiet.
type backupObserver struct {
	mu   sync.Mutex
	seen map[string]string // addon id -> last reported backup timestamp
}

func newBackupObserver() *backupObserver {
	return &backupObserver{seen: map[string]string{}}
}

// observe reports whether this timestamp is new for this addon, and records it.
func (o *backupObserver) observe(addonID, ts string) bool {
	if ts == "" {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen[addonID] == ts {
		return false
	}
	o.seen[addonID] = ts
	return true
}

// forget drops an addon's entry so a deleted addon does not leak a map slot.
func (o *backupObserver) forget(addonID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.seen, addonID)
}

// lastSuccessfulBackup reads the timestamp CloudNativePG publishes on the
// Cluster. Returns "" when absent — a cluster that has never been backed up,
// or one whose status has not been populated yet. Both are silence here; the
// AddonBackupNeverCompleted alert is what speaks up about them, because a
// reconciler that has never seen a backup cannot tell "not yet" from "never".
func lastSuccessfulBackup(cluster *unstructured.Unstructured) string {
	ts, found, err := unstructured.NestedString(cluster.Object, "status", "lastSuccessfulBackup")
	if err != nil || !found {
		return ""
	}
	return ts
}

// emitBackupCompleted forwards a newly observed backup to the usage pipeline.
// Best-effort: a usage-pipeline outage must not disturb reconciliation.
func (r *AddonReconciler) emitBackupCompleted(
	ctx context.Context, addon *types.DatabaseAddon, backupTS string, logger *logrus.Entry,
) {
	if r.usage == nil || r.backups == nil {
		return
	}
	if !r.backups.observe(addon.ID.String(), backupTS) {
		return
	}

	occurred, err := time.Parse(time.RFC3339, backupTS)
	if err != nil {
		// Report it anyway, timestamped now: the fact of the backup is worth
		// more than its exact clock reading, and the key still dedupes.
		occurred = time.Now().UTC()
	}

	if err := r.usage.EmitAddonEvent(ctx, addons.AddonUsageEvent{
		IdempotencyKey: "backup:" + addon.ID.String() + ":" + backupTS,
		EventType:      "addon.backup.completed",
		Addon:          addon,
		OccurredAt:     occurred,
		Extra:          map[string]string{"backup_completed_at": backupTS},
	}); err != nil {
		logger.WithError(err).WithField("backup_completed_at", backupTS).
			Warn("Failed to forward addon.backup.completed to Waybill")
	}
}
