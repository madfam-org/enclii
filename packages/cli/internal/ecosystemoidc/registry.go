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
//
// Three client shapes flow through here. A LOGIN client (authorization_code)
// has redirect_uris and no OrganizationID. A machine client
// (client_credentials) has an OrganizationID (org-bound service identity) and
// NO redirect_uris — nauta-symbiosis-hcm is the first of these. Janua #595
// (merged 2026-09-04) emits an org-bound client's app:role scopes verbatim
// into the roles claim, which is what makes an org binding + a scope like
// hcm:hr a working edge; a machine client that is NOT org-bound would not get
// that treatment. The third shape is a PUBLIC login client
// (`is_confidential: false`): a browser or device app that signs in with
// authorization_code + PKCE and holds no secret — the Yantra4D Studio
// (app.yantra4d.com, 2026-09-17) is the first. See Platform.publicLogin.
type JanuaClientSpec struct {
	Name           string   `yaml:"name"`
	ClientKey      string   `yaml:"client_key"`
	Audience       string   `yaml:"audience"`
	ClientID       string   `yaml:"client_id,omitempty"`
	Description    string   `yaml:"description,omitempty"`
	IsConfidential *bool    `yaml:"is_confidential,omitempty"`
	WebsiteURL     string   `yaml:"website_url,omitempty"`
	OrganizationID string   `yaml:"organization_id,omitempty"`
	RedirectURIs   []string `yaml:"redirect_uris,omitempty"`
	AllowedScopes  []string `yaml:"allowed_scopes"`
	GrantTypes     []string `yaml:"grant_types"`
}

func (s JanuaClientSpec) confidential() bool {
	if s.IsConfidential == nil {
		return true
	}
	return *s.IsConfidential
}

// publicLogin reports whether the platform's client is a PUBLIC login client:
// a browser or device app that signs in with authorization_code + PKCE and
// holds no secret. Nothing about such a client is secret — the client_id is a
// public identifier the consumer pins in its own repository (yantra4d's
// janua.client.yaml, read at build time) — so the intake_target is OPTIONAL,
// and a secret is never resolved, never rotated and never written to Vault.
// An org-bound machine client is never public: it authenticates as itself.
func (p Platform) publicLogin() bool {
	return !p.JanuaClient.confidential() && strings.TrimSpace(p.JanuaClient.OrganizationID) == ""
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
