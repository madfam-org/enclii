package builder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"go.uber.org/zap"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// =============================================================================
// Constants
// =============================================================================

const (
	// KanikoBuildNamespace is the namespace where build jobs run
	KanikoBuildNamespace = "enclii-builds"

	// KanikoImage is the container image for Kaniko executor
	KanikoImage = "gcr.io/kaniko-project/executor:v1.19.0"

	// SyftImage is the container image for SBOM generation
	SyftImage = "anchore/syft:v1.4.1"

	// CosignImage is the container image for image signing
	CosignImage = "gcr.io/projectsigstore/cosign:v2.2.3"

	// Labels for build jobs
	LabelBuildID   = "enclii.dev/build-id"
	LabelServiceID = "enclii.dev/service-id"
	LabelAppName   = "app.kubernetes.io/name"

	// Job types for post-build operations
	JobTypeSBOM    = "sbom"
	JobTypeSigning = "signing"
)

// =============================================================================
// Executor Types and Construction
// =============================================================================

// KanikoExecutor handles builds using Kubernetes Jobs with Kaniko
type KanikoExecutor struct {
	k8sClient      kubernetes.Interface
	registry       string
	registryUser   string
	registryPass   string
	generateSBOM   bool
	signImages     bool
	cosignKey      string
	timeout        time.Duration
	cacheRepo      string
	gitCredentials string // Secret name for git credentials
	logger         *zap.Logger
	logFunc        func(jobID uuid.UUID, line string)
}

// KanikoExecutorConfig configures the Kaniko executor
type KanikoExecutorConfig struct {
	K8sClient      kubernetes.Interface
	Registry       string
	RegistryUser   string
	RegistryPass   string
	GenerateSBOM   bool
	SignImages     bool
	CosignKey      string
	Timeout        time.Duration
	CacheRepo      string // Optional: registry path for layer caching
	GitCredentials string // Optional: secret name with git token
}

// NewKanikoExecutor creates a new Kaniko-based build executor
func NewKanikoExecutor(cfg *KanikoExecutorConfig, logger *zap.Logger, logFunc func(uuid.UUID, string)) *KanikoExecutor {
	cacheRepo := cfg.CacheRepo
	if cacheRepo == "" {
		cacheRepo = cfg.Registry + "/cache"
	}

	return &KanikoExecutor{
		k8sClient:      cfg.K8sClient,
		registry:       cfg.Registry,
		registryUser:   cfg.RegistryUser,
		registryPass:   cfg.RegistryPass,
		generateSBOM:   cfg.GenerateSBOM,
		signImages:     cfg.SignImages,
		cosignKey:      cfg.CosignKey,
		timeout:        cfg.Timeout,
		cacheRepo:      cacheRepo,
		gitCredentials: cfg.GitCredentials,
		logger:         logger,
		logFunc:        logFunc,
	}
}

// =============================================================================
// Build Execution
// =============================================================================

// Execute runs the build using a Kubernetes Job with Kaniko
func (e *KanikoExecutor) Execute(ctx context.Context, job *queue.BuildJob) (*queue.BuildResult, error) {
	startTime := time.Now()

	result := &queue.BuildResult{
		JobID:     job.ID,
		ReleaseID: job.ReleaseID,
	}

	e.log(job.ID, "📦 Starting Kaniko build for %s @ %s", job.GitRepo, job.GitSHA[:8])

	// Generate image tag
	imageTag := e.generateImageTag(job)
	result.ImageURI = imageTag

	// Create the Kubernetes Job
	k8sJob, err := e.createBuildJob(ctx, job, imageTag)
	if err != nil {
		return e.failResult(result, startTime, "failed to create build job: %v", err)
	}

	e.log(job.ID, "🚀 Created Kubernetes Job: %s", k8sJob.Name)

	// Watch for job completion
	err = e.watchJobCompletion(ctx, job.ID, k8sJob.Name)
	if err != nil {
		// Try to get logs before failing
		e.streamJobLogs(ctx, job.ID, k8sJob.Name)
		return e.failResult(result, startTime, "build failed: %v", err)
	}

	e.log(job.ID, "✅ Kaniko build completed successfully")

	// Get final logs
	e.streamJobLogs(ctx, job.ID, k8sJob.Name)

	// Get image digest from registry (post-push)
	// Note: Kaniko pushes directly, so we need to query the registry
	digest, err := e.getImageDigestFromRegistry(ctx, imageTag)
	if err != nil {
		e.logger.Warn("failed to get image digest", zap.Error(err))
	} else {
		result.ImageDigest = digest
	}

	// Generate SBOM (run as separate job if enabled)
	if e.generateSBOM {
		e.log(job.ID, "📋 Generating SBOM...")
		sbom, format, err := e.runSBOMGeneration(ctx, job.ID, imageTag)
		if err != nil {
			e.logger.Warn("failed to generate SBOM", zap.Error(err))
		} else {
			result.SBOM = sbom
			result.SBOMFormat = format
			e.log(job.ID, "✅ SBOM generated (%s)", format)
		}
	}

	// Sign image (run as separate job if enabled)
	if e.signImages && e.cosignKey != "" {
		e.log(job.ID, "🔐 Signing image...")
		signature, err := e.runImageSigning(ctx, job.ID, imageTag)
		if err != nil {
			e.logger.Warn("failed to sign image", zap.Error(err))
		} else {
			result.ImageSignature = signature
			e.log(job.ID, "✅ Image signed")
		}
	}

	result.Success = true
	result.DurationSecs = time.Since(startTime).Seconds()

	e.log(job.ID, "🎉 Build completed in %.1fs", result.DurationSecs)

	return result, nil
}

// =============================================================================
// Build Job Creation
// =============================================================================

// createBuildJob creates a Kubernetes Job for the Kaniko build
func (e *KanikoExecutor) createBuildJob(ctx context.Context, job *queue.BuildJob, imageTag string) (*batchv1.Job, error) {
	// Build Kaniko args
	args := e.buildKanikoArgs(job, imageTag)

	// Job configuration
	backoffLimit := int32(0)  // Don't retry failed builds
	ttlSeconds := int32(3600) // Clean up after 1 hour
	activeDeadlineSeconds := int64(e.timeout.Seconds())

	// Security context - Kaniko MUST run as root (UID 0) to unpack container filesystem layers.
	// When building images, Kaniko needs to create directories like /bin, /usr, etc. which
	// are owned by root. This is safe because Kaniko runs in an unprivileged container
	// (no elevated host capabilities), it just needs root within the container namespace.
	runAsNonRoot := false
	runAsUser := int64(0)
	runAsGroup := int64(0)
	fsGroup := int64(0)

	k8sJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("build-%s", job.ID.String()[:8]),
			Namespace: KanikoBuildNamespace,
			Labels: map[string]string{
				LabelBuildID:   job.ID.String(),
				LabelServiceID: job.ServiceID.String(),
				LabelAppName:   "kaniko-build",
			},
			Annotations: map[string]string{
				"enclii.dev/git-repo":   job.GitRepo,
				"enclii.dev/git-sha":    job.GitSHA,
				"enclii.dev/git-branch": job.GitBranch,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSeconds,
			ActiveDeadlineSeconds:   &activeDeadlineSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						LabelBuildID: job.ID.String(),
						LabelAppName: "kaniko-build",
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
						RunAsUser:    &runAsUser,
						RunAsGroup:   &runAsGroup,
						FSGroup:      &fsGroup,
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					// Avoid GPU nodes
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
								{
									Weight: 100,
									Preference: corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "nvidia.com/gpu",
												Operator: corev1.NodeSelectorOpDoesNotExist,
											},
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "kaniko",
							Image: KanikoImage,
							Args:  args,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("4"),
									corev1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged:               boolPtr(false),
								AllowPrivilegeEscalation: boolPtr(false),
								ReadOnlyRootFilesystem:   boolPtr(false), // Kaniko needs writable /kaniko
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "docker-config",
									MountPath: "/kaniko/.docker",
									ReadOnly:  true,
								},
							},
							Env: e.buildEnvVars(job),
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "docker-config",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "ghcr-credentials",
									Items: []corev1.KeyToPath{
										{
											Key:  ".dockerconfigjson",
											Path: "config.json",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Add git credentials volume if configured
	if e.gitCredentials != "" {
		k8sJob.Spec.Template.Spec.Volumes = append(k8sJob.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "git-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: e.gitCredentials,
				},
			},
		})
	}

	return e.k8sClient.BatchV1().Jobs(KanikoBuildNamespace).Create(ctx, k8sJob, metav1.CreateOptions{})
}

// buildKanikoArgs constructs the Kaniko executor arguments
func (e *KanikoExecutor) buildKanikoArgs(job *queue.BuildJob, imageTag string) []string {
	dockerfile := job.BuildConfig.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	contextPath := job.BuildConfig.Context
	if contextPath == "" {
		contextPath = "."
	}

	// Git context URL format: git://[repository]#[ref]#[commit-sha]
	// Strip https:// or http:// prefix from repo URL if present
	repoURL := job.GitRepo
	repoURL = strings.TrimPrefix(repoURL, "https://")
	repoURL = strings.TrimPrefix(repoURL, "http://")
	gitContext := fmt.Sprintf("git://%s#refs/heads/%s#%s",
		repoURL, job.GitBranch, job.GitSHA)

	// If context is a subdirectory, append to git context
	if contextPath != "." {
		gitContext = gitContext + ":" + contextPath
	}

	args := []string{
		"--dockerfile=" + dockerfile,
		"--context=" + gitContext,
		"--destination=" + imageTag,
		"--destination=" + e.generateLatestTag(job),
		// Layer caching
		"--cache=true",
		"--cache-repo=" + e.cacheRepo,
		"--cache-ttl=168h", // 7 days
		// Reproducibility
		"--reproducible",
		"--snapshot-mode=redo",
		// Build metadata
		"--label=org.opencontainers.image.source=" + job.GitRepo,
		"--label=org.opencontainers.image.revision=" + job.GitSHA,
		"--label=org.opencontainers.image.created=" + time.Now().UTC().Format(time.RFC3339),
		"--label=io.enclii.service-id=" + job.ServiceID.String(),
		"--label=io.enclii.release-id=" + job.ReleaseID.String(),
		// Verbosity
		"--verbosity=info",
	}

	// Add build args
	for key, value := range job.BuildConfig.BuildArgs {
		args = append(args, fmt.Sprintf("--build-arg=%s=%s", key, value))
	}

	// Add target if specified (multi-stage builds)
	if job.BuildConfig.Target != "" {
		args = append(args, "--target="+job.BuildConfig.Target)
	}

	return args
}

// buildEnvVars constructs environment variables for the build
func (e *KanikoExecutor) buildEnvVars(job *queue.BuildJob) []corev1.EnvVar {
	envVars := []corev1.EnvVar{}

	// Add git token if credentials secret exists
	if e.gitCredentials != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name: "GIT_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: e.gitCredentials,
					},
					Key:      "token",
					Optional: boolPtr(true),
				},
			},
		})
	}

	return envVars
}

// =============================================================================
// Image Tag Generation
// =============================================================================

// imageBaseName returns the project-scoped image path for a build job.
// Produces: {project-slug}/{service-name} when project slug is set,
// otherwise falls back to just {service-name} for backwards compatibility.
func (e *KanikoExecutor) imageBaseName(job *queue.BuildJob) string {
	if job.ProjectSlug != "" {
		return job.ProjectSlug + "/" + job.ServiceName
	}
	return job.ServiceName
}

// generateImageTag generates the full image tag
func (e *KanikoExecutor) generateImageTag(job *queue.BuildJob) string {
	shortSHA := job.GitSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	// Project-scoped image naming to prevent cross-customer collisions
	// Produces: ghcr.io/madfam-org/project-slug/service-name:abc12345
	return fmt.Sprintf("%s/%s:%s",
		e.registry,
		e.imageBaseName(job),
		shortSHA,
	)
}

// generateLatestTag generates the :latest tag variant
func (e *KanikoExecutor) generateLatestTag(job *queue.BuildJob) string {
	return fmt.Sprintf("%s/%s:latest",
		e.registry,
		e.imageBaseName(job),
	)
}

// =============================================================================
// Utility Functions
// =============================================================================

func (e *KanikoExecutor) log(jobID uuid.UUID, format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	e.logger.Info(line, zap.String("job_id", jobID.String()))
	if e.logFunc != nil {
		e.logFunc(jobID, line)
	}
}

func (e *KanikoExecutor) failResult(result *queue.BuildResult, startTime time.Time, format string, args ...interface{}) (*queue.BuildResult, error) {
	result.Success = false
	result.ErrorMessage = fmt.Sprintf(format, args...)
	result.DurationSecs = time.Since(startTime).Seconds()
	e.log(result.JobID, "❌ %s", result.ErrorMessage)
	return result, fmt.Errorf("%s", result.ErrorMessage)
}

// Helper functions
func boolPtr(b bool) *bool {
	return &b
}
