package reconciler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// PortSource indicates where the container port value was derived from
type PortSource string

const (
	PortSourceEncliiPort PortSource = "ENCLII_PORT"
	PortSourcePort       PortSource = "PORT"
	PortSourceDefault    PortSource = "default"
)

// parseContainerPort extracts and validates the container port from environment variables.
// Returns the port number (defaulting to 4200 per Enclii port allocation) and any validation error.
// Checks ENCLII_PORT first (Enclii convention), then falls back to PORT (industry standard).
func parseContainerPort(envVars map[string]string) (int32, error) {
	port, _, err := parseContainerPortWithSource(envVars)
	return port, err
}

// parseContainerPortWithSource extracts the container port and returns its source for logging.
// Returns: port number, source (ENCLII_PORT/PORT/default), and any validation error.
func parseContainerPortWithSource(envVars map[string]string) (int32, PortSource, error) {
	const defaultPort int32 = 4200
	const minPort = 1
	const maxPort = 65535

	// Check ENCLII_PORT first (Enclii convention)
	if portStr, ok := envVars["ENCLII_PORT"]; ok && portStr != "" {
		port, err := strconv.ParseInt(portStr, 10, 32)
		if err != nil {
			return defaultPort, PortSourceDefault, fmt.Errorf("invalid ENCLII_PORT value '%s': %w", portStr, err)
		}
		if port < minPort || port > maxPort {
			return defaultPort, PortSourceDefault, fmt.Errorf("ENCLII_PORT %d out of valid range (%d-%d)", port, minPort, maxPort)
		}
		return int32(port), PortSourceEncliiPort, nil
	}

	// Fallback to PORT (industry standard, used by Heroku, Railway, etc.)
	if portStr, ok := envVars["PORT"]; ok && portStr != "" {
		port, err := strconv.ParseInt(portStr, 10, 32)
		if err != nil {
			return defaultPort, PortSourceDefault, fmt.Errorf("invalid PORT value '%s': %w", portStr, err)
		}
		if port < minPort || port > maxPort {
			return defaultPort, PortSourceDefault, fmt.Errorf("PORT %d out of valid range (%d-%d)", port, minPort, maxPort)
		}
		return int32(port), PortSourcePort, nil
	}

	return defaultPort, PortSourceDefault, nil
}

// Helper function to parse Kubernetes resource quantities
func mustParseQuantity(s string) resource.Quantity {
	return resource.MustParse(s)
}

// buildResourceRequirements creates container resource requirements from config or defaults
func buildResourceRequirements(cfg *types.ResourceConfig) corev1.ResourceRequirements {
	// Default values
	cpuRequest := "100m"
	cpuLimit := "500m"
	memRequest := "128Mi"
	memLimit := "512Mi"

	if cfg != nil {
		if cfg.CPURequest != "" {
			cpuRequest = cfg.CPURequest
		}
		if cfg.CPULimit != "" {
			cpuLimit = cfg.CPULimit
		}
		if cfg.MemoryRequest != "" {
			memRequest = cfg.MemoryRequest
		}
		if cfg.MemoryLimit != "" {
			memLimit = cfg.MemoryLimit
		}
	}

	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    mustParseQuantity(cpuRequest),
			corev1.ResourceMemory: mustParseQuantity(memRequest),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    mustParseQuantity(cpuLimit),
			corev1.ResourceMemory: mustParseQuantity(memLimit),
		},
	}
}

// buildLivenessProbe creates a liveness probe from config or defaults
func buildLivenessProbe(cfg *types.HealthCheckConfig, containerPort int32) *corev1.Probe {
	// Check if probes are disabled
	if cfg != nil && cfg.Disabled {
		return nil
	}

	// Default values
	path := "/health"
	port := containerPort
	initialDelay := int32(30)
	timeout := int32(5)
	period := int32(10)
	failureThreshold := int32(3)

	if cfg != nil {
		if cfg.LivenessPath != "" {
			path = cfg.LivenessPath
		} else if cfg.Path != "" {
			path = cfg.Path
		}
		if cfg.Port > 0 {
			port = int32(cfg.Port)
		}
		if cfg.InitialDelaySeconds > 0 {
			initialDelay = int32(cfg.InitialDelaySeconds)
		}
		if cfg.TimeoutSeconds > 0 {
			timeout = int32(cfg.TimeoutSeconds)
		}
		if cfg.PeriodSeconds > 0 {
			period = int32(cfg.PeriodSeconds)
		}
		if cfg.FailureThreshold > 0 {
			failureThreshold = int32(cfg.FailureThreshold)
		}
	}

	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: path,
				Port: intstr.FromInt32(port),
			},
		},
		InitialDelaySeconds: initialDelay,
		TimeoutSeconds:      timeout,
		PeriodSeconds:       period,
		FailureThreshold:    failureThreshold,
	}
}

// buildReadinessProbe creates a readiness probe from config or defaults
func buildReadinessProbe(cfg *types.HealthCheckConfig, containerPort int32) *corev1.Probe {
	// Check if probes are disabled
	if cfg != nil && cfg.Disabled {
		return nil
	}

	// Default values
	path := "/health"
	port := containerPort
	initialDelay := int32(5)
	timeout := int32(3)
	period := int32(5)
	failureThreshold := int32(2)

	if cfg != nil {
		if cfg.ReadinessPath != "" {
			path = cfg.ReadinessPath
		} else if cfg.Path != "" {
			path = cfg.Path
		}
		if cfg.Port > 0 {
			port = int32(cfg.Port)
		}
		if cfg.InitialDelaySeconds > 0 {
			// For readiness, use a shorter initial delay if not explicitly set
			initialDelay = int32(cfg.InitialDelaySeconds)
		}
		if cfg.TimeoutSeconds > 0 {
			timeout = int32(cfg.TimeoutSeconds)
		}
		if cfg.PeriodSeconds > 0 {
			period = int32(cfg.PeriodSeconds)
		}
		if cfg.FailureThreshold > 0 {
			failureThreshold = int32(cfg.FailureThreshold)
		}
	}

	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: path,
				Port: intstr.FromInt32(port),
			},
		},
		InitialDelaySeconds: initialDelay,
		TimeoutSeconds:      timeout,
		PeriodSeconds:       period,
		FailureThreshold:    failureThreshold,
	}
}

// generateManifests creates Kubernetes Deployment and Service manifests for a service
func (r *ServiceReconciler) generateManifests(req *ReconcileRequest, namespace, secretName string) (*appsv1.Deployment, *corev1.Service, error) {
	labels := map[string]string{
		"app":                   req.Service.Name,
		"version":               req.Release.Version,
		"enclii.dev/service":    req.Service.Name,
		"enclii.dev/project":    req.Service.ProjectID.String(),
		"enclii.dev/release":    req.Release.ID.String(),
		"enclii.dev/deployment": req.Deployment.ID.String(),
		"enclii.dev/managed-by": "switchyard",
	}

	// Default configuration
	replicas := int32(1)

	// Determine the port to use (from ENCLII_PORT env var or default to 8080)
	containerPort, portErr := parseContainerPort(req.EnvVars)
	if portErr != nil {
		// Log the error but continue with default - this is a configuration issue
		logrus.WithFields(logrus.Fields{
			"service":      req.Service.Name,
			"enclii_port":  req.EnvVars["ENCLII_PORT"],
			"error":        portErr.Error(),
			"default_port": 8080,
		}).Warn("Invalid ENCLII_PORT value, using default port 4200")
	} else if _, ok := req.EnvVars["ENCLII_PORT"]; ok {
		logrus.WithFields(logrus.Fields{
			"service": req.Service.Name,
			"port":    containerPort,
		}).Info("Using ENCLII_PORT from environment variables")
	} else {
		logrus.WithFields(logrus.Fields{
			"service": req.Service.Name,
			"port":    containerPort,
		}).Debug("No ENCLII_PORT set, using default port 4200")
	}

	// Build environment variables
	var envVars []corev1.EnvVar

	// Add standard environment variables
	envVars = append(envVars, []corev1.EnvVar{
		{Name: "ENCLII_SERVICE_NAME", Value: req.Service.Name},
		{Name: "ENCLII_PROJECT_ID", Value: req.Service.ProjectID.String()},
		{Name: "ENCLII_RELEASE_VERSION", Value: req.Release.Version},
		{Name: "ENCLII_DEPLOYMENT_ID", Value: req.Deployment.ID.String()},
		{Name: "PORT", Value: strconv.Itoa(int(containerPort))}, // Use configured port
	}...)

	// Add user-defined environment variables (from database)
	// Secrets are referenced via K8s Secret, non-secrets are inline values
	hasSecrets := false
	if len(req.EnvVarsWithMeta) > 0 {
		// New path: use metadata-aware env vars
		for _, ev := range req.EnvVarsWithMeta {
			if ev.IsSecret {
				// Secret values are stored in K8s Secret, reference via secretKeyRef
				envVars = append(envVars, corev1.EnvVar{
					Name: ev.Key,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: ev.Key,
						},
					},
				})
				hasSecrets = true
			} else {
				// Non-secret values are inline
				envVars = append(envVars, corev1.EnvVar{
					Name:  ev.Key,
					Value: ev.Value,
				})
			}
		}
	} else {
		// Legacy path: all values inline (backwards compatibility)
		for key, value := range req.EnvVars {
			envVars = append(envVars, corev1.EnvVar{
				Name:  key,
				Value: value,
			})
		}
	}

	// Log secret injection status
	if hasSecrets {
		logrus.WithFields(logrus.Fields{
			"service":     req.Service.Name,
			"secret_name": secretName,
		}).Info("Injecting secrets via K8s Secret reference")
	}

	// Add database addon environment variables (injected from bindings)
	addonEnvVars := buildAddonEnvVars(req.AddonBindings)
	envVars = append(envVars, addonEnvVars...)

	var replicasPtr *int32
	if req.Service.Type != types.ServiceTypeFunction {
		replicasPtr = &replicas
	}

	// Create deployment manifest
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Service.Name,
			Namespace: namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"enclii.dev/git-sha":         req.Release.GitSHA,
				"enclii.dev/deployment-time": req.Deployment.CreatedAt.Format(time.RFC3339),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicasPtr,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":                req.Service.Name,
					"enclii.dev/service": req.Service.Name,
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
					MaxSurge:       &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"enclii.dev/git-sha": req.Release.GitSHA,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  req.Service.Name,
							Image: req.Release.ImageURI,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: containerPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env:            envVars,
							Resources:      buildResourceRequirements(req.Service.Resources),
							LivenessProbe:  buildLivenessProbe(req.Service.HealthCheck, containerPort),
							ReadinessProbe: buildReadinessProbe(req.Service.HealthCheck, containerPort),
							VolumeMounts:   buildVolumeMountsWithKubeconfig(req.Service.Volumes, req.EnvVars),
							SecurityContext: &corev1.SecurityContext{
								Privileged:               func() *bool { b := false; return &b }(),
								AllowPrivilegeEscalation: func() *bool { b := false; return &b }(),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
						},
					},
					// Security context - Kyverno requires non-root, drop all capabilities
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: func() *bool { b := true; return &b }(),
						RunAsUser:    func() *int64 { i := int64(1000); return &i }(),
						RunAsGroup:   func() *int64 { i := int64(1000); return &i }(),
						FSGroup:      func() *int64 { i := int64(1000); return &i }(),
					},
					// ImagePullSecrets for private registries (GHCR, etc.)
					ImagePullSecrets: []corev1.LocalObjectReference{
						{Name: "ghcr-credentials"},
					},
					Volumes:                       buildVolumesWithKubeconfig(req.Service.Volumes, req.Service.Name, req.EnvVars),
					RestartPolicy:                 corev1.RestartPolicyAlways,
					TerminationGracePeriodSeconds: &[]int64{30}[0],
				},
			},
		},
	}

	// Add Multi-Region Node Selector if explicitly defined
	if req.Service.Region != "" && req.Service.Region != "default" {
		deployment.Spec.Template.Spec.NodeSelector = map[string]string{
			"topology.kubernetes.io/region": req.Service.Region,
		}
	}

	// Create service manifest
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Service.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app":                req.Service.Name,
				"enclii.dev/service": req.Service.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt32(containerPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	return deployment, service, nil
}

// generateCronJobs creates Kubernetes CronJob manifests for the scheduled tasks defined in a service
func (r *ServiceReconciler) generateCronJobs(req *ReconcileRequest, namespace, secretName string) ([]*batchv1.CronJob, error) {
	if len(req.Service.Jobs) == 0 {
		return nil, nil
	}

	labels := map[string]string{
		"app":                   req.Service.Name,
		"version":               req.Release.Version,
		"enclii.dev/service":    req.Service.Name,
		"enclii.dev/project":    req.Service.ProjectID.String(),
		"enclii.dev/release":    req.Release.ID.String(),
		"enclii.dev/deployment": req.Deployment.ID.String(),
		"enclii.dev/managed-by": "switchyard",
	}

	// Build environment variables (same as deployment)
	var envVars []corev1.EnvVar
	envVars = append(envVars, []corev1.EnvVar{
		{Name: "ENCLII_SERVICE_NAME", Value: req.Service.Name},
		{Name: "ENCLII_PROJECT_ID", Value: req.Service.ProjectID.String()},
		{Name: "ENCLII_RELEASE_VERSION", Value: req.Release.Version},
		{Name: "ENCLII_DEPLOYMENT_ID", Value: req.Deployment.ID.String()},
	}...)

	if len(req.EnvVarsWithMeta) > 0 {
		for _, ev := range req.EnvVarsWithMeta {
			if ev.IsSecret {
				envVars = append(envVars, corev1.EnvVar{
					Name: ev.Key,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
							Key:                  ev.Key,
						},
					},
				})
			} else {
				envVars = append(envVars, corev1.EnvVar{
					Name:  ev.Key,
					Value: ev.Value,
				})
			}
		}
	} else {
		for key, value := range req.EnvVars {
			envVars = append(envVars, corev1.EnvVar{Name: key, Value: value})
		}
	}

	addonEnvVars := buildAddonEnvVars(req.AddonBindings)
	envVars = append(envVars, addonEnvVars...)

	var cronJobs []*batchv1.CronJob
	for _, job := range req.Service.Jobs {
		jobName := serviceManagedCronJobName(namespace, req.Service.Name, job.Name)
		// Ensure name is valid k8s name
		jobName = strings.ReplaceAll(jobName, "_", "-")
		if len(jobName) > 52 {
			jobName = jobName[:52]
		}

		var startingDeadlineSeconds *int64
		if job.Timeout > 0 {
			timeout := int64(job.Timeout)
			startingDeadlineSeconds = &timeout
		}

		var backoffLimit *int32
		if job.Retries > 0 {
			retries := int32(job.Retries)
			backoffLimit = &retries
		}

		cronJob := &batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
				Labels:    labels,
			},
			Spec: batchv1.CronJobSpec{
				Schedule: job.Schedule,
				TimeZone: func() *string {
					if job.Timezone != "" {
						return &job.Timezone
					} else {
						return nil
					}
				}(),
				StartingDeadlineSeconds: startingDeadlineSeconds,
				ConcurrencyPolicy:       batchv1.ForbidConcurrent,
				JobTemplate: batchv1.JobTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
					},
					Spec: batchv1.JobSpec{
						BackoffLimit: backoffLimit,
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: labels,
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:    jobName,
										Image:   req.Release.ImageURI,
										Command: job.Command,
										Env:     envVars,
										SecurityContext: &corev1.SecurityContext{
											Privileged:               func() *bool { b := false; return &b }(),
											AllowPrivilegeEscalation: func() *bool { b := false; return &b }(),
											Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
										},
									},
								},
								SecurityContext: &corev1.PodSecurityContext{
									RunAsNonRoot: func() *bool { b := true; return &b }(),
									RunAsUser:    func() *int64 { i := int64(1000); return &i }(),
									RunAsGroup:   func() *int64 { i := int64(1000); return &i }(),
									FSGroup:      func() *int64 { i := int64(1000); return &i }(),
								},
								ImagePullSecrets: []corev1.LocalObjectReference{{Name: "ghcr-credentials"}},
								RestartPolicy:    corev1.RestartPolicyNever,
							},
						},
					},
				},
			},
		}
		cronJobs = append(cronJobs, cronJob)
	}

	return cronJobs, nil
}

func serviceManagedCronJobName(namespace, serviceName, jobName string) string {
	if strings.HasPrefix(jobName, serviceName+"-") || strings.HasPrefix(jobName, namespace+"-") {
		return jobName
	}
	prefix := serviceName
	if serviceName == namespace || strings.HasPrefix(serviceName, namespace+"-") {
		prefix = namespace
	}
	return fmt.Sprintf("%s-%s", prefix, jobName)
}

// generateHTTPScaledObject creates a KEDA HTTPScaledObject for scale-to-zero functions
func (r *ServiceReconciler) generateHTTPScaledObject(req *ReconcileRequest, namespace string, containerPort int32) (*unstructured.Unstructured, error) {
	if req.Service.Type != types.ServiceTypeFunction {
		return nil, nil
	}

	hosts := make([]string, 0, len(req.CustomDomains))
	for _, domain := range req.CustomDomains {
		hosts = append(hosts, domain.Domain)
	}

	// Default to scaling to 10 if MaxReplicas is not set
	maxReplicas := 10
	minReplicas := 0

	if req.Service.MaxReplicas != nil {
		maxReplicas = *req.Service.MaxReplicas
	}
	if req.Service.MinReplicas != nil {
		minReplicas = *req.Service.MinReplicas
	}

	scaledObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "http.keda.sh/v1alpha1",
			"kind":       "HTTPScaledObject",
			"metadata": map[string]interface{}{
				"name":      req.Service.Name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					"app":                   req.Service.Name,
					"enclii.dev/service":    req.Service.Name,
					"enclii.dev/project":    req.Service.ProjectID.String(),
					"enclii.dev/managed-by": "switchyard",
				},
			},
			"spec": map[string]interface{}{
				"hosts": hosts,
				"scaleTargetRef": map[string]interface{}{
					"name":       req.Service.Name,
					"kind":       "Deployment",
					"apiVersion": "apps/v1",
					"service":    req.Service.Name,
					"port":       containerPort,
				},
				"replicas": map[string]interface{}{
					"min": minReplicas,
					"max": maxReplicas,
				},
			},
		},
	}

	return scaledObj, nil
}

// generateInterceptorService creates an ExternalName service pointing to the KEDA HTTP interceptor
func (r *ServiceReconciler) generateInterceptorService(req *ReconcileRequest, namespace string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-interceptor", req.Service.Name),
			Namespace: namespace,
			Labels: map[string]string{
				"app":                   req.Service.Name,
				"enclii.dev/service":    req.Service.Name,
				"enclii.dev/project":    req.Service.ProjectID.String(),
				"enclii.dev/managed-by": "switchyard",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "keda-add-ons-http-interceptor-proxy.keda.svc.cluster.local",
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt32(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
