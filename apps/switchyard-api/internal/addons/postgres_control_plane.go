package addons

import (
	"context"
	"errors"
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
// once: a new instance scheduled there crash-looped on "connection refused"
// to the cluster's API Service while the same initdb passed on a node that
// does host one. Same ports the service namespaces already allow
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

// CNPGOperatorNamespace is where the CloudNativePG operator runs.
const CNPGOperatorNamespace = "cnpg-system"

// cnpgInstanceStatusPort is the instance manager's status endpoint. The
// operator polls it on every reconcile and stops before any other step
// (hibernation, failover, pod recreation) when it cannot reach it.
const cnpgInstanceStatusPort = 8000

// buildOperatorIngressPolicy lets the CNPG operator reach one cluster's
// instance manager. The addon namespace's default-deny refuses all ingress, so
// without this rule the operator reaches an instance only when both share a
// node (node-local traffic skips pod ingress filtering). Everywhere else the
// Cluster sits in «Instance Status Extraction Error: HTTP communication issue»:
// the database still serves, but the operator cannot manage it, and a
// cnpg.io/hibernation annotation is never acted on (crea-sim kernel01g,
// 2026-10-24: the instance pod was deleted and recreated within seconds).
// Same rule the janua-staging database policy already carries. Only the
// operator's pods, only the status port; 5432 stays with the data-access policy.
func buildOperatorIngressPolicy(namespace, resourceName string) *networkingv1.NetworkPolicy {
	status := intstr.FromInt(cnpgInstanceStatusPort)
	tcp := corev1.ProtocolTCP
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName + "-operator-ingress",
			Namespace: namespace,
			Labels:    map[string]string{"enclii.dev/managed-by": "onboarding-api"},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": resourceName}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": CNPGOperatorNamespace}},
					PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "cloudnative-pg"}},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &status}},
			}},
		},
	}
}

// ensureOperatorIngress creates or converges the policy; it is idempotent.
func (p *PostgresProvisioner) ensureOperatorIngress(ctx context.Context, namespace, resourceName string) error {
	if p == nil || p.k8sClient == nil || p.k8sClient.Clientset == nil && p.k8sClient.KubeClient == nil || namespace == "" || resourceName == "" {
		return fmt.Errorf("operator ingress: cluster client or addon identity unavailable")
	}
	return p.k8sClient.EnsureNetworkPolicy(ctx, buildOperatorIngressPolicy(namespace, resourceName))
}

// ensureCNPGNetworkPolicies converges both policies a CNPG cluster needs in a
// default-deny namespace: instances out to the API server, operator in to the
// instances. Both are attempted; the errors are joined.
func (p *PostgresProvisioner) ensureCNPGNetworkPolicies(ctx context.Context, namespace, resourceName string) error {
	return errors.Join(p.ensureControlPlaneEgress(ctx, namespace, resourceName), p.ensureOperatorIngress(ctx, namespace, resourceName))
}
