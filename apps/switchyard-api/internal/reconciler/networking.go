package reconciler

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// generateIngress creates an Ingress manifest for custom domains
func (r *ServiceReconciler) generateIngress(req *ReconcileRequest, namespace string) (*networkingv1.Ingress, error) {
	labels := map[string]string{
		"app":                   req.Service.Name,
		"enclii.dev/service":    req.Service.Name,
		"enclii.dev/project":    req.Service.ProjectID.String(),
		"enclii.dev/managed-by": "switchyard",
	}

	// Build ingress rules
	var rules []networkingv1.IngressRule
	var tlsConfigs []networkingv1.IngressTLS

	pathType := networkingv1.PathTypePrefix

	for _, domain := range req.CustomDomains {
		// Default path if no routes specified
		paths := []networkingv1.HTTPIngressPath{
			{
				Path:     "/",
				PathType: &pathType,
				Backend: networkingv1.IngressBackend{
					Service: &networkingv1.IngressServiceBackend{
						Name: req.Service.Name,
						Port: networkingv1.ServiceBackendPort{
							Number: 80,
						},
					},
				},
			},
		}

		// Override with custom routes if specified
		if len(req.Routes) > 0 {
			paths = []networkingv1.HTTPIngressPath{}
			for _, route := range req.Routes {
				routePathType := networkingv1.PathTypePrefix
				if route.PathType == "Exact" {
					routePathType = networkingv1.PathTypeExact
				} else if route.PathType == "ImplementationSpecific" {
					routePathType = networkingv1.PathTypeImplementationSpecific
				}

				paths = append(paths, networkingv1.HTTPIngressPath{
					Path:     route.Path,
					PathType: &routePathType,
					Backend: networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: req.Service.Name,
							Port: networkingv1.ServiceBackendPort{
								Number: int32(route.Port),
							},
						},
					},
				})
			}
		}

		rules = append(rules, networkingv1.IngressRule{
			Host: domain.Domain,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: paths,
				},
			},
		})

		// Add TLS configuration if enabled
		if domain.TLSEnabled {
			// TLS issuer is configured at the ingress level below, not per-domain
			tlsConfigs = append(tlsConfigs, networkingv1.IngressTLS{
				Hosts:      []string{domain.Domain},
				SecretName: fmt.Sprintf("%s-%s-tls", req.Service.Name, sanitizeDomainForSecret(domain.Domain)),
			})
		}
	}

	// Determine cert-manager issuer
	tlsIssuer := "letsencrypt-prod"
	if len(req.CustomDomains) > 0 && req.CustomDomains[0].TLSIssuer != "" {
		tlsIssuer = req.CustomDomains[0].TLSIssuer
	}

	annotations := map[string]string{
		"kubernetes.io/ingress.class":                    "nginx",
		"cert-manager.io/cluster-issuer":                 tlsIssuer,
		"nginx.ingress.kubernetes.io/ssl-redirect":       "true",
		"nginx.ingress.kubernetes.io/force-ssl-redirect": "true",
	}

	// Inject per-service custom response headers via nginx configuration snippet
	if len(req.Service.Headers) > 0 {
		var snippetLines []string
		for key, value := range req.Service.Headers {
			safeKey := sanitizeHeaderName(key)
			safeValue := sanitizeHeaderValue(value)
			if safeKey != "" && safeValue != "" {
				snippetLines = append(snippetLines, fmt.Sprintf("more_set_headers \"%s: %s\";", safeKey, safeValue))
			}
		}
		sort.Strings(snippetLines) // Deterministic output for idempotent reconciliation
		if len(snippetLines) > 0 {
			annotations["nginx.ingress.kubernetes.io/configuration-snippet"] = strings.Join(snippetLines, "\n")
		}
	}

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        req.Service.Name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: stringPtr("nginx"),
			TLS:              tlsConfigs,
			Rules:            rules,
		},
	}

	return ingress, nil
}

// applyIngress creates or updates an Ingress
func (r *ServiceReconciler) applyIngress(ctx context.Context, ingress *networkingv1.Ingress) error {
	ingressClient := r.k8sClient.Clientset.NetworkingV1().Ingresses(ingress.Namespace)

	// Try to get existing ingress
	existing, err := ingressClient.Get(ctx, ingress.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new ingress
			_, err = ingressClient.Create(ctx, ingress, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create ingress: %w", err)
			}
			r.logger.WithField("ingress", ingress.Name).Info("Created new ingress")
			return nil
		}
		return fmt.Errorf("failed to get ingress: %w", err)
	}

	// Update existing ingress
	existing.Labels = ingress.Labels
	existing.Annotations = ingress.Annotations
	existing.Spec = ingress.Spec

	_, err = ingressClient.Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ingress: %w", err)
	}

	r.logger.WithField("ingress", ingress.Name).Info("Updated existing ingress")
	return nil
}

// generateNetworkPolicies creates ingress and egress NetworkPolicy manifests for service isolation
func (r *ServiceReconciler) generateNetworkPolicies(req *ReconcileRequest, namespace string) ([]*networkingv1.NetworkPolicy, error) {
	labels := map[string]string{
		"app":                   req.Service.Name,
		"enclii.dev/service":    req.Service.Name,
		"enclii.dev/project":    req.Service.ProjectID.String(),
		"enclii.dev/managed-by": "switchyard",
	}

	podSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app":                req.Service.Name,
			"enclii.dev/service": req.Service.Name,
		},
	}

	// Determine the container port with source tracking for observability
	containerPort, portSource, err := parseContainerPortWithSource(req.EnvVars)
	if err != nil {
		r.logger.WithFields(map[string]interface{}{
			"service": req.Service.Name,
			"error":   err.Error(),
		}).Warn("Port parsing error, using default port")
	}

	// Log port configuration for debugging and audit trail
	r.logger.WithFields(map[string]interface{}{
		"service":    req.Service.Name,
		"namespace":  namespace,
		"port":       containerPort,
		"portSource": string(portSource),
	}).Debug("NetworkPolicy port configuration")

	// Warn if using default port (may indicate missing port configuration)
	if portSource == PortSourceDefault {
		r.logger.WithFields(map[string]interface{}{
			"service":   req.Service.Name,
			"namespace": namespace,
			"port":      containerPort,
		}).Warn("Using default port for NetworkPolicy - consider setting ENCLII_PORT or PORT env var")
	}

	var policies []*networkingv1.NetworkPolicy

	// 1. Ingress Policy: Allow traffic only from ingress-nginx (Cloudflare Tunnel entry point)
	ingressPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ingress", req.Service.Name),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: podSelector,
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					// Allow from ingress-nginx namespace (where cloudflared routes traffic)
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "ingress-nginx",
								},
							},
						},
						{
							// Also allow from cloudflare-tunnel namespace
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "cloudflare-tunnel",
								},
							},
						},
						{
							// Allow traffic from same namespace (inter-service communication)
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": namespace,
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: protocolPtr(corev1.ProtocolTCP),
							Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: containerPort},
						},
					},
				},
			},
		},
	}
	policies = append(policies, ingressPolicy)

	// 2. Egress Policy: Allow DNS, addon namespaces, and Kubernetes API
	egressRules := []networkingv1.NetworkPolicyEgressRule{
		// DNS egress (kube-dns in kube-system)
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "kube-system",
						},
					},
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"k8s-app": "kube-dns",
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: protocolPtr(corev1.ProtocolUDP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 53}},
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 53}},
			},
		},
		// Kubernetes API server (for services that need K8s access)
		// Internal cluster IPs (10.x.x.x)
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: &networkingv1.IPBlock{
						CIDR: "10.0.0.0/8",
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 443}},
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 6443}},
			},
		},
		// External K8s API server (k3s single-node uses node's external IP)
		// Port 6443 is K8s API specific, safe to allow to any destination
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 6443}},
			},
		},
	}

	// Add egress rules for each addon binding (database access)
	for _, binding := range req.AddonBindings {
		addonPort := getAddonPort(binding.AddonType)
		egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": binding.K8sNamespace,
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: protocolPtr(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: addonPort}},
			},
		})
	}

	// Allow egress to data namespace (postgres, redis in shared data tier)
	egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": "data",
					},
				},
			},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: protocolPtr(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 5432}}, // PostgreSQL
			{Protocol: protocolPtr(corev1.ProtocolTCP), Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 6379}}, // Redis
		},
	})

	// Allow egress to same namespace (inter-service communication)
	egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": namespace,
					},
				},
			},
		},
	})

	egressPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-egress", req.Service.Name),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: podSelector,
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      egressRules,
		},
	}
	policies = append(policies, egressPolicy)

	return policies, nil
}

// applyNetworkPolicy creates or updates a NetworkPolicy
func (r *ServiceReconciler) applyNetworkPolicy(ctx context.Context, np *networkingv1.NetworkPolicy) error {
	npClient := r.k8sClient.Clientset.NetworkingV1().NetworkPolicies(np.Namespace)

	// Try to get existing NetworkPolicy
	existing, err := npClient.Get(ctx, np.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new NetworkPolicy
			_, err = npClient.Create(ctx, np, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create network policy: %w", err)
			}
			r.logger.WithField("networkpolicy", np.Name).Info("Created new network policy")
			return nil
		}
		return fmt.Errorf("failed to get network policy: %w", err)
	}

	// Detect port mismatch before update (for observability/debugging)
	existingPort := extractNetworkPolicyPort(existing)
	newPort := extractNetworkPolicyPort(np)
	if existingPort != 0 && newPort != 0 && existingPort != newPort {
		r.logger.WithFields(map[string]interface{}{
			"networkpolicy": np.Name,
			"namespace":     np.Namespace,
			"existingPort":  existingPort,
			"newPort":       newPort,
		}).Warn("NetworkPolicy port mismatch detected - updating to correct port")
	}

	// Update existing NetworkPolicy
	np.ResourceVersion = existing.ResourceVersion
	_, err = npClient.Update(ctx, np, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update network policy: %w", err)
	}

	r.logger.WithField("networkpolicy", np.Name).Info("Updated existing network policy")
	return nil
}

// extractNetworkPolicyPort extracts the first ingress port from a NetworkPolicy.
// Returns 0 if no port is found.
func extractNetworkPolicyPort(np *networkingv1.NetworkPolicy) int32 {
	if np == nil {
		return 0
	}
	for _, rule := range np.Spec.Ingress {
		for _, port := range rule.Ports {
			if port.Port != nil {
				return port.Port.IntVal
			}
		}
	}
	return 0
}

// headerNameRegex matches valid HTTP header name characters (RFC 7230: token characters)
var headerNameRegex = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)

// sanitizeHeaderName validates that a header name contains only safe characters.
// Returns empty string if the name is invalid.
func sanitizeHeaderName(name string) string {
	if name == "" || !headerNameRegex.MatchString(name) {
		return ""
	}
	return name
}

// headerValueUnsafeRegex matches characters that could cause nginx config injection
var headerValueUnsafeRegex = regexp.MustCompile(`[\r\n\x00;"\\]`)

// sanitizeHeaderValue rejects values containing characters that could cause nginx config injection.
// Returns empty string if the value contains unsafe characters.
func sanitizeHeaderValue(value string) string {
	if headerValueUnsafeRegex.MatchString(value) {
		return ""
	}
	return value
}
