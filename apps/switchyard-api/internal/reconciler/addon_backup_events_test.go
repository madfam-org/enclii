package reconciler

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type recordingEmitter struct {
	events []addons.AddonUsageEvent
	err    error
}

func (r *recordingEmitter) EmitAddonEvent(_ context.Context, ev addons.AddonUsageEvent) error {
	r.events = append(r.events, ev)
	return r.err
}

func backupAddon() *types.DatabaseAddon {
	return &types.DatabaseAddon{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Type:      types.DatabaseAddonTypePostgres,
		Name:      "map-db",
		Plan:      "standard-0",
	}
}

func clusterWithBackup(ts string) *unstructured.Unstructured {
	obj := map[string]interface{}{"status": map[string]interface{}{}}
	if ts != "" {
		obj["status"].(map[string]interface{})["lastSuccessfulBackup"] = ts
	}
	return &unstructured.Unstructured{Object: obj}
}

func TestLastSuccessfulBackupIsEmptyWhenAbsent(t *testing.T) {
	if got := lastSuccessfulBackup(clusterWithBackup("")); got != "" {
		t.Errorf("got %q, want empty for a cluster that has never backed up", got)
	}
	if got := lastSuccessfulBackup(&unstructured.Unstructured{Object: map[string]interface{}{}}); got != "" {
		t.Errorf("got %q, want empty for a cluster with no status", got)
	}
	if got := lastSuccessfulBackup(clusterWithBackup("2026-09-05T04:00:00Z")); got != "2026-09-05T04:00:00Z" {
		t.Errorf("got %q", got)
	}
}

// The steady state is silence: the reconciler polls every cycle and the same
// backup must not be reported on each pass.
func TestTheSameBackupIsReportedOnce(t *testing.T) {
	em := &recordingEmitter{}
	r := &AddonReconciler{usage: em, backups: newBackupObserver(), logger: logrus.New()}
	addon := backupAddon()
	entry := logrus.NewEntry(logrus.New())

	for i := 0; i < 3; i++ {
		r.emitBackupCompleted(context.Background(), addon, "2026-09-05T04:00:00Z", entry)
	}
	if len(em.events) != 1 {
		t.Fatalf("emitted %d events for one backup, want 1", len(em.events))
	}
	ev := em.events[0]
	if ev.EventType != "addon.backup.completed" {
		t.Errorf("event type = %q", ev.EventType)
	}
	if ev.Extra["backup_completed_at"] != "2026-09-05T04:00:00Z" {
		t.Errorf("extra = %v", ev.Extra)
	}
}

// The key is a property of the BACKUP, not of the observation, so two
// observers — or one process before and after a restart — produce the same key
// and Waybill records the backup once.
func TestTheKeyIdentifiesTheBackupNotTheObserver(t *testing.T) {
	addon := backupAddon()
	entry := logrus.NewEntry(logrus.New())

	first := &recordingEmitter{}
	r1 := &AddonReconciler{usage: first, backups: newBackupObserver(), logger: logrus.New()}
	r1.emitBackupCompleted(context.Background(), addon, "2026-09-05T04:00:00Z", entry)

	// A fresh observer stands in for a restarted or second replica: its map is
	// empty, so it re-observes and re-emits.
	second := &recordingEmitter{}
	r2 := &AddonReconciler{usage: second, backups: newBackupObserver(), logger: logrus.New()}
	r2.emitBackupCompleted(context.Background(), addon, "2026-09-05T04:00:00Z", entry)

	if len(first.events) != 1 || len(second.events) != 1 {
		t.Fatalf("want one emission each, got %d and %d", len(first.events), len(second.events))
	}
	if first.events[0].IdempotencyKey != second.events[0].IdempotencyKey {
		t.Errorf("keys differ across observers (%q vs %q) — the duplicate would be recorded twice",
			first.events[0].IdempotencyKey, second.events[0].IdempotencyKey)
	}
	if first.events[0].IdempotencyKey == "" {
		t.Error("no idempotency key")
	}
}

func TestANewerBackupIsReported(t *testing.T) {
	em := &recordingEmitter{}
	r := &AddonReconciler{usage: em, backups: newBackupObserver(), logger: logrus.New()}
	addon := backupAddon()
	entry := logrus.NewEntry(logrus.New())

	r.emitBackupCompleted(context.Background(), addon, "2026-09-05T04:00:00Z", entry)
	r.emitBackupCompleted(context.Background(), addon, "2026-09-06T04:00:00Z", entry)
	if len(em.events) != 2 {
		t.Fatalf("emitted %d, want 2 (one per distinct backup)", len(em.events))
	}
	if em.events[0].IdempotencyKey == em.events[1].IdempotencyKey {
		t.Error("two distinct backups share an idempotency key — the second would be discarded")
	}
}

// A cluster that has never been backed up says nothing here. The
// AddonBackupNeverCompleted alert is what speaks up about it: the reconciler
// cannot tell "not yet" from "never".
func TestNoBackupEmitsNothing(t *testing.T) {
	em := &recordingEmitter{}
	r := &AddonReconciler{usage: em, backups: newBackupObserver(), logger: logrus.New()}
	r.emitBackupCompleted(context.Background(), backupAddon(), "", logrus.NewEntry(logrus.New()))
	if len(em.events) != 0 {
		t.Errorf("emitted %d events for a cluster with no backup", len(em.events))
	}
}

// No usage pipeline configured must be a no-op, not a panic.
func TestNilEmitterIsANoOp(t *testing.T) {
	r := &AddonReconciler{backups: newBackupObserver(), logger: logrus.New()}
	r.emitBackupCompleted(context.Background(), backupAddon(), "2026-09-05T04:00:00Z", logrus.NewEntry(logrus.New()))
}

func TestForgetDropsTheAddonEntry(t *testing.T) {
	o := newBackupObserver()
	if !o.observe("a", "t1") {
		t.Fatal("first observation was not new")
	}
	if o.observe("a", "t1") {
		t.Fatal("repeat observation reported as new")
	}
	o.forget("a")
	if !o.observe("a", "t1") {
		t.Error("forget did not drop the entry")
	}
}
