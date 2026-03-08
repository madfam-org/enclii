package reconciler

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// FunctionReconciler monitors and syncs serverless function status
type FunctionReconciler struct {
	repos         *db.Repositories
	k8sClient     *k8s.Client
	dynamicClient dynamic.Interface
	logger        *logrus.Logger
	stopCh        chan struct{}
	baseDomain    string // e.g., "fn.enclii.dev"
}

// KEDA HTTPScaledObject Group Version Resource (for HTTP add-on)
var kedaHTTPScaledObjectGVR = schema.GroupVersionResource{
	Group:    "http.keda.sh",
	Version:  "v1alpha1",
	Resource: "httpscaledobjects",
}

// NewFunctionReconciler creates a new function reconciler
func NewFunctionReconciler(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger, baseDomain string) *FunctionReconciler {
	// Create dynamic client for CRD operations
	dynamicClient, err := dynamic.NewForConfig(k8sClient.Config())
	if err != nil {
		logger.WithError(err).Error("Failed to create dynamic client for function reconciler")
	}

	if baseDomain == "" {
		baseDomain = "fn.enclii.dev"
	}

	return &FunctionReconciler{
		repos:         repos,
		k8sClient:     k8sClient,
		dynamicClient: dynamicClient,
		logger:        logger,
		stopCh:        make(chan struct{}),
		baseDomain:    baseDomain,
	}
}

// Start begins the function reconciliation loop
func (r *FunctionReconciler) Start(ctx context.Context) {
	r.logger.Info("Starting function reconciler")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial reconciliation
	r.reconcileAll(ctx)

	for {
		select {
		case <-ticker.C:
			r.reconcileAll(ctx)
		case <-r.stopCh:
			r.logger.Info("Function reconciler stopped")
			return
		case <-ctx.Done():
			r.logger.Info("Function reconciler context cancelled")
			return
		}
	}
}

// Stop gracefully shuts down the reconciler
func (r *FunctionReconciler) Stop() {
	close(r.stopCh)
}

// reconcileAll checks all functions that need reconciliation
func (r *FunctionReconciler) reconcileAll(ctx context.Context) {
	// Get all functions that need reconciliation (pending, building, deploying, deleting)
	statuses := []types.FunctionStatus{
		types.FunctionStatusPending,
		types.FunctionStatusBuilding,
		types.FunctionStatusDeploying,
		types.FunctionStatusDeleting,
	}

	for _, status := range statuses {
		functions, err := r.repos.Functions.ListByStatus(ctx, status)
		if err != nil {
			r.logger.WithError(err).WithField("status", status).Error("Failed to list functions")
			continue
		}

		for _, fn := range functions {
			r.reconcileFunction(ctx, fn)
		}
	}

	// Also check ready functions to update replica counts
	readyFunctions, err := r.repos.Functions.ListByStatus(ctx, types.FunctionStatusReady)
	if err == nil {
		for _, fn := range readyFunctions {
			r.updateFunctionReplicas(ctx, fn)
		}
	}
}

// reconcileFunction checks and updates a single function's status
func (r *FunctionReconciler) reconcileFunction(ctx context.Context, fn *types.Function) {
	logger := r.logger.WithFields(logrus.Fields{
		"function_id": fn.ID,
		"name":        fn.Name,
		"status":      fn.Status,
		"namespace":   fn.K8sNamespace,
	})

	switch fn.Status {
	case types.FunctionStatusPending:
		r.handlePendingFunction(ctx, fn, logger)
	case types.FunctionStatusBuilding:
		r.handleBuildingFunction(ctx, fn, logger)
	case types.FunctionStatusDeploying:
		r.handleDeployingFunction(ctx, fn, logger)
	case types.FunctionStatusDeleting:
		r.handleDeletingFunction(ctx, fn, logger)
	}
}

// handlePendingFunction starts the deployment process for a pending function
func (r *FunctionReconciler) handlePendingFunction(ctx context.Context, fn *types.Function, logger *logrus.Entry) {
	logger.Info("Processing pending function")

	// Get the project to determine namespace
	project, err := r.repos.Projects.GetByID(ctx, fn.ProjectID)
	if err != nil {
		logger.WithError(err).Error("Failed to get project for function")
		r.updateFunctionStatus(ctx, fn, types.FunctionStatusFailed, "Project not found")
		return
	}

	// Set namespace based on project (following service pattern)
	namespace := fmt.Sprintf("fn-%s", project.Slug)
	fn.K8sNamespace = namespace
	fn.K8sResourceName = fmt.Sprintf("fn-%s", fn.Name)

	// Ensure namespace exists
	if err := r.ensureNamespace(ctx, namespace); err != nil {
		logger.WithError(err).Error("Failed to create namespace")
		r.updateFunctionStatus(ctx, fn, types.FunctionStatusFailed, "Failed to create namespace")
		return
	}

	// If function has an image, deploy it; otherwise, mark for building
	if fn.ImageURI != "" {
		// Deploy directly
		if err := r.deployFunction(ctx, fn, logger); err != nil {
			logger.WithError(err).Error("Failed to deploy function")
			r.updateFunctionStatus(ctx, fn, types.FunctionStatusFailed, err.Error())
			return
		}
		r.updateFunctionStatus(ctx, fn, types.FunctionStatusDeploying, "Deploying function")
	} else {
		// Mark for building (will be picked up by Roundhouse)
		r.updateFunctionStatus(ctx, fn, types.FunctionStatusBuilding, "Waiting for build")
	}

	// Update K8s info in database
	if err := r.repos.Functions.Update(ctx, fn); err != nil {
		logger.WithError(err).Error("Failed to update function K8s info")
	}
}

// handleBuildingFunction checks if a function build is complete
func (r *FunctionReconciler) handleBuildingFunction(ctx context.Context, fn *types.Function, logger *logrus.Entry) {
	// Check if image URI has been set (by Roundhouse after build)
	if fn.ImageURI != "" {
		logger.Info("Build complete, starting deployment")
		if err := r.deployFunction(ctx, fn, logger); err != nil {
			logger.WithError(err).Error("Failed to deploy function after build")
			r.updateFunctionStatus(ctx, fn, types.FunctionStatusFailed, err.Error())
			return
		}
		r.updateFunctionStatus(ctx, fn, types.FunctionStatusDeploying, "Deploying function")
	}
	// Otherwise, still waiting for build - no action needed
}

// handleDeployingFunction checks if a function deployment is ready
func (r *FunctionReconciler) handleDeployingFunction(ctx context.Context, fn *types.Function, logger *logrus.Entry) {
	if fn.K8sNamespace == "" || fn.K8sResourceName == "" {
		logger.Warn("Function missing K8s info, skipping reconciliation")
		return
	}

	// Check deployment status
	deployment, err := r.k8sClient.Clientset.AppsV1().Deployments(fn.K8sNamespace).Get(
		ctx,
		fn.K8sResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Warn("Deployment not found, recreating")
			if err := r.deployFunction(ctx, fn, logger); err != nil {
				r.updateFunctionStatus(ctx, fn, types.FunctionStatusFailed, err.Error())
			}
		} else {
			logger.WithError(err).Error("Failed to get deployment")
		}
		return
	}

	// Check if deployment is ready
	if deployment.Status.ReadyReplicas > 0 || deployment.Status.AvailableReplicas > 0 {
		// Function is ready
		fn.AvailableReplicas = int(deployment.Status.AvailableReplicas)
		fn.Endpoint = fmt.Sprintf("https://%s.%s", fn.Name, r.baseDomain)

		now := time.Now()
		fn.DeployedAt = &now

		if err := r.repos.Functions.Update(ctx, fn); err != nil {
			logger.WithError(err).Error("Failed to update function")
			return
		}

		r.updateFunctionStatus(ctx, fn, types.FunctionStatusReady, "Function deployed and ready")
		logger.Info("Function is now ready")
	} else if deployment.Status.UnavailableReplicas > 0 {
		// Still deploying, check conditions for issues
		for _, cond := range deployment.Status.Conditions {
			if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
				r.updateFunctionStatus(ctx, fn, types.FunctionStatusFailed, cond.Message)
				return
			}
		}
	}
	// Scaled to zero is also a valid "ready" state for scale-to-zero functions
	if fn.Config.MinReplicas == 0 && deployment.Status.Replicas == 0 {
		fn.AvailableReplicas = 0
		fn.Endpoint = fmt.Sprintf("https://%s.%s", fn.Name, r.baseDomain)

		now := time.Now()
		fn.DeployedAt = &now

		if err := r.repos.Functions.Update(ctx, fn); err != nil {
			logger.WithError(err).Error("Failed to update function")
			return
		}

		r.updateFunctionStatus(ctx, fn, types.FunctionStatusReady, "Function deployed (scaled to zero)")
		logger.Info("Function is ready (scaled to zero)")
	}
}

// handleDeletingFunction cleans up K8s resources for a function
func (r *FunctionReconciler) handleDeletingFunction(ctx context.Context, fn *types.Function, logger *logrus.Entry) {
	logger.Info("Cleaning up function resources")

	if fn.K8sNamespace == "" || fn.K8sResourceName == "" {
		// No K8s resources to clean up
		if err := r.repos.Functions.Delete(ctx, fn.ID); err != nil {
			logger.WithError(err).Error("Failed to delete function from database")
		}
		return
	}

	// Delete HTTPScaledObject (KEDA)
	err := r.dynamicClient.Resource(kedaHTTPScaledObjectGVR).Namespace(fn.K8sNamespace).Delete(
		ctx,
		fn.K8sResourceName,
		metav1.DeleteOptions{},
	)
	if err != nil && !errors.IsNotFound(err) {
		logger.WithError(err).Warn("Failed to delete HTTPScaledObject")
	}

	// Delete Service
	err = r.k8sClient.Clientset.CoreV1().Services(fn.K8sNamespace).Delete(
		ctx,
		fn.K8sResourceName,
		metav1.DeleteOptions{},
	)
	if err != nil && !errors.IsNotFound(err) {
		logger.WithError(err).Warn("Failed to delete Service")
	}

	// Delete Deployment
	err = r.k8sClient.Clientset.AppsV1().Deployments(fn.K8sNamespace).Delete(
		ctx,
		fn.K8sResourceName,
		metav1.DeleteOptions{},
	)
	if err != nil && !errors.IsNotFound(err) {
		logger.WithError(err).Warn("Failed to delete Deployment")
	}

	// Hard delete from database
	if err := r.repos.Functions.Delete(ctx, fn.ID); err != nil {
		logger.WithError(err).Error("Failed to delete function from database")
		return
	}

	logger.Info("Function deleted successfully")
}

// updateFunctionReplicas updates the replica count for a ready function
func (r *FunctionReconciler) updateFunctionReplicas(ctx context.Context, fn *types.Function) {
	if fn.K8sNamespace == "" || fn.K8sResourceName == "" {
		return
	}

	deployment, err := r.k8sClient.Clientset.AppsV1().Deployments(fn.K8sNamespace).Get(
		ctx,
		fn.K8sResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		return
	}

	newReplicas := int(deployment.Status.AvailableReplicas)
	if fn.AvailableReplicas != newReplicas {
		if err := r.repos.Functions.UpdateReplicas(ctx, fn.ID, newReplicas); err != nil {
			r.logger.WithError(err).WithField("function_id", fn.ID).Warn("Failed to update replica count")
		}
	}
}

// deployFunction creates K8s resources for a function
func (r *FunctionReconciler) deployFunction(ctx context.Context, fn *types.Function, logger *logrus.Entry) error {
	// Create Deployment
	if err := r.createDeployment(ctx, fn); err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	// Create Service
	if err := r.createService(ctx, fn); err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// Create HTTPScaledObject for KEDA
	if err := r.createHTTPScaledObject(ctx, fn); err != nil {
		logger.WithError(err).Warn("Failed to create HTTPScaledObject (KEDA may not be installed)")
		// Don't fail - function can still work without scale-to-zero
	}

	return nil
}

// createDeployment creates a Kubernetes Deployment for the function
func (r *FunctionReconciler) createDeployment(ctx context.Context, fn *types.Function) error {
	// Parse resource limits
	memoryLimit := fn.Config.Memory
	if memoryLimit == "" {
		memoryLimit = types.FunctionDefaults.Memory
	}
	cpuLimit := fn.Config.CPU
	if cpuLimit == "" {
		cpuLimit = types.FunctionDefaults.CPU
	}

	replicas := int32(fn.Config.MinReplicas)
	if replicas < 0 {
		replicas = 0
	}

	labels := map[string]string{
		"app":                    fn.K8sResourceName,
		"enclii.dev/function":    fn.Name,
		"enclii.dev/function-id": fn.ID.String(),
	}

	// Build environment variables
	envVars := []corev1.EnvVar{
		{Name: "FUNCTION_NAME", Value: fn.Name},
		{Name: "FUNCTION_HANDLER", Value: fn.Config.Handler},
		{Name: "FUNCTION_TIMEOUT", Value: fmt.Sprintf("%d", fn.Config.Timeout)},
	}
	for _, env := range fn.Config.Env {
		envVars = append(envVars, corev1.EnvVar{
			Name:  env.Name,
			Value: env.Value,
		})
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.K8sResourceName,
			Namespace: fn.K8sNamespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "function",
							Image: fn.ImageURI,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: envVars,
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse(memoryLimit),
									corev1.ResourceCPU:    resource.MustParse(cpuLimit),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse(memoryLimit),
									corev1.ResourceCPU:    resource.MustParse(cpuLimit),
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 2,
								PeriodSeconds:       5,
								TimeoutSeconds:      3,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8080),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       10,
								TimeoutSeconds:      5,
							},
						},
					},
				},
			},
		},
	}

	_, err := r.k8sClient.Clientset.AppsV1().Deployments(fn.K8sNamespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing deployment
			_, err = r.k8sClient.Clientset.AppsV1().Deployments(fn.K8sNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
		}
	}
	return err
}

// createService creates a Kubernetes Service for the function
func (r *FunctionReconciler) createService(ctx context.Context, fn *types.Function) error {
	labels := map[string]string{
		"app":                    fn.K8sResourceName,
		"enclii.dev/function":    fn.Name,
		"enclii.dev/function-id": fn.ID.String(),
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fn.K8sResourceName,
			Namespace: fn.K8sNamespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	_, err := r.k8sClient.Clientset.CoreV1().Services(fn.K8sNamespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing service
			existing, getErr := r.k8sClient.Clientset.CoreV1().Services(fn.K8sNamespace).Get(ctx, fn.K8sResourceName, metav1.GetOptions{})
			if getErr == nil {
				service.ResourceVersion = existing.ResourceVersion
				service.Spec.ClusterIP = existing.Spec.ClusterIP
				_, err = r.k8sClient.Clientset.CoreV1().Services(fn.K8sNamespace).Update(ctx, service, metav1.UpdateOptions{})
			}
		}
	}
	return err
}

// createHTTPScaledObject creates a KEDA HTTPScaledObject for scale-to-zero
func (r *FunctionReconciler) createHTTPScaledObject(ctx context.Context, fn *types.Function) error {
	if r.dynamicClient == nil {
		return fmt.Errorf("dynamic client not available")
	}

	cooldownPeriod := fn.Config.CooldownPeriod
	if cooldownPeriod <= 0 {
		cooldownPeriod = types.FunctionDefaults.CooldownPeriod
	}

	maxReplicas := fn.Config.MaxReplicas
	if maxReplicas <= 0 {
		maxReplicas = types.FunctionDefaults.MaxReplicas
	}

	concurrency := fn.Config.Concurrency
	if concurrency <= 0 {
		concurrency = types.FunctionDefaults.Concurrency
	}

	httpScaledObject := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "http.keda.sh/v1alpha1",
			"kind":       "HTTPScaledObject",
			"metadata": map[string]interface{}{
				"name":      fn.K8sResourceName,
				"namespace": fn.K8sNamespace,
				"labels": map[string]interface{}{
					"enclii.dev/function":    fn.Name,
					"enclii.dev/function-id": fn.ID.String(),
				},
			},
			"spec": map[string]interface{}{
				"hosts": []interface{}{
					fmt.Sprintf("%s.%s", fn.Name, r.baseDomain),
				},
				"targetPendingRequests": concurrency,
				"scaledownPeriod":       cooldownPeriod,
				"scaleTargetRef": map[string]interface{}{
					"name":    fn.K8sResourceName,
					"service": fn.K8sResourceName,
					"port":    80,
				},
				"replicas": map[string]interface{}{
					"min": fn.Config.MinReplicas,
					"max": maxReplicas,
				},
			},
		},
	}

	_, err := r.dynamicClient.Resource(kedaHTTPScaledObjectGVR).Namespace(fn.K8sNamespace).Create(
		ctx,
		httpScaledObject,
		metav1.CreateOptions{},
	)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing
			existing, getErr := r.dynamicClient.Resource(kedaHTTPScaledObjectGVR).Namespace(fn.K8sNamespace).Get(
				ctx,
				fn.K8sResourceName,
				metav1.GetOptions{},
			)
			if getErr == nil {
				httpScaledObject.SetResourceVersion(existing.GetResourceVersion())
				_, err = r.dynamicClient.Resource(kedaHTTPScaledObjectGVR).Namespace(fn.K8sNamespace).Update(
					ctx,
					httpScaledObject,
					metav1.UpdateOptions{},
				)
			}
		}
	}
	return err
}

// ensureNamespace creates the namespace if it doesn't exist
func (r *FunctionReconciler) ensureNamespace(ctx context.Context, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"enclii.dev/type": "function",
			},
		},
	}

	_, err := r.k8sClient.Clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// updateFunctionStatus updates the function status in the database
func (r *FunctionReconciler) updateFunctionStatus(ctx context.Context, fn *types.Function, status types.FunctionStatus, message string) {
	if err := r.repos.Functions.UpdateStatus(ctx, fn.ID, status, message); err != nil {
		r.logger.WithError(err).WithField("function_id", fn.ID).Error("Failed to update function status")
	}
}
