package manifest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// EncliiYAML represents the parsed enclii.yaml configuration from a project repo
type EncliiYAML struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   EncliiYAMLMeta `yaml:"metadata"`
	Spec       EncliiYAMLSpec `yaml:"spec"`
}

// EncliiYAMLMeta contains service identification
type EncliiYAMLMeta struct {
	Name    string `yaml:"name"`
	Project string `yaml:"project"`
}

// EncliiYAMLSpec contains the service configuration
type EncliiYAMLSpec struct {
	Domains []EncliiYAMLDomain `yaml:"domains,omitempty"`
	Runtime EncliiYAMLRuntime  `yaml:"runtime,omitempty"`
	Headers map[string]string  `yaml:"headers,omitempty"`
	Network *EncliiYAMLNetwork `yaml:"network,omitempty"`
	Status  *EncliiYAMLStatus  `yaml:"status,omitempty"`
}

// EncliiYAMLNetwork declares the network policy requirements for a project's services.
// During onboarding, this is used to auto-generate Kubernetes NetworkPolicy resources.
type EncliiYAMLNetwork struct {
	Services []EncliiYAMLNetworkService `yaml:"services,omitempty"`
	Custom   []EncliiYAMLCustomRule     `yaml:"custom,omitempty"`
}

// EncliiYAMLNetworkService declares network requirements for a single pod/service.
type EncliiYAMLNetworkService struct {
	Name    string   `yaml:"name"`              // pod app label value
	Label   string   `yaml:"label,omitempty"`   // label key, default "app"
	Port    int      `yaml:"port"`              // container port for ingress
	Ingress []string `yaml:"ingress,omitempty"` // ["cloudflare-tunnel"]
	Egress  []string `yaml:"egress,omitempty"`  // ["dns","https","postgres","redis","http","janua","pgbouncer"]
}

// EncliiYAMLCustomRule declares a custom network policy rule (e.g., intra-namespace proxy).
type EncliiYAMLCustomRule struct {
	Name      string            `yaml:"name"`
	From      map[string]string `yaml:"from,omitempty"` // pod label selector
	To        map[string]string `yaml:"to,omitempty"`   // pod label selector
	Port      int               `yaml:"port"`
	Direction string            `yaml:"direction,omitempty"` // "ingress","egress","both"
}

// EncliiYAMLStatus declares status page registration entries for a project.
type EncliiYAMLStatus struct {
	Enabled bool                    `yaml:"enabled,omitempty"` // default: true
	Entries []EncliiYAMLStatusEntry `yaml:"entries,omitempty"`
}

// EncliiYAMLStatusEntry represents a single service entry on the status page.
//
// Schema mirrors the deployed `services-config` JSON so the regenerate
// pipeline is a pure projection. Optional fields (href, family, description)
// are preserved when set and omitted otherwise — an unset family lets the UI
// fall back to the "Other" bucket per RFC 0002 S1.
type EncliiYAMLStatusEntry struct {
	Name        string `yaml:"name"`
	URL         string `yaml:"url"`
	Href        string `yaml:"href,omitempty"`
	Group       string `yaml:"group"`
	Family      string `yaml:"family,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// EncliiYAMLDomain represents a custom domain declared in enclii.yaml
type EncliiYAMLDomain struct {
	Name        string `yaml:"name"`           // e.g., "api.qubic.quest"
	Environment string `yaml:"environment"`    // e.g., "production" (defaults to "production")
	TLSEnabled  *bool  `yaml:"tlsEnabled"`     // defaults to true
	Port        int    `yaml:"port,omitempty"` // per-domain port override (defaults to runtime port or 80)

	// External opts the domain into the Cloudflare for SaaS custom-hostname
	// path: the domain owner keeps their registrar and nameservers and only
	// adds a CNAME plus a verification TXT record.
	//
	//   external: true   → always custom hostname (client-owned domain)
	//   external: false  → always zone + CNAME (we control the nameservers)
	//   absent           → auto-detect: zone+CNAME when the apex already has a
	//                      zone in our Cloudflare account, custom hostname
	//                      otherwise (and zone+CNAME as before when the
	//                      fallback origin is not configured)
	External *bool `yaml:"external,omitempty"`
}

// GetPort returns the effective service port for this domain.
// Priority: domain-level port > runtime port > 80.
func (d *EncliiYAMLDomain) GetPort(runtimePort int) int {
	if d.Port > 0 {
		return d.Port
	}
	if runtimePort > 0 {
		return runtimePort
	}
	return 80
}

// EncliiYAMLRuntime contains runtime configuration
type EncliiYAMLRuntime struct {
	Port int `yaml:"port,omitempty"`
}

// IsTLSEnabled returns whether TLS is enabled, defaulting to true
func (d *EncliiYAMLDomain) IsTLSEnabled() bool {
	if d.TLSEnabled == nil {
		return true
	}
	return *d.TLSEnabled
}

// ExternalOverride returns the explicit `external` setting and whether the
// field was declared at all. Absent (nil) means "auto-detect", which is the
// pre-existing behaviour for every manifest written before this field existed.
func (d *EncliiYAMLDomain) ExternalOverride() (value bool, declared bool) {
	if d == nil || d.External == nil {
		return false, false
	}
	return *d.External, true
}

var supportedEncliiAPIVersions = map[string]struct{}{
	"enclii.dev/v1":       {},
	"enclii.madfam.io/v1": {},
}

type encliiYAMLProjectSpec struct {
	Network  *EncliiYAMLNetwork         `yaml:"network,omitempty"`
	Status   *EncliiYAMLStatus          `yaml:"status,omitempty"`
	Services []encliiYAMLProjectService `yaml:"services,omitempty"`
}

type encliiYAMLProjectService struct {
	Name    string                    `yaml:"name"`
	Port    int                       `yaml:"port,omitempty"`
	Domains []encliiYAMLProjectDomain `yaml:"domains,omitempty"`
}

type encliiYAMLProjectDomain struct {
	Host    string `yaml:"host"`
	Primary bool   `yaml:"primary,omitempty"`
}

func validateEncliiYAMLHeader(config *EncliiYAML) error {
	if _, ok := supportedEncliiAPIVersions[config.APIVersion]; !ok {
		return fmt.Errorf("unsupported apiVersion: %s (expected enclii.dev/v1 or enclii.madfam.io/v1)", config.APIVersion)
	}
	switch config.Kind {
	case "Service", "Project":
	default:
		return fmt.Errorf("unsupported kind: %s (expected Service or Project)", config.Kind)
	}
	return nil
}

func applyEncliiYAMLDefaults(config *EncliiYAML) {
	if config.Kind == "Project" && config.Metadata.Project == "" {
		config.Metadata.Project = config.Metadata.Name
	}
	for i := range config.Spec.Domains {
		if config.Spec.Domains[i].Environment == "" {
			config.Spec.Domains[i].Environment = "production"
		}
	}
}

func normalizeProjectSpec(config *EncliiYAML, projectSpec encliiYAMLProjectSpec) {
	if projectSpec.Network != nil {
		config.Spec.Network = projectSpec.Network
	}
	if projectSpec.Status != nil {
		config.Spec.Status = projectSpec.Status
	}
	if len(projectSpec.Services) == 0 {
		return
	}
	runtimePort := 0
	for _, svc := range projectSpec.Services {
		for _, d := range svc.Domains {
			if d.Host == "" {
				continue
			}
			domain := EncliiYAMLDomain{
				Name:        d.Host,
				Environment: "production",
				Port:        svc.Port,
			}
			tls := true
			domain.TLSEnabled = &tls
			config.Spec.Domains = append(config.Spec.Domains, domain)
		}
		if runtimePort == 0 && svc.Port > 0 {
			runtimePort = svc.Port
		}
	}
	if runtimePort > 0 {
		config.Spec.Runtime.Port = runtimePort
	}
}

// ParseEncliiYAML parses an enclii.yaml file content
func ParseEncliiYAML(content []byte) (*EncliiYAML, error) {
	var header struct {
		APIVersion string         `yaml:"apiVersion"`
		Kind       string         `yaml:"kind"`
		Metadata   EncliiYAMLMeta `yaml:"metadata"`
		Spec       yaml.Node      `yaml:"spec"`
	}
	if err := yaml.Unmarshal(content, &header); err != nil {
		return nil, fmt.Errorf("failed to parse enclii.yaml: %w", err)
	}

	config := EncliiYAML{
		APIVersion: header.APIVersion,
		Kind:       header.Kind,
		Metadata:   header.Metadata,
	}
	if err := validateEncliiYAMLHeader(&config); err != nil {
		return nil, err
	}

	if header.Kind == "Project" {
		var projectSpec encliiYAMLProjectSpec
		if err := header.Spec.Decode(&projectSpec); err != nil {
			return nil, fmt.Errorf("failed to parse Project spec: %w", err)
		}
		normalizeProjectSpec(&config, projectSpec)
		applyEncliiYAMLDefaults(&config)
		return &config, nil
	}

	if err := header.Spec.Decode(&config.Spec); err != nil {
		return nil, fmt.Errorf("failed to parse Service spec: %w", err)
	}
	applyEncliiYAMLDefaults(&config)
	return &config, nil
}

// FetchAndParse fetches enclii.yaml from a GitHub repo and parses domains.
// Returns nil (not error) if the file doesn't exist — it's optional.
func FetchAndParse(ctx context.Context, logger logging.Logger, githubToken, repoFullName, gitSHA string) *EncliiYAML {
	// Parse owner/repo from full name (e.g., "madfam-org/qubic")
	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		logger.Warn(ctx, "Invalid repository full name for enclii.yaml fetch",
			logging.String("repo", repoFullName))
		return nil
	}
	owner, repo := parts[0], parts[1]

	content, err := fetchGitHubRawFile(ctx, githubToken, owner, repo, "enclii.yaml", gitSHA)
	if err != nil {
		logger.Warn(ctx, "Failed to fetch enclii.yaml from repo",
			logging.String("repo", repoFullName),
			logging.Error("error", err))
		return nil
	}

	if content == nil {
		return nil // File doesn't exist — that's fine
	}

	config, err := ParseEncliiYAML(content)
	if err != nil {
		logger.Warn(ctx, "Failed to parse enclii.yaml",
			logging.String("repo", repoFullName),
			logging.Error("error", err))
		return nil
	}

	if len(config.Spec.Domains) > 0 {
		logger.Info(ctx, "Parsed domains from enclii.yaml",
			logging.String("repo", repoFullName),
			logging.Int("domain_count", len(config.Spec.Domains)))
	}

	if len(config.Spec.Headers) > 0 {
		logger.Info(ctx, "Parsed custom headers from enclii.yaml",
			logging.String("repo", repoFullName),
			logging.Int("header_count", len(config.Spec.Headers)))
	}

	return config
}

// fetchGitHubRawFile fetches raw file content from the GitHub Contents API.
// Returns nil content (not error) if the file doesn't exist (404).
func fetchGitHubRawFile(ctx context.Context, token, owner, repo, path, ref string) ([]byte, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	if ref != "" {
		apiURL += "?ref=" + ref
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	// Request raw content to avoid base64 decoding
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // File doesn't exist
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
	}

	// Read up to 64KB (enclii.yaml should be tiny)
	content, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"repo": owner + "/" + repo,
		"path": path,
		"size": len(content),
	}).Debug("Fetched file content from GitHub")

	return content, nil
}
