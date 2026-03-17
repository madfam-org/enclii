package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func findSubCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestNewDomainsCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewDomainsCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "domains", cmd.Use)
	assert.Equal(t, []string{"domain", "dns"}, cmd.Aliases)

	// Verify subcommands exist
	subcommands := cmd.Commands()
	subNames := make([]string, len(subcommands))
	for i, sc := range subcommands {
		subNames[i] = sc.Name()
	}
	assert.Contains(t, subNames, "list")
	assert.Contains(t, subNames, "add")
	assert.Contains(t, subNames, "remove")
	assert.Contains(t, subNames, "verify")
	assert.Contains(t, subNames, "status")
}

func TestNewDomainsListCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewDomainsCommand(cfg)
	cmd := findSubCommand(parent, "list")
	require.NotNil(t, cmd, "list subcommand should exist")

	assert.Equal(t, "list", cmd.Name())
	assert.Equal(t, []string{"ls"}, cmd.Aliases)

	// Verify flags exist with correct defaults
	serviceFlag := cmd.Flags().Lookup("service")
	require.NotNil(t, serviceFlag)
	assert.Equal(t, "", serviceFlag.DefValue)
	assert.Equal(t, "s", serviceFlag.Shorthand)

	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "", envFlag.DefValue)
	assert.Equal(t, "e", envFlag.Shorthand)

	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)
	assert.Equal(t, "f", fileFlag.Shorthand)

	allFlag := cmd.Flags().Lookup("all")
	require.NotNil(t, allFlag)
	assert.Equal(t, "false", allFlag.DefValue)
	assert.Equal(t, "a", allFlag.Shorthand)
}

func TestNewDomainsAddCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewDomainsCommand(cfg)
	cmd := findSubCommand(parent, "add")
	require.NotNil(t, cmd, "add subcommand should exist")

	assert.Equal(t, "add DOMAIN", cmd.Use)

	// Verify flags exist with correct defaults
	serviceFlag := cmd.Flags().Lookup("service")
	require.NotNil(t, serviceFlag)
	assert.Equal(t, "", serviceFlag.DefValue)
	assert.Equal(t, "s", serviceFlag.Shorthand)

	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "production", envFlag.DefValue)
	assert.Equal(t, "e", envFlag.Shorthand)

	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)
	assert.Equal(t, "f", fileFlag.Shorthand)

	tlsFlag := cmd.Flags().Lookup("tls")
	require.NotNil(t, tlsFlag)
	assert.Equal(t, "true", tlsFlag.DefValue)

	tlsIssuerFlag := cmd.Flags().Lookup("tls-issuer")
	require.NotNil(t, tlsIssuerFlag)
	assert.Equal(t, "", tlsIssuerFlag.DefValue)
}

func TestNewDomainsRemoveCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewDomainsCommand(cfg)
	cmd := findSubCommand(parent, "remove")
	require.NotNil(t, cmd, "remove subcommand should exist")

	assert.Equal(t, "remove DOMAIN", cmd.Use)
	assert.Equal(t, []string{"rm", "delete"}, cmd.Aliases)

	// Verify flags exist with correct defaults
	serviceFlag := cmd.Flags().Lookup("service")
	require.NotNil(t, serviceFlag)
	assert.Equal(t, "", serviceFlag.DefValue)
	assert.Equal(t, "s", serviceFlag.Shorthand)

	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "", envFlag.DefValue)
	assert.Equal(t, "e", envFlag.Shorthand)

	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)
	assert.Equal(t, "f", fileFlag.Shorthand)

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestNewDomainsVerifyCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewDomainsCommand(cfg)
	cmd := findSubCommand(parent, "verify")
	require.NotNil(t, cmd, "verify subcommand should exist")

	assert.Equal(t, "verify DOMAIN", cmd.Use)

	// Verify flags exist with correct defaults
	serviceFlag := cmd.Flags().Lookup("service")
	require.NotNil(t, serviceFlag)
	assert.Equal(t, "", serviceFlag.DefValue)
	assert.Equal(t, "s", serviceFlag.Shorthand)

	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "", envFlag.DefValue)
	assert.Equal(t, "e", envFlag.Shorthand)

	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)
	assert.Equal(t, "f", fileFlag.Shorthand)
}

func TestNewDomainsStatusCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewDomainsCommand(cfg)
	cmd := findSubCommand(parent, "status")
	require.NotNil(t, cmd, "status subcommand should exist")

	assert.Equal(t, "status [DOMAIN]", cmd.Use)

	// Verify flags exist with correct defaults
	serviceFlag := cmd.Flags().Lookup("service")
	require.NotNil(t, serviceFlag)
	assert.Equal(t, "", serviceFlag.DefValue)
	assert.Equal(t, "s", serviceFlag.Shorthand)

	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "", envFlag.DefValue)
	assert.Equal(t, "e", envFlag.Shorthand)

	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)
	assert.Equal(t, "f", fileFlag.Shorthand)

	verboseFlag := cmd.Flags().Lookup("verbose")
	require.NotNil(t, verboseFlag)
	assert.Equal(t, "false", verboseFlag.DefValue)
	assert.Equal(t, "v", verboseFlag.Shorthand)
}
