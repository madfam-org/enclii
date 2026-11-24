package addons

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// A ScheduledBackup wedged behind a walArchivingFailing Backup.
//
// WHY (2026-11-01). In CNPG 1.29 a ScheduledBackup creates nothing while any
// Backup it created is not done, and "done" means completed or failed only
// (BackupStatus.IsDone). An instance that finds WAL archiving broken when a
// backup starts sets that Backup to walArchivingFailing and stops; nothing ever
// moves it on. So the schedule logged «The system is already taking a backup
// for this ScheduledBackup, retrying in 60 seconds» every minute and took no
// backup, even after archiving recovered: two addon clusters went 4–5 days
// without a daily. Deleting the stuck Backup by hand let each schedule catch up
// within seconds, with a Backup named after the missed slot.
//
// clearWedgedBackups is that delete, done by the sweep. It is the only CNPG
// object the sweep deletes, so it is narrow:
//
//   - only Backups the cluster's own Enclii schedule created (CNPG's
//     cnpg.io/scheduled-backup label names <cluster>-daily) for this Cluster;
//   - only in phase walArchivingFailing: completed, failed, pending, running
//     and every other phase are left alone;
//   - only while the Cluster reports ContinuousArchiving=True, so a Backup is
//     never cleared into an archiving failure that is still going on;
//   - only an Enclii-managed, awake Cluster of this addon, in the addon's
//     project namespace;
//   - each delete is preconditioned on the Backup's UID and resourceVersion,
//     and has an audit row written before it (no row, no delete) and one
//     after it.
//
// Delete, not "mark failed": the phase is status the instance manager owns, and
// a delete is what was verified on 11-01. A walArchivingFailing Backup stopped
// before taking a base backup, so there is no backup data behind it.
const (
	cnpgScheduledBackupLabel       = "cnpg.io/scheduled-backup"
	backupPhaseWALArchivingFailing = "walArchivingFailing"
	conditionContinuousArchiving   = "ContinuousArchiving"

	// The audit trail's actor and action for the delete.
	backupSweepActor               = "addon-backup-sweep@system.enclii.dev"
	auditActionWedgedBackupDeleted = "addon.backup.wedged_deleted"
)

var cnpgBackupGVR = schema.GroupVersionResource{
	Group:    "postgresql.cnpg.io",
	Version:  "v1",
	Resource: "backups",
}

// backupAuditLog is the slice of the audit store the sweep writes. Satisfied
// by *db.AuditLogRepository.
type backupAuditLog interface {
	Log(ctx context.Context, log *types.AuditLog) error
}

// continuousArchivingHealthy reports whether the Cluster's ContinuousArchiving
// condition is True. An absent condition is not healthy.
func continuousArchivingHealthy(cluster *unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(cluster.Object, "status", "conditions")
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if ok && cond["type"] == conditionContinuousArchiving {
			return cond["status"] == string(metav1.ConditionTrue)
		}
	}
	return false
}

// wedgedBackupAudit is one audit row for the delete. Each row gets its own copy
// of details.
func wedgedBackupAudit(addon *types.DatabaseAddon, outcome string, details map[string]interface{}) *types.AuditLog {
	project := addon.ProjectID
	return &types.AuditLog{
		ActorEmail: backupSweepActor, ActorRole: types.RoleSystem,
		Action: auditActionWedgedBackupDeleted, ResourceType: "database_addon", ResourceID: addon.ID.String(),
		ResourceName: addon.Name, ProjectID: &project, Outcome: outcome, Context: maps.Clone(details),
	}
}

// clearWedgedBackups deletes the cluster's wedged Backups (see above) and
// returns their names. Without an audit store it deletes nothing and says so.
func (p *PostgresProvisioner) clearWedgedBackups(ctx context.Context, addon *types.DatabaseAddon, audit backupAuditLog) ([]string, error) {
	if p == nil || p.dynamicClient == nil {
		return nil, errors.New("postgres provisioner has no cluster client")
	}
	ns, name := addon.K8sNamespace, addon.K8sResourceName
	if ns != "project-"+addon.ProjectID.String()[:8] {
		return nil, nil
	}
	cluster, err := p.dynamicClient.Resource(cnpgGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read CNPG cluster %s/%s: %w", ns, name, err)
	}
	if !managedForAddon(cluster, addon) || cluster.GetAnnotations()[CNPGHibernationAnnotation] == "on" || !continuousArchivingHealthy(cluster) {
		return nil, nil
	}
	schedule := name + "-daily"
	backups, err := p.dynamicClient.Resource(cnpgBackupGVR).Namespace(ns).List(ctx, metav1.ListOptions{LabelSelector: cnpgScheduledBackupLabel + "=" + schedule})
	if err != nil {
		return nil, fmt.Errorf("list Backups of ScheduledBackup %s/%s: %w", ns, schedule, err)
	}
	var deleted []string
	for i := range backups.Items {
		b := &backups.Items[i]
		phase, _, _ := unstructured.NestedString(b.Object, "status", "phase")
		of, _, _ := unstructured.NestedString(b.Object, "spec", "cluster", "name")
		if phase != backupPhaseWALArchivingFailing || of != name || b.GetLabels()[cnpgScheduledBackupLabel] != schedule || b.GetDeletionTimestamp() != nil {
			continue
		}
		if audit == nil {
			return deleted, fmt.Errorf("backup %s/%s is wedged in %s but no audit store is configured; not deleted", ns, b.GetName(), phase)
		}
		uid, rv := b.GetUID(), b.GetResourceVersion()
		details := map[string]interface{}{
			"namespace": ns, "cluster": name, "scheduled_backup": schedule,
			"backup": b.GetName(), "backup_uid": string(uid), "backup_phase": phase,
			"continuous_archiving": string(metav1.ConditionTrue),
		}
		if err := audit.Log(ctx, wedgedBackupAudit(addon, "requested", details)); err != nil {
			return deleted, fmt.Errorf("audit the delete of wedged Backup %s/%s (left in place): %w", ns, b.GetName(), err)
		}
		if err := p.dynamicClient.Resource(cnpgBackupGVR).Namespace(ns).Delete(ctx, b.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &rv}}); err != nil {
			details["error"] = err.Error()
			_ = audit.Log(ctx, wedgedBackupAudit(addon, "failure", details))
			return deleted, fmt.Errorf("delete wedged Backup %s/%s: %w", ns, b.GetName(), err)
		}
		deleted = append(deleted, b.GetName())
		if err := audit.Log(ctx, wedgedBackupAudit(addon, "success", details)); err != nil {
			p.logger.WithError(err).WithFields(logrus.Fields{"namespace": ns, "backup": b.GetName()}).Warn("Wedged Backup deleted; its result audit row was not written (the request row exists)")
		}
	}
	return deleted, nil
}
