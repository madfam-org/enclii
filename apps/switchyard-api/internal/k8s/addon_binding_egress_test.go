package k8s

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func bindingNetworkFixture() (uuid.UUID, *sdktypes.Service, *sdktypes.DatabaseAddon) {
	project := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	namespace := "training-fixture"
	service := &sdktypes.Service{ID: uuid.New(), ProjectID: project, Name: "training-kernel", K8sNamespace: &namespace}
	addon := &sdktypes.DatabaseAddon{
		ID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), ProjectID: project,
		Name: "training-db", Plan: "standard-1", Type: sdktypes.DatabaseAddonTypePostgres,
		Status: sdktypes.DatabaseAddonStatusReady, K8sNamespace: "project-11111111", K8sResourceName: "pg-training-db-22222222",
	}
	return uuid.New(), service, addon
}

func TestBoundAddonEgress_ExactServiceProjectClusterAndPort(t *testing.T) {
	id, service, addon := bindingNetworkFixture()
	policy, err := buildBoundAddonEgressPolicy(id, service, addon)
	require.NoError(t, err)
	require.Equal(t, *service.K8sNamespace, policy.Namespace)
	require.Equal(t, map[string]string{"enclii.dev/service": service.Name}, policy.Spec.PodSelector.MatchLabels)
	require.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, policy.Spec.PolicyTypes)
	require.Empty(t, policy.Spec.Ingress)
	require.Len(t, policy.Spec.Egress, 1)
	rule := policy.Spec.Egress[0]
	require.Len(t, rule.To, 1)
	require.Equal(t, map[string]string{
		"kubernetes.io/metadata.name": addon.K8sNamespace, LabelProjectNamespace: addon.ProjectID.String(),
	}, rule.To[0].NamespaceSelector.MatchLabels)
	require.Equal(t, map[string]string{"cnpg.io/cluster": addon.K8sResourceName}, rule.To[0].PodSelector.MatchLabels)
	require.Nil(t, rule.To[0].IPBlock)
	require.Len(t, rule.Ports, 1)
	require.Equal(t, int32(5432), rule.Ports[0].Port.IntVal)
	require.Equal(t, corev1.ProtocolTCP, *rule.Ports[0].Protocol)
}

func TestBoundAddonEgress_RefusesCrossProjectSharedUnknownAndUnreadyTargets(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*sdktypes.Service, *sdktypes.DatabaseAddon)
	}{
		{"other project", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { s.ProjectID = uuid.New() }},
		{"missing project", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { s.ProjectID = uuid.Nil; a.ProjectID = uuid.Nil }},
		{"no service namespace", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { s.K8sNamespace = nil }},
		{"invalid service namespace", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { n := "bad/namespace"; s.K8sNamespace = &n }},
		{"shared namespace", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { a.K8sNamespace = "data" }},
		{"other addon namespace", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { a.K8sNamespace = "project-33333333" }},
		{"other cluster", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { a.K8sResourceName = "pg-other-db-22222222" }},
		{"shared discovered", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { a.Plan = "shared-discovered" }},
		{"not ready", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) {
			a.Status = sdktypes.DatabaseAddonStatusProvisioning
		}},
		{"other engine", func(s *sdktypes.Service, a *sdktypes.DatabaseAddon) { a.Type = sdktypes.DatabaseAddonTypeRedis }},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, service, addon := bindingNetworkFixture()
			test.mutate(service, addon)
			policy, err := buildBoundAddonEgressPolicy(id, service, addon)
			require.Error(t, err)
			require.Nil(t, policy)
		})
	}
}

func TestBoundAddonEgress_VerifiesBothNamespacesAndDoesNotAdoptAnotherBinding(t *testing.T) {
	id, service, addon := bindingNetworkFixture()
	ctx := context.Background()
	client := &Client{KubeClient: fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: *service.K8sNamespace, Labels: map[string]string{LabelProjectNamespace: service.ProjectID.String()}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: addon.K8sNamespace, Labels: map[string]string{LabelProjectNamespace: uuid.NewString()}}},
	)}
	require.Error(t, client.EnsureBoundAddonEgressPolicy(ctx, id, service, addon))
	policies, err := client.KubeClient.NetworkingV1().NetworkPolicies(*service.K8sNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, policies.Items)

	ns, err := client.KubeClient.CoreV1().Namespaces().Get(ctx, addon.K8sNamespace, metav1.GetOptions{})
	require.NoError(t, err)
	ns.Labels[LabelProjectNamespace] = addon.ProjectID.String()
	_, err = client.KubeClient.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, client.EnsureBoundAddonEgressPolicy(ctx, id, service, addon))
	require.NoError(t, client.EnsureBoundAddonEgressPolicy(ctx, id, service, addon))
	policies, err = client.KubeClient.NetworkingV1().NetworkPolicies(*service.K8sNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, policies.Items, 1)

	foreign := policies.Items[0].DeepCopy()
	foreign.Labels[bindingPolicyLabel] = uuid.NewString()
	_, err = client.KubeClient.NetworkingV1().NetworkPolicies(foreign.Namespace).Update(ctx, foreign, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Error(t, client.EnsureBoundAddonEgressPolicy(ctx, id, service, addon))
	require.Error(t, client.RemoveBoundAddonEgressPolicy(ctx, foreign.Namespace, addon.ProjectID, id))
	retained, err := client.KubeClient.NetworkingV1().NetworkPolicies(foreign.Namespace).Get(ctx, foreign.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, foreign.Labels, retained.Labels)
}

func TestBoundAddonEgress_RemovalIsOwnedAndIdempotent(t *testing.T) {
	id, service, addon := bindingNetworkFixture()
	policy, err := buildBoundAddonEgressPolicy(id, service, addon)
	require.NoError(t, err)
	client := &Client{KubeClient: fake.NewSimpleClientset(policy)}
	ctx := context.Background()
	require.NoError(t, client.RemoveBoundAddonEgressPolicy(ctx, policy.Namespace, addon.ProjectID, id))
	require.NoError(t, client.RemoveBoundAddonEgressPolicy(ctx, policy.Namespace, addon.ProjectID, id))
	policies, err := client.KubeClient.NetworkingV1().NetworkPolicies(policy.Namespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, policies.Items)
}
