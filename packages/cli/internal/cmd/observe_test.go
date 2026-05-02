package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewObserveCommand(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewObserveCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "observe", cmd.Use)
	assert.Contains(t, cmd.Aliases, "metrics")
}

func TestObserveCommand_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewObserveCommand(cfg)

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"metrics", "history", "health", "errors", "alerts"} {
		assert.Contains(t, names, want)
	}
}

func TestObserveHistory_DefaultWindow(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	historyCmd := findSubcommand(NewObserveCommand(cfg), "history")
	require.NotNil(t, historyCmd)
	assert.Equal(t, "1h", historyCmd.Flags().Lookup("window").DefValue)
}

func TestObserveMetrics_RequiresService(t *testing.T) {
	assert.Error(t, requireServiceFlag(""))
	assert.NoError(t, requireServiceFlag("svc_abc"))
}
