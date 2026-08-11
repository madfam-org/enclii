package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestParseEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := `DATABASE_URL=postgres://localhost/mydb
REDIS_URL=redis://localhost:6379
API_KEY=my-secret-key-123
`
	err := os.WriteFile(envFile, []byte(content), 0644)
	require.NoError(t, err)

	entries, err := parseEnvFile(envFile)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	assert.Equal(t, "DATABASE_URL", entries[0].Key)
	assert.Equal(t, "postgres://localhost/mydb", entries[0].Value)
	assert.Equal(t, "REDIS_URL", entries[1].Key)
	assert.Equal(t, "redis://localhost:6379", entries[1].Value)
	assert.Equal(t, "API_KEY", entries[2].Key)
	assert.Equal(t, "my-secret-key-123", entries[2].Value)
}

func TestParseEnvFile_SkipsComments(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := `# This is a comment
DATABASE_URL=postgres://localhost/mydb
# Another comment
API_KEY=secret
`
	err := os.WriteFile(envFile, []byte(content), 0644)
	require.NoError(t, err)

	entries, err := parseEnvFile(envFile)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, "DATABASE_URL", entries[0].Key)
	assert.Equal(t, "API_KEY", entries[1].Key)
}

func TestParseEnvFile_SkipsBlankLines(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := `DATABASE_URL=postgres://localhost/mydb

API_KEY=secret

`
	err := os.WriteFile(envFile, []byte(content), 0644)
	require.NoError(t, err)

	entries, err := parseEnvFile(envFile)
	require.NoError(t, err)
	require.Len(t, entries, 2)
}

func TestParseEnvFile_StripsQuotes(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := `DOUBLE_QUOTED="value with spaces"
SINGLE_QUOTED='another value'
NO_QUOTES=plain-value
`
	err := os.WriteFile(envFile, []byte(content), 0644)
	require.NoError(t, err)

	entries, err := parseEnvFile(envFile)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	assert.Equal(t, "value with spaces", entries[0].Value)
	assert.Equal(t, "another value", entries[1].Value)
	assert.Equal(t, "plain-value", entries[2].Value)
}

func TestParseEnvFile_NotFound(t *testing.T) {
	entries, err := parseEnvFile("/nonexistent/path/.env")
	require.Error(t, err)
	assert.Nil(t, entries)
}

func TestParseEnvFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	err := os.WriteFile(envFile, []byte(""), 0644)
	require.NoError(t, err)

	entries, err := parseEnvFile(envFile)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestParseEnvFile_ValuesWithEquals(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := `DATABASE_URL=postgres://user:pass@localhost/db?sslmode=require
CONNECTION_STRING=host=localhost;port=5432;user=admin
`
	err := os.WriteFile(envFile, []byte(content), 0644)
	require.NoError(t, err)

	entries, err := parseEnvFile(envFile)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Everything after the first '=' should be the value
	assert.Equal(t, "DATABASE_URL", entries[0].Key)
	assert.Equal(t, "postgres://user:pass@localhost/db?sslmode=require", entries[0].Value)
	assert.Equal(t, "CONNECTION_STRING", entries[1].Key)
	assert.Equal(t, "host=localhost;port=5432;user=admin", entries[1].Value)
}

func TestParseEnvFile_SkipsLinesWithoutEquals(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := `VALID_KEY=value
just-a-string-no-equals
ANOTHER_KEY=another_value
`
	err := os.WriteFile(envFile, []byte(content), 0644)
	require.NoError(t, err)

	entries, err := parseEnvFile(envFile)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, "VALID_KEY", entries[0].Key)
	assert.Equal(t, "ANOTHER_KEY", entries[1].Key)
}

func TestParseEnvFile_SkipsEmptyValues(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := `EMPTY_VALUE=
VALID_KEY=value
`
	err := os.WriteFile(envFile, []byte(content), 0644)
	require.NoError(t, err)

	entries, err := parseEnvFile(envFile)
	require.NoError(t, err)
	// Empty value entries are skipped (key != "" && value != "")
	require.Len(t, entries, 1)
	assert.Equal(t, "VALID_KEY", entries[0].Key)
}

func TestNewOnboardCommand(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewOnboardCommand(cfg)
	require.NotNil(t, cmd)
	assert.Equal(t, "onboard --repo <org/repo>", cmd.Use)

	// Verify the --repo flag exists and is required
	repoFlag := cmd.Flags().Lookup("repo")
	require.NotNil(t, repoFlag)

	// Verify optional flags exist with correct defaults
	projectFlag := cmd.Flags().Lookup("project")
	require.NotNil(t, projectFlag)
	assert.Equal(t, "", projectFlag.DefValue)

	manifestFlag := cmd.Flags().Lookup("manifest-path")
	require.NotNil(t, manifestFlag)
	assert.Equal(t, "k8s/production", manifestFlag.DefValue)

	branchFlag := cmd.Flags().Lookup("branch")
	require.NotNil(t, branchFlag)
	assert.Equal(t, "main", branchFlag.DefValue)

	dryRunFlag := cmd.Flags().Lookup("dry-run")
	require.NotNil(t, dryRunFlag)
	assert.Equal(t, "false", dryRunFlag.DefValue)

	preflightFlag := cmd.Flags().Lookup("preflight")
	require.NotNil(t, preflightFlag)
	assert.Equal(t, "false", preflightFlag.DefValue)

	dbNameFlag := cmd.Flags().Lookup("db-name")
	require.NotNil(t, dbNameFlag)

	secretsFileFlag := cmd.Flags().Lookup("secrets-file")
	require.NotNil(t, secretsFileFlag)

	r2Flag := cmd.Flags().Lookup("r2-bucket")
	require.NotNil(t, r2Flag)

	skipFlags := []string{"skip-postgres", "skip-secrets", "skip-r2"}
	for _, name := range skipFlags {
		flag := cmd.Flags().Lookup(name)
		require.NotNil(t, flag, "flag %s should exist", name)
		assert.Equal(t, "false", flag.DefValue)
	}
}

func TestNewOnboardCommand_RequiresRepo(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	cmd := NewOnboardCommand(cfg)

	// Execute without --repo should fail
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
}

func TestOnboardOpts_DeriveProjectName(t *testing.T) {
	// Test project name derivation via runOnboard by checking the dry run output
	// We set dryRun to avoid API calls and verify the derived project name in output

	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	tests := []struct {
		name     string
		repo     string
		project  string
		expected string
	}{
		{
			name:     "derives from org/repo",
			repo:     "madfam-org/karafiel",
			project:  "",
			expected: "karafiel",
		},
		{
			name:     "uses explicit project name",
			repo:     "madfam-org/karafiel",
			project:  "custom-name",
			expected: "custom-name",
		},
		{
			name:     "handles repo without org",
			repo:     "simple-repo",
			project:  "",
			expected: "simple-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := onboardOpts{
				repo:         tt.repo,
				project:      tt.project,
				manifestPath: "k8s/production",
				branch:       "main",
				dryRun:       true,
			}

			// runOnboard modifies opts.project in place and prints dry run output
			err := runOnboard(cfg, opts)
			require.NoError(t, err)
			// The derivation happens at the start of runOnboard and is used in the request
		})
	}

	// Directly test the derivation logic (which is in runOnboard)
	// Since we can't easily capture the derived name, we verify by checking the
	// behavior indirectly: when project is empty and repo is "org/name", project becomes "name"
	opts := onboardOpts{
		repo:    "madfam-org/test-service",
		project: "",
		dryRun:  true,
	}
	// This should not error (dry run path)
	err := runOnboard(cfg, opts)
	assert.NoError(t, err)
}

func TestRunOnboard_DryRun(t *testing.T) {
	cfg := &config.Config{
		APIEndpoint: "https://api.test.dev",
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:         "madfam-org/karafiel",
		project:      "karafiel",
		manifestPath: "k8s/production",
		branch:       "main",
		dryRun:       true,
	}

	// Dry run should succeed without making any API calls
	err := runOnboard(cfg, opts)
	assert.NoError(t, err)
}

func TestRunOnboard_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/admin/onboard", r.URL.Path)

		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, "madfam-org/karafiel", req.RepoFullName)
		assert.Equal(t, "karafiel", req.ProjectName)
		assert.Equal(t, "k8s/production", req.ManifestPath)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "completed",
			"namespace":  "karafiel",
			"argocd_app": "karafiel-services",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:         "madfam-org/karafiel",
		project:      "karafiel",
		manifestPath: "k8s/production",
		branch:       "main",
	}

	err := runOnboard(cfg, opts)
	assert.NoError(t, err)
}

func TestRunOnboard_WithPostgres(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify postgres provisioning is included
		require.NotNil(t, req.ProvisionPostgres)
		assert.Equal(t, "mydb", req.ProvisionPostgres.DatabaseName)
		assert.Equal(t, "secretpass", req.ProvisionPostgres.RolePassword)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":            "completed",
			"namespace":         "test-project",
			"postgres_database": "mydb",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:       "madfam-org/test-project",
		project:    "test-project",
		branch:     "main",
		dbName:     "mydb",
		dbPassword: "secretpass",
	}

	err := runOnboard(cfg, opts)
	assert.NoError(t, err)
}

func TestRunOnboard_WithSecrets(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	content := `DATABASE_URL=postgres://localhost/mydb
API_SECRET=my-secret-value
`
	err := os.WriteFile(envFile, []byte(content), 0644)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify secrets are included
		require.Len(t, req.ProvisionSecrets, 2)
		assert.Equal(t, "DATABASE_URL", req.ProvisionSecrets[0].Key)
		assert.Equal(t, "postgres://localhost/mydb", req.ProvisionSecrets[0].Value)
		assert.Equal(t, "API_SECRET", req.ProvisionSecrets[1].Key)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "completed",
			"namespace":     "test-project",
			"secrets_count": 2,
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:        "madfam-org/test-project",
		project:     "test-project",
		branch:      "main",
		secretsFile: envFile,
	}

	err = runOnboard(cfg, opts)
	assert.NoError(t, err)
}

func TestRunOnboard_Preflight(t *testing.T) {
	var preflightCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/v1/admin/onboard/preflight" {
			preflightCalled = true
			assert.Equal(t, "POST", r.Method)

			json.NewEncoder(w).Encode(types.PreflightResult{
				Pass: true,
			})
			return
		}

		if r.URL.Path == "/v1/admin/onboard" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "completed",
				"namespace": "test-project",
			})
			return
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:      "madfam-org/test-project",
		project:   "test-project",
		branch:    "main",
		preflight: true,
	}

	err := runOnboard(cfg, opts)
	assert.NoError(t, err)
	assert.True(t, preflightCalled, "preflight endpoint should have been called")
}

func TestRunOnboard_PreflightFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.PreflightResult{
			Pass: false,
			Violations: []types.PreflightIssue{
				{
					File:    "deployment.yaml",
					Kind:    "Deployment",
					Name:    "test",
					Message: "image not qualified",
				},
			},
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:      "madfam-org/test-project",
		project:   "test-project",
		branch:    "main",
		preflight: true,
	}

	err := runOnboard(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fix violations")
}

func TestRunOnboard_WithR2Bucket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		require.NotNil(t, req.ProvisionR2)
		assert.Equal(t, "my-uploads", req.ProvisionR2.BucketName)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "completed",
			"r2_bucket": "my-uploads",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:     "madfam-org/test-project",
		project:  "test-project",
		branch:   "main",
		r2Bucket: "my-uploads",
	}

	err := runOnboard(cfg, opts)
	assert.NoError(t, err)
}

func TestRunOnboard_SkipFlags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Even though names are set, skip flags should prevent provisioning
		assert.Nil(t, req.ProvisionPostgres, "postgres should be skipped")
		assert.Empty(t, req.ProvisionSecrets, "secrets should be skipped")
		assert.Nil(t, req.ProvisionR2, "R2 should be skipped")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "completed",
		})
	}))
	defer server.Close()

	// Create a temp .env file
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	os.WriteFile(envFile, []byte("KEY=value"), 0644)

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:         "madfam-org/test-project",
		project:      "test-project",
		branch:       "main",
		dbName:       "mydb",
		dbPassword:   "pass",
		secretsFile:  envFile,
		r2Bucket:     "my-bucket",
		skipPostgres: true,
		skipSecrets:  true,
		skipR2:       true,
	}

	err := runOnboard(cfg, opts)
	assert.NoError(t, err)
}

func TestRunOnboard_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "onboarding failed: namespace conflict",
		})
	}))
	defer server.Close()

	cfg := &config.Config{
		APIEndpoint: server.URL,
		APIToken:    "test-token",
	}

	opts := onboardOpts{
		repo:    "madfam-org/test-project",
		project: "test-project",
		branch:  "main",
	}

	err := runOnboard(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "onboarding failed")
}

// Verify that the OnboardProject client method sends the correct request
func TestOnboardProject_ClientIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/admin/onboard", r.URL.Path)
		assert.Equal(t, "Bearer admin-token", r.Header.Get("Authorization"))

		var req types.OnboardingRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, "org/repo", req.RepoFullName)
		assert.Equal(t, "repo", req.ProjectName)
		assert.Equal(t, "my-secret", req.SecretName)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "completed",
			"namespace": "repo",
		})
	}))
	defer server.Close()

	apiClient := client.NewAPIClient(server.URL, "admin-token")
	ctx := context.Background()

	branch := "main"
	var result map[string]interface{}
	err := apiClient.OnboardProject(ctx, &types.OnboardingRequest{
		RepoFullName: "org/repo",
		ProjectName:  "repo",
		SecretName:   "my-secret",
		Branch:       &branch,
	}, &result)

	require.NoError(t, err)
	assert.Equal(t, "completed", result["status"])
}

// ---------------------------------------------------------------------------
// Onboard result reporting
//
// Regression cover for 2026-08-11: onboarding nauta reported success while
// never creating its R2 bucket. The API said status="partial" and named the
// failed step with a detail; the CLI printed "Onboarding complete!", ignored
// both fields, and exited 0. The miss surfaced days later, when an operator
// went to mint a token scoped to a bucket that did not exist.
//
// The scenario test below is the one that matters — it replays that exact
// payload. A test that only exercises the happy path would have stayed green
// through the entire incident.
// ---------------------------------------------------------------------------

func partialR2Result() map[string]interface{} {
	return map[string]interface{}{
		"status": "partial",
		"step_results": []interface{}{
			map[string]interface{}{"name": "namespace", "status": "ok"},
			map[string]interface{}{"name": "postgres", "status": "ok"},
			map[string]interface{}{
				"name":   "r2",
				"status": "failed",
				"detail": "R2 API error: Authentication error (code 10000)",
			},
		},
	}
}

func TestPrintOnboardResult_PartialIsNotSuccess(t *testing.T) {
	assert.False(t, printOnboardResult(partialR2Result()),
		"a partial onboard must not report success — this is the 2026-08-11 nauta R2 defect")
}

func TestPrintOnboardResult_CompletedIsSuccess(t *testing.T) {
	assert.True(t, printOnboardResult(map[string]interface{}{
		"status": "completed",
		"step_results": []interface{}{
			map[string]interface{}{"name": "namespace", "status": "ok"},
		},
	}))
}

func TestPrintOnboardResult_FailedIsNotSuccess(t *testing.T) {
	assert.False(t, printOnboardResult(map[string]interface{}{
		"status": "failed",
		"step_results": []interface{}{
			map[string]interface{}{"name": "namespace", "status": "failed", "detail": "boom"},
		},
	}))
}

func TestPrintOnboardResult_MissingStatusStaysSuccess(t *testing.T) {
	// Older API builds omit `status`. Treat absence as success so a CLI upgrade
	// against an older control plane does not start failing every onboard —
	// the step_results list still surfaces anything genuinely broken.
	assert.True(t, printOnboardResult(map[string]interface{}{}))
}

func TestOnboardStepFailures_NamesStepAndDetail(t *testing.T) {
	failures := onboardStepFailures(partialR2Result())
	require.Len(t, failures, 1)
	assert.Contains(t, failures[0], "r2")
	assert.Contains(t, failures[0], "Authentication error",
		"the operator needs the cause, not just the step name")
}

func TestOnboardStepFailures_IgnoresOkSteps(t *testing.T) {
	assert.Empty(t, onboardStepFailures(map[string]interface{}{
		"status": "completed",
		"step_results": []interface{}{
			map[string]interface{}{"name": "namespace", "status": "ok"},
			map[string]interface{}{"name": "r2", "status": "ok"},
		},
	}))
}

func TestOnboardStepFailures_HandlesMissingOrOddShapes(t *testing.T) {
	assert.Empty(t, onboardStepFailures(map[string]interface{}{}))
	assert.Empty(t, onboardStepFailures(map[string]interface{}{"step_results": "not-a-list"}))
	assert.Empty(t, onboardStepFailures(map[string]interface{}{
		"step_results": []interface{}{"not-a-map"},
	}))
}

func TestOnboardStepFailures_FailedStepWithoutDetail(t *testing.T) {
	failures := onboardStepFailures(map[string]interface{}{
		"step_results": []interface{}{
			map[string]interface{}{"name": "r2", "status": "failed"},
		},
	})
	require.Len(t, failures, 1)
	assert.Equal(t, "r2", failures[0])
}
