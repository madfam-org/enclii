package signup

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPJanuaClient talks to Janua's public HTTP API. It's the production
// impl of the JanuaClient interface the signup service uses.
//
// Janua already ships:
//
//	POST /api/v1/auth/signup       — create user (returns user + tokens)
//	POST /api/v1/auth/oauth/link/{provider} — build authorize URL
//	GET  /api/v1/auth/oauth/link/callback/{provider} — completion
//
// We deliberately avoid adding new surfaces in Janua for Sprint 1 — the
// only net-new Janua change is an admin/internal lookup helper that
// returns a Janua user's GitHub connection metadata by sub (in case the
// normal callback path isn't reachable).
type HTTPJanuaClient struct {
	baseURL    string
	adminToken string // machine-to-machine token used for admin lookups
	http       *http.Client
}

// NewHTTPJanuaClient constructs the default client.
func NewHTTPJanuaClient(baseURL, adminToken string) *HTTPJanuaClient {
	return &HTTPJanuaClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		adminToken: adminToken,
		http:       &http.Client{Timeout: 15 * time.Second},
	}
}

// RegisterUser creates a new Janua user. We generate a random password —
// the user will set their own later via password reset or magic link
// (deferred to Sprint 2).
func (c *HTTPJanuaClient) RegisterUser(ctx context.Context, email, companyName string) (string, error) {
	randomPassword, err := generateRandomPassword()
	if err != nil {
		return "", fmt.Errorf("generate random password: %w", err)
	}

	body, _ := json.Marshal(map[string]any{
		"email":    email,
		"password": randomPassword,
		"metadata": map[string]any{
			"source":       "enclii-signup",
			"company_name": companyName,
		},
	})

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/api/v1/auth/signup", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("janua signup http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		if strings.Contains(strings.ToLower(string(raw)), "already registered") ||
			strings.Contains(strings.ToLower(string(raw)), "email already") {
			return "", ErrEmailAlreadyRegistered
		}
		return "", fmt.Errorf("janua signup %d: %s", resp.StatusCode, string(raw))
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("janua signup %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode janua signup: %w", err)
	}
	if result.User.ID == "" {
		return "", fmt.Errorf("janua signup returned empty user id")
	}
	return result.User.ID, nil
}

// BuildGithubAuthorizeURL asks Janua to mint an authorize URL for GitHub.
// Janua takes the (state, redirect_uri) and returns the upstream GitHub URL.
func (c *HTTPJanuaClient) BuildGithubAuthorizeURL(ctx context.Context, januaUserSub, state, callbackURL string) (string, error) {
	// Janua's /oauth/link/{provider} is user-authenticated, but our user
	// isn't signed in yet (they just verified email). For Sprint 1 we
	// call the admin-authenticated variant: POST /oauth/link/github/on-behalf
	// with the sub in the body. If that endpoint doesn't exist yet, we
	// fall back to returning an error the handler can surface — the
	// operator enables it via Janua's admin panel (see companion PR).
	body, _ := json.Marshal(map[string]any{
		"user_sub":     januaUserSub,
		"state":        state,
		"redirect_uri": callbackURL,
	})
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/api/v1/auth/oauth/link/github/on-behalf", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("janua oauth link http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("janua oauth link %d: %s", resp.StatusCode, string(raw))
	}
	var result struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode janua oauth link: %w", err)
	}
	if result.AuthorizationURL == "" {
		return "", fmt.Errorf("janua returned empty authorization_url")
	}
	return result.AuthorizationURL, nil
}

// CompleteGithubOAuth exchanges the OAuth code back to Janua, which has
// already done the PKCE-ish exchange with GitHub and now holds the
// access token server-side. We ask Janua to hand us (username, token)
// for THIS user so we can write it to our K8s Secret.
func (c *HTTPJanuaClient) CompleteGithubOAuth(ctx context.Context, januaUserSub, code string) (string, string, error) {
	params := url.Values{}
	params.Set("code", code)
	params.Set("user_sub", januaUserSub)

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/api/v1/auth/oauth/link/github/complete?"+params.Encode(), nil)
	if err != nil {
		return "", "", err
	}
	if c.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminToken)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("janua oauth complete http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("janua oauth complete %d: %s", resp.StatusCode, string(raw))
	}
	var result struct {
		GithubUsername string `json:"github_username"`
		AccessToken    string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decode janua oauth complete: %w", err)
	}
	if result.AccessToken == "" {
		return "", "", fmt.Errorf("janua returned empty access token")
	}
	return result.GithubUsername, result.AccessToken, nil
}

// generateRandomPassword produces a 32-byte base64url password. The user
// never sees this; they'll rotate it via /password/forgot when they want
// to log in with a password, or they'll just stay on the OIDC path.
func generateRandomPassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Prefix with "Enc!" so it trivially passes Janua's complexity policy
	// (uppercase + symbol + number + length) regardless of tuning.
	return "Enc!1" + base64.RawURLEncoding.EncodeToString(b), nil
}
