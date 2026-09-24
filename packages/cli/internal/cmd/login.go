package cmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/cli/internal/exitcodes"
)

// OAuth configuration for Janua
const (
	// Janua OAuth endpoints - defaults to auth.madfam.io (production SSO alias)
	// Override with ENCLII_OIDC_ISSUER for custom deployments
	// NOTE: Must use auth.madfam.io (not api.janua.dev) because Janua sets
	// session cookies with Domain=.madfam.io — api.janua.dev won't receive them.
	authorizePath = "/api/v1/oauth/authorize"
	tokenPath     = "/api/v1/oauth/token" // #nosec G101 -- OAuth token endpoint path, not a credential

	// CLI OAuth client (public client with PKCE)
	// Registered in Janua SSO - public client for PKCE flow
	cliClientID = "jnc_LrbLxHFQltYGazjmqPLB-JwN9FpYQKMB"

	// Scopes needed for CLI access
	cliScopes = "openid profile email offline_access"
)

// getDefaultIssuer returns the OIDC issuer URL from env or default
func getDefaultIssuer() string {
	if issuer := os.Getenv("ENCLII_OIDC_ISSUER"); issuer != "" {
		return issuer
	}
	return "https://auth.madfam.io"
}

// Credentials stores the OAuth tokens
type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	Issuer       string    `json:"issuer"`
}

// TokenResponse from OAuth token endpoint
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// Janua prompt values the CLI forwards (the ecosystem account-switching model):
// select_account shows the chooser over the accounts the browser holds;
// login forces a fresh sign-in ("sign in as someone else").
var allowedLoginPrompts = map[string]bool{"select_account": true, "login": true}

func validateLoginPrompt(prompt string) error {
	if prompt == "" || allowedLoginPrompts[prompt] {
		return nil
	}
	return fmt.Errorf("invalid --prompt %q: use select_account or login", prompt)
}

func NewLoginCommand(cfg *config.Config) *cobra.Command {
	var issuer string
	var clientID string
	var prompt string
	var noBrowser bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Enclii via Janua SSO",
		Long: `Authenticate with the Enclii platform using Janua SSO.

This command opens your browser to complete the OAuth login flow.
After successful authentication, your credentials are stored locally
for future CLI commands, under the active profile (--profile or
ENCLII_PROFILE; "default" when neither is set).

By default the browser's current Janua session answers silently, so the
CLI gets whichever account that browser is signed in as. To choose:
  --prompt select_account   show Janua's chooser over the accounts the
                            browser holds (choosing one also makes it the
                            browser's active account)
  --prompt login            sign in as someone else
  --no-browser              only print the login URL, e.g. to open it in a
                            private window so the browser's active account
                            stays as it is

Set ENCLII_BROWSER to a command (such as
'open -na "Google Chrome" --args --incognito') to open login URLs with it.

Examples:
  enclii login
  enclii --profile admin login --no-browser --prompt login
  enclii --profile admin whoami
  enclii login --issuer https://auth.example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateLoginPrompt(prompt); err != nil {
				return &exitcodes.ValidationError{Err: err}
			}
			return runLogin(cmd, cfg, issuer, clientID, prompt, noBrowser)
		},
	}

	cmd.Flags().StringVar(&issuer, "issuer", getDefaultIssuer(), "OAuth issuer URL")
	cmd.Flags().StringVar(&clientID, "client-id", cliClientID, "OAuth client ID")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Janua prompt: select_account (account chooser) or login (sign in as someone else)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the login URL instead of opening a browser (open it in a private window to keep your browser's account)")

	return cmd
}

func NewLogoutCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out and remove stored credentials",
		Long:  `Remove stored authentication credentials from your system.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(cmd)
		},
	}
}

func NewWhoamiCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current authenticated user",
		Long:  `Display information about the currently authenticated user.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhoami(cmd, cfg)
		},
	}
}

func runLogin(cmd *cobra.Command, cfg *config.Config, issuer, clientID, prompt string, noBrowser bool) error {
	profile := config.ActiveProfile()
	if profile == config.DefaultProfile {
		cmd.Println("🔐 Authenticating with Enclii via Janua SSO...")
	} else {
		cmd.Printf("🔐 Authenticating with Enclii via Janua SSO (profile %q)...\n", profile)
	}
	cmd.Println()

	// Generate PKCE code verifier and challenge
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return fmt.Errorf("failed to generate code verifier: %w", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Generate state for CSRF protection
	state, err := generateState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	// Start local callback server on fixed port for OAuth redirect URI matching
	// Try port 8080 first, fall back to 3000 if busy
	var listener net.Listener
	var port int
	var listenErr error

	for _, tryPort := range []int{8080, 3000} {
		listener, listenErr = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", tryPort))
		if listenErr == nil {
			port = tryPort
			break
		}
	}
	if listener == nil {
		return fmt.Errorf("failed to start callback server on ports 8080 or 3000: %w", listenErr)
	}
	defer func() { _ = listener.Close() }()

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// Build authorization URL
	authURL := buildAuthURL(issuer, redirectURI, state, codeChallenge, clientID, prompt)

	if noBrowser {
		cmd.Printf("Open this URL to sign in:\n%s\n\n", authURL)
		cmd.Println("To sign in as a different account without changing your browser's active")
		cmd.Println("Janua account, open it in a private window. This terminal waits for the redirect.")
		cmd.Println()
	} else {
		cmd.Println("Opening browser for authentication...")
		cmd.Printf("If the browser doesn't open, visit:\n%s\n\n", authURL)

		// Open browser
		if err := openBrowser(authURL); err != nil {
			cmd.Printf("⚠️  Could not open browser automatically: %v\n", err)
		}
	}

	// Wait for callback
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}

			// Verify state
			if r.URL.Query().Get("state") != state {
				errChan <- fmt.Errorf("state mismatch - possible CSRF attack")
				http.Error(w, "State mismatch", http.StatusBadRequest)
				return
			}

			// Check for error
			if errMsg := r.URL.Query().Get("error"); errMsg != "" {
				errDesc := r.URL.Query().Get("error_description")
				errChan <- fmt.Errorf("OAuth error: %s - %s", errMsg, errDesc)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Login Failed</title></head>
<body style="font-family: system-ui, sans-serif; text-align: center; padding: 50px;">
<h1 style="color: #ef4444;">Authentication Failed</h1>
<p>%s: %s</p>
<p>You can close this window.</p>
</body>
</html>`, errMsg, errDesc)
				return
			}

			// Get authorization code
			code := r.URL.Query().Get("code")
			if code == "" {
				errChan <- fmt.Errorf("no authorization code received")
				http.Error(w, "No code received", http.StatusBadRequest)
				return
			}

			codeChan <- code

			// Success response
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Login Success</title></head>
<body style="font-family: system-ui, sans-serif; text-align: center; padding: 50px;">
<h1 style="color: #22c55e;">&#x2705; Authentication Successful!</h1>
<p>You can close this window and return to the terminal.</p>
<script>setTimeout(function(){window.close();}, 2000);</script>
</body>
</html>`)
		}),
	}

	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for code or error with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var code string
	select {
	case code = <-codeChan:
		// Success
	case err := <-errChan:
		_ = server.Shutdown(ctx)
		return &exitcodes.AuthenticationError{Err: fmt.Errorf("authentication failed: %w", err)}
	case <-ctx.Done():
		_ = server.Shutdown(ctx)
		return &exitcodes.TimeoutError{Err: fmt.Errorf("authentication timed out after 5 minutes")}
	}

	// Shutdown server
	_ = server.Shutdown(ctx)

	cmd.Println("✓ Authorization code received")
	cmd.Println("Exchanging for access token...")

	// Exchange code for tokens
	tokens, err := exchangeCodeForTokens(issuer, code, redirectURI, codeVerifier, clientID)
	if err != nil {
		return &exitcodes.AuthenticationError{Err: fmt.Errorf("failed to exchange code for tokens: %w", err)}
	}

	// Save credentials
	creds := &Credentials{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    tokens.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
		Issuer:       issuer,
	}

	if err := saveCredentials(creds); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	cmd.Println()
	cmd.Println("✅ Successfully logged in!")
	cmd.Println()
	if profile == config.DefaultProfile {
		cmd.Println("Your credentials have been saved. You can now use Enclii CLI commands.")
		cmd.Println("Run 'enclii whoami' to see your user information.")
	} else {
		cmd.Printf("Your credentials have been saved to profile %q.\n", profile)
		cmd.Printf("Use 'enclii --profile %s <command>' (or ENCLII_PROFILE=%s) to act as this identity;\n", profile, profile)
		cmd.Println("commands without a profile keep using your default login.")
		cmd.Printf("Run 'enclii --profile %s whoami' to see your user information.\n", profile)
	}

	return nil
}

func runLogout(cmd *cobra.Command) error {
	credsPath := getCredentialsPath()
	profile := config.ActiveProfile()

	if _, err := os.Stat(credsPath); os.IsNotExist(err) {
		if profile == config.DefaultProfile {
			cmd.Println("You are not logged in.")
		} else {
			cmd.Printf("You are not logged in on profile %q.\n", profile)
		}
		return nil
	}

	if err := os.Remove(credsPath); err != nil {
		return fmt.Errorf("failed to remove credentials: %w", err)
	}

	if profile == config.DefaultProfile {
		cmd.Println("✅ Successfully logged out.")
	} else {
		cmd.Printf("✅ Successfully logged out of profile %q. Other profiles are unchanged.\n", profile)
	}
	return nil
}

func runWhoami(cmd *cobra.Command, cfg *config.Config) error {
	loginHint := "enclii login"
	if profile := config.ActiveProfile(); profile != config.DefaultProfile {
		loginHint = fmt.Sprintf("enclii --profile %s login", profile)
	}

	creds, err := LoadCredentials()
	if err != nil {
		if profile := config.ActiveProfile(); profile != config.DefaultProfile {
			cmd.Printf("Not logged in on profile %q. Run '%s' to authenticate.\n", profile, loginHint)
		} else {
			cmd.Printf("Not logged in. Run '%s' to authenticate.\n", loginHint)
		}
		return nil
	}

	// Check if token is expired
	if time.Now().After(creds.ExpiresAt) {
		cmd.Printf("⚠️  Your session has expired. Run '%s' to re-authenticate.\n", loginHint)
		return nil
	}

	// Decode JWT to get user info (basic decode without verification for display)
	claims, err := decodeJWTClaims(creds.AccessToken)
	if err != nil {
		cmd.Printf("Logged in (token expires: %s)\n", creds.ExpiresAt.Format(time.RFC3339))
		return nil
	}

	cmd.Println("👤 Currently logged in as:")
	cmd.Println()
	cmd.Printf("   Profile: %s\n", config.ActiveProfile())
	if email, ok := claims["email"].(string); ok {
		cmd.Printf("   Email: %s\n", email)
	}
	if name, ok := claims["name"].(string); ok {
		cmd.Printf("   Name:  %s\n", name)
	}
	if sub, ok := claims["sub"].(string); ok {
		cmd.Printf("   ID:    %s\n", sub)
	}
	cmd.Printf("   Issuer: %s\n", creds.Issuer)
	cmd.Printf("   Expires: %s\n", creds.ExpiresAt.Format(time.RFC3339))

	return nil
}

// Helper functions

func generateCodeVerifier() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func generateState() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func buildAuthURL(issuer, redirectURI, state, codeChallenge, clientID, prompt string) string {
	// Issuer should be the full API endpoint (e.g., https://api.janua.dev)
	// No transformation needed - configure ENCLII_OIDC_ISSUER for custom deployments
	baseURL := issuer

	params := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {cliScopes},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	if prompt != "" {
		params.Set("prompt", prompt)
	}

	return baseURL + authorizePath + "?" + params.Encode()
}

func exchangeCodeForTokens(issuer, code, redirectURI, codeVerifier, clientID string) (*TokenResponse, error) {
	// Issuer should be the full API endpoint - no transformation needed
	tokenURL := issuer + tokenPath

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	}

	resp, err := http.PostForm(tokenURL, data)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var tokens TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}

	if tokens.Error != "" {
		return nil, fmt.Errorf("%s: %s", tokens.Error, tokens.ErrorDesc)
	}

	return &tokens, nil
}

// getCredentialsPath returns the active profile's credentials file (see
// config.GetCredentialsPath), so login, logout and whoami follow --profile.
func getCredentialsPath() string {
	return config.GetCredentialsPath()
}

func saveCredentials(creds *Credentials) error {
	credsPath := getCredentialsPath()

	// Create directory if needed
	dir := filepath.Dir(credsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	// Write with restricted permissions
	return os.WriteFile(credsPath, data, 0600)
}

// LoadCredentials loads saved credentials from disk
func LoadCredentials() (*Credentials, error) {
	credsPath := getCredentialsPath()

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

func decodeJWTClaims(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Try with padding
		payload, err = base64.StdEncoding.DecodeString(parts[1] + "==")
		if err != nil {
			return nil, err
		}
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}

	return claims, nil
}

func openBrowser(url string) error {
	cmd, err := browserCommand(url)
	if err != nil {
		return err
	}
	return cmd.Start()
}

// browserCommand builds the command that opens url. ENCLII_BROWSER, when set
// (outside Windows), is run through the shell with the URL passed as "$1", so
// the URL is never interpolated into the command string.
func browserCommand(url string) (*exec.Cmd, error) {
	if custom := strings.TrimSpace(os.Getenv("ENCLII_BROWSER")); custom != "" && runtime.GOOS != "windows" {
		return exec.Command("/bin/sh", "-c", custom+` "$1"`, "enclii-browser", url), nil // #nosec G204 -- operator-set opener; URL passed as a positional arg
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url), nil
	case "linux":
		return exec.Command("xdg-open", url), nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url), nil
	default:
		return nil, fmt.Errorf("unsupported platform")
	}
}
