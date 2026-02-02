package reconciler

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DefaultNamespaceQuota defines the default resource quota for customer namespaces.
// These can be overridden per plan in the future.
var DefaultNamespaceQuota = corev1.ResourceList{
	corev1.ResourcePods:                   resource.MustParse("20"),
	corev1.ResourceRequestsCPU:            resource.MustParse("4"),
	corev1.ResourceRequestsMemory:         resource.MustParse("8Gi"),
	corev1.ResourceLimitsCPU:              resource.MustParse("8"),
	corev1.ResourceLimitsMemory:           resource.MustParse("16Gi"),
	corev1.ResourcePersistentVolumeClaims: resource.MustParse("5"),
}

// ensureResourceQuota creates or updates the default ResourceQuota in the namespace
func (r *ServiceReconciler) ensureResourceQuota(ctx context.Context, namespace string) error {
	quotaName := "enclii-default"
	quotaClient := r.k8sClient.Clientset.CoreV1().ResourceQuotas(namespace)

	quota := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      quotaName,
			Namespace: namespace,
			Labels: map[string]string{
				"enclii.dev/managed-by": "switchyard-reconciler",
			},
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: DefaultNamespaceQuota,
		},
	}

	existing, err := quotaClient.Get(ctx, quotaName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = quotaClient.Create(ctx, quota, metav1.CreateOptions{})
			if err != nil && !errors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create ResourceQuota: %w", err)
			}
			r.logger.WithField("namespace", namespace).Info("Created ResourceQuota")
			return nil
		}
		return fmt.Errorf("failed to get ResourceQuota: %w", err)
	}

	// Update if managed by us
	quota.ResourceVersion = existing.ResourceVersion
	_, err = quotaClient.Update(ctx, quota, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ResourceQuota: %w", err)
	}
	return nil
}

// ensureLimitRange creates default container resource limits in the namespace
func (r *ServiceReconciler) ensureLimitRange(ctx context.Context, namespace string) error {
	lrName := "enclii-default"
	lrClient := r.k8sClient.Clientset.CoreV1().LimitRanges(namespace)

	lr := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lrName,
			Namespace: namespace,
			Labels: map[string]string{
				"enclii.dev/managed-by": "switchyard-reconciler",
			},
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Default: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		},
	}

	existing, err := lrClient.Get(ctx, lrName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = lrClient.Create(ctx, lr, metav1.CreateOptions{})
			if err != nil && !errors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create LimitRange: %w", err)
			}
			r.logger.WithField("namespace", namespace).Info("Created LimitRange")
			return nil
		}
		return fmt.Errorf("failed to get LimitRange: %w", err)
	}

	lr.ResourceVersion = existing.ResourceVersion
	_, err = lrClient.Update(ctx, lr, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update LimitRange: %w", err)
	}
	return nil
}

// ensureNamespaceNetworkPolicy creates a default NetworkPolicy that:
// - Allows egress to DNS (kube-dns:53) and HTTPS (0.0.0.0/0 except RFC1918)
// - Allows ingress only from cloudflare-tunnel namespace
func (r *ServiceReconciler) ensureNamespaceNetworkPolicy(ctx context.Context, namespace string) error {
	npName := "enclii-namespace-isolation"
	npClient := r.k8sClient.Clientset.NetworkingV1().NetworkPolicies(namespace)

	protocolTCP := corev1.ProtocolTCP
	protocolUDP := corev1.ProtocolUDP
	dnsPort := intstr.FromInt32(53)
	httpsPort := intstr.FromInt32(443)

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: namespace,
			Labels: map[string]string{
				"enclii.dev/managed-by": "switchyard-reconciler",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			// Apply to all pods in namespace
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					// Allow ingress from cloudflare-tunnel namespace only
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "cloudflare-tunnel",
								},
							},
						},
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					// Allow DNS
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "kube-system",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &protocolUDP, Port: &dnsPort},
						{Protocol: &protocolTCP, Port: &dnsPort},
					},
				},
				{
					// Allow HTTPS egress to external (non-RFC1918) addresses
					To: []networkingv1.NetworkPolicyPeer{
						{
							IPBlock: &networkingv1.IPBlock{
								CIDR: "0.0.0.0/0",
								Except: []string{
									"10.0.0.0/8",
									"172.16.0.0/12",
									"192.168.0.0/16",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &protocolTCP, Port: &httpsPort},
					},
				},
			},
		},
	}

	existing, err := npClient.Get(ctx, npName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = npClient.Create(ctx, np, metav1.CreateOptions{})
			if err != nil && !errors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create namespace NetworkPolicy: %w", err)
			}
			r.logger.WithField("namespace", namespace).Info("Created namespace isolation NetworkPolicy")
			return nil
		}
		return fmt.Errorf("failed to get namespace NetworkPolicy: %w", err)
	}

	np.ResourceVersion = existing.ResourceVersion
	_, err = npClient.Update(ctx, np, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update namespace NetworkPolicy: %w", err)
	}
	return nil
}
