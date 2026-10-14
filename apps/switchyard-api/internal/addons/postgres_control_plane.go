package addons

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// buildControlPlaneEgressPolicy lets one CNPG cluster's pods (instances and
// initdb/join Jobs) reach the Kubernetes API, which their instance manager
// watches. The addon namespace's default-deny admits only DNS egress, so
// without this rule a pod reaches the API only when kube-proxy happens to pick
// an API-server endpoint on its own node (node-local traffic skips pod egress
// filtering). On a node that hosts no endpoint every attempt is refused at
// once: observed 2026-10-14, a new instance on foundry-worker-01 crash-looped
// on "dial tcp 10.43.0.1:443: connect: connection refused" while its initdb
// retry on foundry-cp passed. Same ports the service namespaces already allow
// (reconciler/networking.go); nothing else is opened.
func buildControlPlaneEgressPolicy(namespace, resourceName string) *networkingv1.NetworkPolicy {
	https, api := intstr.FromInt(443), intstr.FromInt(6443)
	tcp := corev1.ProtocolTCP
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName + "-control-plane",
			Namespace: namespace,
			Labels:    map[string]string{"enclii.dev/managed-by": "onboarding-api"},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": resourceName}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				// In-cluster API service (10.43.0.1:443) and private node addresses.
				{
					To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.0.0.0/8"}}},
					Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &https}, {Protocol: &tcp, Port: &api}},
				},
				// k3s servers that serve the API on a node's external address.
				{Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &api}}},
			},
		},
	}
}

// ensureControlPlaneEgress creates or converges the policy; it is idempotent.
func (p *PostgresProvisioner) ensureControlPlaneEgress(ctx context.Context, namespace, resourceName string) error {
	if p == nil || p.k8sClient == nil || p.k8sClient.Clientset == nil && p.k8sClient.KubeClient == nil || namespace == "" || resourceName == "" {
		return fmt.Errorf("control-plane egress: cluster client or addon identity unavailable")
	}
	return p.k8sClient.EnsureNetworkPolicy(ctx, buildControlPlaneEgressPolicy(namespace, resourceName))
}
