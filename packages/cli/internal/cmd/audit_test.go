package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewAuditCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewAuditCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "audit", cmd.Use)
}

func TestAuditCommand_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev", APIToken: "tok"}
	cmd := NewAuditCommand(cfg)

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"list", "export"} {
		assert.Contains(t, names, want)
	}
}

func TestAuditList_HasFilterFlags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewAuditCommand(cfg)
	listCmd := findSubcommand(cmd, "list")
	require.NotNil(t, listCmd)
	for _, flag := range []string{"actor", "action", "resource-type", "resource-id", "from", "to", "limit", "page", "json"} {
		assert.NotNil(t, listCmd.Flags().Lookup(flag), "missing flag: %s", flag)
	}
	assert.Equal(t, "50", listCmd.Flags().Lookup("limit").DefValue)
}
