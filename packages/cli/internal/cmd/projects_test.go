package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewProjectsCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewProjectsCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "projects", cmd.Use)
	assert.Equal(t, []string{"project"}, cmd.Aliases)
}

func TestProjectsCommand_HasExpectedSubcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewProjectsCommand(cfg)

	expected := []string{"list", "get", "create", "delete", "environments", "services"}
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range expected {
		assert.True(t, names[want], "missing subcommand: %s", want)
	}
	assert.Len(t, cmd.Commands(), len(expected))
}

func TestProjectsDelete_RequiresForceOrConfirm(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewProjectsCommand(cfg)
	delCmd := findSubcommand(cmd, "delete")
	require.NotNil(t, delCmd)
	forceFlag := delCmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestProjectsListAndGet_HaveJSONFlag(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewProjectsCommand(cfg)
	for _, name := range []string{"list", "get", "environments", "services"} {
		sub := findSubcommand(cmd, name)
		require.NotNil(t, sub, "subcommand %q should exist", name)
		jsonFlag := sub.Flags().Lookup("json")
		require.NotNil(t, jsonFlag, "subcommand %q should have --json flag", name)
	}
}
