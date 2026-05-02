package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewIntegrationsCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewIntegrationsCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "integrations", cmd.Use)

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.Contains(t, names, "github")
}

func TestIntegrationsGitHub_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	root := NewIntegrationsCommand(cfg)
	gh := findSubcommand(root, "github")
	require.NotNil(t, gh)

	names := make([]string, 0, len(gh.Commands()))
	for _, sub := range gh.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"status", "repos", "branches", "link", "analyze"} {
		assert.Contains(t, names, want)
	}
}

func TestGitHubBranches_RequiresTwoArgs(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	gh := findSubcommand(NewIntegrationsCommand(cfg), "github")
	branches := findSubcommand(gh, "branches")
	require.NotNil(t, branches)
	assert.NotNil(t, branches.Args)
}
