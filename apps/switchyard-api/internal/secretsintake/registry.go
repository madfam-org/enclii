package secretsintake

import (
	_ "embed"
	"fmt"
	"sort"
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
	Bytes    int    `yaml:"bytes" json:"bytes,omitempty"`
	Profile  string `yaml:"profile" json:"profile,omitempty"`
	Disabled bool   `yaml:"disabled" json:"disabled,omitempty"`
	// Group names the shared group that alone may write this target. A member
	// refuses single-target generation and supplied values: its one key holds
	// the value Switchyard minted for the whole group (see SharedGroup).
	Group string `yaml:"group" json:"group,omitempty"`
}

// SharedGroup is one server-generated value that several targets must hold
// identically, such as an HMAC key one service verifies and others sign with.
// Intake writes one Vault path per target, so a group is the only way to give
// every member the same value without a human or a file ever holding it.
type SharedGroup struct {
	ID          string   `yaml:"-" json:"id"`
	Label       string   `yaml:"label" json:"label"`
	Description string   `yaml:"description" json:"description"`
	Bytes       int      `yaml:"bytes" json:"bytes,omitempty"`
	Members     []string `yaml:"members" json:"members"`
}

// GenerateBytes returns the entropy of the group's value (hex-encoded, so the
// stored string is twice as long).
func (g SharedGroup) GenerateBytes() int {
	if g.Bytes > 0 {
		return g.Bytes
	}
	return DefaultGenerateBytes
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
	Targets map[string]Target      `yaml:"targets"`
	Groups  map[string]SharedGroup `yaml:"groups"`
}

// LoadRegistry parses the embedded intake registry.
func LoadRegistry() (map[string]Target, error) {
	targets, _, err := loadRegistryFile(registryYAML)
	return targets, err
}

// LoadGroups parses the embedded registry's shared groups.
func LoadGroups() (map[string]SharedGroup, error) {
	_, groups, err := loadRegistryFile(registryYAML)
	return groups, err
}

// GetGroup returns one shared group.
func GetGroup(id string) (SharedGroup, error) {
	groups, err := LoadGroups()
	if err != nil {
		return SharedGroup{}, err
	}
	g, ok := groups[strings.TrimSpace(id)]
	if !ok {
		return SharedGroup{}, fmt.Errorf("unknown shared intake group %q", id)
	}
	return g, nil
}

// ListGroups returns the shared groups sorted by id.
func ListGroups() ([]SharedGroup, error) {
	groups, err := LoadGroups()
	if err != nil {
		return nil, err
	}
	out := make([]SharedGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func loadRegistryFile(raw []byte) (map[string]Target, map[string]SharedGroup, error) {
	var f file
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, nil, fmt.Errorf("parse intake registry: %w", err)
	}
	out := make(map[string]Target, len(f.Targets))
	for id, t := range f.Targets {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		t.ID = id
		if t.VaultPath == "" {
			return nil, nil, fmt.Errorf("target %q missing vault_path", id)
		}
		if len(t.Keys) == 0 {
			return nil, nil, fmt.Errorf("target %q missing keys", id)
		}
		if t.Generate != nil && t.Generate.Bytes != 0 {
			if t.Generate.Bytes < MinGenerateBytes || t.Generate.Bytes > MaxGenerateBytes {
				return nil, nil, fmt.Errorf("target %q generate.bytes must be between %d and %d, got %d",
					id, MinGenerateBytes, MaxGenerateBytes, t.Generate.Bytes)
			}
		}
		if err := validateGenerationPolicy(t); err != nil {
			return nil, nil, err
		}
		out[id] = t
	}
	groups, err := validateGroups(f.Groups, out)
	if err != nil {
		return nil, nil, err
	}
	return out, groups, nil
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

// validateGroups pins the group contract: every member is a one-key target that
// names this group and nothing else can generate, and every target naming a
// group is listed by it. A member outside its group, or a group member that
// could also be generated alone, would let the copies drift apart.
func validateGroups(raw map[string]SharedGroup, targets map[string]Target) (map[string]SharedGroup, error) {
	groups := make(map[string]SharedGroup, len(raw))
	listedBy := map[string]string{}
	for id, g := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("shared group with empty id")
		}
		if _, clash := targets[id]; clash {
			return nil, fmt.Errorf("shared group %q collides with a target id", id)
		}
		g.ID = id
		if strings.TrimSpace(g.Label) == "" || strings.TrimSpace(g.Description) == "" {
			return nil, fmt.Errorf("shared group %q needs a label and a description", id)
		}
		if g.Bytes != 0 && (g.Bytes < MinGenerateBytes || g.Bytes > MaxGenerateBytes) {
			return nil, fmt.Errorf("shared group %q bytes must be between %d and %d, got %d",
				id, MinGenerateBytes, MaxGenerateBytes, g.Bytes)
		}
		if len(g.Members) < 2 {
			return nil, fmt.Errorf("shared group %q needs at least two members", id)
		}
		properties := map[string]string{}
		for _, member := range g.Members {
			t, ok := targets[member]
			if !ok {
				return nil, fmt.Errorf("shared group %q lists unknown target %q", id, member)
			}
			if prev, dup := listedBy[member]; dup {
				return nil, fmt.Errorf("target %q is listed by shared groups %q and %q", member, prev, id)
			}
			listedBy[member] = id
			if t.Generate == nil || t.Generate.Group != id {
				return nil, fmt.Errorf("target %q is in shared group %q but its generate.group does not name it", member, id)
			}
			if len(t.Keys) != 1 {
				return nil, fmt.Errorf("shared group %q member %q must declare exactly one key", id, member)
			}
			slot := t.VaultPath + "#" + strings.ToLower(t.Keys[0])
			if prev, dup := properties[slot]; dup {
				return nil, fmt.Errorf("shared group %q members %q and %q write the same Vault property", id, prev, member)
			}
			properties[slot] = member
		}
		groups[id] = g
	}
	for id, t := range targets {
		if t.Generate == nil || t.Generate.Group == "" {
			continue
		}
		if listedBy[id] != t.Generate.Group {
			return nil, fmt.Errorf("target %q names shared group %q, which does not list it", id, t.Generate.Group)
		}
	}
	return groups, nil
}
