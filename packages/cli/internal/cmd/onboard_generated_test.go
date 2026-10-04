package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// captureStdoutStderr runs fn with os.Stdout and os.Stderr redirected.
func captureStdoutStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	fn()
	_ = w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-done
}

// recordingServer answers onboard/ensure with reply and records the request.
func recordingServer(t *testing.T, reply map[string]interface{}, status int) (*httptest.Server, *types.OnboardingRequest, *string, *int32) {
	t.Helper()
	var got types.OnboardingRequest
	var raw string
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		raw = string(body)
		require.NoError(t, json.Unmarshal(body, &got))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &raw, &calls
}

func fabricationPrepGen() generatedCredentialFlags {
	return generatedCredentialFlags{
		generateDBPassword: true,
		appRole:            "fabrication_prep_app",
		appRoleConnLimit:   8,
		appRoleURLKey:      defaultAppRoleURLKey,
		generateSecrets:    []string{"ARTIFACT_URL_KEYS"},
	}
}

func TestOnboardCommands_RegisterGenerationFlags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	flags := []string{"generate-db-password", "rotate-db-password", "db-url-key", "db-connection-limit",
		"app-role", "app-role-connection-limit", "app-role-url-key", "app-role-password-key",
		"rotate-app-role-password", "generate-secret", "rotate-secret"}
	onboard := NewOnboardCommand(cfg)
	ensure := NewOnboardEnsureCommand(cfg)
	for _, f := range flags {
		assert.NotNil(t, onboard.Flags().Lookup(f), "onboard --%s", f)
		assert.NotNil(t, ensure.Flags().Lookup(f), "ensure --%s", f)
	}
	for _, f := range []string{"db-name", "db-extensions", "secret-name"} {
		assert.NotNil(t, ensure.Flags().Lookup(f), "ensure --%s", f)
	}
	assert.Equal(t, "5", onboard.Flags().Lookup("app-role-connection-limit").DefValue)
	assert.Equal(t, "APP_DATABASE_URL", onboard.Flags().Lookup("app-role-url-key").DefValue)
}

func TestOnboardCommand_ParsesGenerationFlags(t *testing.T) {
	cfg := &config.Config{APIEndpoint: "https://api.test.dev"}
	cmd := NewOnboardCommand(cfg)
	require.NoError(t, cmd.ParseFlags([]string{
		"--repo", "madfam-org/fabrication-prep", "--db-name", "fabrication_prep", "--generate-db-password",
		"--app-role", "fabrication_prep_app", "--app-role-connection-limit", "8",
		"--generate-secret", "ARTIFACT_URL_KEYS", "--generate-secret", "OTHER_KEY:64", "--rotate-secret", "OTHER_KEY",
	}))
	gen, _ := cmd.Flags().GetBool("generate-db-password")
	limit, _ := cmd.Flags().GetInt("app-role-connection-limit")
	secrets, _ := cmd.Flags().GetStringArray("generate-secret")
	assert.True(t, gen)
	assert.Equal(t, 8, limit)
	assert.Equal(t, []string{"ARTIFACT_URL_KEYS", "OTHER_KEY:64"}, secrets)
}

func TestGenerationFlags_Validate(t *testing.T) {
	tests := []struct {
		name   string
		db     string
		typed  bool
		mutate func(g *generatedCredentialFlags)
		want   string
	}{
		{"valid", "fabrication_prep", false, func(g *generatedCredentialFlags) {}, ""},
		{"typed and generated", "fabrication_prep", true, func(g *generatedCredentialFlags) {}, "mutually exclusive"},
		{"rotate and typed", "fabrication_prep", true, func(g *generatedCredentialFlags) {
			g.generateDBPassword = false
			g.rotateDBPassword = true
		}, "mutually exclusive"},
		{"generate without db", "", false, func(g *generatedCredentialFlags) { g.appRole = "" }, "needs --db-name"},
		{"url key without generate", "fabrication_prep", false, func(g *generatedCredentialFlags) {
			g.generateDBPassword = false
			g.dbURLKey = "OWNER_URL"
		}, "only applies"},
		{"foreign app role", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.appRole = "janua" }, "must start with"},
		{"bare prefix", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.appRole = "fabrication_prep_" }, "must start with"},
		{"reserved", "pg", false, func(g *generatedCredentialFlags) { g.appRole = "pg_app" }, "reserved"},
		{"limit 0", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.appRoleConnLimit = 0 }, "out of range"},
		{"limit 21", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.appRoleConnLimit = 21 }, "out of range"},
		{"owner limit 21", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.dbConnectionLimit = 21 }, "out of range"},
		{"rotate role without role", "fabrication_prep", false, func(g *generatedCredentialFlags) {
			g.appRole = ""
			g.rotateAppRolePassword = true
		}, "need --app-role"},
		{"bytes 15", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.generateSecrets = []string{"K:15"} }, "16-128"},
		{"bytes 129", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.generateSecrets = []string{"K:129"} }, "16-128"},
		{"bytes text", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.generateSecrets = []string{"K:big"} }, "16-128"},
		{"lower-case key", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.generateSecrets = []string{"key"} }, "invalid"},
		{"duplicate secret", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.generateSecrets = []string{"K", "K"} }, "twice"},
		{"collides with app url", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.generateSecrets = []string{"APP_DATABASE_URL"} }, "written by both"},
		{"collides with owner url", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.appRoleURLKey = "DATABASE_URL" }, "written by both"},
		{"collides with R2", "fabrication_prep", false, func(g *generatedCredentialFlags) { g.generateSecrets = []string{"R2_ACCESS_KEY_ID"} }, "written by both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := fabricationPrepGen()
			tt.mutate(&g)
			err := g.validate(tt.db, tt.typed)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRunOnboard_InvalidGenerationFlagsMakeNoNetworkCall(t *testing.T) {
	srv, _, _, calls := recordingServer(t, map[string]interface{}{"status": "completed"}, http.StatusOK)
	gen := fabricationPrepGen()
	gen.appRole = "janua"
	err := runOnboard(&config.Config{APIEndpoint: srv.URL}, onboardOpts{
		repo: "madfam-org/fabrication-prep", dbName: "fabrication_prep", gen: gen,
	})
	require.Error(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(calls))
}

func TestRunOnboard_BuildsGenerationRequestWithoutPassword(t *testing.T) {
	srv, got, raw, _ := recordingServer(t, map[string]interface{}{"status": "completed"}, http.StatusOK)
	gen := fabricationPrepGen()
	gen.generateSecrets = []string{"ARTIFACT_URL_KEYS:48"}
	gen.rotateSecrets = []string{"SESSION_KEY"}
	out := captureStdoutStderr(t, func() {
		require.NoError(t, runOnboard(&config.Config{APIEndpoint: srv.URL}, onboardOpts{
			repo: "madfam-org/fabrication-prep", project: "fabrication-prep", dbName: "fabrication_prep",
			dbExtensions: "pgcrypto", r2Bucket: "fabrication-prep-artifacts", gen: gen,
		}))
	})
	require.NotNil(t, got.ProvisionPostgres)
	assert.True(t, got.ProvisionPostgres.GeneratePassword)
	assert.Empty(t, got.ProvisionPostgres.RolePassword)
	assert.NotContains(t, *raw, "role_password", "no password field may be sent when generating")
	assert.Equal(t, []string{"pgcrypto"}, got.ProvisionPostgres.Extensions)
	require.NotNil(t, got.ProvisionAppRole)
	assert.Equal(t, types.AppRoleSpec{
		RoleName: "fabrication_prep_app", DatabaseName: "fabrication_prep", ConnectionLimit: 8, URLSecretKey: "APP_DATABASE_URL",
	}, *got.ProvisionAppRole)
	assert.Equal(t, []types.GeneratedSecretSpec{{Key: "ARTIFACT_URL_KEYS", Bytes: 48}, {Key: "SESSION_KEY", Rotate: true}}, got.GenerateSecrets)
	assert.NotContains(t, out, "Enter password", "no prompt with --generate-db-password")
}

func TestRunOnboard_TypedPasswordPathUnchanged(t *testing.T) {
	srv, got, _, _ := recordingServer(t, map[string]interface{}{"status": "completed"}, http.StatusOK)
	require.NoError(t, runOnboard(&config.Config{APIEndpoint: srv.URL}, onboardOpts{
		repo: "madfam-org/karafiel", dbName: "karafiel", dbPassword: "typed-fixture-pw", // pragma: allowlist secret
	}))
	require.NotNil(t, got.ProvisionPostgres)
	assert.False(t, got.ProvisionPostgres.GeneratePassword)
	assert.Equal(t, "typed-fixture-pw", got.ProvisionPostgres.RolePassword) // pragma: allowlist secret
	assert.Nil(t, got.ProvisionAppRole)
	assert.Empty(t, got.GenerateSecrets)
}

func TestRunOnboard_SecretsFileMayNotShadowGeneratedKey(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "x.env")
	require.NoError(t, os.WriteFile(envFile, []byte("APP_DATABASE_URL=postgresql://fixture\n"), 0o600))
	err := runOnboard(&config.Config{APIEndpoint: "http://127.0.0.1:1"}, onboardOpts{
		repo: "madfam-org/fabrication-prep", dbName: "fabrication_prep", secretsFile: envFile, gen: fabricationPrepGen(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with a generated value")
}

func TestRunOnboard_DryRunListsNamesNotValues(t *testing.T) {
	gen := fabricationPrepGen()
	gen.rotateAppRolePassword = true
	out := captureStdoutStderr(t, func() {
		require.NoError(t, runOnboard(&config.Config{APIEndpoint: "http://127.0.0.1:1"}, onboardOpts{
			repo: "madfam-org/fabrication-prep", project: "fabrication-prep", dbName: "fabrication_prep",
			dryRun: true, gen: gen,
		}))
	})
	assert.Contains(t, out, "Generated server-side (values are never printed or returned)")
	assert.Contains(t, out, `owner role "fabrication_prep" password -> Secret key DATABASE_URL (kept if it already exists)`)
	assert.Contains(t, out, `app role "fabrication_prep_app" on "fabrication_prep", CONNECTION LIMIT 8, no BYPASSRLS -> Secret key(s) APP_DATABASE_URL (ROTATED)`)
	assert.Contains(t, out, "Secret key ARTIFACT_URL_KEYS: 32 random bytes")
	assert.NotContains(t, out, "postgresql://")
}

// The server never returns generated values. Even if a response wrongly
// carried one, the CLI prints only kind, name, action, Secret and key names.
func TestGeneratedCredentialOutputNeverPrintsValues(t *testing.T) {
	const leak = "LEAKED-GENERATED-VALUE-9f3c" // pragma: allowlist secret
	reply := map[string]interface{}{
		"status": "completed",
		"generated_credentials": []interface{}{
			map[string]interface{}{"kind": "app_role", "name": "fabrication_prep_app", "secret": "fabrication-prep-credentials",
				"keys": []interface{}{"APP_DATABASE_URL"}, "action": "created", "value": leak, "password": leak},
			map[string]interface{}{"kind": "secret", "name": "ARTIFACT_URL_KEYS", "secret": "fabrication-prep-credentials",
				"keys": []interface{}{"ARTIFACT_URL_KEYS"}, "action": "kept", "url": "postgresql://u:" + leak + "@h/db"},
		},
	}
	for _, mode := range []string{"onboard", "ensure"} {
		t.Run(mode, func(t *testing.T) {
			srv, _, _, _ := recordingServer(t, reply, http.StatusOK)
			cfg := &config.Config{APIEndpoint: srv.URL}
			out := captureStdoutStderr(t, func() {
				if mode == "onboard" {
					require.NoError(t, runOnboard(cfg, onboardOpts{repo: "madfam-org/fabrication-prep", dbName: "fabrication_prep", gen: fabricationPrepGen()}))
				} else {
					require.NoError(t, runOnboardEnsure(cfg, onboardEnsureOpts{repo: "madfam-org/fabrication-prep", dbName: "fabrication_prep", gen: fabricationPrepGen()}))
				}
			})
			assert.Contains(t, out, "app_role fabrication_prep_app: created (Secret fabrication-prep-credentials: APP_DATABASE_URL)")
			assert.Contains(t, out, "secret ARTIFACT_URL_KEYS: kept")
			assert.NotContains(t, out, leak)
		})
	}
}

func TestRunOnboardEnsure_ExitCodes(t *testing.T) {
	for _, tc := range []struct {
		status  string
		code    int
		wantErr bool
	}{
		{"completed", http.StatusOK, false},
		{"partial", http.StatusOK, true},
		{"failed", http.StatusInternalServerError, true},
	} {
		t.Run(tc.status, func(t *testing.T) {
			srv, _, _, _ := recordingServer(t, map[string]interface{}{"status": tc.status, "mode": "repair"}, tc.code)
			var err error
			_ = captureStdoutStderr(t, func() {
				err = runOnboardEnsure(&config.Config{APIEndpoint: srv.URL}, onboardEnsureOpts{repo: "madfam-org/pravara-mes"})
			})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRunOnboardEnsure_BuildsAppRoleRequest(t *testing.T) {
	srv, got, raw, _ := recordingServer(t, map[string]interface{}{"status": "completed"}, http.StatusOK)
	_ = captureStdoutStderr(t, func() {
		require.NoError(t, runOnboardEnsure(&config.Config{APIEndpoint: srv.URL}, onboardEnsureOpts{
			repo: "madfam-org/pravara-mes", secretName: "pravara-secrets", dbName: "pravara",
			gen: generatedCredentialFlags{appRole: "pravara_app", appRoleConnLimit: 10, appRoleURLKey: "APP_DATABASE_URL",
				appRolePasswordKey: "DATABASE_PASSWORD", rotateAppRolePassword: true},
		}))
	})
	assert.Nil(t, got.ProvisionPostgres, "ensure provisions the owner only with --generate-db-password")
	assert.Equal(t, "pravara-secrets", got.SecretName)
	assert.Equal(t, &types.AppRoleSpec{RoleName: "pravara_app", DatabaseName: "pravara", ConnectionLimit: 10,
		URLSecretKey: "APP_DATABASE_URL", PasswordSecretKey: "DATABASE_PASSWORD", RotatePassword: true}, got.ProvisionAppRole)
	assert.False(t, strings.Contains(*raw, "role_password"))
}
