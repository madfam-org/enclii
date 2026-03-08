package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear env vars that could interfere
	envVars := []string{
		"API_PORT", "WORKER_ID", "DATABASE_URL", "REDIS_URL",
		"BUILD_MODE", "BUILD_WORK_DIR", "BUILD_TIMEOUT",
		"REGISTRY", "REGISTRY_USER", "REGISTRY_PASSWORD",
		"KANIKO_CACHE_REPO", "KANIKO_GIT_CREDENTIALS", "KUBECONFIG",
		"GENERATE_SBOM", "SIGN_IMAGES", "COSIGN_KEY",
		"GITHUB_WEBHOOK_SECRET", "GITLAB_WEBHOOK_SECRET", "BITBUCKET_WEBHOOK_SECRET",
		"SWITCHYARD_INTERNAL_URL", "SWITCHYARD_API_KEY",
		"PREVIEWS_ENABLED", "MAX_CONCURRENT_BUILDS", "POLL_INTERVAL",
	}
	for _, v := range envVars {
		os.Unsetenv(v)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"APIPort", cfg.APIPort, "8081"},
		{"BuildMode", cfg.BuildMode, "docker"},
		{"BuildWorkDir", cfg.BuildWorkDir, "/tmp/roundhouse-builds"},
		{"BuildTimeout", cfg.BuildTimeout, 30 * time.Minute},
		{"GenerateSBOM", cfg.GenerateSBOM, true},
		{"SignImages", cfg.SignImages, true},
		{"MaxConcurrentBuilds", cfg.MaxConcurrentBuilds, 3},
		{"PollInterval", cfg.PollInterval, 5 * time.Second},
		{"Registry", cfg.Registry, "ghcr.io"},
		{"KanikoGitCredentials", cfg.KanikoGitCredentials, "git-credentials"},
		{"PreviewsEnabled", cfg.PreviewsEnabled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("API_PORT", "9090")
	t.Setenv("BUILD_MODE", "kaniko")
	t.Setenv("REGISTRY", "registry.example.com")
	t.Setenv("REGISTRY_USER", "testuser")
	t.Setenv("REGISTRY_PASSWORD", "testpass")
	t.Setenv("MAX_CONCURRENT_BUILDS", "10")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.APIPort != "9090" {
		t.Errorf("APIPort: got %q, want %q", cfg.APIPort, "9090")
	}

	if cfg.BuildMode != "kaniko" {
		t.Errorf("BuildMode: got %q, want %q", cfg.BuildMode, "kaniko")
	}

	if cfg.Registry != "registry.example.com" {
		t.Errorf("Registry: got %q, want %q", cfg.Registry, "registry.example.com")
	}

	if cfg.RegistryUser != "testuser" {
		t.Errorf("RegistryUser: got %q, want %q", cfg.RegistryUser, "testuser")
	}

	if cfg.RegistryPassword != "testpass" {
		t.Errorf("RegistryPassword: got %q, want %q", cfg.RegistryPassword, "testpass")
	}

	if cfg.MaxConcurrentBuilds != 10 {
		t.Errorf("MaxConcurrentBuilds: got %d, want %d", cfg.MaxConcurrentBuilds, 10)
	}

	if cfg.GitHubWebhookSecret != "secret123" {
		t.Errorf("GitHubWebhookSecret: got %q, want %q", cfg.GitHubWebhookSecret, "secret123")
	}
}

func TestLoad_SecuritySettings(t *testing.T) {
	t.Setenv("GENERATE_SBOM", "false")
	t.Setenv("SIGN_IMAGES", "false")
	t.Setenv("COSIGN_KEY", "/path/to/cosign.key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.GenerateSBOM {
		t.Error("expected GenerateSBOM to be false")
	}

	if cfg.SignImages {
		t.Error("expected SignImages to be false")
	}

	if cfg.CosignKey != "/path/to/cosign.key" {
		t.Errorf("CosignKey: got %q, want %q", cfg.CosignKey, "/path/to/cosign.key")
	}
}

func TestLoad_CallbackSettings(t *testing.T) {
	t.Setenv("SWITCHYARD_INTERNAL_URL", "http://switchyard:8080")
	t.Setenv("SWITCHYARD_API_KEY", "internal-key-abc")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.SwitchyardInternalURL != "http://switchyard:8080" {
		t.Errorf("SwitchyardInternalURL: got %q, want %q", cfg.SwitchyardInternalURL, "http://switchyard:8080")
	}

	if cfg.SwitchyardAPIKey != "internal-key-abc" {
		t.Errorf("SwitchyardAPIKey: got %q, want %q", cfg.SwitchyardAPIKey, "internal-key-abc")
	}
}

func TestLoad_KanikoSettings(t *testing.T) {
	t.Setenv("KANIKO_CACHE_REPO", "ghcr.io/org/cache")
	t.Setenv("KANIKO_GIT_CREDENTIALS", "my-git-secret")
	t.Setenv("KUBECONFIG", "/home/user/.kube/config")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.KanikoCacheRepo != "ghcr.io/org/cache" {
		t.Errorf("KanikoCacheRepo: got %q, want %q", cfg.KanikoCacheRepo, "ghcr.io/org/cache")
	}

	if cfg.KanikoGitCredentials != "my-git-secret" {
		t.Errorf("KanikoGitCredentials: got %q, want %q", cfg.KanikoGitCredentials, "my-git-secret")
	}

	if cfg.KubeConfig != "/home/user/.kube/config" {
		t.Errorf("KubeConfig: got %q, want %q", cfg.KubeConfig, "/home/user/.kube/config")
	}
}

func TestLoad_EmptyOptionalFields(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("REDIS_URL")
	os.Unsetenv("WORKER_ID")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL should be empty, got %q", cfg.DatabaseURL)
	}

	if cfg.RedisURL != "" {
		t.Errorf("RedisURL should be empty, got %q", cfg.RedisURL)
	}

	if cfg.WorkerID != "" {
		t.Errorf("WorkerID should be empty, got %q", cfg.WorkerID)
	}
}
