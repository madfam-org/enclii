package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Credentials stores OAuth tokens from login
type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	Issuer       string    `json:"issuer"`
}

type Config struct {
	Environment string
	LogLevel    logrus.Level

	// API Configuration
	APIEndpoint string
	APIToken    string

	// OAuth Credentials (loaded from ~/.enclii/credentials.json)
	Credentials *Credentials

	// Project Configuration
	Project    string
	ProjectDir string
	ConfigFile string
}

func Load() (*Config, error) {
	viper.AutomaticEnv()
	viper.SetEnvPrefix("ENCLII")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// Set defaults
	viper.SetDefault("environment", "development")
	viper.SetDefault("log-level", "info")
	// Production default; local dev should set ENCLII_API_ENDPOINT=http://localhost:4200
	// (see docs/contracts/DEV_ENV_ALIGNMENT.md).
	viper.SetDefault("api-endpoint", "https://api.enclii.dev")
	viper.SetDefault("project", "default")
	viper.SetDefault("project-dir", ".")
	viper.SetDefault("config-file", os.Getenv("HOME")+"/.enclii/config.yml")

	// Parse log level
	logLevelStr := viper.GetString("log-level")
	logLevel, err := logrus.ParseLevel(logLevelStr)
	if err != nil {
		return nil, err
	}

	apiEndpoint := viper.GetString("api-endpoint")
	if viper.GetString("environment") == "development" && os.Getenv("ENCLII_API_ENDPOINT") == "" {
		// Align with switchyard-ui local default when the operator has not set a remote API.
		apiEndpoint = "http://localhost:4200"
	}

	config := &Config{
		Environment: viper.GetString("environment"),
		LogLevel:    logLevel,
		APIEndpoint: apiEndpoint,
		APIToken:    viper.GetString("api-token"),
		Project:     viper.GetString("project"),
		ProjectDir:  viper.GetString("project-dir"),
		ConfigFile:  viper.GetString("config-file"),
	}

	// Load OAuth credentials if available
	creds, err := loadCredentials()
	if err == nil && creds != nil {
		// If the access token is within the refresh window and we have a
		// refresh token, swap in a fresh access token before the API
		// rejects the old one. Failures here are non-fatal — fall back to
		// whatever token we already have and let the API surface 401s.
		if shouldRefresh(creds) && creds.RefreshToken != "" {
			if refreshed, rerr := refreshAccessToken(creds); rerr == nil {
				creds = refreshed
				_ = saveCredentials(creds)
			}
		}
		config.Credentials = creds
		if config.APIToken == "" && creds.AccessToken != "" {
			if time.Now().Before(creds.ExpiresAt) {
				config.APIToken = creds.AccessToken
			}
		}
	}

	return config, nil
}

// refreshLeeway is how close to expiry we tolerate before doing a synchronous
// refresh on next CLI invocation. Keeps a one-minute floor so a token that
// expires mid-request still gets renewed before the API rejects it.
const refreshLeeway = 60 * time.Second

func shouldRefresh(c *Credentials) bool {
	if c == nil || c.AccessToken == "" {
		return false
	}
	return time.Now().Add(refreshLeeway).After(c.ExpiresAt)
}

// CLI public OAuth client — must match the one in internal/cmd/login.go.
// Duplicated here to avoid a config→cmd import cycle. Override with
// ENCLII_OIDC_CLIENT_ID for self-hosted deployments.
const defaultRefreshClientID = "jnc_LrbLxHFQltYGazjmqPLB-JwN9FpYQKMB"

// refreshAccessToken exchanges a refresh token for a new access token via the
// OIDC token endpoint. Returns a *new* Credentials value with updated tokens
// and ExpiresAt; the caller is responsible for persisting it.
func refreshAccessToken(creds *Credentials) (*Credentials, error) {
	if creds.Issuer == "" {
		return nil, fmt.Errorf("credentials missing issuer; cannot refresh")
	}
	clientID := os.Getenv("ENCLII_OIDC_CLIENT_ID")
	if clientID == "" {
		clientID = defaultRefreshClientID
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {clientID},
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.PostForm(creds.Issuer+"/api/v1/oauth/token", form)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("refresh: HTTP %d", resp.StatusCode)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("refresh decode: %w", err)
	}
	if body.AccessToken == "" {
		return nil, fmt.Errorf("refresh: empty access_token")
	}

	out := &Credentials{
		AccessToken:  body.AccessToken,
		RefreshToken: creds.RefreshToken,
		TokenType:    body.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
		Issuer:       creds.Issuer,
	}
	// Some IdPs rotate the refresh token; honor that if present.
	if body.RefreshToken != "" {
		out.RefreshToken = body.RefreshToken
	}
	return out, nil
}

// saveCredentials persists Credentials to ~/.enclii/credentials.json with
// 0600 permissions. Mirrors the writer in internal/cmd/login.go — kept here
// so refreshAccessToken can update the file without an import cycle.
func saveCredentials(creds *Credentials) error {
	if creds == nil {
		return fmt.Errorf("nil credentials")
	}
	credsPath := GetCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(credsPath), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(credsPath, data, 0600)
}

// loadCredentials loads saved OAuth credentials from disk
func loadCredentials() (*Credentials, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	credsPath := filepath.Join(home, ".enclii", "credentials.json")
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return nil, err
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}

	return &creds, nil
}

// GetCredentialsPath returns the path to the credentials file
func GetCredentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".enclii", "credentials.json")
}
