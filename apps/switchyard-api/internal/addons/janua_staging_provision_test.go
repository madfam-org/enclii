package addons

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

type januaProvisionFixture struct {
	service    *AddonService
	addon      *types.DatabaseAddon
	operation  JanuaStagingOperation
	journalUID k8stypes.UID
	binding    JanuaStagingNamespaceBinding
	kube       *fake.Clientset
	dynamic    *dynamicfake.FakeDynamicClient
}

func newJanuaProvisionFixture(t *testing.T) *januaProvisionFixture {
	t.Helper()
	a, o := januaStageFixture(), januaOperationFixture()
	o.ProjectID, o.EnvironmentID, o.AddonID = a.ProjectID, *a.EnvironmentID, a.ID
	b := JanuaStagingNamespaceBinding{ApplicationUID: "app-ns-birth", AddonUID: "db-ns-birth"}
	objects := []runtime.Object{}
	for name, uid := range map[string]k8stypes.UID{JanuaStagingNamespace: b.ApplicationUID, "project-" + a.ProjectID.String()[:8]: b.AddonUID} {
		objects = append(objects, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, UID: uid, ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: a.ProjectID.String(), "enclii.dev/verify-signatures": "true", "enclii.dev/staging-profile": JanuaStagingProfile, "enclii.dev/data-access": "false", "enclii.dev/postgres-consumer": "false", "enclii.dev/redis-consumer": "false"}}})
		port, tcp, udp := intstr.FromInt32(53), corev1.ProtocolTCP, corev1.ProtocolUDP
		objects = append(objects, &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: name, UID: k8stypes.UID(uuid.NewString()), ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: a.ProjectID.String(), "enclii.dev/managed-by": "onboarding-api"}}, Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &port}, {Protocol: &tcp, Port: &port}}}}}})
	}
	kube := fake.NewSimpleClientset(objects...)
	kube.PrependReactor("create", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		object := action.(ktesting.CreateAction).GetObject()
		meta := object.(metav1.Object)
		meta.SetUID(k8stypes.UID(uuid.NewString()))
		meta.SetResourceVersion("1")
		if len(action.(interface{ GetCreateOptions() metav1.CreateOptions }).GetCreateOptions().DryRun) > 0 {
			return true, object, nil
		}
		return false, nil, nil
	})
	kube.PrependReactor("update", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
		action.(ktesting.UpdateAction).GetObject().(metav1.Object).SetResourceVersion("2")
		return false, nil, nil
	})
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cnpgGVR: "ClusterList"})
	dynamic.PrependReactor("create", "clusters", func(action ktesting.Action) (bool, runtime.Object, error) {
		object := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
		object.SetUID("cluster-birth")
		object.SetResourceVersion("1")
		if len(action.(interface{ GetCreateOptions() metav1.CreateOptions }).GetCreateOptions().DryRun) > 0 {
			return true, object, nil
		}
		return false, nil, nil
	})
	client := &k8s.Client{KubeClient: kube, DynamicClient: dynamic}
	s := &AddonService{k8sClient: client, logger: logrus.New(), provisioners: map[types.DatabaseAddonType]AddonProvisioner{types.DatabaseAddonTypePostgres: &PostgresProvisioner{k8sClient: client, dynamicClient: dynamic, logger: logrus.New()}}}
	_, uid, err := ClaimJanuaStagingOperation(context.Background(), kube, o)
	require.NoError(t, err)
	kube.ClearActions()
	return &januaProvisionFixture{service: s, addon: a, operation: o, journalUID: uid, binding: b, kube: kube, dynamic: dynamic}
}

func (f *januaProvisionFixture) run(dry bool) (*JanuaStagingBirthReceipt, error) {
	return f.service.ProvisionJanuaStaging(context.Background(), f.addon, f.operation, f.journalUID, f.binding, 100, []KarafielControlPeer{{HostRoute: "192.0.2.1/32", Port: 443}}, metav1.LabelSelector{MatchLabels: map[string]string{"app": "fixture-cnpg"}}, func(context.Context) error { return nil }, dry)
}

func TestJanuaProvisionDryRunDoesNotCreateCustodyOrCluster(t *testing.T) {
	f := newJanuaProvisionFixture(t)
	receipt, err := f.run(true)
	require.NoError(t, err)
	require.Nil(t, receipt)
	for _, action := range f.kube.Actions() {
		if action.GetVerb() == "create" {
			require.Equal(t, []string{metav1.DryRunAll}, action.(interface{ GetCreateOptions() metav1.CreateOptions }).GetCreateOptions().DryRun)
		}
		if action.GetVerb() == "create" {
			require.NotEqual(t, "secrets", action.GetResource().Resource)
		}
	}
	for _, action := range f.dynamic.Actions() {
		if action.GetVerb() == "create" {
			require.Equal(t, []string{metav1.DryRunAll}, action.(interface{ GetCreateOptions() metav1.CreateOptions }).GetCreateOptions().DryRun)
		}
	}
}

func TestJanuaProvisionPersistsExactBirthAndNeverAdoptsPartialReplay(t *testing.T) {
	f := newJanuaProvisionFixture(t)
	receipt, err := f.run(false)
	require.NoError(t, err)
	require.True(t, receipt.valid())
	retained, err := ReadJanuaStagingBirthReceipt(context.Background(), f.kube, f.operation, f.journalUID)
	require.NoError(t, err)
	require.Equal(t, *receipt, retained)
	secret, err := f.kube.CoreV1().Secrets("project-"+f.addon.ProjectID.String()[:8]).Get(context.Background(), JanuaStagingRuntimeSecretName, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, *secret.Immutable)
	require.True(t, ownedByCNPG(secret, JanuaStagingClusterName, receipt.ClusterUID))
	f.kube.ClearActions()
	f.dynamic.ClearActions()
	_, err = f.run(false)
	require.Error(t, err)
	for _, action := range f.kube.Actions() {
		require.Contains(t, []string{"get", "list"}, action.GetVerb())
	}
	for _, action := range f.dynamic.Actions() {
		require.Contains(t, []string{"get", "list"}, action.GetVerb())
	}
}

func TestJanuaProvisionAdmissionExpansionPreventsRealEffects(t *testing.T) {
	f := newJanuaProvisionFixture(t)
	f.dynamic.PrependReactor("create", "clusters", func(action ktesting.Action) (bool, runtime.Object, error) {
		object := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		require.NoError(t, unstructured.SetNestedSlice(object.Object, []interface{}{map[string]interface{}{"name": "injected", "value": "refused"}}, "spec", "env"))
		return true, object, nil
	})
	_, err := f.run(false)
	require.Error(t, err)
	for _, action := range f.kube.Actions() {
		require.NotEqual(t, "create", action.GetVerb())
	}
}

func TestJanuaProvisionUnknownClusterCreateRetainsEvidenceAndHoldsRetry(t *testing.T) {
	f := newJanuaProvisionFixture(t)
	f.dynamic.PrependReactor("create", "clusters", func(action ktesting.Action) (bool, runtime.Object, error) {
		if len(action.(interface{ GetCreateOptions() metav1.CreateOptions }).GetCreateOptions().DryRun) == 0 {
			return true, nil, fmt.Errorf("timeout")
		}
		return false, nil, nil
	})
	_, err := f.run(false)
	require.Error(t, err)
	f.kube.ClearActions()
	f.dynamic.ClearActions()
	_, err = f.run(false)
	require.Error(t, err)
	for _, action := range f.kube.Actions() {
		require.Contains(t, []string{"get", "list"}, action.GetVerb())
	}
}
