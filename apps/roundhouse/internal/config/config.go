package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	// Server
	APIPort  string `mapstructure:"API_PORT"`
	WorkerID string `mapstructure:"WORKER_ID"`

	// Database
	DatabaseURL string `mapstructure:"DATABASE_URL"`

	// Redis
	RedisURL string `mapstructure:"REDIS_URL"`

	// Build settings
	BuildMode        string        `mapstructure:"BUILD_MODE"` // "docker" or "kaniko"
	BuildWorkDir     string        `mapstructure:"BUILD_WORK_DIR"`
	BuildTimeout     time.Duration `mapstructure:"BUILD_TIMEOUT"`
	Registry         string        `mapstructure:"REGISTRY"`
	RegistryUser     string        `mapstructure:"REGISTRY_USER"`
	RegistryPassword string        `mapstructure:"REGISTRY_PASSWORD"`

	// Kaniko-specific settings (when BUILD_MODE=kaniko)
	KanikoCacheRepo      string `mapstructure:"KANIKO_CACHE_REPO"`      // Registry path for layer cache
	KanikoGitCredentials string `mapstructure:"KANIKO_GIT_CREDENTIALS"` // K8s secret name with git token
	KubeConfig           string `mapstructure:"KUBECONFIG"`             // Path to kubeconfig (empty = in-cluster)

	// Security
	GenerateSBOM bool   `mapstructure:"GENERATE_SBOM"`
	SignImages   bool   `mapstructure:"SIGN_IMAGES"`
	CosignKey    string `mapstructure:"COSIGN_KEY"`

	// Webhooks
	GitHubWebhookSecret    string `mapstructure:"GITHUB_WEBHOOK_SECRET"`
	GitLabWebhookSecret    string `mapstructure:"GITLAB_WEBHOOK_SECRET"`
	BitbucketWebhookSecret string `mapstructure:"BITBUCKET_WEBHOOK_SECRET"`

	// Callbacks
	SwitchyardInternalURL string `mapstructure:"SWITCHYARD_INTERNAL_URL"`
	SwitchyardAPIKey      string `mapstructure:"SWITCHYARD_API_KEY"`

	// Preview Environments
	PreviewsEnabled bool `mapstructure:"PREVIEWS_ENABLED"`

	// Worker settings
	MaxConcurrentBuilds int           `mapstructure:"MAX_CONCURRENT_BUILDS"`
	PollInterval        time.Duration `mapstructure:"POLL_INTERVAL"`
}

func Load() (*Config, error) {
	viper.SetDefault("API_PORT", "8081")
	viper.SetDefault("BUILD_MODE", "docker") // Use "kaniko" in production for security
	viper.SetDefault("BUILD_WORK_DIR", "/tmp/roundhouse-builds")
	viper.SetDefault("BUILD_TIMEOUT", 30*time.Minute)
	viper.SetDefault("GENERATE_SBOM", true)
	viper.SetDefault("SIGN_IMAGES", true)
	viper.SetDefault("MAX_CONCURRENT_BUILDS", 3)
	viper.SetDefault("POLL_INTERVAL", 5*time.Second)
	viper.SetDefault("REGISTRY", "ghcr.io")
	viper.SetDefault("KANIKO_GIT_CREDENTIALS", "git-credentials")
	viper.SetDefault("PREVIEWS_ENABLED", true)

	// Bind environment variables explicitly for reliable reading
	_ = viper.BindEnv("REDIS_URL")
	_ = viper.BindEnv("DATABASE_URL")
	_ = viper.BindEnv("REGISTRY")
	_ = viper.BindEnv("REGISTRY_USER")
	_ = viper.BindEnv("REGISTRY_PASSWORD")
	_ = viper.BindEnv("BUILD_MODE")
	_ = viper.BindEnv("BUILD_WORK_DIR")
	_ = viper.BindEnv("BUILD_TIMEOUT")
	_ = viper.BindEnv("KANIKO_CACHE_REPO")
	_ = viper.BindEnv("KANIKO_GIT_CREDENTIALS")
	_ = viper.BindEnv("KUBECONFIG")
	_ = viper.BindEnv("GENERATE_SBOM")
	_ = viper.BindEnv("SIGN_IMAGES")
	_ = viper.BindEnv("COSIGN_KEY")
	_ = viper.BindEnv("GITHUB_WEBHOOK_SECRET")
	_ = viper.BindEnv("GITLAB_WEBHOOK_SECRET")
	_ = viper.BindEnv("BITBUCKET_WEBHOOK_SECRET")
	_ = viper.BindEnv("SWITCHYARD_INTERNAL_URL")
	_ = viper.BindEnv("SWITCHYARD_API_KEY")
	_ = viper.BindEnv("PREVIEWS_ENABLED")
	_ = viper.BindEnv("MAX_CONCURRENT_BUILDS")
	_ = viper.BindEnv("POLL_INTERVAL")

	viper.AutomaticEnv()

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
