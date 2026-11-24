package addons

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// bkBackup is a Backup the cluster's Enclii schedule created, in phase.
func bkBackup(name, phase string, mutate func(u *unstructured.Unstructured)) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec":   map[string]interface{}{"cluster": map[string]interface{}{"name": bkCluster}, "method": "barmanObjectStore"},
		"status": map[string]interface{}{"phase": phase},
	}}
	u.SetAPIVersion(CloudNativePGAPIVersion)
	u.SetKind("Backup")
	u.SetNamespace(bkNS)
	u.SetName(name)
	u.SetUID(k8stypes.UID("uid-" + name))
	u.SetLabels(map[string]string{cnpgScheduledBackupLabel: bkCluster + "-daily"})
	if mutate != nil {
		mutate(u)
	}
	return u
}

// archiving sets the Cluster's ContinuousArchiving condition ("" leaves it out).
func archiving(status string) func(u *unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		conditions := []interface{}{map[string]interface{}{"type": "Ready", "status": "True"}}
		if status != "" {
			conditions = append(conditions, map[string]interface{}{"type": conditionContinuousArchiving, "status": status})
		}
		_ = unstructured.SetNestedSlice(u.Object, conditions, "status", "conditions")
	}
}

type fakeBackupAudit struct {
	rows []*types.AuditLog
	err  error
}

func (f *fakeBackupAudit) Log(_ context.Context, l *types.AuditLog) error {
	if f.err != nil {
		return f.err
	}
	f.rows = append(f.rows, l)
	return nil
}

func (f *fakeBackupAudit) outcomes() []string {
	out := []string{}
	for _, r := range f.rows {
		out = append(out, r.Outcome)
	}
	return out
}

func bkBackupNames(t *testing.T, dyn *dynamicfake.FakeDynamicClient) []string {
	t.Helper()
	list, err := dyn.Resource(cnpgBackupGVR).Namespace(bkNS).List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	names := []string{}
	for _, b := range list.Items {
		names = append(names, b.GetName())
	}
	return names
}

const (
	bkWedged = bkCluster + "-daily-20261028040000"
	bkDone   = bkCluster + "-daily-20261027040000"
)

// 2026-11-01: two addon schedules sat behind a walArchivingFailing Backup for
// days after archiving recovered. The sweep deletes that Backup, audited.
func TestAWedgedBackupIsDeletedOnceArchivingIsHealthyAgain(t *testing.T) {
	ctx := context.Background()
	p, dyn, _ := bkProvisionerWith(t, []runtime.Object{
		bkClusterObj(archiving("True")),
		bkBackup(bkDone, "completed", nil),
		bkBackup(bkWedged, backupPhaseWALArchivingFailing, nil),
	})
	audit := &fakeBackupAudit{}

	deleted, err := p.clearWedgedBackups(ctx, bkAddon(), audit)
	require.NoError(t, err)
	require.Equal(t, []string{bkWedged}, deleted)
	require.Equal(t, []string{bkDone}, bkBackupNames(t, dyn), "the completed Backup is kept")
	require.Equal(t, []string{"delete backups"}, writes(dyn.Actions()))

	require.Equal(t, []string{"requested", "success"}, audit.outcomes(), "a row before the delete and one after it")
	for _, row := range audit.rows {
		require.Equal(t, types.RoleSystem, row.ActorRole)
		require.Equal(t, backupSweepActor, row.ActorEmail)
		require.Equal(t, auditActionWedgedBackupDeleted, row.Action)
		require.Equal(t, "database_addon", row.ResourceType)
		require.Equal(t, bkAddonID.String(), row.ResourceID)
		require.Equal(t, bkProjectID, *row.ProjectID)
		require.Equal(t, bkWedged, row.Context["backup"])
		require.Equal(t, "uid-"+bkWedged, row.Context["backup_uid"])
		require.Equal(t, backupPhaseWALArchivingFailing, row.Context["backup_phase"])
		require.Equal(t, bkCluster+"-daily", row.Context["scheduled_backup"])
	}

	// Nothing is wedged any more: no write, no audit row.
	dyn.ClearActions()
	deleted, err = p.clearWedgedBackups(ctx, bkAddon(), audit)
	require.NoError(t, err)
	require.Nil(t, deleted)
	require.Empty(t, writes(dyn.Actions()))
	require.Len(t, audit.rows, 2)
}

func TestAWedgedBackupStaysWhileArchivingStillFails(t *testing.T) {
	for name, condition := range map[string]string{
		"archiving failing":    "False",
		"archiving unknown":    "Unknown",
		"no archiving reading": "",
	} {
		t.Run(name, func(t *testing.T) {
			p, dyn, _ := bkProvisionerWith(t, []runtime.Object{
				bkClusterObj(archiving(condition)),
				bkBackup(bkWedged, backupPhaseWALArchivingFailing, nil),
			})
			audit := &fakeBackupAudit{}
			deleted, err := p.clearWedgedBackups(context.Background(), bkAddon(), audit)
			require.NoError(t, err)
			require.Empty(t, deleted)
			require.Equal(t, []string{bkWedged}, bkBackupNames(t, dyn), "a cleared Backup would only wedge again")
			require.Empty(t, writes(dyn.Actions()))
			require.Empty(t, audit.rows)
		})
	}
}

func TestBackupsInAnyOtherPhaseAreNeverTouched(t *testing.T) {
	for _, phase := range []string{"completed", "failed", "running", "pending", "started", "finalizing", ""} {
		t.Run("phase "+phase, func(t *testing.T) {
			p, dyn, _ := bkProvisionerWith(t, []runtime.Object{
				bkClusterObj(archiving("True")),
				bkBackup(bkWedged, phase, nil),
			})
			audit := &fakeBackupAudit{}
			deleted, err := p.clearWedgedBackups(context.Background(), bkAddon(), audit)
			require.NoError(t, err)
			require.Empty(t, deleted)
			require.Equal(t, []string{bkWedged}, bkBackupNames(t, dyn))
			require.Empty(t, writes(dyn.Actions()))
			require.Empty(t, audit.rows)
		})
	}
}

// Only Backups of the cluster's own Enclii schedule, only for an awake Cluster
// Enclii manages for this addon, only in the addon's project namespace.
func TestOnlyTheEncliiScheduleOfAnAddonClusterIsUnwedged(t *testing.T) {
	wedgedWith := func(mutate func(u *unstructured.Unstructured)) *unstructured.Unstructured {
		return bkBackup(bkWedged, backupPhaseWALArchivingFailing, mutate)
	}
	for name, tc := range map[string]struct {
		cluster *unstructured.Unstructured
		backup  *unstructured.Unstructured
		addon   func(a *types.DatabaseAddon)
	}{
		"on-demand Backup (no schedule label)": {
			cluster: bkClusterObj(archiving("True")),
			backup:  wedgedWith(func(u *unstructured.Unstructured) { u.SetLabels(nil) }),
		},
		"another schedule's Backup": {
			cluster: bkClusterObj(archiving("True")),
			backup: wedgedWith(func(u *unstructured.Unstructured) {
				u.SetLabels(map[string]string{cnpgScheduledBackupLabel: "operator-hourly"})
			}),
		},
		"Backup of another cluster": {
			cluster: bkClusterObj(archiving("True")),
			backup: wedgedWith(func(u *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(u.Object, "pg-other", "spec", "cluster", "name")
			}),
		},
		"cluster not managed by enclii": {
			cluster: bkClusterObj(func(u *unstructured.Unstructured) { archiving("True")(u); u.SetLabels(nil) }),
			backup:  wedgedWith(nil),
		},
		"another addon's cluster": {
			cluster: bkClusterObj(func(u *unstructured.Unstructured) {
				archiving("True")(u)
				u.SetLabels(map[string]string{LabelManagedBy: LabelManagedValue, LabelAddonID: uuid.NewString()})
			}),
			backup: wedgedWith(nil),
		},
		"hibernated cluster": {
			cluster: bkClusterObj(func(u *unstructured.Unstructured) {
				archiving("True")(u)
				u.SetAnnotations(map[string]string{CNPGHibernationAnnotation: "on"})
			}),
			backup: wedgedWith(nil),
		},
		"addon outside its project namespace": {
			cluster: bkClusterObj(archiving("True")),
			backup:  wedgedWith(nil),
			addon:   func(a *types.DatabaseAddon) { a.ProjectID = uuid.MustParse("000000c3-0000-0000-0000-000000000000") },
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, dyn, _ := bkProvisionerWith(t, []runtime.Object{tc.cluster, tc.backup})
			addon := bkAddon()
			if tc.addon != nil {
				tc.addon(addon)
			}
			audit := &fakeBackupAudit{}
			deleted, err := p.clearWedgedBackups(context.Background(), addon, audit)
			require.NoError(t, err)
			require.Empty(t, deleted)
			require.Equal(t, []string{bkWedged}, bkBackupNames(t, dyn))
			require.Empty(t, writes(dyn.Actions()))
			require.Empty(t, audit.rows)
		})
	}
}

func TestAWedgedBackupIsNeverDeletedWithoutAnAuditRow(t *testing.T) {
	for name, audit := range map[string]backupAuditLog{
		"no audit store":       nil,
		"audit store refusing": &fakeBackupAudit{err: errors.New("audit store down")},
	} {
		t.Run(name, func(t *testing.T) {
			p, dyn, _ := bkProvisionerWith(t, []runtime.Object{
				bkClusterObj(archiving("True")),
				bkBackup(bkWedged, backupPhaseWALArchivingFailing, nil),
			})
			deleted, err := p.clearWedgedBackups(context.Background(), bkAddon(), audit)
			require.Error(t, err)
			require.Empty(t, deleted)
			require.Equal(t, []string{bkWedged}, bkBackupNames(t, dyn))
			require.Empty(t, writes(dyn.Actions()))
		})
	}
}

// Through the sweep's entry point, with the real audit repository: the
// convergence runs first, then the wedged Backup goes, with two audit_logs rows.
func TestConvergeAddonBackupsUnwedgesTheScheduleIntoTheAuditTrail(t *testing.T) {
	bkStore(t)
	ctx := context.Background()
	p, dyn, _ := bkProvisionerWith(t, []runtime.Object{
		bkClusterObj(archiving("True")),
		bkSchedule(nil, false),
		bkBackup(bkWedged, backupPhaseWALArchivingFailing, nil),
	}, bkCredential(backupCredentialKeys...))
	raw, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	s := &AddonService{repos: db.NewRepositories(raw), logger: testLogger(), provisioners: map[types.DatabaseAddonType]AddonProvisioner{types.DatabaseAddonTypePostgres: p}}
	for _, outcome := range []string{"requested", "success"} {
		mock.ExpectExec(`INSERT INTO audit_logs`).WithArgs(
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), backupSweepActor, types.RoleSystem,
			auditActionWedgedBackupDeleted, "database_addon", bkAddonID.String(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), outcome, sqlmock.AnyArg(), sqlmock.AnyArg(),
		).WillReturnResult(sqlmock.NewResult(0, 1))
	}

	out, err := s.ConvergeAddonBackups(ctx, bkAddon())
	require.NoError(t, err)
	require.True(t, out.StanzaPatched)
	require.Equal(t, []string{bkWedged}, out.WedgedBackupsDeleted)
	require.Empty(t, bkBackupNames(t, dyn))
	require.NoError(t, mock.ExpectationsWereMet())
}

// A refused convergence (here: hibernated) stops before the delete.
func TestConvergeAddonBackupsClearsNothingWhenTheConvergenceRefuses(t *testing.T) {
	bkStore(t)
	p, dyn, _ := bkProvisionerWith(t, []runtime.Object{
		bkClusterObj(func(u *unstructured.Unstructured) {
			archiving("True")(u)
			u.SetAnnotations(map[string]string{CNPGHibernationAnnotation: "on"})
		}),
		bkBackup(bkWedged, backupPhaseWALArchivingFailing, nil),
	}, bkCredential(backupCredentialKeys...))
	s := &AddonService{logger: testLogger(), provisioners: map[types.DatabaseAddonType]AddonProvisioner{types.DatabaseAddonTypePostgres: p}}
	out, err := s.ConvergeAddonBackups(context.Background(), bkAddon())
	require.ErrorIs(t, err, ErrBackupConvergenceNotApplicable)
	require.Empty(t, out.WedgedBackupsDeleted)
	require.Equal(t, []string{bkWedged}, bkBackupNames(t, dyn))
}
