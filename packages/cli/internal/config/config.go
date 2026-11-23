package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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

	// RefreshError is why the last ReloadCredentials could not renew the
	// session, or nil. It never contains a token.
	RefreshError error

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

	c.RefreshError = nil

	creds, err := loadCredentials()
	if err != nil || creds == nil {
		return
	}
	// If the access token is within the refresh window and we have a
	// refresh token, swap in a fresh access token before the API rejects
	// the old one. A failed refresh is not fatal — the token we hold is
	// used for as long as it is valid — but it is never silent: once the
	// session can no longer be used, the reason goes to stderr.
	creds, c.RefreshError = renewIfDue(activeProfile, creds)
	if c.RefreshError != nil && !time.Now().Before(creds.ExpiresAt) {
		warnRefreshFailed(activeProfile, c.RefreshError)
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

// TokenExpiry is when an access token stops working. A JWT's own `exp` is what
// the API enforces, so it wins; `expires_in` is only the issuer's announcement
// and is used when the token is opaque. The two have disagreed in production:
// Janua announced 3600 s for tokens whose `exp` was eight hours away, so the
// CLI dropped a working token after one hour whenever its refresh was refused.
func TokenExpiry(accessToken string, expiresIn int) time.Time {
	if exp, ok := jwtExpiry(accessToken); ok {
		return exp
	}
	return time.Now().Add(time.Duration(expiresIn) * time.Second)
}

// jwtExpiry reads the `exp` claim of a JWT without verifying it. That is safe
// here: the claim only decides when the CLI stops offering the token; the API
// verifies the signature and the expiry on every request.
func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp *float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == nil || *claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(*claims.Exp), 0), true
}

// renewIfDue returns the credentials to use for profile. It first corrects a
// stored expiry that disagrees with the access token's own `exp` (files
// written before TokenExpiry existed say "one hour"), then refreshes when the
// token is inside the refresh window. A refused or failed refresh returns the
// credentials it was given together with the reason.
func renewIfDue(profile string, creds *Credentials) (*Credentials, error) {
	corrected := false
	if exp, ok := jwtExpiry(creds.AccessToken); ok && !exp.Equal(creds.ExpiresAt) {
		creds.ExpiresAt = exp
		corrected = true
	}
	if shouldRefresh(creds) && creds.RefreshToken != "" {
		refreshed, err := refreshAccessToken(creds)
		if err != nil {
			return creds, err
		}
		_ = saveCredentialsFor(profile, refreshed)
		return refreshed, nil
	}
	if corrected {
		// Other readers of the file (scripts) see the real expiry too.
		_ = saveCredentialsFor(profile, creds)
	}
	return creds, nil
}

// warnOut receives the one-line refresh failure notice; tests replace it.
var warnOut io.Writer = os.Stderr

var (
	warnedMu sync.Mutex
	warned   = map[string]bool{}
)

// warnRefreshFailed says, once per process and profile, why an expired
// session could not be renewed. The text is built from the HTTP status and
// the identity provider's own error message only — never from a token.
func warnRefreshFailed(profile string, reason error) {
	line := fmt.Sprintf("enclii: the session of profile %q expired and could not be renewed: %v. Run `%s`.",
		profile, reason, loginCommand(profile))
	warnedMu.Lock()
	defer warnedMu.Unlock()
	if warned[line] {
		return
	}
	warned[line] = true
	_, _ = fmt.Fprintln(warnOut, line)
}

func loginCommand(profile string) string {
	if profile == "" || profile == DefaultProfile {
		return "enclii login"
	}
	return fmt.Sprintf("enclii --profile %s login", profile)
}

// maxRefreshReason bounds how much of an identity provider's error text is
// repeated to the terminal.
const maxRefreshReason = 200

// refreshRefusal describes a non-2xx answer from the token endpoint: the
// status and, when the body carries one, the provider's stated reason
// (FastAPI `detail`, or OAuth `error` / `error_description`, RFC 6749 §5.2).
func refreshRefusal(status int, body []byte, redact ...string) error {
	var parsed struct {
		Detail           json.RawMessage `json:"detail"`
		Error            string          `json:"error"`
		ErrorDescription string          `json:"error_description"`
	}
	reason := ""
	if json.Unmarshal(body, &parsed) == nil {
		var detail string
		_ = json.Unmarshal(parsed.Detail, &detail)
		switch {
		case detail != "":
			reason = detail
		case parsed.Error != "" && parsed.ErrorDescription != "":
			reason = parsed.Error + ": " + parsed.ErrorDescription
		default:
			reason = parsed.Error + parsed.ErrorDescription
		}
	}
	reason = strings.Join(strings.Fields(reason), " ")
	for _, value := range redact {
		if value != "" {
			reason = strings.ReplaceAll(reason, value, "[redacted]")
		}
	}
	if runes := []rune(reason); len(runes) > maxRefreshReason {
		reason = string(runes[:maxRefreshReason]) + "…"
	}
	if reason == "" {
		return fmt.Errorf("the identity provider answered HTTP %d", status)
	}
	return fmt.Errorf("the identity provider answered HTTP %d (%s)", status, reason)
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
		// A *url.Error names the endpoint and the cause; the form (and so
		// the refresh token) is in the request body, never in the URL.
		return nil, fmt.Errorf("the identity provider could not be reached: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, refreshRefusal(resp.StatusCode, raw, creds.RefreshToken, creds.AccessToken)
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
		ExpiresAt:    TokenExpiry(body.AccessToken, body.ExpiresIn),
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
	return saveCredentialsFor(activeProfile, creds)
}

// saveCredentialsFor persists Credentials to one named profile's file.
func saveCredentialsFor(profile string, creds *Credentials) error {
	if creds == nil {
		return fmt.Errorf("nil credentials")
	}
	credsPath := credentialsPathFor(profile)
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
	return loadCredentialsFor(activeProfile)
}

// loadCredentialsFor loads one named profile's saved OAuth credentials.
func loadCredentialsFor(profile string) (*Credentials, error) {
	if _, err := os.UserHomeDir(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(credentialsPathFor(profile))
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
	return credentialsPathFor(activeProfile)
}

func credentialsPathFor(profile string) string {
	home, _ := os.UserHomeDir()
	if profile == "" || profile == DefaultProfile {
		return filepath.Join(home, ".enclii", "credentials.json")
	}
	return filepath.Join(home, ".enclii", "profiles", profile, "credentials.json")
}

// ProfileAPIToken returns another profile's stored access token without
// switching the active profile. A command that needs two identities at once
// uses it: since ADR-003 an operation can need a Janua admin session for one
// call and the platform_admin operator for the next. A token near expiry is
// refreshed and saved back to that profile, as ReloadCredentials does for the
// active one. It refuses a profile with no unexpired login.
func ProfileAPIToken(name string) (string, error) {
	profile, err := NormalizeProfile(name)
	if err != nil {
		return "", err
	}
	creds, err := loadCredentialsFor(profile)
	if err != nil || creds == nil || creds.AccessToken == "" {
		return "", fmt.Errorf("profile %q is not logged in — run `enclii --profile %s login`", profile, profile)
	}
	creds, refreshErr := renewIfDue(profile, creds)
	if !time.Now().Before(creds.ExpiresAt) {
		if refreshErr != nil {
			return "", fmt.Errorf("profile %q login has expired and could not be renewed: %v — run `enclii --profile %s login`", profile, refreshErr, profile)
		}
		return "", fmt.Errorf("profile %q login has expired — run `enclii --profile %s login`", profile, profile)
	}
	return creds.AccessToken, nil
}
