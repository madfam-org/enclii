package januastagenetwork

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func planFixture(t *testing.T) Plan {
	t.Helper()
	project, intent := uuid.New(), uuid.New()
	a := januastage.Authority{ID: uuid.New(), ProjectID: project, EnvironmentID: uuid.New(), RepositoryRegistrationID: uuid.New(), HeldIntentID: intent, HeldServiceID: januastage.HeldServiceID(project, intent), Profile: januastage.Profile, NamespaceUID: "app-namespace-fixture", DeploymentUID: "api-fixture", APIServiceUID: "api-service-fixture", RedisDeploymentUID: "redis-fixture", RedisServiceUID: "redis-service-fixture", ApplicationUID: "argocd-fixture", AppProjectUID: "argocd-project-fixture", SourceRevision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64), PlanSHA256: strings.Repeat("c", 64), ActivationHeld: true, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	p, e := BuildPlan(uuid.New(), uuid.New(), a, Inputs{Peers: []Peer{{HostRoute: "192.0.2.1/32", Port: 443}}, Operator: metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "cloudnative-pg"}}, ContextSHA256: strings.Repeat("d", 64)})
	require.NoError(t, e)
	require.NoError(t, p.Validate())
	return p
}

type memoryJournal struct {
	effect, mu sync.Mutex
	r          Receipt
	claimed    bool
	failRecord int
}

func (j *memoryJournal) Lock(context.Context, Plan) (func(), error) {
	if !j.effect.TryLock() {
		return nil, ErrRefused
	}
	return j.effect.Unlock, nil
}
func (j *memoryJournal) Claim(_ context.Context, p Plan, _ string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.claimed {
		return ErrUnknown
	}
	j.claimed = true
	j.r.Plan = p
	return nil
}
func (j *memoryJournal) Intent(_ context.Context, p Plan, i int, o Object) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.claimed || i != j.r.Attempted || len(j.r.Objects) != i {
		return ErrRefused
	}
	j.r.Attempted++
	return nil
}
func (j *memoryJournal) Record(_ context.Context, p Plan, r ObjectReceipt) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failRecord == r.Index+1 {
		return ErrUnknown
	}
	j.r.Objects = append(j.r.Objects, r)
	return nil
}
func (j *memoryJournal) Complete(context.Context, Plan) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.r.Objects) != 7 {
		return ErrRefused
	}
	j.r.Complete = true
	return nil
}
func (j *memoryJournal) Read(context.Context, uuid.UUID, uuid.UUID) (Receipt, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r := j.r
	r.Objects = append([]ObjectReceipt(nil), j.r.Objects...)
	return r, nil
}

type nativeFixture struct {
	p          Plan
	n          NativeAdapter
	kube       *kubefake.Clientset
	dynamic    *dynamicfake.FakeDynamicClient
	j          *memoryJournal
	creates    int
	mutate     func(runtime.Object)
	failCreate int
	freshCalls int
	failFresh  int
}

func newNativeFixture(t *testing.T) *nativeFixture {
	t.Helper()
	p := planFixture(t)
	kube := kubefake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: januastage.Namespace, UID: ktypes.UID(p.Authority.NamespaceUID), ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: p.Authority.ProjectID.String()}}}, &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: januastage.Namespace, UID: "app-baseline-fixture", ResourceVersion: "1"}})
	lists := map[schema.GroupVersionResource]string{}
	for _, v := range []struct{ g, v, r, k string }{{"", "v1", "pods", "PodList"}, {"", "v1", "persistentvolumeclaims", "PersistentVolumeClaimList"}, {"", "v1", "replicationcontrollers", "ReplicationControllerList"}, {"apps", "v1", "deployments", "DeploymentList"}, {"apps", "v1", "statefulsets", "StatefulSetList"}, {"apps", "v1", "daemonsets", "DaemonSetList"}, {"apps", "v1", "replicasets", "ReplicaSetList"}, {"batch", "v1", "jobs", "JobList"}, {"batch", "v1", "cronjobs", "CronJobList"}, {"autoscaling", "v2", "horizontalpodautoscalers", "HorizontalPodAutoscalerList"}, {"postgresql.cnpg.io", "v1", "clusters", "ClusterList"}} {
		lists[schema.GroupVersionResource{Group: v.g, Version: v.v, Resource: v.r}] = v.k
	}
	f := &nativeFixture{p: p, kube: kube, dynamic: dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), lists), j: &memoryJournal{}}
	f.n = NativeAdapter{Kube: kube, Dynamic: f.dynamic, Fresh: func(context.Context, Plan) error {
		f.freshCalls++
		if f.failFresh == f.freshCalls {
			return ErrRefused
		}
		return nil
	}}
	kube.PrependReactor("create", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		create := action.(ktesting.CreateAction)
		body := create.GetObject().DeepCopyObject()
		m, e := meta.Accessor(body)
		require.NoError(t, e)
		f.creates++
		m.SetUID(ktypes.UID(fmt.Sprintf("created-fixture-%d", f.creates)))
		m.SetResourceVersion("1")
		if ns, ok := body.(*corev1.Namespace); ok {
			ns.Labels["kubernetes.io/metadata.name"] = ns.Name
			ns.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes}
		}
		if f.mutate != nil {
			f.mutate(body)
		}
		require.NoError(t, kube.Tracker().Create(action.GetResource(), body, action.GetNamespace()))
		if f.failCreate == f.creates {
			return true, nil, errors.New("unknown create response")
		}
		return true, body, nil
	})
	return f
}
func (f *nativeFixture) apply() (Receipt, error) {
	return (Kernel{Native: f.n, Journal: f.j}).Apply(context.Background(), f.p, f.p.SHA256, "synthetic network preparation")
}

func TestFixedPreparationCreatesOnlyConfigurationAndAcceptsNativeNamespaceDefaults(t *testing.T) {
	f := newNativeFixture(t)
	r, e := f.apply()
	require.NoError(t, e)
	require.True(t, r.Complete)
	require.Equal(t, 7, f.creates)
	require.NoError(t, (Kernel{Native: f.n, Journal: f.j}).Observe(context.Background(), r))
	for _, action := range f.kube.Actions() {
		if action.GetVerb() == "create" {
			require.Contains(t, []string{"namespaces", "networkpolicies"}, action.GetResource().Resource)
		}
	}
	for _, action := range f.dynamic.Actions() {
		require.Equal(t, "list", action.GetVerb())
	}
	objects := f.p.Objects()
	require.Len(t, objects, 7)
	redis := objects[5].Body.(*networkingv1.NetworkPolicy)
	require.Equal(t, "janua-staging-redis", redis.Spec.PodSelector.MatchLabels["enclii.dev/service"])
	db := objects[2].Body.(*networkingv1.NetworkPolicy)
	require.Equal(t, "janua-staging-alembic-v1", db.Spec.Ingress[0].From[1].PodSelector.MatchLabels["enclii.dev/janua-migrator"])
	require.Equal(t, januastagedb.Namespace(f.p.Authority.ProjectID), objects[0].Body.(*corev1.Namespace).Name)
}
func TestExistingObjectIsNeverAdoptedEvenWithExactDesiredConfiguration(t *testing.T) {
	f := newNativeFixture(t)
	_, e := f.n.Create(context.Background(), f.p.Objects()[0])
	require.NoError(t, e)
	_, e = f.apply()
	require.ErrorIs(t, e, ErrRefused)
	require.False(t, f.j.claimed)
	require.Equal(t, 1, f.creates)
}
func TestUnknownCreateAndMissingReceiptNeverRetryOrAdopt(t *testing.T) {
	for _, mode := range []string{"create-response", "journal-write"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeFixture(t)
			if mode == "create-response" {
				f.failCreate = 2
			} else {
				f.j.failRecord = 2
			}
			_, e := f.apply()
			require.ErrorIs(t, e, ErrUnknown)
			r, _ := f.j.Read(context.Background(), f.p.ID, f.p.ActorID)
			require.Equal(t, 2, r.Attempted)
			require.Len(t, r.Objects, 1)
			require.False(t, r.Complete)
			f.failCreate = 0
			f.j.failRecord = 0
			_, e = f.apply()
			require.Error(t, e)
			require.Equal(t, 2, f.creates)
			require.ErrorIs(t, (Kernel{Native: f.n, Journal: f.j}).Observe(context.Background(), r), ErrUnknown)
		})
	}
}
func TestAdmittedDriftRetainsFirstUIDThenRefusesFurtherEffects(t *testing.T) {
	for _, mode := range []string{"rule", "label", "finalizer"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeFixture(t)
			f.mutate = func(body runtime.Object) {
				if policy, ok := body.(*networkingv1.NetworkPolicy); ok {
					switch mode {
					case "rule":
						policy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{}}
					case "label":
						policy.Labels["enclii.dev/unreviewed"] = "true"
					case "finalizer":
						policy.Finalizers = []string{"unreviewed.invalid/hold"}
					}
				}
			}
			_, e := f.apply()
			require.ErrorIs(t, e, ErrUnknown)
			r, _ := f.j.Read(context.Background(), f.p.ID, f.p.ActorID)
			require.Len(t, r.Objects, 2)
			require.Equal(t, "created-fixture-2", r.Objects[1].UID)
			require.False(t, r.Complete)
			require.Equal(t, 2, f.creates)
		})
	}
}
func TestCompletedUIDReplacementOrAdditionalPolicyCannotQualify(t *testing.T) {
	for _, mode := range []string{"replacement", "extra-policy", "fresh-source"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeFixture(t)
			r, e := f.apply()
			require.NoError(t, e)
			switch mode {
			case "replacement":
				ns, e := f.kube.CoreV1().Namespaces().Get(context.Background(), namespace(f.p), metav1.GetOptions{})
				require.NoError(t, e)
				ns.UID = "replaced-fixture"
				_, e = f.kube.CoreV1().Namespaces().Update(context.Background(), ns, metav1.UpdateOptions{})
				require.NoError(t, e)
			case "extra-policy":
				_, e = f.kube.NetworkingV1().NetworkPolicies(januastage.Namespace).Create(context.Background(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "unreviewed", Namespace: januastage.Namespace}}, metav1.CreateOptions{})
				require.NoError(t, e)
			case "fresh-source":
				f.failFresh = f.freshCalls + 1
			}
			require.ErrorIs(t, (Kernel{Native: f.n, Journal: f.j}).Observe(context.Background(), r), ErrUnknown)
		})
	}
}
func TestPlanCannotImportUnknownSelectorsOrAlterDesiredHash(t *testing.T) {
	p := planFixture(t)
	p.ObjectSHA256[0] = strings.Repeat("e", 64)
	require.Error(t, p.Validate())
	p = planFixture(t)
	p.Inputs.Peers[0].HostRoute = "192.0.2.0/24"
	require.Error(t, p.Validate())
	p = planFixture(t)
	p.Inputs.Operator.MatchExpressions = []metav1.LabelSelectorRequirement{{Key: "any", Operator: metav1.LabelSelectorOpExists}}
	require.Error(t, p.Validate())
	f := newNativeFixture(t)
	_, e := (Kernel{Native: f.n, Journal: f.j}).Apply(context.Background(), f.p, strings.Repeat("e", 64), "reviewed")
	require.ErrorIs(t, e, ErrRefused)
	require.Zero(t, f.creates)
	require.False(t, f.j.claimed)
}

func TestNamespaceDefaultNormalizationCannotHideSelectorMismatch(t *testing.T) {
	p := planFixture(t)
	want := p.Objects()[0].Body.(*corev1.Namespace)
	actual := want.DeepCopy()
	actual.Labels["kubernetes.io/metadata.name"] = actual.Name
	actual.Spec.Finalizers = []corev1.FinalizerName{corev1.FinalizerKubernetes}
	require.True(t, Matches(want, actual))
	actual.Labels["kubernetes.io/metadata.name"] = "foreign-namespace"
	require.False(t, Matches(want, actual))
}

func TestNamespacePreparationRefusesInjectedLatentWorkload(t *testing.T) {
	f := newNativeFixture(t)
	f.mutate = func(body runtime.Object) {
		if ns, ok := body.(*corev1.Namespace); ok {
			_, e := f.dynamic.Resource(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}).Namespace(ns.Name).Create(context.Background(), &unstructured.Unstructured{Object: map[string]any{"apiVersion": "batch/v1", "kind": "Job", "metadata": map[string]any{"name": "unreviewed-fixture", "namespace": ns.Name}}}, metav1.CreateOptions{})
			require.NoError(t, e)
		}
	}
	_, e := f.apply()
	require.ErrorIs(t, e, ErrUnknown)
	require.Equal(t, 1, f.creates)
	r, _ := f.j.Read(context.Background(), f.p.ID, f.p.ActorID)
	require.Len(t, r.Objects, 1)
	require.False(t, r.Complete)
}

func TestPartialNamespaceReplacementCannotBecomeAReceipt(t *testing.T) {
	f := newNativeFixture(t)
	f.failCreate = 2
	_, e := f.apply()
	require.ErrorIs(t, e, ErrUnknown)
	r, _ := f.j.Read(context.Background(), f.p.ID, f.p.ActorID)
	original := r.Objects[0].UID
	ns, e := f.kube.CoreV1().Namespaces().Get(context.Background(), namespace(f.p), metav1.GetOptions{})
	require.NoError(t, e)
	require.NoError(t, f.kube.CoreV1().Namespaces().Delete(context.Background(), ns.Name, metav1.DeleteOptions{}))
	ns.UID = "recreated-partial-namespace-fixture"
	ns.ResourceVersion = "2"
	require.NoError(t, f.kube.Tracker().Create(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, ns, ""))
	require.ErrorIs(t, (Kernel{Native: f.n, Journal: f.j}).Observe(context.Background(), r), ErrUnknown)
	_, e = f.apply()
	require.Error(t, e)
	require.Equal(t, 2, f.creates)
	retained, _ := f.j.Read(context.Background(), f.p.ID, f.p.ActorID)
	require.Equal(t, original, retained.Objects[0].UID)
}

func TestUnknownNativePrerequisiteReadRefusesBeforeAnyClaimOrCreate(t *testing.T) {
	f := newNativeFixture(t)
	f.dynamic.PrependReactor("list", "clusters", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("unavailable fixed resource")
	})
	_, e := f.apply()
	require.ErrorIs(t, e, ErrRefused)
	require.Zero(t, f.creates)
	require.False(t, f.j.claimed)
}
