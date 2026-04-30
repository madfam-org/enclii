package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provenance"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/signing"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// tracer — builder package emits spans under this tracer name.
var tracer = otel.Tracer("switchyard-api/builder")

// Service orchestrates the complete build process: clone → build → SBOM → sign → cleanup
type Service struct {
	git          *GitService
	builder      *BuildpacksBuilder
	buildCache   *BuildCache
	sbomGen      *provenance.Generator
	signer       *signing.Signer
	logger       *logrus.Logger
	workDir      string
	registry     string
	timeout      time.Duration
	generateSBOM bool // Enable/disable SBOM generation
	signImages   bool // Enable/disable image signing
	useCache     bool // Enable/disable build caching
}

type Config struct {
	WorkDir          string
	Registry         string
	RegistryUsername string
	RegistryPassword string
	CacheDir         string
	Timeout          time.Duration
	GenerateSBOM     bool       // Enable SBOM generation (requires Syft)
	SignImages       bool       // Enable image signing (requires Cosign)
	UseCache         bool       // Enable build layer caching
	CachePrefix      string     // Cache image prefix (e.g., "cache")
	R2Client         R2Uploader // R2 client for cache metadata storage
}

func NewService(cfg *Config, logger *logrus.Logger) *Service {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Minute
	}
	if cfg.CachePrefix == "" {
		cfg.CachePrefix = "build-cache"
	}

	sbomGenerator := provenance.NewGenerator(5 * time.Minute)
	imageSigner := signing.NewSigner(true, 2*time.Minute) // Keyless signing by default

	// Check if Syft is available (non-fatal if missing)
	generateSBOM := cfg.GenerateSBOM
	if generateSBOM {
		if err := sbomGenerator.ValidateSyftInstalled(); err != nil {
			logger.Warn("SBOM generation disabled: Syft not installed")
			logger.Warnf("  %v", err)
			generateSBOM = false
		} else {
			logger.Info("✓ SBOM generation enabled (Syft installed)")
		}
	}

	// Check if Cosign is available (non-fatal if missing)
	signImages := cfg.SignImages
	if signImages {
		if err := imageSigner.ValidateCosignInstalled(); err != nil {
			logger.Warn("Image signing disabled: Cosign not installed")
			logger.Warnf("  %v", err)
			signImages = false
		} else {
			logger.Info("✓ Image signing enabled (Cosign installed)")
		}
	}

	// Initialize build cache if enabled
	var buildCache *BuildCache
	useCache := cfg.UseCache
	if useCache && cfg.R2Client != nil {
		buildCache = NewBuildCache(cfg.Registry, cfg.CachePrefix, cfg.R2Client, cfg.CacheDir)
		logger.Info("✓ Build caching enabled (R2 storage configured)")
	} else if useCache {
		logger.Warn("Build caching disabled: R2 client not configured")
		useCache = false
	}

	builder := NewBuildpacksBuilder(cfg.Registry, cfg.RegistryUsername, cfg.RegistryPassword, cfg.CacheDir, cfg.Timeout)
	if buildCache != nil {
		builder.SetBuildCache(buildCache)
	}

	return &Service{
		git:          NewGitService(cfg.WorkDir),
		builder:      builder,
		buildCache:   buildCache,
		sbomGen:      sbomGenerator,
		signer:       imageSigner,
		logger:       logger,
		workDir:      cfg.WorkDir,
		registry:     cfg.Registry,
		timeout:      cfg.Timeout,
		generateSBOM: generateSBOM,
		signImages:   signImages,
		useCache:     useCache,
	}
}

type CompleteBuildResult struct {
	ImageURI      string
	GitSHA        string
	Success       bool
	Error         error
	Logs          []string
	Duration      time.Duration
	ClonePath     string
	SBOM          *provenance.SBOM          // Software Bill of Materials
	SBOMFormat    string              // e.g., "cyclonedx-json"
	SBOMGenerated bool                // Whether SBOM was successfully generated
	Signature     *signing.SignResult // Image signature information
	ImageSigned   bool                // Whether image was successfully signed
	CacheHit      bool                // Whether build used cached layers
	CacheImage    string              // Cache image URI used
}

// BuildFromGit clones a repository and builds it.
//
// A parent span `builder.BuildFromGit` wraps the whole flow; each of the
// four sub-steps (clone, build, sbom, sign) emits its own child span via
// nested tracer.Start() calls below. That gives Tempo's service-graph
// view a readable "what exactly is slow in a build" breakdown.
func (s *Service) BuildFromGit(ctx context.Context, service *types.Service, gitSHA string) *CompleteBuildResult {
	ctx, span := tracer.Start(ctx, "builder.BuildFromGit")
	defer span.End()
	span.SetAttributes(
		attribute.String("service.name", service.Name),
		attribute.String("git.sha_short", shortSHA(gitSHA)),
	)

	start := time.Now()
	result := &CompleteBuildResult{
		GitSHA: gitSHA,
		Logs:   []string{},
	}

	// Create a timeout context
	buildCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Step 1: Clone the repository
	result.Logs = append(result.Logs, fmt.Sprintf("Cloning repository: %s", service.GitRepo))
	cloneCtx, cloneSpan := tracer.Start(buildCtx, "builder.clone")
	cloneResult := s.git.CloneRepository(cloneCtx, service.GitRepo, gitSHA)
	cloneSpan.End()

	if !cloneResult.Success {
		result.Error = fmt.Errorf("clone failed: %w", cloneResult.Error)
		result.Logs = append(result.Logs, fmt.Sprintf("ERROR: %v", cloneResult.Error))
		span.SetStatus(codes.Error, "clone failed")
		span.RecordError(cloneResult.Error)
		return result
	}

	result.ClonePath = cloneResult.Path
	result.Logs = append(result.Logs, fmt.Sprintf("Successfully cloned to: %s", cloneResult.Path))

	// Ensure cleanup happens
	if cloneResult.CleanupFn != nil {
		defer func() {
			if cleanupErr := cloneResult.CleanupFn(); cleanupErr != nil {
				s.logger.Errorf("Failed to cleanup clone directory: %v", cleanupErr)
			}
		}()
	}

	// Step 2: Build the service
	result.Logs = append(result.Logs, fmt.Sprintf("Starting build for service: %s", service.Name))
	buildCtxSpan, buildSpan := tracer.Start(buildCtx, "builder.build")
	buildResult, err := s.builder.BuildService(buildCtxSpan, service, gitSHA, cloneResult.Path)
	buildSpan.End()

	if err != nil {
		result.Error = fmt.Errorf("build failed: %w", err)
		if buildResult != nil {
			result.Logs = append(result.Logs, buildResult.Logs...)
		}
		result.Logs = append(result.Logs, fmt.Sprintf("ERROR: %v", err))
		span.SetStatus(codes.Error, "build failed")
		span.RecordError(err)
		return result
	}

	// Success!
	result.ImageURI = buildResult.ImageURI
	result.Success = true
	result.CacheHit = buildResult.CacheHit
	result.CacheImage = buildResult.CacheImage
	result.Logs = append(result.Logs, buildResult.Logs...)

	// Step 3: Generate SBOM (if enabled)
	if s.generateSBOM {
		result.Logs = append(result.Logs, "Generating SBOM with Syft...")
		sbomCtx, sbomSpan := tracer.Start(buildCtx, "builder.sbom")
		sbomResult, err := s.sbomGen.GenerateFromImage(sbomCtx, result.ImageURI, provenance.GetDefaultFormat())
		sbomSpan.End()

		if err != nil {
			// SBOM generation failure is non-fatal - log warning and continue
			s.logger.Warnf("SBOM generation failed (non-fatal): %v", err)
			result.Logs = append(result.Logs, fmt.Sprintf("WARNING: SBOM generation failed: %v", err))
			result.SBOMGenerated = false
		} else {
			result.SBOM = sbomResult
			result.SBOMFormat = string(sbomResult.Format)
			result.SBOMGenerated = true
			result.Logs = append(result.Logs, fmt.Sprintf("✓ SBOM generated (%d packages found)", sbomResult.PackageCount))
		}
	}

	// Step 4: Sign image (if enabled)
	if s.signImages {
		result.Logs = append(result.Logs, "Signing image with Cosign...")
		signCtx, signSpan := tracer.Start(buildCtx, "builder.sign")
		signResult, err := s.signer.SignImage(signCtx, result.ImageURI)
		signSpan.End()

		if err != nil {
			// Image signing failure is non-fatal - log warning and continue
			s.logger.Warnf("Image signing failed (non-fatal): %v", err)
			result.Logs = append(result.Logs, fmt.Sprintf("WARNING: Image signing failed: %v", err))
			result.ImageSigned = false
		} else {
			result.Signature = signResult
			result.ImageSigned = true
			result.Logs = append(result.Logs, fmt.Sprintf("✓ Image signed (%s)", signResult.SigningMethod))
		}
	}

	result.Duration = time.Since(start)

	result.Logs = append(result.Logs, fmt.Sprintf("Build completed successfully in %v", result.Duration))
	result.Logs = append(result.Logs, fmt.Sprintf("Image: %s", result.ImageURI))

	span.SetAttributes(
		attribute.Bool("build.cache_hit", result.CacheHit),
		attribute.Bool("build.sbom_generated", result.SBOMGenerated),
		attribute.Bool("build.image_signed", result.ImageSigned),
		attribute.Float64("build.duration_seconds", result.Duration.Seconds()),
	)

	return result
}

// shortSHA returns the first 7 characters of a git SHA, or the whole
// value if it's already shorter. Used on spans so full SHAs (which can
// be 40 chars and leaky across UIs) don't bloat trace attributes.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// ValidateService checks if a service can be built
func (s *Service) ValidateService(ctx context.Context, service *types.Service) error {
	// Validate git repository
	if err := s.git.ValidateRepository(ctx, service.GitRepo); err != nil {
		return fmt.Errorf("invalid git repository: %w", err)
	}

	// Validate build tools
	if err := s.builder.ValidateTools(); err != nil {
		return fmt.Errorf("build tools not available: %w", err)
	}

	return nil
}

// GetBuildStatus returns information about the build environment
func (s *Service) GetBuildStatus() map[string]interface{} {
	status := map[string]interface{}{
		"work_dir": s.workDir,
		"registry": s.registry,
		"timeout":  s.timeout.String(),
	}

	// Check if tools are available
	if err := s.builder.ValidateTools(); err != nil {
		status["tools_available"] = false
		status["tools_error"] = err.Error()
	} else {
		status["tools_available"] = true
	}

	// Check if SBOM generation is enabled and available
	status["sbom_enabled"] = s.generateSBOM
	if s.generateSBOM {
		if err := s.sbomGen.ValidateSyftInstalled(); err != nil {
			status["sbom_available"] = false
			status["sbom_error"] = err.Error()
		} else {
			status["sbom_available"] = true
		}
	}

	// Check if image signing is enabled and available
	status["signing_enabled"] = s.signImages
	if s.signImages {
		if err := s.signer.ValidateCosignInstalled(); err != nil {
			status["signing_available"] = false
			status["signing_error"] = err.Error()
		} else {
			status["signing_available"] = true
		}
	}

	// Check if build caching is enabled
	status["cache_enabled"] = s.useCache
	if s.useCache && s.buildCache != nil {
		status["cache_available"] = true
	}

	return status
}

// GetCacheStats returns build cache performance statistics
func (s *Service) GetCacheStats(ctx context.Context, projectID string) (*CacheStats, error) {
	if s.buildCache == nil {
		return nil, fmt.Errorf("build cache not configured")
	}
	return s.buildCache.GetCacheStats(ctx, projectID)
}

// CleanupCache removes old cache entries for a project
func (s *Service) CleanupCache(ctx context.Context, projectID string, maxAge time.Duration) (int, error) {
	if s.buildCache == nil {
		return 0, fmt.Errorf("build cache not configured")
	}
	return s.buildCache.CleanupOldCaches(ctx, projectID, maxAge)
}
