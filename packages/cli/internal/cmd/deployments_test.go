package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewDeploymentsCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewDeploymentsCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "deployments", cmd.Use)
	assert.Contains(t, cmd.Aliases, "deps")
}

func TestDeploymentsCommand_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewDeploymentsCommand(cfg)

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"list", "get", "latest", "by-version"} {
		assert.Contains(t, names, want)
	}
}

func TestDeploymentsGet_RequiresArg(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	getCmd := findSubcommand(NewDeploymentsCommand(cfg), "get")
	require.NotNil(t, getCmd)
	assert.NotNil(t, getCmd.Args)
}

func TestShortID(t *testing.T) {
	assert.Equal(t, "abc", deploymentShortID("abc"))
	assert.Equal(t, "12345678", deploymentShortID("123456789abcdef"))
	assert.Equal(t, "", deploymentShortID(""))
}
