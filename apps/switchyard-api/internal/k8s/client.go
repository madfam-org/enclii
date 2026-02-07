package k8s

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

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
				},
			},
		}
		_, err = c.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create namespace %s: %w", namespace, err)
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
	return nil
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
		fmt.Sscanf(rsRevision, "%d", &rev)

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
