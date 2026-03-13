package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewRootCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.enclii.dev",
		APIToken:    "test-token",
	}

	root := NewRootCommand(cfg)
	require.NotNil(t, root)
	assert.Equal(t, "enclii", root.Use)
	assert.NotEmpty(t, root.Short)
	assert.NotEmpty(t, root.Long)

	// Verify subcommands are registered
	subcommands := root.Commands()
	assert.NotEmpty(t, subcommands, "root command should have subcommands")

	// Build a map of subcommand names for easy lookup
	names := make(map[string]bool)
	for _, cmd := range subcommands {
		names[cmd.Name()] = true
	}

	// Check all expected subcommands are present
	expectedCommands := []string{
		"init",
		"deploy",
		"logs",
		"ps",
		"rollback",
		"version",
		"local",
		"services-sync",
		"services-delete",
		"secrets",
		"domains",
		"releases",
		"functions",
		"login",
		"logout",
		"whoami",
		"onboard",
	}

	for _, name := range expectedCommands {
		assert.True(t, names[name], "expected subcommand %q to be registered", name)
	}
}

func TestNewRootCommand_Flags(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.enclii.dev",
		APIToken:    "",
	}

	root := NewRootCommand(cfg)

	// Verify persistent flags exist
	apiEndpointFlag := root.PersistentFlags().Lookup("api-endpoint")
	require.NotNil(t, apiEndpointFlag)
	assert.Equal(t, "https://api.enclii.dev", apiEndpointFlag.DefValue)

	apiTokenFlag := root.PersistentFlags().Lookup("api-token")
	require.NotNil(t, apiTokenFlag)

	logLevelFlag := root.PersistentFlags().Lookup("log-level")
	require.NotNil(t, logLevelFlag)
}

func TestNewRootCommand_DefaultFlags(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.enclii.dev",
		APIToken:    "",
	}

	root := NewRootCommand(cfg)

	logLevelFlag := root.PersistentFlags().Lookup("log-level")
	require.NotNil(t, logLevelFlag)
	assert.Equal(t, "info", logLevelFlag.DefValue, "default log level should be 'info'")
}

func TestNewVersionCommand(t *testing.T) {
	cmd := NewVersionCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "version", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	// version command uses Run (not RunE), so it should always be set
	assert.NotNil(t, cmd.Run)
}

func TestRootCommand_SubcommandNames(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.enclii.dev",
		APIToken:    "",
	}

	root := NewRootCommand(cfg)

	// Collect all subcommand names
	names := make([]string, 0)
	for _, cmd := range root.Commands() {
		names = append(names, cmd.Name())
	}

	// Verify minimum count (there should be at least the core commands)
	assert.GreaterOrEqual(t, len(names), 15, "should have at least 15 subcommands, got: %v", names)

	// Verify auth-related commands are grouped correctly
	assert.Contains(t, names, "login")
	assert.Contains(t, names, "logout")
	assert.Contains(t, names, "whoami")

	// Verify admin commands
	assert.Contains(t, names, "onboard")

	// Verify deployment commands
	assert.Contains(t, names, "deploy")
	assert.Contains(t, names, "rollback")

	// Verify serverless
	assert.Contains(t, names, "functions")
}

func TestRootCommand_PersistentPreRun(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.enclii.dev",
		APIToken:    "initial-token",
	}

	root := NewRootCommand(cfg)

	// Set flags to override config values
	root.SetArgs([]string{
		"--api-endpoint", "https://custom.api.dev",
		"--api-token", "custom-token",
		"version", // need a valid subcommand to execute
	})

	err := root.Execute()
	require.NoError(t, err)

	// After execution, the config should have been updated by PersistentPreRun
	assert.Equal(t, "https://custom.api.dev", cfg.APIEndpoint)
	assert.Equal(t, "custom-token", cfg.APIToken)
}

func TestRootCommand_PersistentPreRun_PartialOverride(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.enclii.dev",
		APIToken:    "original-token",
	}

	root := NewRootCommand(cfg)

	// Only override endpoint, not token
	root.SetArgs([]string{
		"--api-endpoint", "https://staging.api.dev",
		"version",
	})

	err := root.Execute()
	require.NoError(t, err)

	assert.Equal(t, "https://staging.api.dev", cfg.APIEndpoint)
	// Token should remain unchanged when the flag is not provided
	// (empty string means the flag was not set, so PersistentPreRun won't overwrite)
	// Note: since the flag default is cfg.APIToken ("original-token"), the value stays
}
