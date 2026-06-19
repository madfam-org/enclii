package ecosystemoidc

import (
	_ "embed"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed data/ecosystem-oidc-provision.yaml
var embeddedRegistry []byte

// Registry is the canonical MADFAM inter-platform OIDC provision map.
type Registry struct {
	Issuer    string              `yaml:"issuer"`
	Platforms map[string]Platform `yaml:"platforms"`
}

// Platform describes one ecosystem app's Janua client + Vault intake routing.
type Platform struct {
	IntakeTarget        string            `yaml:"intake_target"`
	SessionIntakeTarget string            `yaml:"session_intake_target,omitempty"`
	JanuaClient         JanuaClientSpec   `yaml:"janua_client"`
	IntakeKeyMap        map[string]string `yaml:"intake_key_map,omitempty"`
}

// JanuaClientSpec is sent to Janua register/create APIs.
type JanuaClientSpec struct {
	Name           string   `yaml:"name"`
	ClientKey      string   `yaml:"client_key"`
	Audience       string   `yaml:"audience"`
	ClientID       string   `yaml:"client_id,omitempty"`
	Description    string   `yaml:"description,omitempty"`
	IsConfidential *bool    `yaml:"is_confidential,omitempty"`
	WebsiteURL     string   `yaml:"website_url,omitempty"`
	RedirectURIs   []string `yaml:"redirect_uris"`
	AllowedScopes  []string `yaml:"allowed_scopes"`
	GrantTypes     []string `yaml:"grant_types"`
}

func (s JanuaClientSpec) confidential() bool {
	if s.IsConfidential == nil {
		return true
	}
	return *s.IsConfidential
}

// LoadRegistry reads the embedded registry or an override file path.
func LoadRegistry(path string) (*Registry, error) {
	raw := embeddedRegistry
	if strings.TrimSpace(path) != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read registry %q: %w", path, err)
		}
		raw = data
	}
	var reg Registry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		return nil, fmt.Errorf("parse ecosystem OIDC registry: %w", err)
	}
	if strings.TrimSpace(reg.Issuer) == "" {
		reg.Issuer = "https://auth.madfam.io"
	}
	if len(reg.Platforms) == 0 {
		return nil, fmt.Errorf("ecosystem OIDC registry has no platforms")
	}
	return &reg, nil
}

// PlatformIDs returns sorted platform keys.
func (r *Registry) PlatformIDs() []string {
	out := make([]string, 0, len(r.Platforms))
	for id := range r.Platforms {
		out = append(out, id)
	}
	sortStrings(out)
	return out
}

func sortStrings(ss []string) {
	for i := 0; i < len(ss); i++ {
		for j := i + 1; j < len(ss); j++ {
			if ss[j] < ss[i] {
				ss[i], ss[j] = ss[j], ss[i]
			}
		}
	}
}
