package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any ENCLII_ env vars that could interfere with defaults
	envVars := []string{
		"ENCLII_ENVIRONMENT",
		"ENCLII_LOG_LEVEL",
		"ENCLII_API_ENDPOINT",
		"ENCLII_API_TOKEN",
		"ENCLII_PROJECT",
		"ENCLII_PROJECT_DIR",
		"ENCLII_CONFIG_FILE",
	}
	for _, v := range envVars {
		prev := os.Getenv(v)
		t.Cleanup(func() { os.Setenv(v, prev) })
		os.Unsetenv(v)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment != "development" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "development")
	}
	// Development + unset api-endpoint resolves to local Switchyard (see DEV_ENV_ALIGNMENT.md).
	if cfg.APIEndpoint != "http://localhost:4200" {
		t.Errorf("APIEndpoint = %q, want %q", cfg.APIEndpoint, "http://localhost:4200")
	}
	if cfg.Project != "default" {
		t.Errorf("Project = %q, want %q", cfg.Project, "default")
	}
	if cfg.ProjectDir != "." {
		t.Errorf("ProjectDir = %q, want %q", cfg.ProjectDir, ".")
	}
}

func TestLoad_ProductionDefaultAPIEndpoint(t *testing.T) {
	envVars := []string{
		"ENCLII_ENVIRONMENT",
		"ENCLII_API_ENDPOINT",
	}
	for _, v := range envVars {
		prev := os.Getenv(v)
		t.Cleanup(func() { os.Setenv(v, prev) })
		os.Unsetenv(v)
	}
	os.Setenv("ENCLII_ENVIRONMENT", "production")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIEndpoint != "https://api.enclii.dev" {
		t.Errorf("APIEndpoint = %q, want https://api.enclii.dev", cfg.APIEndpoint)
	}
}

func TestLoad_EnvVarOverride(t *testing.T) {
	// Save and restore env vars
	envVars := map[string]string{
		"ENCLII_ENVIRONMENT":  "production",
		"ENCLII_LOG_LEVEL":    "debug",
		"ENCLII_API_ENDPOINT": "https://api.custom.dev",
		"ENCLII_API_TOKEN":    "test-token-123",
		"ENCLII_PROJECT":      "my-project",
		"ENCLII_PROJECT_DIR":  "/opt/projects",
	}

	for k, v := range envVars {
		prev := os.Getenv(k)
		os.Setenv(k, v)
		t.Cleanup(func() {
			if prev == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, prev)
			}
		})
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "production")
	}
	if cfg.APIEndpoint != "https://api.custom.dev" {
		t.Errorf("APIEndpoint = %q, want %q", cfg.APIEndpoint, "https://api.custom.dev")
	}
	if cfg.APIToken != "test-token-123" {
		t.Errorf("APIToken = %q, want %q", cfg.APIToken, "test-token-123")
	}
	if cfg.Project != "my-project" {
		t.Errorf("Project = %q, want %q", cfg.Project, "my-project")
	}
	if cfg.ProjectDir != "/opt/projects" {
		t.Errorf("ProjectDir = %q, want %q", cfg.ProjectDir, "/opt/projects")
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	prev := os.Getenv("ENCLII_LOG_LEVEL")
	os.Setenv("ENCLII_LOG_LEVEL", "not-a-level")
	t.Cleanup(func() {
		if prev == "" {
			os.Unsetenv("ENCLII_LOG_LEVEL")
		} else {
			os.Setenv("ENCLII_LOG_LEVEL", prev)
		}
	})

	_, err := Load()
	if err == nil {
		t.Error("Load() with invalid log level should return error")
	}
}

func TestLoad_CredentialsIntegration(t *testing.T) {
	// Create a temporary credentials file in a temp home dir
	tmpHome := t.TempDir()
	encliiDir := filepath.Join(tmpHome, ".enclii")
	if err := os.MkdirAll(encliiDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create credentials that expire in the future
	creds := Credentials{
		AccessToken:  "oauth-access-token",
		RefreshToken: "oauth-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		Issuer:       "https://auth.example.com",
	}
	data, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	credsPath := filepath.Join(encliiDir, "credentials.json")
	if err := os.WriteFile(credsPath, data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Override HOME so loadCredentials finds our temp file
	prevHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", prevHome) })

	// Clear ENCLII_API_TOKEN so OAuth token is used
	prevToken := os.Getenv("ENCLII_API_TOKEN")
	os.Unsetenv("ENCLII_API_TOKEN")
	t.Cleanup(func() {
		if prevToken == "" {
			os.Unsetenv("ENCLII_API_TOKEN")
		} else {
			os.Setenv("ENCLII_API_TOKEN", prevToken)
		}
	})

	// Also clear the log level override from other tests
	prevLogLevel := os.Getenv("ENCLII_LOG_LEVEL")
	os.Unsetenv("ENCLII_LOG_LEVEL")
	t.Cleanup(func() {
		if prevLogLevel == "" {
			os.Unsetenv("ENCLII_LOG_LEVEL")
		} else {
			os.Setenv("ENCLII_LOG_LEVEL", prevLogLevel)
		}
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Credentials == nil {
		t.Fatal("Credentials should be loaded from file")
	}
	if cfg.Credentials.AccessToken != "oauth-access-token" {
		t.Errorf("Credentials.AccessToken = %q, want %q", cfg.Credentials.AccessToken, "oauth-access-token")
	}
	if cfg.APIToken != "oauth-access-token" {
		t.Errorf("APIToken should use OAuth token when no explicit API token set, got %q", cfg.APIToken)
	}
}

func TestLoad_ExpiredCredentialsNotUsed(t *testing.T) {
	tmpHome := t.TempDir()
	encliiDir := filepath.Join(tmpHome, ".enclii")
	if err := os.MkdirAll(encliiDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create credentials that are already expired
	creds := Credentials{
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		Issuer:       "https://auth.example.com",
	}
	data, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(encliiDir, "credentials.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	prevHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", prevHome) })

	prevToken := os.Getenv("ENCLII_API_TOKEN")
	os.Unsetenv("ENCLII_API_TOKEN")
	t.Cleanup(func() {
		if prevToken == "" {
			os.Unsetenv("ENCLII_API_TOKEN")
		} else {
			os.Setenv("ENCLII_API_TOKEN", prevToken)
		}
	})

	prevLogLevel := os.Getenv("ENCLII_LOG_LEVEL")
	os.Unsetenv("ENCLII_LOG_LEVEL")
	t.Cleanup(func() {
		if prevLogLevel == "" {
			os.Unsetenv("ENCLII_LOG_LEVEL")
		} else {
			os.Setenv("ENCLII_LOG_LEVEL", prevLogLevel)
		}
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.APIToken != "" {
		t.Errorf("APIToken should be empty when credentials are expired, got %q", cfg.APIToken)
	}
}

func TestLoad_ExplicitAPITokenTakesPrecedence(t *testing.T) {
	tmpHome := t.TempDir()
	encliiDir := filepath.Join(tmpHome, ".enclii")
	if err := os.MkdirAll(encliiDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	creds := Credentials{
		AccessToken: "oauth-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Issuer:      "https://auth.example.com",
	}
	data, _ := json.Marshal(creds)
	os.WriteFile(filepath.Join(encliiDir, "credentials.json"), data, 0o600)

	prevHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", prevHome) })

	prevToken := os.Getenv("ENCLII_API_TOKEN")
	os.Setenv("ENCLII_API_TOKEN", "explicit-api-key")
	t.Cleanup(func() {
		if prevToken == "" {
			os.Unsetenv("ENCLII_API_TOKEN")
		} else {
			os.Setenv("ENCLII_API_TOKEN", prevToken)
		}
	})

	prevLogLevel := os.Getenv("ENCLII_LOG_LEVEL")
	os.Unsetenv("ENCLII_LOG_LEVEL")
	t.Cleanup(func() {
		if prevLogLevel == "" {
			os.Unsetenv("ENCLII_LOG_LEVEL")
		} else {
			os.Setenv("ENCLII_LOG_LEVEL", prevLogLevel)
		}
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.APIToken != "explicit-api-key" {
		t.Errorf("APIToken = %q, want %q (explicit token should take precedence)", cfg.APIToken, "explicit-api-key")
	}
}

func TestLoad_MissingCredentialsFileIsNotAnError(t *testing.T) {
	tmpHome := t.TempDir()
	// Do NOT create .enclii/credentials.json

	prevHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", prevHome) })

	prevToken := os.Getenv("ENCLII_API_TOKEN")
	os.Unsetenv("ENCLII_API_TOKEN")
	t.Cleanup(func() {
		if prevToken == "" {
			os.Unsetenv("ENCLII_API_TOKEN")
		} else {
			os.Setenv("ENCLII_API_TOKEN", prevToken)
		}
	})

	prevLogLevel := os.Getenv("ENCLII_LOG_LEVEL")
	os.Unsetenv("ENCLII_LOG_LEVEL")
	t.Cleanup(func() {
		if prevLogLevel == "" {
			os.Unsetenv("ENCLII_LOG_LEVEL")
		} else {
			os.Setenv("ENCLII_LOG_LEVEL", prevLogLevel)
		}
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() should succeed without credentials file, got error: %v", err)
	}
	if cfg.Credentials != nil {
		t.Error("Credentials should be nil when file does not exist")
	}
}

func TestGetCredentialsPath(t *testing.T) {
	path := GetCredentialsPath()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".enclii", "credentials.json")
	if path != want {
		t.Errorf("GetCredentialsPath() = %q, want %q", path, want)
	}
}

func TestShouldRefresh(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		c    *Credentials
		want bool
	}{
		{"nil credentials", nil, false},
		{"no access token", &Credentials{}, false},
		{"already expired", &Credentials{AccessToken: "x", ExpiresAt: now.Add(-time.Hour)}, true},
		{"within leeway", &Credentials{AccessToken: "x", ExpiresAt: now.Add(30 * time.Second)}, true},
		{"comfortably valid", &Credentials{AccessToken: "x", ExpiresAt: now.Add(10 * time.Minute)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRefresh(tt.c); got != tt.want {
				t.Errorf("shouldRefresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSaveAndLoadCredentialsRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	want := &Credentials{
		AccessToken:  "at",
		RefreshToken: "rt",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		Issuer:       "https://auth.example.com",
	}
	if err := saveCredentials(want); err != nil {
		t.Fatalf("saveCredentials: %v", err)
	}
	// File mode must be 0600.
	info, err := os.Stat(filepath.Join(tmp, ".enclii", "credentials.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("file mode = %o, want 0600", mode)
	}

	got, err := loadCredentials()
	if err != nil {
		t.Fatalf("loadCredentials: %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Errorf("roundtrip mismatch: got %+v want %+v", got, want)
	}
}
