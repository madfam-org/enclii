package secretsintake

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed registry.yaml
var registryYAML []byte

// DefaultGenerateBytes is the entropy used when a target declares no generate
// policy. 32 bytes of crypto/rand is the same strength the CLI already uses for
// dhanam/session-auth, expressed once here so every generated key inherits it.
const DefaultGenerateBytes = 32

// MinGenerateBytes floors the per-target policy. A registry typo (`bytes: 4`)
// must not be able to quietly mint a weak shared key — LoadRegistry rejects it
// rather than silently rounding up, so the weakening is visible at PR review.
const MinGenerateBytes = 16

// MaxGenerateBytes caps the policy so a typo cannot mint a multi-kilobyte value
// that no consumer can carry in an environment variable.
const MaxGenerateBytes = 128

// GeneratePolicy tunes server-side value generation for a target.
type GeneratePolicy struct {
	Bytes int `yaml:"bytes" json:"bytes,omitempty"`
}

// Target describes where an intake writes and what to sync afterward.
type Target struct {
	ID             string          `yaml:"-" json:"id"`
	Label          string          `yaml:"label" json:"label"`
	Description    string          `yaml:"description" json:"description"`
	VaultPath      string          `yaml:"vault_path" json:"vault_path"`
	Namespace      string          `yaml:"namespace" json:"namespace"`
	ExternalSecret string          `yaml:"external_secret" json:"external_secret,omitempty"`
	Keys           []string        `yaml:"keys" json:"keys"`
	Generate       *GeneratePolicy `yaml:"generate" json:"generate,omitempty"`
}

// GenerateBytes returns the entropy this target's generated values should carry.
func (t Target) GenerateBytes() int {
	if t.Generate != nil && t.Generate.Bytes > 0 {
		return t.Generate.Bytes
	}
	return DefaultGenerateBytes
}

type file struct {
	Targets map[string]Target `yaml:"targets"`
}

// LoadRegistry parses the embedded intake registry.
func LoadRegistry() (map[string]Target, error) {
	var f file
	if err := yaml.Unmarshal(registryYAML, &f); err != nil {
		return nil, fmt.Errorf("parse intake registry: %w", err)
	}
	out := make(map[string]Target, len(f.Targets))
	for id, t := range f.Targets {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		t.ID = id
		if t.VaultPath == "" {
			return nil, fmt.Errorf("target %q missing vault_path", id)
		}
		if len(t.Keys) == 0 {
			return nil, fmt.Errorf("target %q missing keys", id)
		}
		if t.Generate != nil && t.Generate.Bytes != 0 {
			if t.Generate.Bytes < MinGenerateBytes || t.Generate.Bytes > MaxGenerateBytes {
				return nil, fmt.Errorf("target %q generate.bytes must be between %d and %d, got %d",
					id, MinGenerateBytes, MaxGenerateBytes, t.Generate.Bytes)
			}
		}
		out[id] = t
	}
	return out, nil
}

// ListTargets returns sorted target ids.
func ListTargets() ([]Target, error) {
	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	list := make([]Target, 0, len(reg))
	for _, t := range reg {
		list = append(list, t)
	}
	// simple sort by id
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].ID < list[i].ID {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	return list, nil
}

// GetTarget returns one registry entry.
func GetTarget(id string) (Target, error) {
	reg, err := LoadRegistry()
	if err != nil {
		return Target{}, err
	}
	t, ok := reg[strings.TrimSpace(id)]
	if !ok {
		return Target{}, fmt.Errorf("unknown intake target %q", id)
	}
	return t, nil
}
