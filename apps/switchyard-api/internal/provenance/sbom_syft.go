package provenance

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Generator creates Software Bill of Materials (SBOM) from container images
type Generator struct {
	timeout time.Duration
}

// NewGenerator creates a new SBOM generator
func NewGenerator(timeout time.Duration) *Generator {
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	return &Generator{
		timeout: timeout,
	}
}

// Format represents SBOM output format
type Format string

const (
	FormatCycloneDXJSON Format = "cyclonedx-json"
	FormatSPDXJSON      Format = "spdx-json"
	FormatSyftJSON      Format = "syft-json"
)

// SBOM represents a software bill of materials
type SBOM struct {
	Format       Format    `json:"format"`
	Content      string    `json:"content"`
	GeneratedAt  time.Time `json:"generated_at"`
	ImageURI     string    `json:"image_uri"`
	PackageCount int       `json:"package_count,omitempty"`
}

// GenerateFromImage creates an SBOM from a container image using Syft
// imageURI: Full image URI (e.g., "ghcr.io/madfam/my-service:v1.0.0")
// format: SBOM format (cyclonedx-json, spdx-json, syft-json)
func (g *Generator) GenerateFromImage(ctx context.Context, imageURI string, format Format) (*SBOM, error) {
	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// Run Syft to generate SBOM
	// Example: syft packages docker:ghcr.io/madfam/my-service:v1.0.0 -o cyclonedx-json
	cmd := exec.CommandContext(timeoutCtx, "syft", "packages", fmt.Sprintf("docker:%s", imageURI), "-o", string(format))

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("syft failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run syft: %w", err)
	}

	// Parse package count from output (for metadata)
	packageCount := g.extractPackageCount(output, format)

	sbom := &SBOM{
		Format:       format,
		Content:      string(output),
		GeneratedAt:  time.Now().UTC(),
		ImageURI:     imageURI,
		PackageCount: packageCount,
	}

	return sbom, nil
}

// GenerateFromDirectory creates an SBOM from a filesystem directory
// This is useful for generating SBOMs before building the container image
func (g *Generator) GenerateFromDirectory(ctx context.Context, path string, format Format) (*SBOM, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// Run Syft on directory
	// Example: syft packages dir:/tmp/my-app -o cyclonedx-json
	cmd := exec.CommandContext(timeoutCtx, "syft", "packages", fmt.Sprintf("dir:%s", path), "-o", string(format))

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("syft failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run syft: %w", err)
	}

	packageCount := g.extractPackageCount(output, format)

	sbom := &SBOM{
		Format:       format,
		Content:      string(output),
		GeneratedAt:  time.Now().UTC(),
		ImageURI:     fmt.Sprintf("dir:%s", path),
		PackageCount: packageCount,
	}

	return sbom, nil
}

// ValidateSyftInstalled checks if Syft is installed and available
func (g *Generator) ValidateSyftInstalled() error {
	cmd := exec.Command("syft", "version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("syft is not installed or not in PATH. Install from: https://github.com/anchore/syft")
	}

	// Syft is installed
	_ = output // version output not currently used
	return nil
}

// extractPackageCount parses the SBOM to extract the number of packages
func (g *Generator) extractPackageCount(sbomData []byte, format Format) int {
	switch format {
	case FormatCycloneDXJSON:
		return g.extractCycloneDXPackageCount(sbomData)
	case FormatSPDXJSON:
		return g.extractSPDXPackageCount(sbomData)
	case FormatSyftJSON:
		return g.extractSyftPackageCount(sbomData)
	default:
		return 0
	}
}

// extractCycloneDXPackageCount extracts package count from CycloneDX JSON
func (g *Generator) extractCycloneDXPackageCount(data []byte) int {
	var parsed struct {
		Components []interface{} `json:"components"`
	}

	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0
	}

	return len(parsed.Components)
}

// extractSPDXPackageCount extracts package count from SPDX JSON
func (g *Generator) extractSPDXPackageCount(data []byte) int {
	var parsed struct {
		Packages []interface{} `json:"packages"`
	}

	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0
	}

	return len(parsed.Packages)
}

// extractSyftPackageCount extracts package count from Syft JSON
func (g *Generator) extractSyftPackageCount(data []byte) int {
	var parsed struct {
		Artifacts []interface{} `json:"artifacts"`
	}

	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0
	}

	return len(parsed.Artifacts)
}

// GetDefaultFormat returns the recommended SBOM format
// CycloneDX is widely supported and includes vulnerability data
func GetDefaultFormat() Format {
	return FormatCycloneDXJSON
}

// CompareVersions compares two SBOMs and returns differences
// This is useful for detecting supply chain changes between releases
func CompareVersions(oldSBOM, newSBOM *SBOM) (*SBOMDiff, error) {
	if oldSBOM.Format != newSBOM.Format {
		return nil, fmt.Errorf("cannot compare SBOMs with different formats: %s vs %s", oldSBOM.Format, newSBOM.Format)
	}

	diff := &SBOMDiff{
		OldPackageCount: oldSBOM.PackageCount,
		NewPackageCount: newSBOM.PackageCount,
		PackageDelta:    newSBOM.PackageCount - oldSBOM.PackageCount,
	}

	return diff, nil
}

// SBOMDiff represents differences between two SBOMs
type SBOMDiff struct {
	OldPackageCount int      `json:"old_package_count"`
	NewPackageCount int      `json:"new_package_count"`
	PackageDelta    int      `json:"package_delta"` // Positive = added packages, negative = removed
	AddedPackages   []string `json:"added_packages,omitempty"`
	RemovedPackages []string `json:"removed_packages,omitempty"`
}
