package addons

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// bkNS is the addon's project namespace: project-<first 8 of bkProjectID>.
const (
	bkNS      = "project-000000b2"
	bkCluster = "pg-kernel09-fixture"
)

var (
	bkAddonID   = uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
	bkProjectID = uuid.MustParse("000000b2-0000-0000-0000-000000000000")
)

func bkAddon() *types.DatabaseAddon {
	return &types.DatabaseAddon{ID: bkAddonID, ProjectID: bkProjectID, Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusReady, K8sNamespace: bkNS, K8sResourceName: bkCluster}
}

func bkStore(t *testing.T) {
	t.Helper()
	t.Setenv("ENCLII_ADDON_BACKUP_DESTINATION_BASE", "s3://addon-backups-fixture/")
	t.Setenv("ENCLII_ADDON_BACKUP_ENDPOINT_URL", "https://fixture.r2.example")
}

// bkClusterObj is an Enclii-managed Cluster; mutate tweaks it before use.
func bkClusterObj(mutate func(u *unstructured.Unstructured)) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{"spec": map[string]interface{}{"instances": int64(1)}}}
	u.SetAPIVersion(CloudNativePGAPIVersion)
	u.SetKind(CloudNativePGKind)
	u.SetNamespace(bkNS)
	u.SetName(bkCluster)
	u.SetUID("uid-cluster")
	u.SetLabels(map[string]string{LabelManagedBy: LabelManagedValue, LabelAddonID: bkAddonID.String()})
	if mutate != nil {
		mutate(u)
	}
	return u
}

// bkCredential is the SOURCE credential Secret with fixture bytes, never a
// real value.
func bkCredential(keys ...string) *corev1.Secret {
	data := map[string][]byte{}
	for _, k := range keys {
		data[k] = []byte("fixture-" + k)
	}
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: BackupCredentialsSecretName, Namespace: BackupCredentialsSourceNS}, Data: data}
}

func bkProvisioner(t *testing.T, cluster *unstructured.Unstructured, kubeObjects ...runtime.Object) (*PostgresProvisioner, *dynamicfake.FakeDynamicClient, *kubefake.Clientset) {
	t.Helper()
	objects := []runtime.Object{}
	if cluster != nil {
		objects = append(objects, cluster)
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		cnpgGVR:                "ClusterList",
		cnpgScheduledBackupGVR: "ScheduledBackupList",
		cnpgBackupGVR:          "BackupList",
	}, objects...)
	kube := kubefake.NewSimpleClientset(kubeObjects...)
	return &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: kube}, dynamicClient: dyn, logger: testLogger()}, dyn, kube
}

func writes(actions []k8stesting.Action) []string {
	out := []string{}
	for _, a := range actions {
		switch a.GetVerb() {
		case "create", "update", "patch", "delete":
			out = append(out, a.GetVerb()+" "+a.GetResource().Resource)
		}
	}
	return out
}

func TestConvergeBackupsWiresCredentialThenEgressThenStanzaThenSchedule(t *testing.T) {
	bkStore(t)
	ctx := context.Background()
	p, dyn, kube := bkProvisioner(t, bkClusterObj(nil), bkCredential(backupCredentialKeys...))

	out, err := p.convergeBackups(ctx, bkAddon())
	require.NoError(t, err)
	require.Equal(t, BackupConvergence{StanzaPatched: true, ScheduledBackupCreated: true, CredentialReloaded: true}, out)

	// Credential copy: owned by THIS cluster, immutable, in the addon namespace.
	copied, err := kube.CoreV1().Secrets(bkNS).Get(ctx, BackupCredentialsSecretName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, copied.OwnerReferences, 1)
	require.Equal(t, "uid-cluster", string(copied.OwnerReferences[0].UID))
	require.NotNil(t, copied.Immutable)
	require.True(t, *copied.Immutable)

	// Egress to the object store: this cluster only, HTTPS only, public only.
	pol, err := kube.NetworkingV1().NetworkPolicies(bkNS).Get(ctx, bkCluster+"-backup-egress", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"cnpg.io/cluster": bkCluster}, pol.Spec.PodSelector.MatchLabels)
	require.Len(t, pol.Spec.Egress, 1)
	require.Equal(t, int32(443), pol.Spec.Egress[0].Ports[0].Port.IntVal)
	require.Equal(t, "0.0.0.0/0", pol.Spec.Egress[0].To[0].IPBlock.CIDR)
	require.Contains(t, pol.Spec.Egress[0].To[0].IPBlock.Except, "10.0.0.0/8")

	// The stanza: per-cluster prefix, the replicated credential, retention.
	live, err := dyn.Resource(cnpgGVR).Namespace(bkNS).Get(ctx, bkCluster, metav1.GetOptions{})
	require.NoError(t, err)
	dest, _, _ := unstructured.NestedString(live.Object, "spec", "backup", "barmanObjectStore", "destinationPath")
	require.Equal(t, "s3://addon-backups-fixture/"+bkNS+"/"+bkCluster, dest)
	cred, _, _ := unstructured.NestedString(live.Object, "spec", "backup", "barmanObjectStore", "s3Credentials", "secretAccessKey", "name")
	require.Equal(t, BackupCredentialsSecretName, cred)
	instances, _, _ := unstructured.NestedInt64(live.Object, "spec", "instances")
	require.Equal(t, int64(1), instances, "a merge patch of spec.backup leaves the rest of the spec alone")

	sb, err := dyn.Resource(cnpgScheduledBackupGVR).Namespace(bkNS).Get(ctx, bkCluster+"-daily", metav1.GetOptions{})
	require.NoError(t, err)
	immediate, _, _ := unstructured.NestedBool(sb.Object, "spec", "immediate")
	require.True(t, immediate, "the first restore point exists minutes after wiring, not tomorrow")

	// Order: nothing that turns WAL archiving on happens before the credential
	// and the egress rule exist.
	require.Equal(t, []string{"create secrets", "create networkpolicies"}, writes(kube.Actions()))
	require.Equal(t, []string{"patch clusters", "patch clusters", "create scheduledbackups"}, writes(dyn.Actions()))

	// Idempotent: a converged cluster writes nothing more.
	kube.ClearActions()
	dyn.ClearActions()
	out, err = p.convergeBackups(ctx, bkAddon())
	require.NoError(t, err)
	require.Equal(t, BackupConvergence{}, out)
	require.Empty(t, writes(dyn.Actions()))
	require.Equal(t, []string{"update networkpolicies"}, writes(kube.Actions()), "only the policy's converge-on-update, as for every other CNPG policy")
}

// CNPG reloads the barman credential only when its Cluster changes, so a
// replaced copy alone leaves running instances on the old one (10-31: the
// 10-character placeholder outlived the real credential). A rotation must
// therefore reach the Cluster as a new version annotation.
func TestARotatedCredentialReachesTheRunningInstancesThroughTheCluster(t *testing.T) {
	bkStore(t)
	ctx := context.Background()
	p, dyn, kube := bkProvisioner(t, bkClusterObj(nil), bkCredential(backupCredentialKeys...))
	_, err := p.convergeBackups(ctx, bkAddon())
	require.NoError(t, err)
	live, err := dyn.Resource(cnpgGVR).Namespace(bkNS).Get(ctx, bkCluster, metav1.GetOptions{})
	require.NoError(t, err)
	first := live.GetAnnotations()[backupCredentialAnnotation]
	require.Len(t, first, 16)
	require.NotContains(t, first, "fixture", "the annotation names a version, never a value")

	rotated := bkCredential(backupCredentialKeys...)
	rotated.Data["SECRET_ACCESS_KEY"] = []byte("fixture-rotated")
	_, err = kube.CoreV1().Secrets(BackupCredentialsSourceNS).Update(ctx, rotated, metav1.UpdateOptions{})
	require.NoError(t, err)
	kube.ClearActions()
	dyn.ClearActions()

	out, err := p.convergeBackups(ctx, bkAddon())
	require.NoError(t, err)
	require.Equal(t, BackupConvergence{CredentialReloaded: true}, out)
	require.Equal(t, []string{"delete secrets", "create secrets", "update networkpolicies"}, writes(kube.Actions()), "the copy is replaced first")
	require.Equal(t, []string{"patch clusters"}, writes(dyn.Actions()), "then the Cluster, which is what wakes the instance manager")
	copied, err := kube.CoreV1().Secrets(bkNS).Get(ctx, BackupCredentialsSecretName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "fixture-rotated", string(copied.Data["SECRET_ACCESS_KEY"]))
	live, err = dyn.Resource(cnpgGVR).Namespace(bkNS).Get(ctx, bkCluster, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotEqual(t, first, live.GetAnnotations()[backupCredentialAnnotation])
	backup, _, _ := unstructured.NestedMap(live.Object, "spec", "backup")
	require.True(t, backupStanzaMatches(backup, backupConfigFromEnv().backupStanza(bkNS, bkCluster)), "the annotation patch leaves the stanza alone")
}

func TestConvergeBackupsRefusesToHalfWireWithoutTheCredential(t *testing.T) {
	bkStore(t)
	for name, objects := range map[string][]runtime.Object{
		"absent":     nil,
		"incomplete": {bkCredential("ACCESS_KEY_ID")},
	} {
		t.Run(name, func(t *testing.T) {
			p, dyn, kube := bkProvisioner(t, bkClusterObj(nil), objects...)
			_, err := p.convergeBackups(context.Background(), bkAddon())
			require.ErrorIs(t, err, ErrBackupCredentialsMissing)
			require.Empty(t, writes(dyn.Actions()), "no stanza: WAL archiving without a credential fills the volume")
			require.Empty(t, writes(kube.Actions()))
		})
	}
}

func TestConvergeBackupsWithoutAStoreWritesNothing(t *testing.T) {
	t.Setenv("ENCLII_ADDON_BACKUP_DESTINATION_BASE", "")
	p, dyn, kube := bkProvisioner(t, bkClusterObj(nil), bkCredential(backupCredentialKeys...))
	_, err := p.convergeBackups(context.Background(), bkAddon())
	require.ErrorIs(t, err, ErrBackupStoreNotConfigured)
	require.Empty(t, dyn.Actions())
	require.Empty(t, writes(kube.Actions()))
}

func TestConvergeBackupsSkipsHibernatedForeignAndMissingClusters(t *testing.T) {
	bkStore(t)
	for name, cluster := range map[string]*unstructured.Unstructured{
		"hibernated": bkClusterObj(func(u *unstructured.Unstructured) {
			u.SetAnnotations(map[string]string{CNPGHibernationAnnotation: "on"})
		}),
		"not managed by enclii": bkClusterObj(func(u *unstructured.Unstructured) { u.SetLabels(nil) }),
		"another addon's cluster": bkClusterObj(func(u *unstructured.Unstructured) {
			u.SetLabels(map[string]string{LabelManagedBy: LabelManagedValue, LabelAddonID: uuid.NewString()})
		}),
		"cluster not created yet": nil,
	} {
		t.Run(name, func(t *testing.T) {
			p, dyn, kube := bkProvisioner(t, cluster, bkCredential(backupCredentialKeys...))
			_, err := p.convergeBackups(context.Background(), bkAddon())
			require.ErrorIs(t, err, ErrBackupConvergenceNotApplicable)
			require.Empty(t, writes(dyn.Actions()))
			require.Empty(t, writes(kube.Actions()))
		})
	}
}

func TestConvergeBackupsReportsAForeignStanzaAndNeverOverwritesIt(t *testing.T) {
	bkStore(t)
	cluster := bkClusterObj(func(u *unstructured.Unstructured) {
		require.NoError(t, unstructured.SetNestedField(u.Object, map[string]interface{}{
			"retentionPolicy":   "7d",
			"barmanObjectStore": map[string]interface{}{"destinationPath": "s3://somewhere-else/x"},
		}, "spec", "backup"))
	})
	p, dyn, kube := bkProvisioner(t, cluster, bkCredential(backupCredentialKeys...))
	_, err := p.convergeBackups(context.Background(), bkAddon())
	require.ErrorIs(t, err, ErrBackupStanzaDrift)
	require.Empty(t, writes(dyn.Actions()))
	require.Empty(t, writes(kube.Actions()))
}

func TestConvergeBackupsAcceptsItsOwnStanzaWithCNPGDefaults(t *testing.T) {
	bkStore(t)
	stanza := backupConfigFromEnv().backupStanza(bkNS, bkCluster)
	stanza["target"] = "prefer-standby" // defaulted by the CNPG webhook
	cluster := bkClusterObj(func(u *unstructured.Unstructured) {
		require.NoError(t, unstructured.SetNestedField(u.Object, runtime.DeepCopyJSON(stanza), "spec", "backup"))
	})
	p, dyn, _ := bkProvisioner(t, cluster, bkCredential(backupCredentialKeys...))
	out, err := p.convergeBackups(context.Background(), bkAddon())
	require.NoError(t, err)
	require.False(t, out.StanzaPatched)
	require.True(t, out.ScheduledBackupCreated, "a stanza without its ScheduledBackup still gets one")
	require.True(t, out.CredentialReloaded, "a cluster wired before the version annotation existed is told to reload once")
	require.Equal(t, []string{"patch clusters", "create scheduledbackups"}, writes(dyn.Actions()))
}

func TestConvergeAddonBackupsSkipsWhatIsNotALiveCluster(t *testing.T) {
	bkStore(t)
	p, dyn, _ := bkProvisioner(t, bkClusterObj(nil), bkCredential(backupCredentialKeys...))
	s := &AddonService{logger: testLogger(), provisioners: map[types.DatabaseAddonType]AddonProvisioner{types.DatabaseAddonTypePostgres: p}}
	for name, addon := range map[string]*types.DatabaseAddon{
		"nil":       nil,
		"redis":     {Type: types.DatabaseAddonTypeRedis, Status: types.DatabaseAddonStatusReady, K8sNamespace: bkNS, K8sResourceName: bkCluster},
		"deleting":  {ID: bkAddonID, Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusDeleting, K8sNamespace: bkNS, K8sResourceName: bkCluster},
		"retention": {ID: bkAddonID, Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusPendingDeletion, K8sNamespace: bkNS, K8sResourceName: bkCluster},
	} {
		_, err := s.ConvergeAddonBackups(context.Background(), addon)
		require.ErrorIs(t, err, ErrBackupConvergenceNotApplicable, name)
	}
	require.Empty(t, writes(dyn.Actions()))
	out, err := s.ConvergeAddonBackups(context.Background(), bkAddon())
	require.NoError(t, err)
	require.True(t, out.StanzaPatched)
}

func TestPrepareProvisionBackupsStripsTheStanzaWhileTheCredentialIsMissing(t *testing.T) {
	bkStore(t)
	logger := logrus.NewEntry(testLogger())
	req := provisionReq("")

	p, _, _ := bkProvisioner(t, nil)
	cluster := p.buildClusterManifest(req, bkCluster)
	require.Contains(t, cluster["spec"], "backup")
	require.False(t, p.prepareProvisionBackups(context.Background(), cluster, logger))
	require.NotContains(t, cluster["spec"], "backup", "a new database must not start with WAL archiving it cannot authenticate")

	ready, _, _ := bkProvisioner(t, nil, bkCredential(backupCredentialKeys...))
	cluster = ready.buildClusterManifest(req, bkCluster)
	require.True(t, ready.prepareProvisionBackups(context.Background(), cluster, logger))
	require.Contains(t, cluster["spec"], "backup")
}

func bkSchedule(annotations map[string]string, suspended bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{"spec": map[string]interface{}{
		"schedule": BackupSchedule, "immediate": true, "suspend": suspended,
		"cluster": map[string]interface{}{"name": bkCluster},
	}}}
	u.SetAPIVersion(CloudNativePGAPIVersion)
	u.SetKind("ScheduledBackup")
	u.SetNamespace(bkNS)
	u.SetName(bkCluster + "-daily")
	u.SetAnnotations(annotations)
	return u
}

func bkProvisionerWith(t *testing.T, dynObjects []runtime.Object, kubeObjects ...runtime.Object) (*PostgresProvisioner, *dynamicfake.FakeDynamicClient, *kubefake.Clientset) {
	t.Helper()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		cnpgGVR:                "ClusterList",
		cnpgScheduledBackupGVR: "ScheduledBackupList",
		cnpgBackupGVR:          "BackupList",
	}, dynObjects...)
	kube := kubefake.NewSimpleClientset(kubeObjects...)
	return &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: kube}, dynamicClient: dyn, logger: testLogger()}, dyn, kube
}

func TestHibernationSuspendsTheScheduleAndWakingResumesIt(t *testing.T) {
	bkStore(t)
	ctx := context.Background()
	hibernated := bkClusterObj(func(u *unstructured.Unstructured) {
		u.SetAnnotations(map[string]string{CNPGHibernationAnnotation: "on"})
	})
	p, dyn, kube := bkProvisionerWith(t, []runtime.Object{hibernated, bkSchedule(nil, false)}, bkCredential(backupCredentialKeys...))

	out, err := p.convergeBackups(ctx, bkAddon())
	require.ErrorIs(t, err, ErrBackupConvergenceNotApplicable, "a hibernated cluster is never wired")
	require.True(t, out.ScheduleSuspended)
	require.Empty(t, writes(kube.Actions()), "no credential copy, no egress rule for a stopped cluster")
	sb, err := dyn.Resource(cnpgScheduledBackupGVR).Namespace(bkNS).Get(ctx, bkCluster+"-daily", metav1.GetOptions{})
	require.NoError(t, err)
	suspended, _, _ := unstructured.NestedBool(sb.Object, "spec", "suspend")
	require.True(t, suspended, "no failed Backup every night for a cluster with no instance")
	require.Equal(t, "true", sb.GetAnnotations()[suspendedForHibernation])

	dyn.ClearActions()
	out, err = p.convergeBackups(ctx, bkAddon())
	require.ErrorIs(t, err, ErrBackupConvergenceNotApplicable)
	require.False(t, out.ScheduleSuspended)
	require.Empty(t, writes(dyn.Actions()), "already suspended: no second write")

	// The cluster wakes.
	awake, err := dyn.Resource(cnpgGVR).Namespace(bkNS).Get(ctx, bkCluster, metav1.GetOptions{})
	require.NoError(t, err)
	awake.SetAnnotations(nil)
	_, err = dyn.Resource(cnpgGVR).Namespace(bkNS).Update(ctx, awake, metav1.UpdateOptions{})
	require.NoError(t, err)
	out, err = p.convergeBackups(ctx, bkAddon())
	require.NoError(t, err)
	require.True(t, out.ScheduleResumed)
	require.True(t, out.StanzaPatched, "a cluster that slept through the wiring is wired on waking")
	sb, err = dyn.Resource(cnpgScheduledBackupGVR).Namespace(bkNS).Get(ctx, bkCluster+"-daily", metav1.GetOptions{})
	require.NoError(t, err)
	suspended, _, _ = unstructured.NestedBool(sb.Object, "spec", "suspend")
	require.False(t, suspended)
	require.NotContains(t, sb.GetAnnotations(), suspendedForHibernation)
}

func TestAnOperatorSuspendedScheduleIsNeverResumed(t *testing.T) {
	bkStore(t)
	p, dyn, _ := bkProvisionerWith(t, []runtime.Object{bkClusterObj(nil), bkSchedule(nil, true)}, bkCredential(backupCredentialKeys...))
	out, err := p.convergeBackups(context.Background(), bkAddon())
	require.NoError(t, err)
	require.False(t, out.ScheduleResumed)
	sb, err := dyn.Resource(cnpgScheduledBackupGVR).Namespace(bkNS).Get(context.Background(), bkCluster+"-daily", metav1.GetOptions{})
	require.NoError(t, err)
	suspended, _, _ := unstructured.NestedBool(sb.Object, "spec", "suspend")
	require.True(t, suspended, "a schedule a human suspended stays suspended")
}

// A new addon gets backups FROM CREATION when the credential exists: the
// manifest Provision creates carries spec.backup (prepareProvisionBackups) and,
// once the Cluster exists, wireNewClusterBackups lays down the credential copy
// owned by it, the egress rule and the schedule. (Provision as a whole needs a
// concrete clientset for EnsureNamespace, so the two halves are tested here.)
func TestNewClusterIsBackedUpFromCreationWhenTheCredentialExists(t *testing.T) {
	bkStore(t)
	ctx := context.Background()
	logger := logrus.NewEntry(testLogger())
	p, dyn, kube := bkProvisionerWith(t, nil, bkCredential(backupCredentialKeys...))
	req := &ProvisionRequest{Addon: &types.DatabaseAddon{ID: bkAddonID, Name: "kernel09", Type: types.DatabaseAddonTypePostgres}, Namespace: bkNS, ProjectID: uuid.New()}

	manifest := p.buildClusterManifest(req, bkCluster)
	require.True(t, p.prepareProvisionBackups(ctx, manifest, logger))
	dest, _, _ := unstructured.NestedString(manifest, "spec", "backup", "barmanObjectStore", "destinationPath")
	require.Equal(t, "s3://addon-backups-fixture/"+bkNS+"/"+bkCluster, dest, "the very first WAL segment is archived")

	p.wireNewClusterBackups(ctx, req, bkCluster, k8s.SecretCopyOwner{APIVersion: CloudNativePGAPIVersion, Kind: CloudNativePGKind, Name: bkCluster, UID: "uid-new"}, logger)
	sb, err := dyn.Resource(cnpgScheduledBackupGVR).Namespace(bkNS).Get(ctx, bkCluster+"-daily", metav1.GetOptions{})
	require.NoError(t, err)
	immediate, _, _ := unstructured.NestedBool(sb.Object, "spec", "immediate")
	require.True(t, immediate)
	_, err = kube.NetworkingV1().NetworkPolicies(bkNS).Get(ctx, bkCluster+"-backup-egress", metav1.GetOptions{})
	require.NoError(t, err)
	copied, err := kube.CoreV1().Secrets(bkNS).Get(ctx, BackupCredentialsSecretName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "uid-new", string(copied.OwnerReferences[0].UID))
}

func TestBackupStoreBucketIsTheS3URLsFirstSegment(t *testing.T) {
	for base, want := range map[string]string{
		"s3://enclii-addon-db-backups":         "enclii-addon-db-backups",
		"s3://enclii-addon-db-backups/":        "enclii-addon-db-backups",
		"s3://enclii-addon-db-backups/tenants": "enclii-addon-db-backups",
	} {
		got, err := BackupStore{DestinationBase: base}.Bucket()
		require.NoError(t, err, base)
		require.Equal(t, want, got, base)
	}
	_, err := BackupStore{}.Bucket()
	require.ErrorIs(t, err, ErrBackupStoreNotConfigured)
	for _, bad := range []string{"https://bucket", "s3://", "s3:///x"} {
		_, err := BackupStore{DestinationBase: bad}.Bucket()
		require.Error(t, err, bad)
	}
}

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
