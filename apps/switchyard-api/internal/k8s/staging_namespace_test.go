package k8s

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestStagingNamespaceRefusesUntypedOrForeignExistingOwner(t *testing.T) {
	project := uuid.New()
	for _, label := range []string{"", uuid.New().String()} {
		client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fixture-staging", UID: types.UID("fixture-uid"), Labels: map[string]string{LabelProjectNamespace: label}}})
		kube := &Client{KubeClient: client}
		for _, dryRun := range []bool{true, false} {
			require.ErrorIs(t, kube.PrepareOwnedStagingNamespace(t.Context(), "fixture-staging", project, dryRun), ErrNamespaceOwnership)
		}
		for _, action := range client.Actions() {
			require.Equal(t, "get", action.GetVerb(), "refusal must precede every namespace/policy write")
		}
	}
}

func TestStagingNamespaceRefusesForeignDefaultDenyBeforeAnyWrite(t *testing.T) {
	project := uuid.New()
	for _, owner := range []string{"", uuid.New().String()} {
		for _, dryRun := range []bool{true, false} {
			client := fake.NewSimpleClientset(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fixture-staging", UID: "fixture-uid", Labels: map[string]string{LabelProjectNamespace: project.String()}}},
				&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: "fixture-staging", Labels: map[string]string{"enclii.dev/managed-by": "onboarding-api", LabelProjectNamespace: owner}}},
			)
			kube := &Client{KubeClient: client}
			require.ErrorIs(t, kube.PrepareOwnedStagingNamespace(t.Context(), "fixture-staging", project, dryRun), ErrNamespaceOwnership)
			for _, action := range client.Actions() {
				require.Equal(t, "get", action.GetVerb(), "even namespace labels must remain untouched on foreign-policy refusal")
			}
		}
	}
}

func TestStagingNamespaceDryRunDoesNotCreateNamespace(t *testing.T) {
	client := fake.NewSimpleClientset()
	kube := &Client{KubeClient: client}
	require.NoError(t, kube.PrepareOwnedStagingNamespace(t.Context(), "fixture-staging", uuid.New(), true))
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
	require.ErrorIs(t, kube.PrepareOwnedStagingNamespace(t.Context(), "production", uuid.New(), false), ErrNamespaceOwnership)
}

func TestStagingNamespaceReusesOnlyOwnedPersistedObject(t *testing.T) {
	project := uuid.New()
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fixture-staging", UID: types.UID("fixture-uid"), ResourceVersion: "1", Labels: map[string]string{LabelProjectNamespace: project.String()}}})
	kube := &Client{KubeClient: client}
	require.NoError(t, kube.PrepareOwnedStagingNamespace(t.Context(), "fixture-staging", project, false))
	policy, err := client.NetworkingV1().NetworkPolicies("fixture-staging").Get(t.Context(), "default-deny", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, project.String(), policy.Labels[LabelProjectNamespace])
	require.Empty(t, policy.Spec.Ingress)
	require.Len(t, policy.Spec.Egress, 1)
	require.Len(t, policy.Spec.Egress[0].Ports, 2)
	for _, port := range policy.Spec.Egress[0].Ports {
		require.EqualValues(t, 53, port.Port.IntVal)
	}
}
