package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewActivityCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewActivityCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "activity", cmd.Use)
}

func TestActivityCommand_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewActivityCommand(cfg)

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"list", "actions", "resource-types"} {
		assert.Contains(t, names, want)
	}
}

func TestActivityList_Flags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	listCmd := findSubcommand(NewActivityCommand(cfg), "list")
	require.NotNil(t, listCmd)
	for _, flag := range []string{"action", "resource-type", "limit", "json"} {
		assert.NotNil(t, listCmd.Flags().Lookup(flag), "missing flag: %s", flag)
	}
}
