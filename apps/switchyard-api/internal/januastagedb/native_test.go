package januastagedb

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

type memoryJournal struct {
	claimed                  bool
	refuseCheck, refuseClaim bool
	phases                   []string
	receipt                  NativeReceipt
}

func (j *memoryJournal) Check(context.Context, Plan) error {
	if j.refuseCheck {
		return ErrRefused
	}
	return nil
}
func (j *memoryJournal) Claim(context.Context, Plan) error {
	if j.claimed || j.refuseClaim {
		return ErrRefused
	}
	j.claimed = true
	return nil
}
func (j *memoryJournal) Record(_ context.Context, _ Plan, phase, uid string) error {
	if !j.claimed || uid == "" {
		return ErrRefused
	}
	switch phase {
	case "RUNTIME_SECRET_CREATED":
		j.receipt.RuntimeSecretUID = ktypes.UID(uid)
	case "CLUSTER_CREATED":
		j.receipt.ClusterUID = ktypes.UID(uid)
	}
	j.phases = append(j.phases, phase)
	return nil
}

func (j *memoryJournal) Receipt(_ context.Context, _ Plan) (NativeReceipt, error) {
	if len(j.phases) != 3 {
		return NativeReceipt{}, ErrRefused
	}
	return j.receipt, nil
}

func nativeFixture(t *testing.T) (Plan, *Kernel, *kubefake.Clientset, *dynamicfake.FakeDynamicClient, *memoryJournal) {
	t.Helper()
	p := fixturePlan(t)
	retain := corev1.PersistentVolumeReclaimRetain
	kube := kubefake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: Namespace(p.Authority.ProjectID), UID: ktypes.UID(p.AddonNamespaceUID), ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: p.Authority.ProjectID.String(), "enclii.dev/verify-signatures": "true"}}}, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "longhorn-replicated", UID: ktypes.UID(p.StorageClassUID), ResourceVersion: p.StorageClassVersion}, ReclaimPolicy: &retain})
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	journal := &memoryJournal{}
	kube.PrependReactor("create", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
		require.True(t, journal.claimed, "journal must commit before custody write")
		obj := action.(ktesting.CreateAction).GetObject().(*corev1.Secret)
		obj.UID = "runtime-secret-fixture"
		obj.ResourceVersion = "1"
		return false, nil, nil
	})
	dynamic.PrependReactor("create", "clusters", func(action ktesting.Action) (bool, runtime.Object, error) {
		a := action.(ktesting.CreateActionImpl)
		obj := a.GetObject().(*unstructured.Unstructured)
		require.Equal(t, metav1.FieldValidationStrict, a.GetCreateOptions().FieldValidation)
		if len(a.GetCreateOptions().DryRun) > 0 {
			require.Equal(t, []string{metav1.DryRunAll}, a.GetCreateOptions().DryRun)
			return true, obj.DeepCopy(), nil
		}
		require.True(t, journal.claimed, "journal must commit before Cluster write")
		obj.SetUID("cluster-fixture")
		obj.SetResourceVersion("1")
		return false, nil, nil
	})
	kernel := NewKernel(kube, dynamic, freshObserver(p), journal, func(context.Context, Plan) error { return nil }, func(context.Context, Plan) error { return nil }, func(_ context.Context, image string) error {
		require.Equal(t, types.JanuaStagingPostgresImage, image)
		return nil
	})
	return p, kernel, kube, dynamic, journal
}

func TestDryAdmissionHasNoCredentialOrPersistentWrite(t *testing.T) {
	p, k, kube, dynamic, j := nativeFixture(t)
	require.NoError(t, k.DryAdmit(context.Background(), p))
	require.False(t, j.claimed)
	for _, action := range kube.Actions() {
		require.NotEqual(t, "secrets", action.GetResource().Resource)
	}
	for _, action := range dynamic.Actions() {
		if action.GetVerb() == "create" {
			require.NotEmpty(t, action.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
		}
	}
}

func TestApplyClaimsBeforeWritesAndRetainsUnqualifiedOutcome(t *testing.T) {
	p, k, kube, dynamic, j := nativeFixture(t)
	require.NoError(t, k.Apply(context.Background(), p))
	require.Equal(t, []string{"RUNTIME_SECRET_CREATED", "CLUSTER_CREATED", "CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED"}, j.phases)
	secret, err := kube.CoreV1().Secrets(Namespace(p.Authority.ProjectID)).Get(context.Background(), RuntimeSecret, metav1.GetOptions{})
	require.NoError(t, err)
	require.Len(t, secret.OwnerReferences, 1)
	require.EqualValues(t, "cluster-fixture", secret.OwnerReferences[0].UID)
	require.Equal(t, RuntimeRole, string(secret.Data["username"]))
	require.Len(t, secret.Data["password"], 64)
	require.ErrorIs(t, k.Apply(context.Background(), p), ErrRefused, "a replay must not adopt or rotate existing native objects")
	for _, action := range append(kube.Actions(), dynamic.Actions()...) {
		require.NotEqual(t, "delete", action.GetVerb())
	}
}

func TestUnqualifiedAuthorityCustodyNetworkSignatureOrJournalNeverWrites(t *testing.T) {
	for _, name := range []string{"observer", "journal", "network", "signature", "namespace", "storage", "revoked-operator", "capacity-budget"} {
		t.Run(name, func(t *testing.T) {
			p, k, kube, dynamic, j := nativeFixture(t)
			switch name {
			case "observer":
				k.observe = nil
			case "journal":
				j.refuseClaim = true
			case "network":
				k.network = func(context.Context, Plan) error { return errors.New("synthetic-private-error") }
			case "signature":
				k.verify = func(context.Context, string) error { return ErrRefused }
			case "namespace":
				p.AddonNamespaceUID = "foreign"
			case "storage":
				p.StorageClassUID = "foreign"
			case "capacity-budget":
				k.capacity = nil
			case "revoked-operator":
				j.refuseCheck = true
			}
			require.ErrorIs(t, k.Apply(context.Background(), p), ErrRefused)
			for _, action := range kube.Actions() {
				require.NotEqual(t, "secrets", action.GetResource().Resource)
			}
			for _, action := range dynamic.Actions() {
				if action.GetVerb() == "create" {
					require.NotEmpty(t, action.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
				}
			}
		})
	}
}

func TestDriftAfterRuntimeCreationPreventsClusterAndDoesNotCleanUpBlindly(t *testing.T) {
	p, k, kube, dynamic, j := nativeFixture(t)
	observer := freshObserver(p)
	k.observe = func(ctx context.Context, id uuid.UUID) (januastage.Authority, error) {
		a, err := observer(ctx, id)
		if len(j.phases) > 0 {
			a.DeploymentUID = "replaced-deployment"
		}
		return a, err
	}
	require.ErrorIs(t, k.Apply(context.Background(), p), ErrRefused)
	require.Equal(t, []string{"RUNTIME_SECRET_CREATED"}, j.phases)
	secret, err := kube.CoreV1().Secrets(Namespace(p.Authority.ProjectID)).Get(context.Background(), RuntimeSecret, metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, secret.OwnerReferences)
	for _, action := range dynamic.Actions() {
		if action.GetVerb() == "create" {
			require.NotEmpty(t, action.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
		}
	}
	for _, action := range kube.Actions() {
		require.NotEqual(t, "delete", action.GetVerb())
	}
}

func TestOwnershipAssociationCannotMutateRetainedCustody(t *testing.T) {
	for _, field := range []string{"password", "type", "label", "annotation"} {
		t.Run(field, func(t *testing.T) {
			p, k, kube, _, j := nativeFixture(t)
			kube.PrependReactor("update", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
				current := action.(ktesting.UpdateAction).GetObject().(*corev1.Secret).DeepCopy()
				switch field {
				case "password":
					current.Data["password"] = []byte("changed-by-synthetic-admission")
				case "type":
					current.Type = corev1.SecretTypeOpaque
				case "label":
					current.Labels["enclii.dev/project-id"] = "foreign"
				case "annotation":
					current.Annotations["enclii.dev/staging-plan-sha256"] = "foreign"
				}
				return true, current, nil
			})
			require.ErrorIs(t, k.Apply(context.Background(), p), ErrRefused)
			require.Equal(t, []string{"RUNTIME_SECRET_CREATED", "CLUSTER_CREATED"}, j.phases)
		})
	}
}
