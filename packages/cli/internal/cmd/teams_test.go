package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewTeamsCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTeamsCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "teams", cmd.Use)
	assert.Equal(t, []string{"team"}, cmd.Aliases)
	assert.NotEmpty(t, cmd.Short)
}

func TestTeamsCommand_HasExpectedSubcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTeamsCommand(cfg)

	expected := []string{
		"list", "get", "create", "update", "delete",
		"members", "members-update", "members-remove",
		"invite", "invitations", "invitations-cancel",
	}
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range expected {
		assert.True(t, names[want], "missing subcommand: %s", want)
	}
	assert.Len(t, cmd.Commands(), len(expected))
}

func TestTeamsListSubcommand_HasJSONFlag(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTeamsCommand(cfg)
	listCmd := findSubcommand(cmd, "list")
	require.NotNil(t, listCmd)
	jsonFlag := listCmd.Flags().Lookup("json")
	require.NotNil(t, jsonFlag)
	assert.Equal(t, "false", jsonFlag.DefValue)
}

func TestTeamsDeleteSubcommand_HasForceFlag(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTeamsCommand(cfg)
	delCmd := findSubcommand(cmd, "delete")
	require.NotNil(t, delCmd)
	assert.Equal(t, []string{"rm"}, delCmd.Aliases)
	forceFlag := delCmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestTeamsCreateSubcommand_RequiredFlags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewTeamsCommand(cfg)
	createCmd := findSubcommand(cmd, "create")
	require.NotNil(t, createCmd)
	assert.NotNil(t, createCmd.Flags().Lookup("name"))
	assert.NotNil(t, createCmd.Flags().Lookup("slug"))
}
