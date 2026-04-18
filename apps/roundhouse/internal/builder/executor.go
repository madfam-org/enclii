package builder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/roundhouse/internal/queue"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/frameworks"
	"go.uber.org/zap"
)

// Executor handles the actual build execution
type Executor struct {
	workDir      string
	registry     string
	registryUser string
	registryPass string
	generateSBOM bool
	signImages   bool
	cosignKey    string
	timeout      time.Duration
	logger       *zap.Logger
	logFunc      func(jobID uuid.UUID, line string)
}

// ExecutorConfig configures the executor
type ExecutorConfig struct {
	WorkDir      string
	Registry     string
	RegistryUser string
	RegistryPass string
	GenerateSBOM bool
	SignImages   bool
	CosignKey    string
	Timeout      time.Duration
}

// NewExecutor creates a new build executor
func NewExecutor(cfg *ExecutorConfig, logger *zap.Logger, logFunc func(uuid.UUID, string)) *Executor {
	return &Executor{
		workDir:      cfg.WorkDir,
		registry:     cfg.Registry,
		registryUser: cfg.RegistryUser,
		registryPass: cfg.RegistryPass,
		generateSBOM: cfg.GenerateSBOM,
		signImages:   cfg.SignImages,
		cosignKey:    cfg.CosignKey,
		timeout:      cfg.Timeout,
		logger:       logger,
		logFunc:      logFunc,
	}
}

// Execute runs the build for a job
func (e *Executor) Execute(ctx context.Context, job *queue.BuildJob) (*queue.BuildResult, error) {
	startTime := time.Now()

	result := &queue.BuildResult{
		JobID:     job.ID,
		ReleaseID: job.ReleaseID,
	}

	// Create build directory
	buildDir := filepath.Join(e.workDir, job.ID.String())
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return e.failResult(result, startTime, "failed to create build directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(buildDir) }() // Clean up after build

	e.log(job.ID, "📦 Starting build for %s @ %s", job.GitRepo, job.GitSHA[:8])

	// Clone repository
	if err := e.cloneRepo(ctx, job, buildDir); err != nil {
		return e.failResult(result, startTime, "clone failed: %v", err)
	}

	// Detect framework slug up-front so the result carries it even if
	// subsequent build steps fail. Non-fatal — empty slug is acceptable.
	if fw := e.detectFrameworkSlug(buildDir, job.BuildConfig.Context); fw != "" {
		result.FrameworkSlug = fw
		e.log(job.ID, "🧭 Framework detected: %s", fw)
	}

	// Detect or use specified build type
	buildType := job.BuildConfig.Type
	if buildType == "auto" || buildType == "" {
		buildType = e.detectBuildType(buildDir, &job.BuildConfig)
	}

	e.log(job.ID, "🔧 Build type: %s", buildType)

	// Build image
	var imageURI string
	var err error

	switch buildType {
	case "dockerfile":
		imageURI, err = e.buildDockerfile(ctx, job, buildDir)
	case "buildpack":
		imageURI, err = e.buildBuildpack(ctx, job, buildDir)
	case "function":
		imageURI, err = e.buildFunction(ctx, job, buildDir)
	default:
		return e.failResult(result, startTime, "unsupported build type: %s", buildType)
	}

	if err != nil {
		return e.failResult(result, startTime, "build failed: %v", err)
	}

	result.ImageURI = imageURI
	e.log(job.ID, "✅ Image built: %s", imageURI)

	// Get image digest
	digest, err := e.getImageDigest(ctx, imageURI)
	if err != nil {
		e.logger.Warn("failed to get image digest", zap.Error(err))
	} else {
		result.ImageDigest = digest
	}

	// Get image size
	size, err := e.getImageSize(ctx, imageURI)
	if err != nil {
		e.logger.Warn("failed to get image size", zap.Error(err))
	} else {
		result.ImageSizeMB = size
	}

	// Generate SBOM
	if e.generateSBOM {
		e.log(job.ID, "📋 Generating SBOM...")
		sbom, format, err := e.generateSBOMForImage(ctx, imageURI)
		if err != nil {
			e.logger.Warn("failed to generate SBOM", zap.Error(err))
		} else {
			result.SBOM = sbom
			result.SBOMFormat = format
			e.log(job.ID, "✅ SBOM generated (%s)", format)
		}
	}

	// Sign image
	if e.signImages && e.cosignKey != "" {
		e.log(job.ID, "🔐 Signing image...")
		signature, err := e.signImage(ctx, imageURI)
		if err != nil {
			e.logger.Warn("failed to sign image", zap.Error(err))
		} else {
			result.ImageSignature = signature
			e.log(job.ID, "✅ Image signed")
		}
	}

	// Push to registry
	e.log(job.ID, "📤 Pushing to registry...")
	if err := e.pushImage(ctx, imageURI); err != nil {
		return e.failResult(result, startTime, "push failed: %v", err)
	}
	e.log(job.ID, "✅ Image pushed successfully")

	result.Success = true
	result.DurationSecs = time.Since(startTime).Seconds()

	e.log(job.ID, "🎉 Build completed in %.1fs", result.DurationSecs)

	return result, nil
}

func (e *Executor) cloneRepo(ctx context.Context, job *queue.BuildJob, buildDir string) error {
	e.log(job.ID, "📥 Cloning repository...")

	// Clone with depth 1 for efficiency
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--single-branch",
		"--branch", job.GitBranch, job.GitRepo, buildDir)

	if _, cloneErr := cmd.CombinedOutput(); cloneErr != nil {
		// If branch clone fails, try fetching specific SHA
		e.log(job.ID, "Branch clone failed, trying SHA fetch...")

		cmd = exec.CommandContext(ctx, "git", "clone", job.GitRepo, buildDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("clone failed: %s", string(output))
		}
	}

	// Checkout specific SHA
	cmd = exec.CommandContext(ctx, "git", "-C", buildDir, "checkout", job.GitSHA)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("checkout failed: %s", string(output))
	}

	e.log(job.ID, "✅ Repository cloned at %s", job.GitSHA[:8])
	return nil
}

// detectFrameworkSlug runs the shared framework detector against the
// cloned source tree. Returns the empty string when detection fails.
//
// ctxPath is BuildConfig.Context (the monorepo subdir) — "" means
// detect at repo root.
func (e *Executor) detectFrameworkSlug(buildDir, ctxPath string) string {
	root := buildDir
	if ctxPath != "" && ctxPath != "." {
		root = filepath.Join(buildDir, ctxPath)
	}

	// Gather top-level file names.
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	files := make([]string, 0, len(entries))
	for _, ent := range entries {
		if !ent.IsDir() {
			files = append(files, ent.Name())
		}
	}

	// Load optional content for refinement.
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return ""
		}
		return string(b)
	}

	packageJSONRaw := read("package.json")
	goMod := read("go.mod")
	cargoToml := read("Cargo.toml")
	requirements := read("requirements.txt")
	pyproject := read("pyproject.toml")
	gemfile := read("Gemfile")
	mixExs := read("mix.exs")

	fw := frameworks.DetectFromContents(
		files, packageJSONRaw, goMod, cargoToml,
		requirements, pyproject, gemfile, mixExs,
	)
	if fw == nil || fw.Slug == "unknown" {
		return ""
	}
	return fw.Slug
}

func (e *Executor) detectBuildType(buildDir string, config *queue.BuildConfig) string {
	// Check for functions/ directory first (serverless functions)
	if IsFunctionBuild(buildDir) {
		return "function"
	}

	// Check for Dockerfile
	dockerfilePath := config.Dockerfile
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	contextPath := config.Context
	if contextPath == "" {
		contextPath = "."
	}

	// Build the full path to the Dockerfile, considering context
	// If dockerfile is a relative name (not a path) and context is not root,
	// look for it inside the context directory
	searchPath := dockerfilePath
	if contextPath != "." && !filepath.IsAbs(dockerfilePath) && !strings.Contains(dockerfilePath, "/") {
		searchPath = filepath.Join(contextPath, dockerfilePath)
	}

	fullPath := filepath.Join(buildDir, searchPath)
	if _, err := os.Stat(fullPath); err == nil {
		config.Dockerfile = dockerfilePath
		return "dockerfile"
	}

	// Check for common buildpack indicators
	indicators := []string{
		"package.json",     // Node.js
		"requirements.txt", // Python
		"Gemfile",          // Ruby
		"go.mod",           // Go
		"pom.xml",          // Java Maven
		"build.gradle",     // Java Gradle
	}

	for _, indicator := range indicators {
		if _, err := os.Stat(filepath.Join(buildDir, indicator)); err == nil {
			return "buildpack"
		}
	}

	return "dockerfile" // Default
}

func (e *Executor) buildDockerfile(ctx context.Context, job *queue.BuildJob, buildDir string) (string, error) {
	dockerfile := job.BuildConfig.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	contextPath := job.BuildConfig.Context
	if contextPath == "" {
		contextPath = "."
	}

	// If dockerfile is a relative name (not a path) and context is not root,
	// we need to prefix the dockerfile with the context path so Docker can find it.
	// Example: context="apps/waybill", dockerfile="Dockerfile" -> "apps/waybill/Dockerfile"
	dockerfilePath := dockerfile
	if contextPath != "." && !filepath.IsAbs(dockerfile) && !strings.Contains(dockerfile, "/") {
		dockerfilePath = filepath.Join(contextPath, dockerfile)
	}

	imageTag := e.generateImageTag(job)

	args := []string{
		"build",
		"-t", imageTag,
		"-f", dockerfilePath,
	}

	// Add build args
	for key, value := range job.BuildConfig.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	// Add target if specified
	if job.BuildConfig.Target != "" {
		args = append(args, "--target", job.BuildConfig.Target)
	}

	// Add labels
	args = append(args,
		"--label", fmt.Sprintf("org.opencontainers.image.revision=%s", job.GitSHA),
		"--label", fmt.Sprintf("org.opencontainers.image.source=%s", job.GitRepo),
		"--label", fmt.Sprintf("io.enclii.service-id=%s", job.ServiceID.String()),
		"--label", fmt.Sprintf("io.enclii.release-id=%s", job.ReleaseID.String()),
	)

	args = append(args, contextPath)

	e.log(job.ID, "🐳 Building Dockerfile: %s (context: %s)", dockerfilePath, contextPath)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = buildDir

	return imageTag, e.runWithLogs(cmd, job.ID)
}

func (e *Executor) buildBuildpack(ctx context.Context, job *queue.BuildJob, buildDir string) (string, error) {
	imageTag := e.generateImageTag(job)

	builder := job.BuildConfig.Buildpack
	if builder == "" {
		builder = "heroku/builder:22" // Default builder
	}

	e.log(job.ID, "📦 Building with buildpack: %s", builder)

	cmd := exec.CommandContext(ctx, "pack", "build", imageTag,
		"--builder", builder,
		"--path", buildDir,
	)

	return imageTag, e.runWithLogs(cmd, job.ID)
}

func (e *Executor) generateImageTag(job *queue.BuildJob) string {
	shortSHA := job.GitSHA
	if len(shortSHA) > 8 {
		shortSHA = shortSHA[:8]
	}

	// Use human-readable service name instead of UUID prefixes
	// Produces: ghcr.io/madfam-org/service-name:abc12345
	return fmt.Sprintf("%s/%s:%s",
		e.registry,
		job.ServiceName,
		shortSHA,
	)
}

func (e *Executor) pushImage(ctx context.Context, imageURI string) error {
	// Login to registry if credentials provided
	if e.registryUser != "" && e.registryPass != "" {
		cmd := exec.CommandContext(ctx, "docker", "login", e.registry,
			"-u", e.registryUser, "--password-stdin")
		cmd.Stdin = strings.NewReader(e.registryPass)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("registry login failed: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "docker", "push", imageURI)
	return cmd.Run()
}

func (e *Executor) getImageDigest(ctx context.Context, imageURI string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Id}}", imageURI)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (e *Executor) getImageSize(ctx context.Context, imageURI string) (float64, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Size}}", imageURI)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var size int64
	_, _ = fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &size) // best-effort parse
	return float64(size) / (1024 * 1024), nil                         // Convert to MB
}

func (e *Executor) generateSBOMForImage(ctx context.Context, imageURI string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "syft", imageURI, "-o", "spdx-json")
	output, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	return string(output), "spdx-json", nil
}

func (e *Executor) signImage(ctx context.Context, imageURI string) (string, error) {
	cmd := exec.CommandContext(ctx, "cosign", "sign", "--key", e.cosignKey, imageURI)
	if err := cmd.Run(); err != nil {
		return "", err
	}

	// Get signature
	cmd = exec.CommandContext(ctx, "cosign", "triangulate", imageURI)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

func (e *Executor) runWithLogs(cmd *exec.Cmd, jobID uuid.UUID) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	// Stream stdout
	go e.streamOutput(jobID, stdout)
	// Stream stderr
	go e.streamOutput(jobID, stderr)

	return cmd.Wait()
}

func (e *Executor) streamOutput(jobID uuid.UUID, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		e.log(jobID, "%s", scanner.Text())
	}
}

func (e *Executor) log(jobID uuid.UUID, format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	e.logger.Info(line, zap.String("job_id", jobID.String()))
	if e.logFunc != nil {
		e.logFunc(jobID, line)
	}
}

func (e *Executor) failResult(result *queue.BuildResult, startTime time.Time, format string, args ...interface{}) (*queue.BuildResult, error) {
	result.Success = false
	result.ErrorMessage = fmt.Sprintf(format, args...)
	result.DurationSecs = time.Since(startTime).Seconds()
	e.log(result.JobID, "❌ %s", result.ErrorMessage)
	return result, fmt.Errorf("%s", result.ErrorMessage)
}
