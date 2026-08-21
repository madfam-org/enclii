package spec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"gopkg.in/yaml.v3"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (p *Parser) ParseServiceSpec(path string) (*types.ServiceSpec, error) {
	return p.ParseServiceSpecNamed(path, "")
}

// ParseServiceSpecNamed reads a service spec from a manifest that may contain
// more than one YAML document.
//
// The onboard-standard `enclii.yaml` is a two-document file — `kind: Project`
// followed by `kind: Service` — but the loader only ever decoded the first
// document. On such a manifest it therefore validated the Project document as
// if it were a Service and failed with "service spec validation failed",
// which is what forced domains to be wired by hand for kalya and crea-map.
//
// Selection rules:
//   - every document in the file is decoded;
//   - documents that are not `kind: Service` (notably `kind: Project`) are
//     skipped, as are wholly empty documents;
//   - when serviceName is non-empty it selects the Service document whose
//     metadata.name matches;
//   - a single Service document is used regardless of serviceName, so
//     single-document service.yaml behavior is unchanged;
//   - several Service documents with no disambiguating serviceName is an
//     error that lists the candidates.
//
// Only the selected document is validated, so a Project document alongside it
// can never fail Service validation.
func (p *Parser) ParseServiceSpecNamed(path, serviceName string) (*types.ServiceSpec, error) {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read service spec file: %w", err)
	}

	spec, err := selectServiceDocument(data, serviceName)
	if err != nil {
		return nil, err
	}

	// Use current working directory as project root for validation
	// This allows spec files to be placed anywhere while paths remain relative to project root
	projectDir, err := os.Getwd()
	if err != nil {
		projectDir = filepath.Dir(path)
	}

	// Validate the spec
	if err := p.ValidateServiceSpec(spec, projectDir); err != nil {
		return nil, fmt.Errorf("service spec validation failed: %w", err)
	}

	return spec, nil
}

// selectServiceDocument decodes every YAML document in data and returns the
// one Service document to validate. See ParseServiceSpecNamed for the rules.
func selectServiceDocument(data []byte, serviceName string) (*types.ServiceSpec, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))

	var services []types.ServiceSpec
	// firstDoc preserves the single-document error behavior: a lone document
	// that is not a Service must still be validated (and fail) rather than
	// being silently skipped into a "no Service document" error.
	var firstDoc *types.ServiceSpec
	docCount := 0

	for {
		var doc types.ServiceSpec
		if err := decoder.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to parse service spec YAML: %w", err)
		}
		docCount++

		// Skip wholly empty documents (e.g. a trailing `---`).
		if doc.APIVersion == "" && doc.Kind == "" && doc.Metadata.Name == "" {
			continue
		}
		if firstDoc == nil {
			docCopy := doc
			firstDoc = &docCopy
		}
		// Skip non-Service documents (kind: Project in the standard
		// two-document enclii.yaml).
		if doc.Kind != "" && doc.Kind != "Service" {
			continue
		}
		services = append(services, doc)
	}

	if docCount == 0 {
		return nil, fmt.Errorf("failed to parse service spec YAML: file contains no YAML documents")
	}

	switch len(services) {
	case 0:
		// No Service document. For a single-document file, hand the document
		// back so validation produces the same message it always did.
		if firstDoc != nil && docCount == 1 {
			return firstDoc, nil
		}
		if firstDoc == nil {
			return nil, fmt.Errorf("failed to parse service spec YAML: file contains no YAML documents")
		}
		return nil, fmt.Errorf("no 'kind: Service' document found in manifest (found %d document(s))", docCount)
	case 1:
		// Exactly one Service document: use it whether or not a name was
		// requested, matching prior single-document behavior.
		selected := services[0]
		if serviceName != "" && selected.Metadata.Name != serviceName {
			return nil, fmt.Errorf("service %q not found in manifest (contains service %q)", serviceName, selected.Metadata.Name)
		}
		return &selected, nil
	}

	// Several Service documents: require a name to disambiguate.
	names := make([]string, 0, len(services))
	for _, svc := range services {
		names = append(names, svc.Metadata.Name)
	}
	if serviceName == "" {
		return nil, fmt.Errorf("manifest contains %d services (%s) — pass --service to select one", len(services), strings.Join(names, ", "))
	}
	for i := range services {
		if services[i].Metadata.Name == serviceName {
			return &services[i], nil
		}
	}
	return nil, fmt.Errorf("service %q not found in manifest (contains: %s)", serviceName, strings.Join(names, ", "))
}

func (p *Parser) ValidateServiceSpec(spec *types.ServiceSpec, projectDir string) error {
	var errors []ValidationError

	// Validate API version
	if spec.APIVersion == "" {
		errors = append(errors, ValidationError{
			Field:   "apiVersion",
			Message: "is required",
		})
	} else if spec.APIVersion != "enclii.dev/v1alpha" && spec.APIVersion != "enclii.dev/v1" {
		errors = append(errors, ValidationError{
			Field:   "apiVersion",
			Message: "must be 'enclii.dev/v1alpha' or 'enclii.dev/v1'",
		})
	}

	// Validate kind
	if spec.Kind == "" {
		errors = append(errors, ValidationError{
			Field:   "kind",
			Message: "is required",
		})
	} else if spec.Kind != "Service" {
		errors = append(errors, ValidationError{
			Field:   "kind",
			Message: "must be 'Service'",
		})
	}

	// Validate metadata
	if err := p.validateMetadata(&spec.Metadata); err != nil {
		if ve, ok := err.(ValidationError); ok {
			errors = append(errors, ve)
		} else {
			errors = append(errors, ValidationError{Field: "metadata", Message: err.Error()})
		}
	}

	// Validate spec
	if err := p.validateSpec(&spec.Spec, projectDir); err != nil {
		if ve, ok := err.(ValidationError); ok {
			errors = append(errors, ve)
		} else {
			errors = append(errors, ValidationError{Field: "spec", Message: err.Error()})
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation errors: %v", errors)
	}

	return nil
}

func (p *Parser) validateMetadata(metadata *types.ServiceMetadata) error {
	if metadata.Name == "" {
		return ValidationError{Field: "metadata.name", Message: "is required"}
	}

	// Validate name format (DNS-compatible)
	if !isValidDNSName(metadata.Name) {
		return ValidationError{
			Field:   "metadata.name",
			Message: "must be a valid DNS name (lowercase letters, numbers, and hyphens only)",
		}
	}

	if metadata.Project == "" {
		return ValidationError{Field: "metadata.project", Message: "is required"}
	}

	if !isValidDNSName(metadata.Project) {
		return ValidationError{
			Field:   "metadata.project",
			Message: "must be a valid DNS name (lowercase letters, numbers, and hyphens only)",
		}
	}

	return nil
}

func (p *Parser) validateSpec(spec *types.ServiceSpecConfig, projectDir string) error {
	// Validate build configuration
	if err := p.validateBuildSpec(&spec.Build, projectDir); err != nil {
		return err
	}

	// Validate runtime configuration
	if err := p.validateRuntimeSpec(&spec.Runtime); err != nil {
		return err
	}

	// Validate environment variables
	if err := p.validateEnvVars(spec.Env); err != nil {
		return err
	}

	return nil
}

func (p *Parser) validateBuildSpec(build *types.BuildSpec, projectDir string) error {
	validTypes := []string{"auto", "dockerfile", "buildpack"}
	validType := false
	for _, t := range validTypes {
		if build.Type == t {
			validType = true
			break
		}
	}

	if !validType {
		return ValidationError{
			Field:   "spec.build.type",
			Message: fmt.Sprintf("must be one of: %s", strings.Join(validTypes, ", ")),
		}
	}

	// If dockerfile is specified, check if it exists
	if build.Type == "dockerfile" && build.Dockerfile != "" {
		dockerfilePath := filepath.Join(projectDir, build.Dockerfile)
		if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
			return ValidationError{
				Field:   "spec.build.dockerfile",
				Message: fmt.Sprintf("file does not exist: %s", build.Dockerfile),
			}
		}
	}

	// Auto-detect validation
	if build.Type == "auto" {
		if err := p.validateAutoDetection(projectDir); err != nil {
			return ValidationError{
				Field:   "spec.build.type",
				Message: fmt.Sprintf("auto-detection failed: %v", err),
			}
		}
	}

	return nil
}

func (p *Parser) validateRuntimeSpec(runtime *types.RuntimeSpec) error {
	// Validate port
	if runtime.Port <= 0 || runtime.Port > 65535 {
		return ValidationError{
			Field:   "spec.runtime.port",
			Message: "must be between 1 and 65535",
		}
	}

	// Validate replicas
	if runtime.Replicas < 0 {
		return ValidationError{
			Field:   "spec.runtime.replicas",
			Message: "must be greater than or equal to 0",
		}
	}

	if runtime.Replicas > 100 {
		return ValidationError{
			Field:   "spec.runtime.replicas",
			Message: "must be less than or equal to 100 (for safety)",
		}
	}

	// Validate health check path
	if runtime.HealthCheck != "" && !strings.HasPrefix(runtime.HealthCheck, "/") {
		return ValidationError{
			Field:   "spec.runtime.healthCheck",
			Message: "must start with '/' if specified",
		}
	}

	return nil
}

func (p *Parser) validateEnvVars(envVars []types.EnvVar) error {
	seenNames := make(map[string]bool)

	for i, env := range envVars {
		if env.Name == "" {
			return ValidationError{
				Field:   fmt.Sprintf("spec.env[%d].name", i),
				Message: "is required",
			}
		}

		// Check for duplicates
		if seenNames[env.Name] {
			return ValidationError{
				Field:   fmt.Sprintf("spec.env[%d].name", i),
				Message: fmt.Sprintf("duplicate environment variable: %s", env.Name),
			}
		}
		seenNames[env.Name] = true

		// Validate environment variable name format
		if !isValidEnvVarName(env.Name) {
			return ValidationError{
				Field:   fmt.Sprintf("spec.env[%d].name", i),
				Message: "must contain only uppercase letters, numbers, and underscores",
			}
		}
	}

	return nil
}

func (p *Parser) validateAutoDetection(projectDir string) error {
	detectedFiles := []string{}

	checkFiles := map[string]string{
		"package.json":     "Node.js",
		"go.mod":           "Go",
		"requirements.txt": "Python",
		"Gemfile":          "Ruby",
		"pom.xml":          "Java",
		"Dockerfile":       "Docker",
	}

	for file, tech := range checkFiles {
		if _, err := os.Stat(filepath.Join(projectDir, file)); err == nil {
			detectedFiles = append(detectedFiles, tech)
		}
	}

	if len(detectedFiles) == 0 {
		return fmt.Errorf("no supported project files found (package.json, go.mod, requirements.txt, Gemfile, pom.xml, or Dockerfile)")
	}

	return nil
}

func isValidDNSName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}

	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}

	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}

	return true
}

func isValidEnvVarName(name string) bool {
	if len(name) == 0 {
		return false
	}

	for i, r := range name {
		if i == 0 {
			// First character must be letter or underscore
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_') {
				return false
			}
		} else {
			// Subsequent characters can be letters, numbers, or underscores
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
				return false
			}
		}
	}

	return true
}

// GenerateServiceSpec creates a new service spec with sensible defaults
func (p *Parser) GenerateServiceSpec(name, project, buildType string) *types.ServiceSpec {
	port := 8080
	if buildType == "node" || buildType == "javascript" || buildType == "typescript" {
		port = 3000
	} else if buildType == "python" {
		port = 8000
	}

	return &types.ServiceSpec{
		APIVersion: "enclii.dev/v1alpha",
		Kind:       "Service",
		Metadata: types.ServiceMetadata{
			Name:    name,
			Project: project,
		},
		Spec: types.ServiceSpecConfig{
			Build: types.BuildSpec{
				Type: buildType,
			},
			Runtime: types.RuntimeSpec{
				Port:        port,
				Replicas:    2,
				HealthCheck: "/health",
			},
			Env: []types.EnvVar{
				{Name: "NODE_ENV", Value: "production"},
				{Name: "PORT", Value: fmt.Sprintf("%d", port)},
			},
		},
	}
}
