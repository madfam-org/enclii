package januastagemigrate

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktypes "k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

type memoryJournal struct {
	claimed, revoked bool
	phases           []string
	native           NativeReceipt
	outcome          *Outcome
}

func (j *memoryJournal) Check(context.Context, Plan) error {
	if j.revoked {
		return ErrRefused
	}
	return nil
}
func (j *memoryJournal) CheckCleanup(ctx context.Context, p Plan) error { return j.Check(ctx, p) }
func (j *memoryJournal) Claim(context.Context, Plan) error {
	if j.claimed {
		return ErrRefused
	}
	j.claimed = true
	return nil
}
func (j *memoryJournal) Record(_ context.Context, _ Plan, phase string, uid ktypes.UID, sha string) error {
	if !j.claimed {
		return ErrRefused
	}
	j.phases = append(j.phases, phase)
	switch phase {
	case "SUSPENDED_JOB_CREATED":
		j.native.JobUID = uid
	case "OWNER_SECRET_CREATED":
		j.native.OwnerSecretUID = uid
		j.native.OwnerSecretSHA256 = sha
	case "JOB_STARTED":
		j.native.Started = true
	case "OWNER_SECRET_REMOVED":
		j.native.OwnerRemoved = true
	}
	return nil
}
func (j *memoryJournal) Receipt(context.Context, Plan) (NativeReceipt, error) {
	if j.native.JobUID == "" || j.native.OwnerSecretUID == "" {
		return NativeReceipt{}, ErrRefused
	}
	return j.native, nil
}
func (j *memoryJournal) ReadOutcome(context.Context, Plan) (*Outcome, error) { return j.outcome, nil }
func (j *memoryJournal) Outcome(_ context.Context, p Plan, o Outcome) error {
	if o.Validate(p) != nil {
		return ErrRefused
	}
	if j.outcome != nil && !reflect.DeepEqual(*j.outcome, o) {
		return ErrRefused
	}
	j.outcome = &o
	return nil
}

type databaseFixture struct {
	p          Plan
	kube       *kubefake.Clientset
	journal    *memoryJournal
	ownerReads int
}

func (d *databaseFixture) ObserveReadiness(context.Context, januastagedb.Plan) (januastagedb.NativeReceipt, error) {
	return januastagedb.NativeReceipt{ClusterUID: d.p.ClusterUID, RuntimeSecretUID: d.p.RuntimeSecretUID}, nil
}
func (d *databaseFixture) OwnerCustody(ctx context.Context, _ januastagedb.Plan) (*januastagedb.OwnerBundle, error) {
	d.ownerReads++
	if !d.journal.claimed {
		return nil, ErrRefused
	}
	job, e := d.kube.BatchV1().Jobs(d.p.DatabaseNamespace()).Get(ctx, d.p.JobName(), metav1.GetOptions{})
	if e != nil || job.Spec.Suspend == nil || !*job.Spec.Suspend {
		return nil, ErrRefused
	}
	connection := url.URL{Scheme: "postgresql", User: url.UserPassword(januastagedb.OwnerRole, strings.Repeat("a", 64)), Host: januastagedb.Host(d.p.Database.Authority.ProjectID) + ":5432", Path: "/janua_staging", RawQuery: "sslmode=verify-full"}
	return &januastagedb.OwnerBundle{DatabaseURL: connection.String(), PublicCA: []byte("synthetic-public-ca"), ClusterUID: d.p.ClusterUID}, nil
}

func planFixture(t *testing.T) Plan {
	t.Helper()
	project, intent := uuid.New(), uuid.New()
	a := januastage.Authority{ID: uuid.New(), ProjectID: project, EnvironmentID: uuid.New(), RepositoryRegistrationID: uuid.New(), HeldIntentID: intent, HeldServiceID: januastage.HeldServiceID(project, intent), Profile: januastage.Profile, NamespaceUID: "app-namespace", DeploymentUID: "api", APIServiceUID: "api-service", RedisDeploymentUID: "redis", RedisServiceUID: "redis-service", ApplicationUID: "argo", AppProjectUID: "argo-project", SourceRevision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64), PlanSHA256: strings.Repeat("c", 64), ActivationHeld: true, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	db, e := januastagedb.PlanFor(a, januastagedb.Allocation{CatalogPlan: "standard-0", CPU: "100m", Memory: "256Mi", StorageGi: 1, Replicas: 1, MaxConnections: 10}, januastagedb.NativeBinding{AddonNamespaceUID: "addon-ns", StorageClassUID: "storage", StorageClassVersion: "1", NetworkSHA256: strings.Repeat("d", 64), CapacityBudgetSHA256: strings.Repeat("e", 64)}, uuid.New(), uuid.New(), "synthetic database intent")
	require.NoError(t, e)
	source := SourceObservation{AuthorityID: a.ID, ManifestSourceRevision: a.SourceRevision, ManifestSourceSHA256: a.SourceSHA256, Descriptor: descriptorFixture(t), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	p, e := BuildPlan(db, uuid.New(), source, januastagedb.NativeReceipt{ClusterUID: "cluster", RuntimeSecretUID: "runtime-secret"}, "registry-secret", "1", strings.Repeat("f", 64), "synthetic migration intent")
	require.NoError(t, e)
	return p
}
func kernelFixture(t *testing.T) (Plan, *Kernel, *kubefake.Clientset, *memoryJournal, *databaseFixture) {
	t.Helper()
	p := planFixture(t)
	kube := kubefake.NewSimpleClientset()
	j := &memoryJournal{}
	db := &databaseFixture{p: p, kube: kube, journal: j}
	source := func(context.Context, uuid.UUID) (SourceObservation, error) {
		s := p.Source
		s.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return s, nil
	}
	k := NewKernel(kube, db, source, func(context.Context, Plan) error { return nil }, func(context.Context, Plan) error { return nil }, j)
	kube.PrependReactor("create", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.CreateActionImpl)
		job := action.GetObject().(*batchv1.Job)
		require.Equal(t, metav1.FieldValidationStrict, action.GetCreateOptions().FieldValidation)
		if len(action.GetCreateOptions().DryRun) > 0 {
			return true, job.DeepCopy(), nil
		}
		require.True(t, j.claimed)
		job.UID = "job"
		job.ResourceVersion = "1"
		return false, nil, nil
	})
	kube.PrependReactor("create", "secrets", func(a ktesting.Action) (bool, runtime.Object, error) {
		require.True(t, j.claimed)
		require.Equal(t, []string{"SUSPENDED_JOB_CREATED"}, j.phases)
		secret := a.(ktesting.CreateAction).GetObject().(*corev1.Secret)
		require.EqualValues(t, "job", secret.OwnerReferences[0].UID)
		secret.UID = "owner-secret"
		secret.ResourceVersion = "1"
		return false, nil, nil
	})
	kube.PrependReactor("update", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
		desired := a.(ktesting.UpdateAction).GetObject().(*batchv1.Job)
		stored, e := kube.Tracker().Get(batchv1.SchemeGroupVersion.WithResource("jobs"), desired.Namespace, desired.Name)
		require.NoError(t, e)
		prior := stored.(*batchv1.Job)
		if desired.ResourceVersion != prior.ResourceVersion {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "batch", Resource: "jobs"}, desired.Name, nil)
		}
		desired.ResourceVersion = prior.ResourceVersion + "1"
		return false, nil, nil
	})
	kube.PrependReactor("delete", "secrets", func(a ktesting.Action) (bool, runtime.Object, error) {
		options := a.(ktesting.DeleteActionImpl).GetDeleteOptions()
		require.NotNil(t, options.Preconditions)
		require.EqualValues(t, "owner-secret", *options.Preconditions.UID)
		require.Equal(t, "1", *options.Preconditions.ResourceVersion)
		return false, nil, nil
	})
	return p, k, kube, j, db
}

func TestMigrationDryAdmitHasNoOwnerCredentialReadOrPersistentWrite(t *testing.T) {
	p, k, kube, j, db := kernelFixture(t)
	require.NoError(t, k.DryAdmit(context.Background(), p))
	require.Zero(t, db.ownerReads)
	require.False(t, j.claimed)
	for _, a := range kube.Actions() {
		require.NotEqual(t, "secrets", a.GetResource().Resource)
		if a.GetVerb() == "create" {
			require.Equal(t, []string{metav1.DryRunAll}, a.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
		}
	}
}
func TestStartClaimsThenCreatesSuspendedJobBeforeOwnerCustody(t *testing.T) {
	p, k, kube, j, db := kernelFixture(t)
	require.NoError(t, k.Start(context.Background(), p))
	require.Equal(t, 1, db.ownerReads)
	require.Equal(t, []string{"SUSPENDED_JOB_CREATED", "OWNER_SECRET_CREATED", "JOB_STARTED"}, j.phases)
	job, e := kube.BatchV1().Jobs(p.DatabaseNamespace()).Get(context.Background(), p.JobName(), metav1.GetOptions{})
	require.NoError(t, e)
	require.False(t, *job.Spec.Suspend)
	secret, e := kube.CoreV1().Secrets(p.DatabaseNamespace()).Get(context.Background(), p.SecretName(), metav1.GetOptions{})
	require.NoError(t, e)
	require.True(t, *secret.Immutable)
	require.Equal(t, secretHash(secret), j.native.OwnerSecretSHA256)
	require.ErrorIs(t, k.Start(context.Background(), p), ErrRefused)
	require.Equal(t, 1, db.ownerReads)
}
func TestMissingSourceNetworkRegistryOperatorOrDatabaseNeverReadsOwner(t *testing.T) {
	for _, name := range []string{"source", "network", "registry", "operator", "database"} {
		t.Run(name, func(t *testing.T) {
			p, k, _, j, db := kernelFixture(t)
			switch name {
			case "source":
				k.source = nil
			case "network":
				k.network = nil
			case "registry":
				k.registry = nil
			case "operator":
				j.revoked = true
			case "database":
				k.database = nil
			}
			require.ErrorIs(t, k.Start(context.Background(), p), ErrRefused)
			require.Zero(t, db.ownerReads)
		})
	}
}
func TestAdmittedJobDefaultsAndPrivilegeMutations(t *testing.T) {
	p := planFixture(t)
	want, e := p.Job()
	require.NoError(t, e)
	valid := want.DeepCopy()
	valid.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
	valid.Spec.Template.Spec.SchedulerName = corev1.DefaultSchedulerName
	valid.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(30))
	valid.Spec.Template.Spec.Containers[0].TerminationMessagePath = corev1.TerminationMessagePathDefault
	valid.Spec.Template.Spec.Containers[0].TerminationMessagePolicy = corev1.TerminationMessageReadFile
	require.True(t, ClosedJob(want, valid, true))
	require.Equal(t, "v1", want.Spec.Template.Spec.Containers[0].Env[1].ValueFrom.FieldRef.APIVersion)
	raw, e := json.Marshal(valid)
	require.NoError(t, e)
	var roundTrip batchv1.Job
	require.NoError(t, json.Unmarshal(raw, &roundTrip))
	require.True(t, ClosedJob(want, &roundTrip, true), "typed API JSON round trip preserves admitted fixed intent")
	for _, mutate := range []func(*batchv1.Job){func(j *batchv1.Job) {
		j.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities.Add = []corev1.Capability{"SYS_ADMIN"}
	}, func(j *batchv1.Job) { j.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true) }, func(j *batchv1.Job) { j.Spec.Template.Spec.Containers[0].Env[1].ValueFrom.FieldRef.APIVersion = "v2" }, func(j *batchv1.Job) { j.Spec.Template.Spec.NodeName = "fixture-node" }, func(j *batchv1.Job) { j.Spec.TTLSecondsAfterFinished = ptr.To(int32(1)) }, func(j *batchv1.Job) { j.Annotations[abortMarker] = p.SHA256 }} {
		bad := valid.DeepCopy()
		mutate(bad)
		require.False(t, ClosedJob(want, bad, true))
	}
}

func TestKnownPreStartCredentialCanAbortWithoutCurrentSourceOrRegistry(t *testing.T) {
	p, k, kube, j, _ := kernelFixture(t)
	k.registry = func(context.Context, Plan) error {
		if len(j.phases) >= 2 {
			return ErrRefused
		}
		return nil
	}
	require.Error(t, k.Start(context.Background(), p))
	k.source = nil
	require.NoError(t, k.AbortBeforeStart(context.Background(), p))
	require.Equal(t, "ABORTED_BEFORE_START", j.outcome.State)
	require.True(t, j.native.OwnerRemoved)
	_, e := kube.CoreV1().Secrets(p.DatabaseNamespace()).Get(context.Background(), p.SecretName(), metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(e))
	require.ErrorIs(t, k.Start(context.Background(), p), ErrRefused)
}
func TestAbortCASWinsAgainstConcurrentUnsuspend(t *testing.T) {
	p, k, kube, j, _ := kernelFixture(t)
	triggered := false
	kube.PrependReactor("update", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
		job := a.(ktesting.UpdateAction).GetObject().(*batchv1.Job)
		if !*job.Spec.Suspend && !triggered {
			triggered = true
			// Simulate the competing API CAS without recursively acquiring the fake
			// client's reactor mutex. The real abort path is exercised immediately
			// after Start observes its stale-resourceVersion conflict.
			stored, e := kube.Tracker().Get(batchv1.SchemeGroupVersion.WithResource("jobs"), job.Namespace, job.Name)
			require.NoError(t, e)
			aborted := stored.(*batchv1.Job).DeepCopy()
			aborted.Annotations[abortMarker] = p.SHA256
			aborted.ResourceVersion = "2"
			require.NoError(t, kube.Tracker().Update(batchv1.SchemeGroupVersion.WithResource("jobs"), aborted, job.Namespace))
		}
		return false, nil, nil
	})
	require.Error(t, k.Start(context.Background(), p))
	require.True(t, triggered)
	require.NoError(t, k.AbortBeforeStart(context.Background(), p))
	require.False(t, j.native.Started)
	require.Equal(t, "ABORTED_BEFORE_START", j.outcome.State)
	current, e := kube.BatchV1().Jobs(p.DatabaseNamespace()).Get(context.Background(), p.JobName(), metav1.GetOptions{})
	require.NoError(t, e)
	require.True(t, *current.Spec.Suspend)
}
func TestAbortNeverDeletesStartedOrPendingOrTerminatingCustody(t *testing.T) {
	for _, state := range []string{"started", "pending", "terminating"} {
		t.Run(state, func(t *testing.T) {
			p, k, kube, j, _ := kernelFixture(t)
			if state != "started" {
				k.registry = func(context.Context, Plan) error {
					if len(j.phases) >= 2 {
						return ErrRefused
					}
					return nil
				}
			}
			_ = k.Start(context.Background(), p)
			if state != "started" {
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "synthetic-pending", Namespace: p.DatabaseNamespace(), Labels: p.labels()}, Status: corev1.PodStatus{Phase: corev1.PodPending}}
				if state == "terminating" {
					now := metav1.Now()
					pod.DeletionTimestamp = &now
				}
				_, e := kube.CoreV1().Pods(p.DatabaseNamespace()).Create(context.Background(), pod, metav1.CreateOptions{})
				require.NoError(t, e)
			}
			require.ErrorIs(t, k.AbortBeforeStart(context.Background(), p), ErrRefused)
			_, e := kube.CoreV1().Secrets(p.DatabaseNamespace()).Get(context.Background(), p.SecretName(), metav1.GetOptions{})
			require.NoError(t, e)
		})
	}
}

func finishFixture(t *testing.T, p Plan, kube *kubefake.Clientset) *corev1.Pod {
	t.Helper()
	ctx := context.Background()
	job, e := kube.BatchV1().Jobs(p.DatabaseNamespace()).Get(ctx, p.JobName(), metav1.GetOptions{})
	require.NoError(t, e)
	job.Status = batchv1.JobStatus{Succeeded: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}
	_, e = kube.BatchV1().Jobs(job.Namespace).UpdateStatus(ctx, job, metav1.UpdateOptions{})
	require.NoError(t, e)
	now := metav1.Now()
	start := metav1.NewTime(now.Add(-time.Second))
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "migration-pod", Namespace: job.Namespace, UID: "pod", ResourceVersion: "1", Labels: job.Spec.Template.Labels, Annotations: job.Spec.Template.Annotations, OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: ptr.To(true)}}}, Spec: *job.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodSucceeded, ContainerStatuses: []corev1.ContainerStatus{{Name: Container, ImageID: p.Source.Descriptor.Image, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, ContainerID: "container-fixture", StartedAt: start, FinishedAt: now}}}}}}
	_, e = kube.CoreV1().Pods(job.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(t, e)
	return pod
}
func receiptFixture(p Plan) *SchemaReceipt {
	return &SchemaReceipt{Profile: Profile, ImageSourceRevision: p.Source.Descriptor.ImageSourceRevision, GraphSHA256: p.Source.Descriptor.GraphSHA256, Head: p.Source.Descriptor.Head, Applied: p.Source.Descriptor.Revisions, Lineage: "FRESH_EMPTY", Database: "janua_staging", OwnerRole: "enclii_migrator", ClusterUID: p.ClusterUID, CatalogSHA256: strings.Repeat("a", 64)}
}
func TestExactTerminalSchemaReceiptThenCleanupRetainsHistoryAndRemovesOnlyOwnerSecret(t *testing.T) {
	p, k, kube, j, _ := kernelFixture(t)
	require.NoError(t, k.Start(context.Background(), p))
	finishFixture(t, p, kube)
	reads := 0
	logs := func(context.Context, string, string, ktypes.UID) ([]byte, error) {
		reads++
		return json.Marshal(receiptFixture(p))
	}
	o, e := k.Inspect(context.Background(), p, logs)
	require.NoError(t, e)
	require.Equal(t, "SUCCEEDED", o.State)
	k.source = nil
	k.registry = nil
	require.NoError(t, k.Cleanup(context.Background(), p, nil))
	require.Equal(t, 1, reads)
	require.True(t, j.native.OwnerRemoved)
	require.NoError(t, k.Cleanup(context.Background(), p, nil))
	_, e = kube.BatchV1().Jobs(p.DatabaseNamespace()).Get(context.Background(), p.JobName(), metav1.GetOptions{})
	require.NoError(t, e, "audit Job remains")
}
func TestTerminalEvidenceRejectsSecretPodImageAndSchemaDrift(t *testing.T) {
	for _, name := range []string{"secret", "terminating", "image", "graph", "principal", "duplicate-wire"} {
		t.Run(name, func(t *testing.T) {
			p, k, kube, _, _ := kernelFixture(t)
			require.NoError(t, k.Start(context.Background(), p))
			pod := finishFixture(t, p, kube)
			receipt := receiptFixture(p)
			raw, _ := json.Marshal(receipt)
			switch name {
			case "secret":
				secret, e := kube.CoreV1().Secrets(p.DatabaseNamespace()).Get(context.Background(), p.SecretName(), metav1.GetOptions{})
				require.NoError(t, e)
				secret.Data["owner.json"] = []byte("changed-after-start")
				_, e = kube.CoreV1().Secrets(secret.Namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
				require.NoError(t, e)
			case "terminating":
				now := metav1.Now()
				pod.DeletionTimestamp = &now
				_, e := kube.CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
				require.NoError(t, e)
			case "image":
				pod.Status.ContainerStatuses[0].ImageID = "containerd://sha256:" + strings.Repeat("f", 64)
				_, e := kube.CoreV1().Pods(pod.Namespace).UpdateStatus(context.Background(), pod, metav1.UpdateOptions{})
				require.NoError(t, e)
			case "graph":
				receipt.GraphSHA256 = strings.Repeat("f", 64)
				raw, _ = json.Marshal(receipt)
			case "principal":
				receipt.PrincipalRows = 1
				raw, _ = json.Marshal(receipt)
			case "duplicate-wire":
				raw = []byte(`{"profile":"wrong","profile":"` + Profile + `"}`)
			}
			_, e := k.Inspect(context.Background(), p, func(context.Context, string, string, ktypes.UID) ([]byte, error) { return raw, nil })
			require.ErrorIs(t, e, ErrRefused)
			require.Error(t, k.Cleanup(context.Background(), p, nil))
		})
	}
}

func TestUnlabelledOwnedPodOrForeignCredentialConsumerCannotHideFromAbort(t *testing.T) {
	for _, kind := range []string{"owned-without-label", "foreign-secret-consumer"} {
		t.Run(kind, func(t *testing.T) {
			p, k, kube, j, _ := kernelFixture(t)
			k.registry = func(context.Context, Plan) error {
				if len(j.phases) >= 2 {
					return ErrRefused
				}
				return nil
			}
			require.Error(t, k.Start(context.Background(), p))
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unlabelled-consumer", Namespace: p.DatabaseNamespace(), UID: "hidden-pod", ResourceVersion: "1"}, Status: corev1.PodStatus{Phase: corev1.PodPending}}
			if kind == "owned-without-label" {
				pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: p.JobName(), UID: j.native.JobUID, Controller: ptr.To(true)}}
			} else {
				pod.Spec.Volumes = []corev1.Volume{{Name: "stolen-custody", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: p.SecretName()}}}}
			}
			_, e := kube.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			require.NoError(t, e)
			require.ErrorIs(t, k.AbortBeforeStart(context.Background(), p), ErrRefused)
			_, e = kube.CoreV1().Secrets(p.DatabaseNamespace()).Get(context.Background(), p.SecretName(), metav1.GetOptions{})
			require.NoError(t, e)
		})
	}
}
