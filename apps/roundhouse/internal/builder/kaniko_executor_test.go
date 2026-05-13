package builder

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNewKanikoExecutor(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	cfg := &KanikoExecutorConfig{
		K8sClient:    client,
		Registry:     "ghcr.io/test",
		RegistryUser: "user",
		RegistryPass: "pass",
		GenerateSBOM: true,
		SignImages:   true,
		CosignKey:    "cosign-key",
		Timeout:      30 * time.Minute,
		CacheRepo:    "ghcr.io/test/cache",
	}

	executor := NewKanikoExecutor(cfg, logger, nil)

	if executor == nil {
		t.Fatal("expected executor to be created")
	}

	if executor.registry != "ghcr.io/test" {
		t.Errorf("expected registry 'ghcr.io/test', got '%s'", executor.registry)
	}

	if executor.cacheRepo != "ghcr.io/test/cache" {
		t.Errorf("expected cache repo 'ghcr.io/test/cache', got '%s'", executor.cacheRepo)
	}

	if !executor.generateSBOM {
		t.Error("expected generateSBOM to be true")
	}

	if !executor.signImages {
		t.Error("expected signImages to be true")
	}
}

func TestNewKanikoExecutor_DefaultCacheRepo(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	cfg := &KanikoExecutorConfig{
		K8sClient: client,
		Registry:  "ghcr.io/test",
		Timeout:   30 * time.Minute,
		// CacheRepo not specified - should default to registry + /cache
	}

	executor := NewKanikoExecutor(cfg, logger, nil)

	expectedCacheRepo := "ghcr.io/test/cache"
	if executor.cacheRepo != expectedCacheRepo {
		t.Errorf("expected default cache repo '%s', got '%s'", expectedCacheRepo, executor.cacheRepo)
	}
}

func TestBuildKanikoArgs(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient: client,
		Registry:  "ghcr.io/test",
		CacheRepo: "ghcr.io/test/cache",
		Timeout:   30 * time.Minute,
	}, logger, nil)

	job := &queue.BuildJob{
		ID:        uuid.New(),
		ReleaseID: uuid.New(),
		ServiceID: uuid.New(),
		ProjectID: uuid.New(),
		GitRepo:   "github.com/test/repo",
		GitSHA:    "abc12345678",
		GitBranch: "main",
		BuildConfig: queue.BuildConfig{
			Type:       "dockerfile",
			Dockerfile: "Dockerfile.prod",
			Context:    "src",
			BuildArgs: map[string]string{
				"GO_VERSION": "1.21",
			},
			Target: "production",
		},
	}

	imageTag := "ghcr.io/test/service:abc12345"
	args := executor.buildKanikoArgs(job, imageTag)

	// Verify critical args are present
	assertContains(t, args, "--dockerfile=Dockerfile.prod")
	assertContains(t, args, "--destination="+imageTag)
	assertContains(t, args, "--cache=true")
	assertContains(t, args, "--cache-repo=ghcr.io/test/cache")
	assertContains(t, args, "--reproducible")
	assertContains(t, args, "--build-arg=GO_VERSION=1.21")
	assertContains(t, args, "--build-arg=NPM_MADFAM_TOKEN")
	assertContains(t, args, "--target=production")

	// Verify context includes git info and subdirectory
	hasContext := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "--context=git://") && strings.Contains(arg, ":src") {
			hasContext = true
			break
		}
	}
	if !hasContext {
		t.Error("expected context with git:// prefix and :src subdirectory")
	}
}

func TestBuildKanikoArgs_Defaults(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient: client,
		Registry:  "ghcr.io/test",
		Timeout:   30 * time.Minute,
	}, logger, nil)

	job := &queue.BuildJob{
		ID:          uuid.New(),
		ReleaseID:   uuid.New(),
		ServiceID:   uuid.New(),
		ProjectID:   uuid.New(),
		GitRepo:     "github.com/test/repo",
		GitSHA:      "abc12345",
		GitBranch:   "main",
		BuildConfig: queue.BuildConfig{
			// Empty - use defaults
		},
	}

	imageTag := "ghcr.io/test/service:abc12345"
	args := executor.buildKanikoArgs(job, imageTag)

	// Should use default Dockerfile
	assertContains(t, args, "--dockerfile=Dockerfile")

	// Context should not have subdirectory suffix when Context is empty or "."
	for _, arg := range args {
		if strings.HasPrefix(arg, "--context=") && strings.HasSuffix(arg, ":.") {
			t.Error("context should not end with :. when using default context")
		}
	}
}

func TestGenerateImageTag(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient: client,
		Registry:  "ghcr.io/madfam-org",
		Timeout:   30 * time.Minute,
	}, logger, nil)

	projectID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	serviceID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

	job := &queue.BuildJob{
		ProjectID: projectID,
		ServiceID: serviceID,
		GitSHA:    "abc123456789abcd",
	}

	tag := executor.generateImageTag(job)

	// Should contain registry
	if !strings.HasPrefix(tag, "ghcr.io/madfam-org/") {
		t.Errorf("expected tag to start with registry, got '%s'", tag)
	}

	// Should contain short SHA (8 chars)
	if !strings.HasSuffix(tag, ":abc12345") {
		t.Errorf("expected tag to end with :abc12345, got '%s'", tag)
	}
}

func TestGenerateLatestTag(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient: client,
		Registry:  "ghcr.io/madfam-org",
		Timeout:   30 * time.Minute,
	}, logger, nil)

	job := &queue.BuildJob{
		ProjectID: uuid.New(),
		ServiceID: uuid.New(),
		GitSHA:    "abc123456789abcd",
	}

	tag := executor.generateLatestTag(job)

	if !strings.HasSuffix(tag, ":latest") {
		t.Errorf("expected tag to end with :latest, got '%s'", tag)
	}
}

func TestCreateBuildJob(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient:      client,
		Registry:       "ghcr.io/test",
		Timeout:        30 * time.Minute,
		GitCredentials: "git-credentials",
	}, logger, nil)

	buildJob := &queue.BuildJob{
		ID:        uuid.New(),
		ReleaseID: uuid.New(),
		ServiceID: uuid.New(),
		ProjectID: uuid.New(),
		GitRepo:   "github.com/test/repo",
		GitSHA:    "abc12345",
		GitBranch: "main",
		BuildConfig: queue.BuildConfig{
			Type: "dockerfile",
		},
	}

	ctx := context.Background()
	imageTag := "ghcr.io/test/service:abc12345"

	k8sJob, err := executor.createBuildJob(ctx, buildJob, imageTag)
	if err != nil {
		t.Fatalf("failed to create build job: %v", err)
	}

	// Verify job name
	expectedName := "build-" + buildJob.ID.String()[:8]
	if k8sJob.Name != expectedName {
		t.Errorf("expected job name '%s', got '%s'", expectedName, k8sJob.Name)
	}

	// Verify namespace
	if k8sJob.Namespace != KanikoBuildNamespace {
		t.Errorf("expected namespace '%s', got '%s'", KanikoBuildNamespace, k8sJob.Namespace)
	}

	// Verify labels
	if k8sJob.Labels[LabelBuildID] != buildJob.ID.String() {
		t.Errorf("expected build-id label '%s', got '%s'", buildJob.ID.String(), k8sJob.Labels[LabelBuildID])
	}

	// Verify security context
	// Kaniko MUST run as root (UID 0) to unpack container filesystem layers
	podSpec := k8sJob.Spec.Template.Spec
	if podSpec.SecurityContext == nil {
		t.Fatal("expected pod security context to be set")
	}
	if *podSpec.SecurityContext.RunAsNonRoot {
		t.Error("expected RunAsNonRoot to be false (Kaniko requires root to unpack layers)")
	}

	// Verify container
	if len(podSpec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(podSpec.Containers))
	}
	container := podSpec.Containers[0]
	if container.Image != KanikoImage {
		t.Errorf("expected image '%s', got '%s'", KanikoImage, container.Image)
	}
	if container.SecurityContext == nil {
		t.Fatal("expected container security context to be set")
	}
	if container.SecurityContext.Privileged == nil || *container.SecurityContext.Privileged {
		t.Error("expected Kaniko container to set privileged=false")
	}
	if container.SecurityContext.Capabilities == nil {
		t.Fatal("expected container capabilities to be set")
	}
	dropsAllCapabilities := false
	for _, capability := range container.SecurityContext.Capabilities.Drop {
		if capability == corev1.Capability("ALL") {
			dropsAllCapabilities = true
			break
		}
	}
	if !dropsAllCapabilities {
		t.Error("expected Kaniko container to drop ALL capabilities")
	}

	// Verify git credentials volume is added
	hasGitCreds := false
	for _, vol := range podSpec.Volumes {
		if vol.Name == "git-credentials" {
			hasGitCreds = true
			break
		}
	}
	if !hasGitCreds {
		t.Error("expected git-credentials volume to be present")
	}
}

func TestCreateBuildJob_NoGitCredentials(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient: client,
		Registry:  "ghcr.io/test",
		Timeout:   30 * time.Minute,
		// No GitCredentials
	}, logger, nil)

	buildJob := &queue.BuildJob{
		ID:        uuid.New(),
		ReleaseID: uuid.New(),
		ServiceID: uuid.New(),
		ProjectID: uuid.New(),
		GitRepo:   "github.com/test/repo",
		GitSHA:    "abc12345",
		GitBranch: "main",
	}

	ctx := context.Background()
	imageTag := "ghcr.io/test/service:abc12345"

	k8sJob, err := executor.createBuildJob(ctx, buildJob, imageTag)
	if err != nil {
		t.Fatalf("failed to create build job: %v", err)
	}

	// Verify git credentials volume is NOT added
	for _, vol := range k8sJob.Spec.Template.Spec.Volumes {
		if vol.Name == "git-credentials" {
			t.Error("expected git-credentials volume to NOT be present when GitCredentials is empty")
		}
	}
}

func TestBuildEnvVars(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	// Test with git credentials
	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient:      client,
		Registry:       "ghcr.io/test",
		Timeout:        30 * time.Minute,
		GitCredentials: "my-git-creds",
	}, logger, nil)

	job := &queue.BuildJob{
		ID:        uuid.New(),
		GitRepo:   "github.com/test/repo",
		GitSHA:    "abc12345",
		GitBranch: "main",
	}

	envVars := executor.buildEnvVars(job)

	// Should have GIT_TOKEN env var
	hasGitToken := false
	hasNPMToken := false
	for _, env := range envVars {
		if env.Name == "GIT_TOKEN" {
			hasGitToken = true
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Error("expected GIT_TOKEN to come from secret")
			}
			if env.ValueFrom.SecretKeyRef.Name != "my-git-creds" {
				t.Errorf("expected secret name 'my-git-creds', got '%s'", env.ValueFrom.SecretKeyRef.Name)
			}
		}
		if env.Name == "NPM_MADFAM_TOKEN" {
			hasNPMToken = true
			if env.ValueFrom == nil || env.ValueFrom.SecretKeyRef == nil {
				t.Error("expected NPM_MADFAM_TOKEN to come from secret")
			}
			if env.ValueFrom.SecretKeyRef.Name != "npm-madfam-token" {
				t.Errorf("expected secret name 'npm-madfam-token', got '%s'", env.ValueFrom.SecretKeyRef.Name)
			}
		}
	}
	if !hasGitToken {
		t.Error("expected GIT_TOKEN env var to be present")
	}
	if !hasNPMToken {
		t.Error("expected NPM_MADFAM_TOKEN env var to be present")
	}
}

func TestBuildEnvVars_NoGitCredentials(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	// Test without git credentials
	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient: client,
		Registry:  "ghcr.io/test",
		Timeout:   30 * time.Minute,
		// No GitCredentials
	}, logger, nil)

	job := &queue.BuildJob{
		ID:        uuid.New(),
		GitRepo:   "github.com/test/repo",
		GitSHA:    "abc12345",
		GitBranch: "main",
	}

	envVars := executor.buildEnvVars(job)

	hasNPMToken := false
	for _, env := range envVars {
		if env.Name == "GIT_TOKEN" {
			t.Error("expected GIT_TOKEN env var to NOT be present when GitCredentials is empty")
		}
		if env.Name == "NPM_MADFAM_TOKEN" {
			hasNPMToken = true
		}
	}
	if !hasNPMToken {
		t.Error("expected NPM_MADFAM_TOKEN env var to be present")
	}
}

func TestFailResult(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := fake.NewSimpleClientset()

	var logMessages []string
	logFunc := func(jobID uuid.UUID, line string) {
		logMessages = append(logMessages, line)
	}

	executor := NewKanikoExecutor(&KanikoExecutorConfig{
		K8sClient: client,
		Registry:  "ghcr.io/test",
		Timeout:   30 * time.Minute,
	}, logger, logFunc)

	result := &queue.BuildResult{
		JobID: uuid.New(),
	}
	startTime := time.Now().Add(-5 * time.Second)

	finalResult, err := executor.failResult(result, startTime, "test error: %s", "something failed")

	if finalResult.Success {
		t.Error("expected Success to be false")
	}

	if finalResult.ErrorMessage != "test error: something failed" {
		t.Errorf("expected error message 'test error: something failed', got '%s'", finalResult.ErrorMessage)
	}

	if finalResult.DurationSecs < 5 {
		t.Errorf("expected duration >= 5 seconds, got %f", finalResult.DurationSecs)
	}

	if err == nil {
		t.Error("expected error to be returned")
	}

	// Verify log was called
	if len(logMessages) == 0 {
		t.Error("expected log message to be recorded")
	}
}

// Helper function to check if a slice contains a string
func assertContains(t *testing.T, slice []string, item string) {
	t.Helper()
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Errorf("expected slice to contain '%s', got %v", item, slice)
}
