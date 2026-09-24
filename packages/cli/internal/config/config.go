package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// DefaultProfile is the profile whose credentials live at
// ~/.enclii/credentials.json, where they always have.
const DefaultProfile = "default"

var profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// activeProfile selects which stored identity the CLI reads and writes. Load
// sets it from ENCLII_PROFILE; the root command's --profile flag overrides it.
// Separate profiles let one operator hold several Janua identities (for
// example an everyday account and admin@) without logging one out.
var activeProfile = DefaultProfile

// NormalizeProfile validates a profile name and maps "" to the default profile.
func NormalizeProfile(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == DefaultProfile {
		return DefaultProfile, nil
	}
	if !profileNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid profile name %q: use lowercase letters, digits, '-' or '_' (max 64 characters)", name)
	}
	return name, nil
}

// SetProfile makes name the active profile for credential reads and writes.
func SetProfile(name string) error {
	normalized, err := NormalizeProfile(name)
	if err != nil {
		return err
	}
	activeProfile = normalized
	return nil
}

// ActiveProfile returns the profile whose credentials are in use.
func ActiveProfile() string {
	return activeProfile
}

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

	// OAuth Credentials (loaded from the active profile's credentials file;
	// see GetCredentialsPath)
	Credentials *Credentials

	// apiTokenExplicit records that APIToken came from the environment or a
	// flag, so reloading a profile's credentials never overrides it.
	apiTokenExplicit bool

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
	// Production default. Local development points the CLI at its own stack
	// explicitly with ENCLII_API_ENDPOINT=http://localhost:4200 (see
	// docs/contracts/DEV_ENV_ALIGNMENT.md). The endpoint never depends on
	// ENCLII_ENVIRONMENT: that only selects the log format, and it defaults to
	// "development" for everyone, so keying the endpoint on it sent every
	// installed CLI to localhost.
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

	apiToken := viper.GetString("api-token")
	if apiToken == "" {
		apiToken = os.Getenv("ENCLII_TOKEN")
	}

	// ENCLII_PROFILE picks the stored identity; unset means the default one.
	if err := SetProfile(os.Getenv("ENCLII_PROFILE")); err != nil {
		return nil, fmt.Errorf("ENCLII_PROFILE: %w", err)
	}

	config := &Config{
		Environment:      viper.GetString("environment"),
		LogLevel:         logLevel,
		APIEndpoint:      apiEndpoint,
		APIToken:         apiToken,
		apiTokenExplicit: apiToken != "",
		Project:          viper.GetString("project"),
		ProjectDir:       viper.GetString("project-dir"),
		ConfigFile:       viper.GetString("config-file"),
	}

	config.ReloadCredentials()

	return config, nil
}

// ReloadCredentials re-reads the active profile's stored OAuth credentials.
// The root command calls it after --profile switches the active profile. An
// API token given explicitly (flag or environment) keeps precedence.
func (c *Config) ReloadCredentials() {
	c.Credentials = nil
	if !c.apiTokenExplicit {
		c.APIToken = ""
	}

	creds, err := loadCredentials()
	if err != nil || creds == nil {
		return
	}
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
	c.Credentials = creds
	if c.APIToken == "" && creds.AccessToken != "" {
		if time.Now().Before(creds.ExpiresAt) {
			c.APIToken = creds.AccessToken
		}
	}
}

// SetAPIToken sets an explicit API token (the --api-token flag). Like an
// environment token, it wins over any profile's stored credentials.
func (c *Config) SetAPIToken(token string) {
	c.APIToken = token
	c.apiTokenExplicit = token != ""
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

// saveCredentials persists Credentials to the active profile's credentials
// file with 0600 permissions. Mirrors the writer in internal/cmd/login.go —
// kept here so refreshAccessToken can update the file without an import cycle.
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

// loadCredentials loads the active profile's saved OAuth credentials from disk
func loadCredentials() (*Credentials, error) {
	if _, err := os.UserHomeDir(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(GetCredentialsPath())
	if err != nil {
		return nil, err
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}

	return &creds, nil
}

// GetCredentialsPath returns the active profile's credentials file:
// ~/.enclii/credentials.json for the default profile, and
// ~/.enclii/profiles/<name>/credentials.json for any other profile.
func GetCredentialsPath() string {
	home, _ := os.UserHomeDir()
	if activeProfile == "" || activeProfile == DefaultProfile {
		return filepath.Join(home, ".enclii", "credentials.json")
	}
	return filepath.Join(home, ".enclii", "profiles", activeProfile, "credentials.json")
}
