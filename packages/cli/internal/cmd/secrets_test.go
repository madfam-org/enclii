package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

// findSubcommand returns the subcommand with the given name, or nil if not found.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

func TestNewSecretsCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewSecretsCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "secrets", cmd.Use)
	assert.Equal(t, []string{"secret", "env"}, cmd.Aliases)

	// Verify subcommands are registered
	subNames := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		subNames = append(subNames, sub.Name())
	}
	assert.Contains(t, subNames, "set")
	assert.Contains(t, subNames, "list")
	assert.Contains(t, subNames, "delete")
	assert.Contains(t, subNames, "get")
	assert.Contains(t, subNames, "sync")
	assert.Contains(t, subNames, "rotate")
	assert.Contains(t, subNames, "vault-backfill")
	assert.Contains(t, subNames, "intake")
}

func TestNewSecretsSetCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewSecretsCommand(cfg)
	cmd := findSubcommand(parent, "set")
	require.NotNil(t, cmd, "set subcommand should exist")

	// Verify flags exist with correct defaults
	secretFlag := cmd.Flags().Lookup("secret")
	require.NotNil(t, secretFlag)
	assert.Equal(t, "false", secretFlag.DefValue)
	assert.Equal(t, "s", secretFlag.Shorthand)

	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "", envFlag.DefValue)
	assert.Equal(t, "e", envFlag.Shorthand)

	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)
	assert.Equal(t, "f", fileFlag.Shorthand)
}

func TestNewSecretsListCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewSecretsCommand(cfg)
	cmd := findSubcommand(parent, "list")
	require.NotNil(t, cmd, "list subcommand should exist")

	// Verify aliases
	assert.Equal(t, []string{"ls"}, cmd.Aliases)

	// Verify flags exist with correct defaults
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

func TestNewSecretsDeleteCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewSecretsCommand(cfg)
	cmd := findSubcommand(parent, "delete")
	require.NotNil(t, cmd, "delete subcommand should exist")

	// Verify aliases
	assert.Equal(t, []string{"rm", "remove"}, cmd.Aliases)

	// Verify flags exist with correct defaults
	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)
	assert.Equal(t, "f", fileFlag.Shorthand)

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
	// force has no shorthand (registered with BoolVar, not BoolVarP)
	assert.Equal(t, "", forceFlag.Shorthand)
}

func TestNewSecretsGetCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewSecretsCommand(cfg)
	cmd := findSubcommand(parent, "get")
	require.NotNil(t, cmd, "get subcommand should exist")

	// Verify flags exist with correct defaults
	fileFlag := cmd.Flags().Lookup("file")
	require.NotNil(t, fileFlag)
	assert.Equal(t, "service.yaml", fileFlag.DefValue)
	assert.Equal(t, "f", fileFlag.Shorthand)

	revealFlag := cmd.Flags().Lookup("reveal")
	require.NotNil(t, revealFlag)
	assert.Equal(t, "false", revealFlag.DefValue)
	// reveal has no shorthand (registered with BoolVar, not BoolVarP)
	assert.Equal(t, "", revealFlag.Shorthand)
}

func TestNewSecretsRotateCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewSecretsCommand(cfg)
	cmd := findSubcommand(parent, "rotate")
	require.NotNil(t, cmd, "rotate subcommand should exist")

	for _, want := range []string{"apply", "reason", "idempotency-key", "namespace", "project", "service", "provider-version", "json"} {
		assert.NotNil(t, cmd.Flags().Lookup(want), "expected --%s", want)
	}
}

func TestNewSecretsVaultBackfillCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	parent := NewSecretsCommand(cfg)
	cmd := findSubcommand(parent, "vault-backfill")
	require.NotNil(t, cmd, "vault-backfill subcommand should exist")

	for _, want := range []string{"apply", "reason", "idempotency-key", "namespace", "project", "service", "vault-path", "external-secret", "json"} {
		assert.NotNil(t, cmd.Flags().Lookup(want), "expected --%s", want)
	}
}
