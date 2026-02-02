package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ServiceReconciler manages the lifecycle of services in Kubernetes
type ServiceReconciler struct {
	k8sClient *k8s.Client
	logger    *logrus.Logger
}

// EnvVarWithMeta represents an environment variable with metadata for K8s secret creation
type EnvVarWithMeta struct {
	Key      string
	Value    string
	IsSecret bool
}

type ReconcileRequest struct {
	Service         *types.Service
	Release         *types.Release
	Deployment      *types.Deployment
	Environment     *types.Environment // The target environment with kube_namespace
	CustomDomains   []types.CustomDomain
	Routes          []types.Route
	EnvVars         map[string]string // User-defined environment variables (decrypted) - DEPRECATED: use EnvVarsWithMeta
	EnvVarsWithMeta []EnvVarWithMeta  // Environment variables with IsSecret metadata for proper K8s secret creation
	AddonBindings   []AddonBinding    // Database addon bindings for env var injection
}

// AddonBinding represents a database addon bound to this service
type AddonBinding struct {
	EnvVarName       string                  // e.g., "DATABASE_URL", "REDIS_URL"
	AddonType        types.DatabaseAddonType // postgres, redis, mysql
	K8sNamespace     string                  // Namespace where addon resources exist
	K8sResourceName  string                  // Name of the addon K8s resource
	ConnectionSecret string                  // K8s secret name with credentials (for postgres)
}

type ReconcileResult struct {
	Success    bool
	Message    string
	K8sObjects []string
	NextCheck  *time.Time
	Error      error
}

func NewServiceReconciler(k8sClient *k8s.Client, logger *logrus.Logger) *ServiceReconciler {
	return &ServiceReconciler{
		k8sClient: k8sClient,
		logger:    logger,
	}
}

// Reconcile ensures the desired state matches the actual state in Kubernetes
func (r *ServiceReconciler) Reconcile(ctx context.Context, req *ReconcileRequest) *ReconcileResult {
	logger := r.logger.WithFields(logrus.Fields{
		"service":    req.Service.Name,
		"release":    req.Release.Version,
		"deployment": req.Deployment.ID,
	})

	logger.Info("Starting service reconciliation")

	// Determine the Kubernetes namespace from the environment
	// The environment MUST have kube_namespace set - this is a data integrity requirement
	namespace := req.Environment.KubeNamespace
	if namespace == "" {
		// This is a data integrity issue - all environments should have kube_namespace set
		// during creation (via CreateEnvironment or auto-deploy)
		logger.Error("Environment has no kube_namespace set - this is a data integrity issue")
		return &ReconcileResult{
			Success: false,
			Message: "Environment has no kubernetes namespace configured",
			Error:   fmt.Errorf("missing kube_namespace for environment %s (ID: %s)", req.Environment.Name, req.Environment.ID),
		}
	}
	logger.WithField("namespace", namespace).Info("Using Kubernetes namespace for deployment")

	// Create namespace if it doesn't exist
	if err := r.ensureNamespace(ctx, namespace); err != nil {
		return &ReconcileResult{
			Success: false,
			Message: "Failed to ensure namespace",
			Error:   err,
		}
	}

	// Create PVCs if volumes are specified
	if len(req.Service.Volumes) > 0 {
		pvcs, err := r.generatePVCs(req, namespace)
		if err != nil {
			return &ReconcileResult{
				Success: false,
				Message: "Failed to generate PVCs",
				Error:   err,
			}
		}

		for _, pvc := range pvcs {
			if err := r.applyPVC(ctx, pvc); err != nil {
				return &ReconcileResult{
					Success: false,
					Message: fmt.Sprintf("Failed to apply PVC %s", pvc.Name),
					Error:   err,
				}
			}
		}
	}

	// Create K8s Secret for secret env vars (values not exposed in pod spec)
	secretName := fmt.Sprintf("%s-secrets", req.Service.Name)
	if err := r.ensureEnvSecret(ctx, req, namespace, secretName); err != nil {
		return &ReconcileResult{
			Success: false,
			Message: "Failed to create environment secrets",
			Error:   err,
		}
	}

	// Generate Kubernetes manifests
	deployment, service, err := r.generateManifests(req, namespace, secretName)
	if err != nil {
		return &ReconcileResult{
			Success: false,
			Message: "Failed to generate manifests",
			Error:   err,
		}
	}

	// Apply deployment
	if err := r.applyDeployment(ctx, deployment); err != nil {
		return &ReconcileResult{
			Success: false,
			Message: "Failed to apply deployment",
			Error:   err,
		}
	}

	// Apply service
	if err := r.applyService(ctx, service); err != nil {
		return &ReconcileResult{
			Success: false,
			Message: "Failed to apply service",
			Error:   err,
		}
	}

	// Apply Ingress if custom domains are configured
	k8sObjects := []string{
		fmt.Sprintf("deployment/%s", deployment.Name),
		fmt.Sprintf("service/%s", service.Name),
	}

	if len(req.CustomDomains) > 0 {
		ingress, err := r.generateIngress(req, namespace)
		if err != nil {
			return &ReconcileResult{
				Success: false,
				Message: "Failed to generate ingress",
				Error:   err,
			}
		}

		if err := r.applyIngress(ctx, ingress); err != nil {
			return &ReconcileResult{
				Success: false,
				Message: "Failed to apply ingress",
				Error:   err,
			}
		}

		k8sObjects = append(k8sObjects, fmt.Sprintf("ingress/%s", ingress.Name))
	}

	// Generate and apply NetworkPolicies for service isolation
	networkPolicies, err := r.generateNetworkPolicies(req, namespace)
	if err != nil {
		return &ReconcileResult{
			Success: false,
			Message: "Failed to generate network policies",
			Error:   err,
		}
	}

	for _, np := range networkPolicies {
		if err := r.applyNetworkPolicy(ctx, np); err != nil {
			return &ReconcileResult{
				Success: false,
				Message: fmt.Sprintf("Failed to apply network policy %s", np.Name),
				Error:   err,
			}
		}
		k8sObjects = append(k8sObjects, fmt.Sprintf("networkpolicy/%s", np.Name))
	}

	// Wait for deployment to be ready
	ready, err := r.waitForDeploymentReady(ctx, deployment.Namespace, deployment.Name, 5*time.Minute)
	if err != nil {
		return &ReconcileResult{
			Success: false,
			Message: "Failed to wait for deployment readiness",
			Error:   err,
		}
	}

	if !ready {
		nextCheck := time.Now().Add(30 * time.Second)
		return &ReconcileResult{
			Success:   false,
			Message:   "Deployment not ready, will retry",
			NextCheck: &nextCheck,
		}
	}

	logger.Info("Service reconciliation completed successfully")

	return &ReconcileResult{
		Success:    true,
		Message:    "Service deployed successfully",
		K8sObjects: k8sObjects,
	}
}

func (r *ServiceReconciler) ensureNamespace(ctx context.Context, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"managed-by": "enclii",
				"platform":   "enclii",
			},
		},
	}

	created := false
	_, err := r.k8sClient.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create namespace %s: %w", namespace, err)
		}
	} else {
		created = true
		r.logger.WithField("namespace", namespace).Info("Created new namespace")
	}

	// GUARDRAIL: Copy registry credentials to new namespaces
	// This ensures pods can pull images from private registries (GHCR)
	if err := r.ensureRegistryCredentials(ctx, namespace); err != nil {
		// Log but don't fail - the credential check in triggerAutoDeploy is the primary guardrail
		r.logger.WithFields(logrus.Fields{
			"namespace": namespace,
			"created":   created,
		}).WithError(err).Warn("Failed to ensure registry credentials in namespace")
	}

	// Ensure customer namespace has ResourceQuota, LimitRange, and NetworkPolicy
	if err := r.ensureResourceQuota(ctx, namespace); err != nil {
		r.logger.WithField("namespace", namespace).WithError(err).Warn("Failed to ensure ResourceQuota")
	}
	if err := r.ensureLimitRange(ctx, namespace); err != nil {
		r.logger.WithField("namespace", namespace).WithError(err).Warn("Failed to ensure LimitRange")
	}
	if err := r.ensureNamespaceNetworkPolicy(ctx, namespace); err != nil {
		r.logger.WithField("namespace", namespace).WithError(err).Warn("Failed to ensure namespace NetworkPolicy")
	}

	return nil
}

// ensureRegistryCredentials copies the registry credentials secret to the target namespace if missing
func (r *ServiceReconciler) ensureRegistryCredentials(ctx context.Context, targetNamespace string) error {
	const secretName = "ghcr-credentials" // #nosec G101 -- secret reference name, not a credential
	const sourceNamespace = "enclii"

	secretClient := r.k8sClient.Clientset.CoreV1().Secrets(targetNamespace)

	// Check if secret already exists
	_, err := secretClient.Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return nil // Already exists
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check for registry credentials: %w", err)
	}

	// Get source secret
	sourceClient := r.k8sClient.Clientset.CoreV1().Secrets(sourceNamespace)
	sourceSecret, err := sourceClient.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			r.logger.WithField("source_namespace", sourceNamespace).Warn("Source registry credentials not found - skipping copy")
			return nil // Source doesn't exist, nothing to copy
		}
		return fmt.Errorf("failed to get source registry credentials: %w", err)
	}

	// Create copy in target namespace
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: targetNamespace,
			Labels: map[string]string{
				"enclii.dev/managed-by":  "switchyard-reconciler",
				"enclii.dev/copied-from": sourceNamespace,
			},
		},
		Type: sourceSecret.Type,
		Data: sourceSecret.Data,
	}

	_, err = secretClient.Create(ctx, newSecret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil // Race condition - another process created it
		}
		return fmt.Errorf("failed to create registry credentials: %w", err)
	}

	r.logger.WithFields(logrus.Fields{
		"namespace": targetNamespace,
		"secret":    secretName,
	}).Info("Copied registry credentials to namespace")

	return nil
}

func (r *ServiceReconciler) applyDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	deploymentClient := r.k8sClient.Clientset.AppsV1().Deployments(deployment.Namespace)

	// Try to get existing deployment
	existing, err := deploymentClient.Get(ctx, deployment.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new deployment
			_, err = deploymentClient.Create(ctx, deployment, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create deployment: %w", err)
			}
			r.logger.WithField("deployment", deployment.Name).Info("Created new deployment")
			return nil
		}
		return fmt.Errorf("failed to get existing deployment: %w", err)
	}

	// Update existing deployment - preserve the immutable selector
	// Kubernetes doesn't allow changing spec.selector on existing deployments
	deployment.ResourceVersion = existing.ResourceVersion
	deployment.Spec.Selector = existing.Spec.Selector

	// Also ensure pod template labels match the selector (required by k8s)
	// Preserve selector labels in pod template while adding our metadata labels
	for key, value := range existing.Spec.Selector.MatchLabels {
		deployment.Spec.Template.Labels[key] = value
	}

	_, err = deploymentClient.Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment: %w", err)
	}
	r.logger.WithField("deployment", deployment.Name).Info("Updated existing deployment")
	return nil
}

func (r *ServiceReconciler) applyService(ctx context.Context, service *corev1.Service) error {
	serviceClient := r.k8sClient.Clientset.CoreV1().Services(service.Namespace)

	// Try to get existing service
	existing, err := serviceClient.Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new service
			_, err = serviceClient.Create(ctx, service, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create service: %w", err)
			}
			r.logger.WithField("service", service.Name).Info("Created new service")
			return nil
		}
		return fmt.Errorf("failed to get existing service: %w", err)
	}

	// Update existing service (preserve cluster IP and selector)
	// Service selectors should generally match what the deployment is using
	service.ResourceVersion = existing.ResourceVersion
	service.Spec.ClusterIP = existing.Spec.ClusterIP

	// Preserve the existing selector to match the deployment's pods
	// Only use our new selector for new services
	if len(existing.Spec.Selector) > 0 {
		service.Spec.Selector = existing.Spec.Selector
	}

	_, err = serviceClient.Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update service: %w", err)
	}
	r.logger.WithField("service", service.Name).Info("Updated existing service")
	return nil
}

// ensureEnvSecret creates or updates a K8s Secret containing secret env vars
// This ensures sensitive values are not exposed in Pod specs and are stored encrypted in etcd
func (r *ServiceReconciler) ensureEnvSecret(ctx context.Context, req *ReconcileRequest, namespace, secretName string) error {
	// Collect secret values
	secretData := make(map[string][]byte)

	if len(req.EnvVarsWithMeta) > 0 {
		for _, ev := range req.EnvVarsWithMeta {
			if ev.IsSecret {
				secretData[ev.Key] = []byte(ev.Value)
			}
		}
	}

	// If no secrets, skip creating the secret
	if len(secretData) == 0 {
		r.logger.WithField("service", req.Service.Name).Debug("No secrets to inject, skipping K8s Secret creation")
		return nil
	}

	// Create K8s Secret resource
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":                   req.Service.Name,
				"enclii.dev/service":    req.Service.Name,
				"enclii.dev/project":    req.Service.ProjectID.String(),
				"enclii.dev/managed-by": "switchyard",
			},
			Annotations: map[string]string{
				"enclii.dev/deployment-id": req.Deployment.ID.String(),
				"enclii.dev/updated":       time.Now().Format(time.RFC3339),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}

	// Apply the secret (create or update)
	secretClient := r.k8sClient.Clientset.CoreV1().Secrets(namespace)

	existing, err := secretClient.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			_, err = secretClient.Create(ctx, secret, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create secret: %w", err)
			}
			r.logger.WithFields(logrus.Fields{
				"service":    req.Service.Name,
				"secret":     secretName,
				"keys_count": len(secretData),
			}).Info("Created K8s Secret for env vars")
			return nil
		}
		return fmt.Errorf("failed to get existing secret: %w", err)
	}

	// Update existing secret
	secret.ResourceVersion = existing.ResourceVersion
	_, err = secretClient.Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret: %w", err)
	}
	r.logger.WithFields(logrus.Fields{
		"service":    req.Service.Name,
		"secret":     secretName,
		"keys_count": len(secretData),
	}).Info("Updated K8s Secret for env vars")

	return nil
}

func (r *ServiceReconciler) waitForDeploymentReady(ctx context.Context, namespace, name string, timeout time.Duration) (bool, error) {
	deploymentClient := r.k8sClient.Clientset.AppsV1().Deployments(namespace)
	podClient := r.k8sClient.Clientset.CoreV1().Pods(namespace)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
			deployment, err := deploymentClient.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			// Check if deployment is ready
			if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas &&
				deployment.Status.UpdatedReplicas == *deployment.Spec.Replicas {
				return true, nil
			}

			// GUARDRAIL: Check for fatal pod conditions that won't self-heal
			// This provides early failure detection for issues like missing credentials
			pods, err := podClient.List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", name),
			})
			if err == nil && len(pods.Items) > 0 {
				for _, pod := range pods.Items {
					if fatalErr := r.checkPodForFatalErrors(&pod); fatalErr != nil {
						r.logger.WithFields(logrus.Fields{
							"namespace":  namespace,
							"deployment": name,
							"pod":        pod.Name,
							"error":      fatalErr.Error(),
						}).Error("Pod has fatal error that won't self-heal")
						return false, fatalErr
					}
				}
			}

			time.Sleep(5 * time.Second)
		}
	}
}

// checkPodForFatalErrors examines a pod for conditions that indicate permanent failure
// Returns an error describing the fatal condition, or nil if the pod might still recover
func (r *ServiceReconciler) checkPodForFatalErrors(pod *corev1.Pod) error {
	// Check container statuses for fatal errors
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			message := cs.State.Waiting.Message

			switch reason {
			case "ImagePullBackOff", "ErrImagePull":
				// Image pull failures are typically due to missing credentials or non-existent images
				// Check if it's a credentials issue (401/403)
				if strings.Contains(message, "401") || strings.Contains(message, "unauthorized") ||
					strings.Contains(message, "403") || strings.Contains(message, "forbidden") {
					return fmt.Errorf("image pull failed due to missing registry credentials: %s - ensure ghcr-credentials secret exists in namespace", message)
				}
				if strings.Contains(message, "not found") || strings.Contains(message, "manifest unknown") {
					return fmt.Errorf("image not found: %s - verify the image exists and tag is correct", message)
				}
				// After multiple backoffs, treat as fatal
				if cs.RestartCount > 0 || strings.Contains(reason, "BackOff") {
					return fmt.Errorf("image pull failed: %s - check registry credentials and image availability", message)
				}

			case "InvalidImageName":
				return fmt.Errorf("invalid image name: %s", message)

			case "CreateContainerConfigError":
				// Usually indicates missing secrets or configmaps
				if strings.Contains(message, "secret") {
					return fmt.Errorf("container config error - missing secret: %s", message)
				}
				return fmt.Errorf("container config error: %s", message)
			}
		}

		// Check for crash loops that indicate code/config issues
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
			if cs.RestartCount >= 5 {
				return fmt.Errorf("container in CrashLoopBackOff after %d restarts - check application logs", cs.RestartCount)
			}
		}
	}

	return nil
}

// Rollback rolls back a deployment to the previous version
func (r *ServiceReconciler) Rollback(ctx context.Context, namespace, serviceName string) error {
	deploymentClient := r.k8sClient.Clientset.AppsV1().Deployments(namespace)

	// Get the deployment
	deployment, err := deploymentClient.Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Trigger rollback by updating the rollback annotation
	if deployment.Annotations == nil {
		deployment.Annotations = make(map[string]string)
	}
	deployment.Annotations["deployment.kubernetes.io/revision"] = "0"

	_, err = deploymentClient.Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to trigger rollback: %w", err)
	}

	r.logger.WithFields(logrus.Fields{
		"namespace":  namespace,
		"deployment": serviceName,
	}).Info("Triggered deployment rollback")

	return nil
}

// Delete removes all Kubernetes resources for a service
func (r *ServiceReconciler) Delete(ctx context.Context, namespace, serviceName string) error {
	// Delete deployment
	deploymentClient := r.k8sClient.Clientset.AppsV1().Deployments(namespace)
	err := deploymentClient.Delete(ctx, serviceName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete deployment: %w", err)
	}

	// Delete service
	serviceClient := r.k8sClient.Clientset.CoreV1().Services(namespace)
	err = serviceClient.Delete(ctx, serviceName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	// Delete PVCs associated with this service
	pvcClient := r.k8sClient.Clientset.CoreV1().PersistentVolumeClaims(namespace)
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("enclii.dev/service=%s", serviceName),
	}
	pvcList, err := pvcClient.List(ctx, listOptions)
	if err != nil && !errors.IsNotFound(err) {
		r.logger.WithError(err).Warn("Failed to list PVCs for deletion")
	} else if pvcList != nil {
		for _, pvc := range pvcList.Items {
			err = pvcClient.Delete(ctx, pvc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				r.logger.WithFields(logrus.Fields{
					"pvc": pvc.Name,
				}).WithError(err).Warn("Failed to delete PVC")
			}
		}
	}

	// Delete NetworkPolicies associated with this service
	npClient := r.k8sClient.Clientset.NetworkingV1().NetworkPolicies(namespace)
	npList, err := npClient.List(ctx, listOptions)
	if err != nil && !errors.IsNotFound(err) {
		r.logger.WithError(err).Warn("Failed to list NetworkPolicies for deletion")
	} else if npList != nil {
		for _, np := range npList.Items {
			err = npClient.Delete(ctx, np.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				r.logger.WithFields(logrus.Fields{
					"networkpolicy": np.Name,
				}).WithError(err).Warn("Failed to delete NetworkPolicy")
			}
		}
	}

	r.logger.WithFields(logrus.Fields{
		"namespace": namespace,
		"service":   serviceName,
	}).Info("Deleted service resources")

	return nil
}

// GetPodEnvVars retrieves environment variables from a running pod
func (r *ServiceReconciler) GetPodEnvVars(ctx context.Context, namespace, podName string) (map[string]string, error) {
	podClient := r.k8sClient.Clientset.CoreV1().Pods(namespace)

	pod, err := podClient.Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	envVars := make(map[string]string)

	// Extract env vars from all containers
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			// Skip vars that reference secrets or configmaps (can't read the actual value)
			if env.ValueFrom != nil {
				continue
			}
			envVars[env.Name] = env.Value
		}
	}

	return envVars, nil
}
