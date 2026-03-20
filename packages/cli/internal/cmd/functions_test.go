package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestNewFunctionsCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewFunctionsCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "functions", cmd.Use)
	assert.Equal(t, []string{"fn", "func"}, cmd.Aliases)
}

func TestFunctionsCommand_HasExpectedSubcommands(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewFunctionsCommand(cfg)

	subNames := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		subNames = append(subNames, sub.Name())
	}

	expectedSubs := []string{"list", "deploy", "logs", "invoke", "delete", "info"}
	for _, name := range expectedSubs {
		assert.Contains(t, subNames, name, "missing subcommand: %s", name)
	}
	assert.Len(t, cmd.Commands(), 6, "expected exactly 6 subcommands")
}

func TestFunctionsListSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewFunctionsCommand(cfg)
	listCmd := findSubcommand(cmd, "list")
	require.NotNil(t, listCmd, "list subcommand should exist")

	projectFlag := listCmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "", projectFlag.DefValue)
	assert.Equal(t, "p", projectFlag.Shorthand)
}

func TestFunctionsDeploySubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewFunctionsCommand(cfg)
	deployCmd := findSubcommand(cmd, "deploy")
	require.NotNil(t, deployCmd, "deploy subcommand should exist")

	// --project is required, shorthand "p"
	projectFlag := deployCmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "p", projectFlag.Shorthand)

	// --name, shorthand "n"
	nameFlag := deployCmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag)
	assert.Equal(t, "n", nameFlag.Shorthand)
	assert.Equal(t, "", nameFlag.DefValue)

	// --runtime, shorthand "r"
	runtimeFlag := deployCmd.Flags().Lookup("runtime")
	require.NotNil(t, runtimeFlag)
	assert.Equal(t, "r", runtimeFlag.Shorthand)
	assert.Equal(t, "", runtimeFlag.DefValue)
}

func TestFunctionsLogsSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewFunctionsCommand(cfg)
	logsCmd := findSubcommand(cmd, "logs")
	require.NotNil(t, logsCmd, "logs subcommand should exist")

	// Requires exactly 1 arg (validated via cobra.ExactArgs)
	assert.NotNil(t, logsCmd.Args, "logs subcommand should have args validation")

	// --follow / -f
	followFlag := logsCmd.Flags().Lookup("follow")
	require.NotNil(t, followFlag)
	assert.Equal(t, "false", followFlag.DefValue)
	assert.Equal(t, "f", followFlag.Shorthand)

	// --lines / -n
	linesFlag := logsCmd.Flags().Lookup("lines")
	require.NotNil(t, linesFlag)
	assert.Equal(t, "50", linesFlag.DefValue)
	assert.Equal(t, "n", linesFlag.Shorthand)
}

func TestFunctionsInvokeSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewFunctionsCommand(cfg)
	invokeCmd := findSubcommand(cmd, "invoke")
	require.NotNil(t, invokeCmd, "invoke subcommand should exist")

	// Requires exactly 1 arg
	assert.NotNil(t, invokeCmd.Args, "invoke subcommand should have args validation")

	// --data / -d
	dataFlag := invokeCmd.Flags().Lookup("data")
	require.NotNil(t, dataFlag)
	assert.Equal(t, "", dataFlag.DefValue)
	assert.Equal(t, "d", dataFlag.Shorthand)

	// --async (no shorthand)
	asyncFlag := invokeCmd.Flags().Lookup("async")
	require.NotNil(t, asyncFlag)
	assert.Equal(t, "false", asyncFlag.DefValue)
	assert.Equal(t, "", asyncFlag.Shorthand)
}

func TestFunctionsDeleteSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewFunctionsCommand(cfg)
	deleteCmd := findSubcommand(cmd, "delete")
	require.NotNil(t, deleteCmd, "delete subcommand should exist")

	// Requires exactly 1 arg
	assert.NotNil(t, deleteCmd.Args, "delete subcommand should have args validation")

	// Aliases
	assert.Equal(t, []string{"rm", "remove"}, deleteCmd.Aliases)

	// --force (no shorthand)
	forceFlag := deleteCmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
	assert.Equal(t, "", forceFlag.Shorthand)
}

func TestFunctionsInfoSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewFunctionsCommand(cfg)
	infoCmd := findSubcommand(cmd, "info")
	require.NotNil(t, infoCmd, "info subcommand should exist")

	// Requires exactly 1 arg
	assert.NotNil(t, infoCmd.Args, "info subcommand should have args validation")
}

func TestGetStatusIcon(t *testing.T) {
	tests := []struct {
		status   types.FunctionStatus
		expected string
	}{
		{types.FunctionStatusReady, "Ready"},
		{types.FunctionStatusPending, "Pending"},
		{types.FunctionStatusBuilding, "Building"},
		{types.FunctionStatusDeploying, "Deploying"},
		{types.FunctionStatusFailed, "Failed"},
		{types.FunctionStatusDeleting, "Deleting"},
		{types.FunctionStatus("unknown"), "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			result := getStatusIcon(tt.status)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDefaultHandler(t *testing.T) {
	tests := []struct {
		runtime  string
		expected string
	}{
		{"go", "main.Handler"},
		{"python", "handler.main"},
		{"node", "handler.main"},
		{"rust", "handler"},
		{"unknown-runtime", "handler"},
	}

	for _, tt := range tests {
		t.Run(tt.runtime, func(t *testing.T) {
			result := getDefaultHandler(tt.runtime)
			assert.Equal(t, tt.expected, result)
		})
	}
}
