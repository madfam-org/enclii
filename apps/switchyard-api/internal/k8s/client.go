package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// =============================================================================
// Client Core (Creation, Configuration, Deployment Operations)
// =============================================================================

type Client struct {
	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
	config        *rest.Config
}

func NewClient(kubeconfig string, kubecontext string) (*Client, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		// Load from kubeconfig file
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
		}
	} else {
		// Try in-cluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load in-cluster config: %w", err)
		}
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create dynamic client for CRD operations
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic kubernetes client: %w", err)
	}

	return &Client{
		Clientset:     clientset,
		DynamicClient: dynClient,
		config:        config,
	}, nil
}

// IsValid checks if the client is properly initialized and safe to use.
func (c *Client) IsValid() bool {
	return c != nil && c.Clientset != nil && c.config != nil
}

// Config returns the Kubernetes REST config for creating additional clients
func (c *Client) Config() *rest.Config {
	return c.config
}

// =============================================================================
// Deployment Specification and Core Operations
// =============================================================================

type DeploymentSpec struct {
	Name        string
	Namespace   string
	ImageURI    string
	Port        int32
	Replicas    int32
	HealthPath  string
	Environment map[string]string
	Labels      map[string]string
}

func (c *Client) DeployService(ctx context.Context, spec *DeploymentSpec) error {
	// Ensure namespace exists
	if err := c.EnsureNamespace(ctx, spec.Namespace); err != nil {
		return fmt.Errorf("failed to ensure namespace: %w", err)
	}

	// Create or update deployment
	if err := c.createOrUpdateDeployment(ctx, spec); err != nil {
		return fmt.Errorf("failed to create/update deployment: %w", err)
	}

	// Create or update service
	if err := c.createOrUpdateService(ctx, spec); err != nil {
		return fmt.Errorf("failed to create/update service: %w", err)
	}

	return nil
}

// EnsureNamespace creates a K8s namespace (or ensures required labels on an existing one).
// Labels applied:
//   - enclii.dev/type=application — grants Janua SSO ingress via janua-api-ingress policy
//   - enclii.dev/data-access=true — grants PostgreSQL, Redis, PgBouncer access via data-network-policies
//   - enclii.dev/verify-signatures=true — Kyverno image signature enforcement
//
// These labels power label-based NetworkPolicies so new namespaces auto-gain
// access to shared platform services without editing any policy files.
func (c *Client) EnsureNamespace(ctx context.Context, namespace string) error {
	existing, err := c.Clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		// Namespace doesn't exist, create it
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					"managed-by":                   "enclii",
					"enclii.dev/type":              "application",
					"enclii.dev/verify-signatures": "true",
					"enclii.dev/data-access":       "true",
				},
			},
		}
		_, err = c.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create namespace %s: %w", namespace, err)
		}
		// Apply default-deny NetworkPolicy to new namespaces
		if npErr := c.ensureDefaultDenyPolicy(ctx, namespace); npErr != nil {
			// Log but don't fail namespace creation
			fmt.Printf("warning: failed to create default-deny NetworkPolicy in %s: %v\n", namespace, npErr)
		}
		return nil
	}

	// Namespace exists — ensure required labels are present
	needsUpdate := false
	if existing.Labels == nil {
		existing.Labels = make(map[string]string)
	}
	requiredLabels := map[string]string{
		"managed-by":                   "enclii",
		"enclii.dev/type":              "application",
		"enclii.dev/verify-signatures": "true",
		"enclii.dev/data-access":       "true",
	}
	for k, v := range requiredLabels {
		if existing.Labels[k] != v {
			existing.Labels[k] = v
			needsUpdate = true
		}
	}
	if needsUpdate {
		_, err = c.Clientset.CoreV1().Namespaces().Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update namespace labels for %s: %w", namespace, err)
		}
	}

	// Ensure default-deny policy exists even on existing namespaces
	_ = c.ensureDefaultDenyPolicy(ctx, namespace)

	return nil
}

// ensureDefaultDenyPolicy creates a default-deny NetworkPolicy that blocks all
// ingress and egress traffic except DNS. Service-specific allow rules are added
// by the reconciler when services are deployed.
func (c *Client) ensureDefaultDenyPolicy(ctx context.Context, namespace string) error {
	npClient := c.Clientset.NetworkingV1().NetworkPolicies(namespace)
	const policyName = "default-deny"

	_, err := npClient.Get(ctx, policyName, metav1.GetOptions{})
	if err == nil {
		return nil // Already exists
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("check existing NetworkPolicy: %w", err)
	}

	dnsPort53 := intstr.FromInt(53)
	protocolUDP := corev1.ProtocolUDP
	protocolTCP := corev1.ProtocolTCP

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: namespace,
			Labels: map[string]string{
				"enclii.dev/managed-by": "onboarding-api",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{}, // Selects all pods
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Port: &dnsPort53, Protocol: &protocolUDP},
						{Port: &dnsPort53, Protocol: &protocolTCP},
					},
				},
			},
		},
	}

	_, err = npClient.Create(ctx, policy, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create default-deny NetworkPolicy in %s: %w", namespace, err)
	}
	return nil
}

// DryRunApply validates a manifest against the cluster using server-side dry-run.
// Returns nil if the manifest would be accepted, or an error with admission violation details.
func (c *Client) DryRunApply(ctx context.Context, namespace string, obj map[string]interface{}) error {
	gvk, ok := obj["apiVersion"].(string)
	if !ok {
		return fmt.Errorf("manifest missing apiVersion")
	}
	kind, ok := obj["kind"].(string)
	if !ok {
		return fmt.Errorf("manifest missing kind")
	}

	// Build GVR from apiVersion and kind
	gvr, err := resolveGVR(c, gvk, kind)
	if err != nil {
		return fmt.Errorf("resolve resource for %s/%s: %w", gvk, kind, err)
	}

	// Convert to unstructured
	unstructuredObj := &unstructured.Unstructured{Object: obj}
	if namespace != "" && unstructuredObj.GetNamespace() == "" {
		unstructuredObj.SetNamespace(namespace)
	}

	// Server-side dry-run
	var resource dynamic.ResourceInterface
	if namespace != "" {
		resource = c.DynamicClient.Resource(gvr).Namespace(namespace)
	} else {
		resource = c.DynamicClient.Resource(gvr)
	}

	_, err = resource.Create(ctx, unstructuredObj, metav1.CreateOptions{
		DryRun: []string{metav1.DryRunAll},
	})
	return err
}

// ApplyUnstructured creates or updates an unstructured resource (CRD).
func (c *Client) ApplyUnstructured(ctx context.Context, namespace string, unstructuredObj *unstructured.Unstructured) error {
	gvk := unstructuredObj.GroupVersionKind()
	
	// Build GVR from GroupVersionKind
	gvr, err := resolveGVR(c, gvk.GroupVersion().String(), gvk.Kind)
	if err != nil {
		return fmt.Errorf("resolve resource for %s/%s: %w", gvk.GroupVersion().String(), gvk.Kind, err)
	}

	if namespace != "" && unstructuredObj.GetNamespace() == "" {
		unstructuredObj.SetNamespace(namespace)
	}

	var resource dynamic.ResourceInterface
	if namespace != "" {
		resource = c.DynamicClient.Resource(gvr).Namespace(namespace)
	} else {
		resource = c.DynamicClient.Resource(gvr)
	}

	existing, err := resource.Get(ctx, unstructuredObj.GetName(), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = resource.Create(ctx, unstructuredObj, metav1.CreateOptions{})
			return err
		}
		return fmt.Errorf("failed to get existing resource: %w", err)
	}

	unstructuredObj.SetResourceVersion(existing.GetResourceVersion())
	_, err = resource.Update(ctx, unstructuredObj, metav1.UpdateOptions{})
	return err
}

func (c *Client) createOrUpdateDeployment(ctx context.Context, spec *DeploymentSpec) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": spec.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": spec.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  spec.Name,
							Image: spec.ImageURI,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: spec.Port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: c.buildEnvVars(spec.Environment),
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: spec.HealthPath,
										Port: intstr.FromInt(int(spec.Port)),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: spec.HealthPath,
										Port: intstr.FromInt(int(spec.Port)),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       30,
								TimeoutSeconds:      5,
								FailureThreshold:    3,
							},
						},
					},
				},
			},
		},
	}

	// Try to update first, if not found, create
	_, err := c.Clientset.AppsV1().Deployments(spec.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		_, err = c.Clientset.AppsV1().Deployments(spec.Namespace).Create(ctx, deployment, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create deployment: %w", err)
		}
	}

	return nil
}

func (c *Client) createOrUpdateService(ctx context.Context, spec *DeploymentSpec) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    spec.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": spec.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       spec.Port,
					TargetPort: intstr.FromInt(int(spec.Port)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	// Try to update first, if not found, create
	_, err := c.Clientset.CoreV1().Services(spec.Namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		_, err = c.Clientset.CoreV1().Services(spec.Namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create service: %w", err)
		}
	}

	return nil
}

func (c *Client) buildEnvVars(env map[string]string) []corev1.EnvVar {
	var envVars []corev1.EnvVar
	for key, value := range env {
		envVars = append(envVars, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}
	return envVars
}

// =============================================================================
// Deployment Status and Rollback
// =============================================================================

func (c *Client) GetDeploymentStatus(ctx context.Context, name, namespace string) (*types.Deployment, error) {
	deployment, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	status := types.DeploymentStatusPending
	health := types.HealthStatusUnknown

	if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
		status = types.DeploymentStatusRunning
		health = types.HealthStatusHealthy
	} else if deployment.Status.ReadyReplicas > 0 {
		status = types.DeploymentStatusRunning
		health = types.HealthStatusUnhealthy
	}

	return &types.Deployment{
		Status:   status,
		Health:   health,
		Replicas: int(deployment.Status.ReadyReplicas),
	}, nil
}

func (c *Client) RollbackDeployment(ctx context.Context, name, namespace string) error {
	// Get deployment
	deployment, err := c.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Find the previous image from ReplicaSet history
	previousImage, err := c.getPreviousImage(ctx, name, namespace, deployment)
	if err != nil {
		return fmt.Errorf("failed to find previous image: %w", err)
	}

	if previousImage == "" {
		return fmt.Errorf("no previous revision found to rollback to")
	}

	// Update the deployment with the previous image
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("deployment has no containers")
	}

	currentImage := deployment.Spec.Template.Spec.Containers[0].Image
	if currentImage == previousImage {
		return fmt.Errorf("already at previous revision (image: %s)", currentImage)
	}

	deployment.Spec.Template.Spec.Containers[0].Image = previousImage

	// Add rollback annotation for audit trail
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations["enclii.dev/rollback-from"] = currentImage
	deployment.Spec.Template.Annotations["enclii.dev/rollback-at"] = metav1.Now().Format(time.RFC3339)

	_, err = c.Clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to rollback deployment: %w", err)
	}

	return nil
}

// ApplyNetworkPolicies parses multi-document YAML containing NetworkPolicy
// resources and applies each to the cluster via create-or-update. Returns the
// number of policies applied and any error. This replaces the previous pattern
// of committing NetworkPolicy YAML to git for ArgoCD sync.
func (c *Client) ApplyNetworkPolicies(ctx context.Context, namespace string, yamlBytes []byte) (int, error) {
	npClient := c.Clientset.NetworkingV1().NetworkPolicies(namespace)
	applied := 0

	// Split multi-document YAML
	docs := splitYAMLDocuments(yamlBytes)
	for _, doc := range docs {
		if len(doc) == 0 {
			continue
		}

		var np networkingv1.NetworkPolicy
		if err := yamlDecodeNetworkPolicy(doc, &np); err != nil {
			// Skip non-NetworkPolicy documents (comments, empty docs)
			continue
		}
		if np.Kind != "NetworkPolicy" && np.Kind != "" {
			continue
		}
		if np.Name == "" {
			continue
		}

		// Override namespace to match the target
		np.Namespace = namespace

		existing, err := npClient.Get(ctx, np.Name, metav1.GetOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return applied, fmt.Errorf("check existing NetworkPolicy %s: %w", np.Name, err)
			}
			// Create new
			if _, err := npClient.Create(ctx, &np, metav1.CreateOptions{}); err != nil {
				return applied, fmt.Errorf("create NetworkPolicy %s: %w", np.Name, err)
			}
		} else {
			// Update existing — preserve resourceVersion
			np.ResourceVersion = existing.ResourceVersion
			if _, err := npClient.Update(ctx, &np, metav1.UpdateOptions{}); err != nil {
				return applied, fmt.Errorf("update NetworkPolicy %s: %w", np.Name, err)
			}
		}
		applied++
	}

	return applied, nil
}

// splitYAMLDocuments splits a multi-document YAML byte slice on "---" separators.
func splitYAMLDocuments(data []byte) [][]byte {
	var docs [][]byte
	lines := strings.Split(string(data), "\n")
	var current []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if len(current) > 0 {
				docs = append(docs, []byte(strings.Join(current, "\n")))
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		docs = append(docs, []byte(strings.Join(current, "\n")))
	}
	return docs
}

// yamlDecodeNetworkPolicy decodes YAML bytes into a NetworkPolicy, skipping
// comment-only or empty documents gracefully.
func yamlDecodeNetworkPolicy(data []byte, np *networkingv1.NetworkPolicy) error {
	jsonData, err := sigsyaml.YAMLToJSON(data)
	if err != nil {
		return err
	}
	// Skip empty/null JSON documents
	s := strings.TrimSpace(string(jsonData))
	if s == "" || s == "null" || s == "{}" {
		return fmt.Errorf("empty document")
	}
	return json.Unmarshal(jsonData, np)
}

// resolveGVR maps an apiVersion + kind to a GroupVersionResource using the discovery API.
func resolveGVR(c *Client, apiVersion, kind string) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("parse apiVersion %q: %w", apiVersion, err)
	}

	resources, err := c.Clientset.Discovery().ServerResourcesForGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("discover resources for %s: %w", apiVersion, err)
	}

	for _, r := range resources.APIResources {
		if r.Kind == kind && !strings.Contains(r.Name, "/") {
			return schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: r.Name,
			}, nil
		}
	}

	return schema.GroupVersionResource{}, fmt.Errorf("no resource found for %s/%s", apiVersion, kind)
}

// getPreviousImage finds the image from the previous ReplicaSet revision
func (c *Client) getPreviousImage(ctx context.Context, deploymentName, namespace string, deployment *appsv1.Deployment) (string, error) {
	// List all ReplicaSets owned by this deployment
	rsList, err := c.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", deploymentName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list replica sets: %w", err)
	}

	if len(rsList.Items) < 2 {
		return "", fmt.Errorf("no previous revision available (only %d replica set(s) found)", len(rsList.Items))
	}

	// Find the current revision number
	currentRevision := deployment.Annotations["deployment.kubernetes.io/revision"]

	// Find the previous revision's ReplicaSet
	var previousRS *appsv1.ReplicaSet
	var previousRevision int64

	for i := range rsList.Items {
		rs := &rsList.Items[i]

		// Skip ReplicaSets not owned by this deployment
		isOwned := false
		for _, ownerRef := range rs.OwnerReferences {
			if ownerRef.UID == deployment.UID {
				isOwned = true
				break
			}
		}
		if !isOwned {
			continue
		}

		rsRevision := rs.Annotations["deployment.kubernetes.io/revision"]
		if rsRevision == currentRevision {
			continue // Skip current revision
		}

		// Parse revision number
		var rev int64
		_, _ = fmt.Sscanf(rsRevision, "%d", &rev)

		// Keep track of the highest revision that's not current
		if rev > previousRevision {
			previousRevision = rev
			previousRS = rs
		}
	}

	if previousRS == nil {
		return "", fmt.Errorf("could not find previous replica set")
	}

	// Get the image from the previous ReplicaSet
	if len(previousRS.Spec.Template.Spec.Containers) == 0 {
		return "", fmt.Errorf("previous replica set has no containers")
	}

	return previousRS.Spec.Template.Spec.Containers[0].Image, nil
}
