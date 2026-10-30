package k8s

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func januaNamespaceFake() *fake.Clientset {
	kube := fake.NewSimpleClientset()
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
	return kube
}

func TestJanuaNamespaceCreatesIsolatedBaselineAndReplayRequiresBirthUID(t *testing.T) {
	ctx, kube, project := context.Background(), januaNamespaceFake(), uuid.New()
	c := &Client{KubeClient: kube}
	ns, err := c.PrepareJanuaStagingNamespace(ctx, "janua-staging", project, "", januaNamespaceAuthority, false)
	require.NoError(t, err)
	for _, key := range []string{"enclii.dev/data-access", "enclii.dev/postgres-consumer", "enclii.dev/redis-consumer"} {
		require.Equal(t, "false", ns.Labels[key])
	}
	policy, err := kube.NetworkingV1().NetworkPolicies(ns.Name).Get(ctx, "default-deny", metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, policy.Spec.Ingress)
	require.Len(t, policy.Spec.Egress, 1)
	require.Len(t, policy.Spec.Egress[0].To, 1)
	require.Equal(t, "kube-system", policy.Spec.Egress[0].To[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])
	kube.ClearActions()
	_, err = c.PrepareJanuaStagingNamespace(ctx, ns.Name, project, "", januaNamespaceAuthority, false)
	require.Error(t, err)
	_, err = c.PrepareJanuaStagingNamespace(ctx, ns.Name, project, "replacement", januaNamespaceAuthority, false)
	require.Error(t, err)
	_, err = c.PrepareJanuaStagingNamespace(ctx, ns.Name, project, ns.UID, januaNamespaceAuthority, false)
	require.NoError(t, err)
	for _, action := range kube.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
}

func TestJanuaNamespaceDryRunDoesNotPersistAndProductionIsRefused(t *testing.T) {
	ctx, kube, project := context.Background(), januaNamespaceFake(), uuid.New()
	c := &Client{KubeClient: kube}
	_, err := c.PrepareJanuaStagingNamespace(ctx, "janua", project, "", januaNamespaceAuthority, false)
	require.Error(t, err)
	require.Empty(t, kube.Actions())
	_, err = c.PrepareJanuaStagingNamespace(ctx, "janua-staging", project, "", januaNamespaceAuthority, true)
	require.NoError(t, err)
	namespaces, err := kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, namespaces.Items)
}

func TestJanuaNamespacePolicyFailureIsHeldWithoutDeletionOrRepair(t *testing.T) {
	kube := januaNamespaceFake()
	kube.PrependReactor("create", "networkpolicies", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, fmt.Errorf("unavailable") })
	_, err := (&Client{KubeClient: kube}).PrepareJanuaStagingNamespace(context.Background(), "janua-staging", uuid.New(), "", januaNamespaceAuthority, false)
	require.Error(t, err)
	for _, action := range kube.Actions() {
		require.NotEqual(t, "delete", action.GetVerb())
		require.NotEqual(t, "update", action.GetVerb())
	}
}

func januaNamespaceAuthority(context.Context) error { return nil }

func TestJanuaNamespaceRejectsAdmissionMutatedRequiredLabels(t *testing.T) {
	for _, key := range []string{"enclii.dev/data-access", "enclii.dev/postgres-consumer", "enclii.dev/redis-consumer", "enclii.dev/verify-signatures"} {
		t.Run(key, func(t *testing.T) {
			kube := januaNamespaceFake()
			kube.PrependReactor("create", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
				actual := action.(ktesting.CreateAction).GetObject().(*corev1.Namespace).DeepCopy()
				actual.UID = "admitted"
				actual.ResourceVersion = "1"
				actual.Labels[key] = "true"
				if key == "enclii.dev/verify-signatures" {
					actual.Labels[key] = "false"
				}
				return true, actual, nil
			})
			_, err := (&Client{KubeClient: kube}).PrepareJanuaStagingNamespace(context.Background(), "janua-staging", uuid.New(), "", januaNamespaceAuthority, false)
			require.Error(t, err)
			for _, action := range kube.Actions() {
				if action.GetResource().Resource == "networkpolicies" {
					require.NotEqual(t, "create", action.GetVerb())
				}
			}
		})
	}
}

func TestJanuaNamespaceRechecksAuthorityBeforeChildPolicyWrite(t *testing.T) {
	kube := januaNamespaceFake()
	calls := 0
	check := func(context.Context) error {
		calls++
		if calls == 2 {
			return fmt.Errorf("revoked")
		}
		return nil
	}
	_, err := (&Client{KubeClient: kube}).PrepareJanuaStagingNamespace(context.Background(), "janua-staging", uuid.New(), "", check, false)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	for _, action := range kube.Actions() {
		if action.GetResource().Resource == "networkpolicies" {
			require.NotEqual(t, "create", action.GetVerb())
		}
	}
}

func TestJanuaNamespaceFinalReadbackCannotAcceptNewSharedAccess(t *testing.T) {
	kube := januaNamespaceFake()
	reads := 0
	kube.PrependReactor("get", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
		reads++
		if reads != 3 {
			return false, nil, nil
		}
		actual, err := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("namespaces"), "", action.(ktesting.GetAction).GetName())
		if err != nil {
			return true, nil, err
		}
		copy := actual.(*corev1.Namespace).DeepCopy()
		copy.Labels["enclii.dev/data-access"] = "true"
		return true, copy, nil
	})
	_, err := (&Client{KubeClient: kube}).PrepareJanuaStagingNamespace(context.Background(), "janua-staging", uuid.New(), "", januaNamespaceAuthority, false)
	require.Error(t, err)
	require.Equal(t, 3, reads)
}
