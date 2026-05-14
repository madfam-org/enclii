package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewOpsCommand_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	root := NewOpsCommand(cfg)
	require.NotNil(t, root)
	assert.Equal(t, "ops", root.Use)

	for _, want := range []string{"capabilities", "apps", "pods", "jobs", "storage", "secrets", "policy", "runners"} {
		assert.NotNil(t, findSubcommand(root, want), "expected ops %s", want)
	}
}

func TestOpsApps_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	apps := findSubcommand(NewOpsCommand(cfg), "apps")
	require.NotNil(t, apps)

	for _, want := range []string{"status", "sync", "diff", "rollback"} {
		assert.NotNil(t, findSubcommand(apps, want), "expected ops apps %s", want)
	}
}

func TestOpsJobs_Subcommands(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	jobs := findSubcommand(NewOpsCommand(cfg), "jobs")
	require.NotNil(t, jobs)

	for _, want := range []string{"list", "trigger"} {
		assert.NotNil(t, findSubcommand(jobs, want), "expected ops jobs %s", want)
	}
}

func TestOpsPodsLogsFlags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	logs := findSubcommand(findSubcommand(NewOpsCommand(cfg), "pods"), "logs")
	require.NotNil(t, logs)

	for _, want := range []string{"container", "tail", "limit-bytes", "namespace", "project", "service", "json"} {
		assert.NotNil(t, logs.Flags().Lookup(want), "expected --%s", want)
	}
}

func TestOpsMutationsRequireReasonWithApply(t *testing.T) {
	err := validateOperationFlags(operationFlags{apply: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--reason")

	err = validateOperationFlags(operationFlags{apply: true, reason: "recover production rollout"})
	assert.NoError(t, err)
}

func TestOpsActionFlags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	sync := findSubcommand(findSubcommand(NewOpsCommand(cfg), "apps"), "sync")
	require.NotNil(t, sync)

	for _, want := range []string{"apply", "reason", "idempotency-key", "namespace", "project", "service", "json"} {
		assert.NotNil(t, sync.Flags().Lookup(want), "expected --%s", want)
	}
}
