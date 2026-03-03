package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	Environment string
	Port        string
	DatabaseURL string
	LogLevel    logrus.Level

	// Container Registry
	Registry         string
	RegistryUsername string
	RegistryPassword string

	// Authentication Mode
	AuthMode string // "local" (default) or "oidc"

	// OIDC Configuration
	OIDCIssuer           string
	OIDCClientID         string
	OIDCClientSecret     string
	OIDCRedirectURL      string
	PostLoginRedirectURL string // URL to redirect to after successful OIDC login (e.g., UI callback)

	// External Token Validation (for CLI/API direct access)
	ExternalJWKSURL      string // JWKS URL for validating external tokens (e.g., Janua)
	ExternalIssuer       string // Expected issuer for external tokens
	ExternalJWKSCacheTTL int    // Cache TTL in seconds for external JWKS

	// Token Expiration Settings
	AccessTokenExpireMinutes int // Access token lifetime in minutes (default: 15, set to 480 for 8 hours)
	RefreshTokenExpireDays   int // Refresh token lifetime in days (default: 7)

	// Janua Integration (for OAuth token retrieval)
	JanuaAPIURL string // Base URL for Janua API (e.g., https://api.janua.dev)

	// Kubernetes
	KubeConfig  string
	KubeContext string

	// Build Configuration
	BuildkitAddr  string
	BuildTimeout  int
	BuildWorkDir  string // Directory for cloning repositories during builds
	BuildCacheDir string // Directory for buildpack layer cache

	// Build Mode (in-process vs roundhouse worker)
	BuildMode        string // "in-process" (default) or "roundhouse"
	RoundhouseURL    string // URL of roundhouse worker (e.g., http://roundhouse:8080)
	RoundhouseAPIKey string // API key for authenticating with roundhouse
	SelfURL          string // This service's URL for callbacks (e.g., http://switchyard-api:4200)

	// Provenance / PR Approval
	GitHubToken         string // GitHub API token for PR verification
	GitHubWebhookSecret string // Secret for verifying GitHub webhook signatures

	// ArgoCD Integration
	ArgocdWebhookSecret string

	// Compliance Webhooks
	ComplianceWebhooksEnabled bool
	VantaWebhookURL           string
	DrataWebhookURL           string

	// Secret Rotation (Vault)
	SecretRotationEnabled bool
	VaultAddress          string
	VaultToken            string
	VaultNamespace        string
	VaultPollInterval     int // Seconds

	// Redis Cache (for session revocation)
	RedisHost     string
	RedisPort     int
	RedisPassword string

	// Session Revocation Fail Mode (SOC 2 compliance)
	// "closed" = deny access when Redis unavailable (secure default for production)
	// "open" = allow access when Redis unavailable (for development/availability)
	SessionRevocationFailMode string

	// Redis Sentinel (for HA failover - activate when multi-node cluster is deployed)
	RedisSentinelEnabled    bool     // Enable Sentinel failover mode
	RedisSentinelAddrs      []string // Sentinel addresses (e.g., ["redis-0:26379", "redis-1:26379", "redis-2:26379"])
	RedisSentinelMasterName string   // Sentinel master name (default: "enclii-master")

	// Cloudflare Integration (for domain status sync)
	CloudflareAPIToken  string
	CloudflareAccountID string
	CloudflareZoneID    string
	CloudflareTunnelID  string

	// Serverless Functions
	FunctionBaseDomain string // Base domain for functions (default: fn.enclii.dev)

	// Database Pool Configuration
	DBPoolSize int // Maximum number of database connections (default: 25)

	// Cache Configuration
	CacheTTLSeconds int // Cache TTL in seconds (default: 3600)

	// Rate Limiting Configuration
	RateLimitRequestsPerMinute int  // Max requests per minute (default: 1000)
	RateLimitEnabled           bool // Enable rate limiting (default: true)

	// Request Size Limits
	MaxRequestSizeBytes int64 // Maximum request body size in bytes (default: 10MB)

	// WebSocket Configuration
	WebSocketAllowedOrigins []string // Allowed origins for WebSocket connections (comma-separated)

	// Profiling
	ProfilingEnabled bool // Enable pprof profiling endpoints (default: false)

	// Admin Configuration
	AdminEmails []string // Comma-separated list of admin email addresses

	// Email Configuration (for transactional emails like invitations)
	EmailAPIKey      string // RESEND_API_KEY - Resend API key for sending emails
	EmailFromAddress string // EMAIL_FROM_ADDRESS - From email address (default: noreply@enclii.dev)
	EmailFromName    string // EMAIL_FROM_NAME - From name (default: Enclii)
	AppBaseURL       string // APP_BASE_URL - Base URL for app links in emails (default: https://app.enclii.dev)

	// Enclii Repo Coordinates (for auto-committing ArgoCD apps)
	EncliiRepoOwner string // ENCLII_ENCLII_REPO_OWNER - GitHub owner for Enclii repo (default: madfam-org)
	EncliiRepoName  string // ENCLII_ENCLII_REPO_NAME - GitHub repo name (default: enclii)
}

func Load() (*Config, error) {
	viper.AutomaticEnv()
	viper.SetEnvPrefix("ENCLII")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// Set defaults for development ONLY
	// SECURITY WARNING: These defaults are for local development only.
	// Production deployments MUST override these via environment variables.
	// Port 4200 per PORT_ALLOCATION.md in solarpunk-foundry (Enclii block: 4200-4299)
	viper.SetDefault("environment", "development")
	viper.SetDefault("port", "4200")
	// SEC-001: No hardcoded credentials - DATABASE_URL must be provided via environment
	// For development, set: ENCLII_DATABASE_URL=postgres://user:pass@localhost:5432/enclii_dev?sslmode=disable
	// For production, use: ENCLII_DATABASE_URL=postgres://user:pass@host:5432/enclii?sslmode=require
	viper.SetDefault("database-url", "")
	viper.SetDefault("log-level", "info")
	viper.SetDefault("registry", "ghcr.io/madfam-org")
	viper.SetDefault("registry-username", "")
	viper.SetDefault("registry-password", "")
	viper.SetDefault("auth-mode", "local") // Default to local bootstrap mode
	viper.SetDefault("oidc-issuer", "http://localhost:5556")
	viper.SetDefault("oidc-client-id", "enclii")
	viper.SetDefault("oidc-client-secret", "")
	viper.SetDefault("oidc-redirect-url", "http://localhost:4200/v1/auth/callback")
	viper.SetDefault("post-login-redirect-url", "")            // Empty = return JSON (for API clients)
	viper.SetDefault("external-jwks-url", "")                  // Empty = disabled
	viper.SetDefault("external-issuer", "")                    // Expected issuer for external tokens
	viper.SetDefault("external-jwks-cache-ttl", 300)           // 5 minutes default
	viper.SetDefault("access-token-expire-minutes", 15)        // 15 minutes default (set to 480 for 8 hours)
	viper.SetDefault("refresh-token-expire-days", 7)           // 7 days default
	viper.SetDefault("janua-api-url", "https://api.janua.dev") // Janua API for OAuth tokens
	viper.SetDefault("kube-config", os.Getenv("HOME")+"/.kube/config")
	viper.SetDefault("kube-context", "kind-enclii")
	viper.SetDefault("buildkit-addr", "docker://")
	viper.SetDefault("build-timeout", 1800) // 30 minutes
	viper.SetDefault("build-work-dir", "/tmp/enclii-builds")
	viper.SetDefault("build-cache-dir", "/var/cache/enclii-buildpacks")
	viper.SetDefault("build-mode", "in-process")               // "in-process" or "roundhouse"
	viper.SetDefault("roundhouse-url", "http://roundhouse")    // Roundhouse worker URL (K8s service on port 80)
	viper.SetDefault("roundhouse-api-key", "")                 // API key for roundhouse
	viper.SetDefault("self-url", "http://switchyard-api:4200") // This service's URL for callbacks
	viper.SetDefault("github-webhook-secret", "")              // Webhook disabled until secret configured
	viper.SetDefault("argocd-webhook-secret", "")
	viper.SetDefault("compliance-webhooks-enabled", false)
	viper.SetDefault("secret-rotation-enabled", false)
	viper.SetDefault("vault-poll-interval", 60) // Poll every 60 seconds
	viper.SetDefault("redis-host", "localhost")
	viper.SetDefault("redis-port", 6379)
	viper.SetDefault("redis-password", "")
	viper.SetDefault("session-revocation-fail-mode", "closed") // SOC 2: fail-closed by default
	viper.SetDefault("redis-sentinel-enabled", false)
	viper.SetDefault("redis-sentinel-addrs", "") // Comma-separated: "redis-0:26379,redis-1:26379,redis-2:26379"
	viper.SetDefault("redis-sentinel-master-name", "enclii-master")
	viper.SetDefault("cloudflare-api-token", "")
	viper.SetDefault("cloudflare-account-id", "")
	viper.SetDefault("cloudflare-zone-id", "")
	viper.SetDefault("cloudflare-tunnel-id", "")
	viper.SetDefault("function-base-domain", "fn.enclii.dev")

	// K8s environment variable defaults (wired from infra/k8s docs)
	viper.SetDefault("db-pool-size", 25)                                                                                // DB_POOL_SIZE
	viper.SetDefault("cache-ttl-seconds", 3600)                                                                         // CACHE_TTL_SECONDS (1 hour)
	viper.SetDefault("rate-limit-requests-per-minute", 1000)                                                            // RATE_LIMIT_REQUESTS_PER_MINUTE
	viper.SetDefault("rate-limit-enabled", true)                                                                        // RATE_LIMIT_ENABLED
	viper.SetDefault("max-request-size-bytes", int64(10485760))                                                         // MAX_REQUEST_SIZE (10MB)
	viper.SetDefault("websocket-allowed-origins", "http://localhost:3000,http://localhost:4201,https://app.enclii.dev") // WS_ALLOWED_ORIGINS (comma-separated)
	viper.SetDefault("profiling-enabled", false)                                                                        // ENABLE_PROFILING
	viper.SetDefault("admin-emails", "")                                                                                // ADMIN_EMAILS (comma-separated)

	// Email configuration
	viper.SetDefault("resend-api-key", "")                       // RESEND_API_KEY
	viper.SetDefault("email-from-address", "noreply@enclii.dev") // EMAIL_FROM_ADDRESS
	viper.SetDefault("email-from-name", "Enclii")                // EMAIL_FROM_NAME
	viper.SetDefault("app-base-url", "https://app.enclii.dev")   // APP_BASE_URL

	// Enclii repo coordinates (for auto-committing ArgoCD apps)
	viper.SetDefault("enclii-repo-owner", "madfam-org") // ENCLII_ENCLII_REPO_OWNER
	viper.SetDefault("enclii-repo-name", "enclii")      // ENCLII_ENCLII_REPO_NAME

	// Parse log level
	logLevelStr := viper.GetString("log-level")
	logLevel, err := logrus.ParseLevel(logLevelStr)
	if err != nil {
		return nil, err
	}

	config := &Config{
		Environment:                viper.GetString("environment"),
		Port:                       viper.GetString("port"),
		DatabaseURL:                viper.GetString("database-url"),
		LogLevel:                   logLevel,
		Registry:                   viper.GetString("registry"),
		RegistryUsername:           viper.GetString("registry-username"),
		RegistryPassword:           viper.GetString("registry-password"),
		AuthMode:                   viper.GetString("auth-mode"),
		OIDCIssuer:                 viper.GetString("oidc-issuer"),
		OIDCClientID:               viper.GetString("oidc-client-id"),
		OIDCClientSecret:           viper.GetString("oidc-client-secret"),
		OIDCRedirectURL:            viper.GetString("oidc-redirect-url"),
		PostLoginRedirectURL:       viper.GetString("post-login-redirect-url"),
		ExternalJWKSURL:            viper.GetString("external-jwks-url"),
		ExternalIssuer:             viper.GetString("external-issuer"),
		ExternalJWKSCacheTTL:       viper.GetInt("external-jwks-cache-ttl"),
		AccessTokenExpireMinutes:   viper.GetInt("access-token-expire-minutes"),
		RefreshTokenExpireDays:     viper.GetInt("refresh-token-expire-days"),
		JanuaAPIURL:                viper.GetString("janua-api-url"),
		KubeConfig:                 viper.GetString("kube-config"),
		KubeContext:                viper.GetString("kube-context"),
		BuildkitAddr:               viper.GetString("buildkit-addr"),
		BuildTimeout:               viper.GetInt("build-timeout"),
		BuildWorkDir:               viper.GetString("build-work-dir"),
		BuildCacheDir:              viper.GetString("build-cache-dir"),
		BuildMode:                  viper.GetString("build-mode"),
		RoundhouseURL:              viper.GetString("roundhouse-url"),
		RoundhouseAPIKey:           viper.GetString("roundhouse-api-key"),
		SelfURL:                    viper.GetString("self-url"),
		GitHubToken:                viper.GetString("github-token"),
		GitHubWebhookSecret:        viper.GetString("github-webhook-secret"),
		ArgocdWebhookSecret:        viper.GetString("argocd-webhook-secret"),
		ComplianceWebhooksEnabled:  viper.GetBool("compliance-webhooks-enabled"),
		VantaWebhookURL:            viper.GetString("vanta-webhook-url"),
		DrataWebhookURL:            viper.GetString("drata-webhook-url"),
		SecretRotationEnabled:      viper.GetBool("secret-rotation-enabled"),
		VaultAddress:               viper.GetString("vault-address"),
		VaultToken:                 viper.GetString("vault-token"),
		VaultNamespace:             viper.GetString("vault-namespace"),
		VaultPollInterval:          viper.GetInt("vault-poll-interval"),
		RedisHost:                  viper.GetString("redis-host"),
		RedisPort:                  viper.GetInt("redis-port"),
		RedisPassword:              viper.GetString("redis-password"),
		SessionRevocationFailMode:  viper.GetString("session-revocation-fail-mode"),
		RedisSentinelEnabled:       viper.GetBool("redis-sentinel-enabled"),
		RedisSentinelAddrs:         parseCommaSeparatedList(viper.GetString("redis-sentinel-addrs")),
		RedisSentinelMasterName:    viper.GetString("redis-sentinel-master-name"),
		CloudflareAPIToken:         viper.GetString("cloudflare-api-token"),
		CloudflareAccountID:        viper.GetString("cloudflare-account-id"),
		CloudflareZoneID:           viper.GetString("cloudflare-zone-id"),
		CloudflareTunnelID:         viper.GetString("cloudflare-tunnel-id"),
		FunctionBaseDomain:         viper.GetString("function-base-domain"),
		DBPoolSize:                 viper.GetInt("db-pool-size"),
		CacheTTLSeconds:            viper.GetInt("cache-ttl-seconds"),
		RateLimitRequestsPerMinute: viper.GetInt("rate-limit-requests-per-minute"),
		RateLimitEnabled:           viper.GetBool("rate-limit-enabled"),
		MaxRequestSizeBytes:        viper.GetInt64("max-request-size-bytes"),
		WebSocketAllowedOrigins:    parseCommaSeparatedList(viper.GetString("websocket-allowed-origins")),
		ProfilingEnabled:           viper.GetBool("profiling-enabled"),
		AdminEmails:                parseAdminEmails(viper.GetString("admin-emails")),
		EmailAPIKey:                viper.GetString("resend-api-key"),
		EmailFromAddress:           viper.GetString("email-from-address"),
		EmailFromName:              viper.GetString("email-from-name"),
		AppBaseURL:                 viper.GetString("app-base-url"),
		EncliiRepoOwner:            viper.GetString("enclii-repo-owner"),
		EncliiRepoName:             viper.GetString("enclii-repo-name"),
	}

	// SEC-001: Validate required configuration
	if config.DatabaseURL == "" {
		return nil, fmt.Errorf("ENCLII_DATABASE_URL is required. Set it in your environment:\n" +
			"  Development: export ENCLII_DATABASE_URL='postgres://user:pass@localhost:5432/enclii_dev?sslmode=disable'\n" +
			"  Production:  export ENCLII_DATABASE_URL='postgres://user:pass@host:5432/enclii?sslmode=require'")
	}

	// SEC-003: Require explicit CORS origins in production
	if config.Environment == "production" && os.Getenv("ENCLII_ALLOWED_ORIGINS") == "" {
		return nil, fmt.Errorf("SEC-003: ENCLII_ALLOWED_ORIGINS is required in production.\n" +
			"  Set a comma-separated list of allowed origins, e.g.:\n" +
			"  export ENCLII_ALLOWED_ORIGINS='https://app.enclii.dev,https://admin.enclii.dev'")
	}

	// SEC-002: Warn about insecure SSL mode in production
	if config.Environment == "production" && strings.Contains(config.DatabaseURL, "sslmode=disable") {
		logrus.Warn("SEC-002: Database SSL is disabled in production. This is a security risk. " +
			"Update DATABASE_URL to use sslmode=require or sslmode=verify-full")
	}

	return config, nil
}

// parseCommaSeparatedList parses a comma-separated list of strings
func parseCommaSeparatedList(value string) []string {
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// parseAdminEmails parses a comma-separated list of admin email addresses
func parseAdminEmails(emails string) []string {
	return parseCommaSeparatedList(emails)
}
