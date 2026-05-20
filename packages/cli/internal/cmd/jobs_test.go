package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
)

func TestNewJobsCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)
	require.NotNil(t, cmd)

	assert.Equal(t, "jobs", cmd.Use)
	assert.Equal(t, []string{"job", "cron"}, cmd.Aliases)
}

func TestJobsCommand_HasExpectedSubcommands(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)

	subNames := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		subNames = append(subNames, sub.Name())
	}

	expectedSubs := []string{"list", "create", "get", "delete", "runs", "run-once"}
	for _, name := range expectedSubs {
		assert.Contains(t, subNames, name, "missing subcommand: %s", name)
	}
	assert.Len(t, cmd.Commands(), 6, "expected exactly 6 subcommands")
}

func TestJobsListSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)
	listCmd := findSubcommand(cmd, "list")
	require.NotNil(t, listCmd, "list subcommand should exist")

	projectFlag := listCmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "p", projectFlag.Shorthand)
	assert.Equal(t, "", projectFlag.DefValue)
}

func TestJobsCreateSubcommand_RequiredFlags(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)
	createCmd := findSubcommand(cmd, "create")
	require.NotNil(t, createCmd, "create subcommand should exist")

	// Required flags
	projectFlag := createCmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "p", projectFlag.Shorthand)

	nameFlag := createCmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag)
	assert.Equal(t, "n", nameFlag.Shorthand)

	scheduleFlag := createCmd.Flags().Lookup("schedule")
	require.NotNil(t, scheduleFlag)
	assert.Equal(t, "s", scheduleFlag.Shorthand)

	commandFlag := createCmd.Flags().Lookup("command")
	require.NotNil(t, commandFlag)
	assert.Equal(t, "c", commandFlag.Shorthand)

	serviceIDFlag := createCmd.Flags().Lookup("service-id")
	require.NotNil(t, serviceIDFlag)
}

func TestJobsCreateSubcommand_OptionalFlags(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)
	createCmd := findSubcommand(cmd, "create")
	require.NotNil(t, createCmd, "create subcommand should exist")

	timeoutFlag := createCmd.Flags().Lookup("timeout")
	require.NotNil(t, timeoutFlag)
	assert.Equal(t, "3600", timeoutFlag.DefValue)

	retriesFlag := createCmd.Flags().Lookup("retries")
	require.NotNil(t, retriesFlag)
	assert.Equal(t, "0", retriesFlag.DefValue)

	concurrencyFlag := createCmd.Flags().Lookup("concurrency")
	require.NotNil(t, concurrencyFlag)
	assert.Equal(t, "forbid", concurrencyFlag.DefValue)
}

func TestJobsDeleteSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)
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

func TestJobsRunsSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)
	runsCmd := findSubcommand(cmd, "runs")
	require.NotNil(t, runsCmd, "runs subcommand should exist")

	// Requires exactly 1 arg
	assert.NotNil(t, runsCmd.Args, "runs subcommand should have args validation")
}

func TestJobsRunOnceSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)
	runOnceCmd := findSubcommand(cmd, "run-once")
	require.NotNil(t, runOnceCmd, "run-once subcommand should exist")

	// Required flags
	projectFlag := runOnceCmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "p", projectFlag.Shorthand)

	nameFlag := runOnceCmd.Flags().Lookup("name")
	require.NotNil(t, nameFlag)
	assert.Equal(t, "n", nameFlag.Shorthand)

	commandFlag := runOnceCmd.Flags().Lookup("command")
	require.NotNil(t, commandFlag)
	assert.Equal(t, "c", commandFlag.Shorthand)

	serviceIDFlag := runOnceCmd.Flags().Lookup("service-id")
	require.NotNil(t, serviceIDFlag)

	// Optional flag with default
	timeoutFlag := runOnceCmd.Flags().Lookup("timeout")
	require.NotNil(t, timeoutFlag)
	assert.Equal(t, "3600", timeoutFlag.DefValue)
}

func TestServiceManagedCronJobNameUsesProjectPrefixForRoleServices(t *testing.T) {
	assert.Equal(t, "tulana-pull-catalog", serviceManagedCronJobName("tulana", "tulana-api", "pull-catalog"))
	assert.Equal(t, "tulana-pull-catalog", serviceManagedCronJobName("tulana", "tulana-api", "tulana-pull-catalog"))
	assert.Equal(t, "worker-refresh", serviceManagedCronJobName("platform", "worker", "refresh"))
}

func TestJobsRequest_SetsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer test-token-123", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/test-path", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token-123",
	}

	resp, err := jobsRequest(cfg, http.MethodGet, "/v1/test-path", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestJobsRequest_NoAuthHeaderWhenTokenEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "",
	}

	resp, err := jobsRequest(cfg, http.MethodGet, "/v1/test-path", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDecodeOrError_ReturnsErrorForHTTP4xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	resp, err := jobsRequest(cfg, http.MethodGet, "/v1/missing", nil)
	require.NoError(t, err)

	var target map[string]string
	err = decodeOrError(resp, &target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API error (HTTP 404)")
	assert.Contains(t, err.Error(), "not found")
}

func TestDecodeOrError_DecodesSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"name": "test-job"})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	resp, err := jobsRequest(cfg, http.MethodGet, "/v1/jobs/123", nil)
	require.NoError(t, err)

	var target map[string]string
	err = decodeOrError(resp, &target)
	require.NoError(t, err)
	assert.Equal(t, "test-job", target["name"])
}
