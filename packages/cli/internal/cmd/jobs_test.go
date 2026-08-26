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

	expectedSubs := []string{"list", "create", "get", "delete", "runs", "run-once", "logs"}
	for _, name := range expectedSubs {
		assert.Contains(t, subNames, name, "missing subcommand: %s", name)
	}
	assert.Len(t, cmd.Commands(), 7, "expected exactly 7 subcommands")
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

	// Optional flags with defaults
	timeoutFlag := runOnceCmd.Flags().Lookup("timeout")
	require.NotNil(t, timeoutFlag)
	assert.Equal(t, "3600", timeoutFlag.DefValue)

	// --image is optional: default is the service's deployment image + env
	imageFlag := runOnceCmd.Flags().Lookup("image")
	require.NotNil(t, imageFlag)
	assert.Equal(t, "", imageFlag.DefValue)
	assert.Contains(t, imageFlag.Usage, "service's current deployment image")
}

func TestJobsLogsSubcommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewJobsCommand(cfg)
	logsCmd := findSubcommand(cmd, "logs")
	require.NotNil(t, logsCmd, "logs subcommand should exist")

	// Requires exactly 1 arg
	assert.NotNil(t, logsCmd.Args, "logs subcommand should have args validation")
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

// --- jobs get: one-off fallback ---

func TestRunJobsGet_FallsBackToOneOffOn404(t *testing.T) {
	jobID := "660e8400-e29b-41d4-a716-446655440099"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/cron-jobs/" + jobID:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"cron job not found"}`))
		case "/v1/one-off-jobs/" + jobID:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"one_off_job": {
					"id": "` + jobID + `",
					"project_id": "550e8400-e29b-41d4-a716-446655440000",
					"service_id": "550e8400-e29b-41d4-a716-446655440001",
					"name": "db-migrate",
					"command": "rails db:migrate",
					"timeout": 300,
					"status": "completed",
					"exit_code": 0,
					"created_at": "2026-03-19T02:00:00Z"
				},
				"k8s_job_name": "job-db-migrate-660e8400",
				"namespace": "my-api"
			}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	err := runJobsGet(cfg, jobID)
	require.NoError(t, err)
}

func TestRunJobsGet_BothNotFound(t *testing.T) {
	jobID := "660e8400-e29b-41d4-a716-446655440099"

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

	err := runJobsGet(cfg, jobID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found (checked cron jobs and one-off jobs)")
}

// --- jobs list: one-off section ---

func TestRunJobsList_IncludesOneOffJobs(t *testing.T) {
	var cronCalled, oneOffCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/projects/my-api/cron-jobs":
			cronCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"cron_jobs": [{
					"id": "550e8400-e29b-41d4-a716-446655440001",
					"name": "nightly-backup",
					"schedule": "0 2 * * *"
				}],
				"total": 1
			}`))
		case "/v1/projects/my-api/one-off-jobs":
			oneOffCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"one_off_jobs": [{
					"id": "660e8400-e29b-41d4-a716-446655440099",
					"name": "db-migrate",
					"status": "completed",
					"exit_code": 0,
					"created_at": "2026-03-19T02:00:00Z"
				}],
				"total": 1
			}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	err := runJobsList(cfg, "my-api")
	require.NoError(t, err)
	assert.True(t, cronCalled, "cron jobs endpoint should be called")
	assert.True(t, oneOffCalled, "one-off jobs endpoint should be called")
}

func TestRunJobsList_ToleratesMissingOneOffEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/projects/my-api/cron-jobs":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"cron_jobs": [{
					"id": "550e8400-e29b-41d4-a716-446655440001",
					"name": "nightly-backup",
					"schedule": "0 2 * * *"
				}],
				"total": 1
			}`))
		default:
			// Old server: one-off listing endpoint does not exist.
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	err := runJobsList(cfg, "my-api")
	require.NoError(t, err)
}

// --- jobs logs ---

func TestRunJobsLogs_PrintsLogs(t *testing.T) {
	jobID := "660e8400-e29b-41d4-a716-446655440099"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/one-off-jobs/"+jobID+"/logs", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"logs":   "migration complete\n",
			"pod":    "job-db-migrate-660e8400-abc",
			"status": "completed",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	err := runJobsLogs(cfg, jobID)
	require.NoError(t, err)
}

func TestRunJobsLogs_ShowsUnavailableMessage(t *testing.T) {
	jobID := "660e8400-e29b-41d4-a716-446655440099"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"logs":    "",
			"pod":     "",
			"status":  "completed",
			"message": "logs no longer available: the job's pods were cleaned up",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	err := runJobsLogs(cfg, jobID)
	require.NoError(t, err)
}

// --- jobs run-once: --image passthrough ---

func TestRunJobsRunOnce_SendsImageWhenSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "migrate/migrate:v4", body["image"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"one_off_job": {"id": "660e8400-e29b-41d4-a716-446655440099", "name": "schema-check", "command": "migrate status", "image": "migrate/migrate:v4", "status": "pending"}, "message": "One-off job created successfully"}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	err := runJobsRunOnce(cfg, "my-api", "schema-check", "migrate status", "550e8400-e29b-41d4-a716-446655440001", "migrate/migrate:v4", 300)
	require.NoError(t, err)
}

func TestRunJobsRunOnce_OmitsImageWhenEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_, hasImage := body["image"]
		assert.False(t, hasImage, "image key should be omitted when not set")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"one_off_job": {"id": "660e8400-e29b-41d4-a716-446655440099", "name": "db-migrate", "command": "rails db:migrate", "status": "pending"}, "message": "One-off job created successfully"}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	err := runJobsRunOnce(cfg, "my-api", "db-migrate", "rails db:migrate", "550e8400-e29b-41d4-a716-446655440001", "", 300)
	require.NoError(t, err)
}
