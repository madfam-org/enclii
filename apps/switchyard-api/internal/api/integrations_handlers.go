// Package api provides HTTP handlers for the Switchyard API.
// This file contains handlers for third-party integrations (GitHub, etc.)
// that use OAuth tokens from Janua.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

// FlexibleTime is a time.Time that can unmarshal from multiple formats
// including RFC3339, RFC3339Nano, and timestamps without timezone
type FlexibleTime struct {
	time.Time
}

// UnmarshalJSON implements json.Unmarshaler for FlexibleTime
func (ft *FlexibleTime) UnmarshalJSON(data []byte) error {
	// Remove quotes
	s := strings.Trim(string(data), "\"")
	if s == "null" || s == "" {
		return nil
	}

	// Try multiple formats
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999", // No timezone (Janua format)
		"2006-01-02T15:04:05.999999",    // No timezone with microseconds
		"2006-01-02T15:04:05",           // No timezone basic
	}

	var err error
	for _, format := range formats {
		ft.Time, err = time.Parse(format, s)
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("unable to parse time: %s", s)
}

// JanuaIntegrationToken represents the response from Janua's integrations API
type JanuaIntegrationToken struct {
	Provider       string        `json:"provider"`
	AccessToken    string        `json:"access_token"`
	RefreshToken   *string       `json:"refresh_token,omitempty"`
	TokenExpiresAt *FlexibleTime `json:"token_expires_at,omitempty"`
	ProviderUserID *string       `json:"provider_user_id,omitempty"`
	ProviderEmail  *string       `json:"provider_email,omitempty"`
	LinkedAt       FlexibleTime  `json:"linked_at"`
}

// JanuaIntegrationStatus represents the status response from Janua
type JanuaIntegrationStatus struct {
	Provider       string        `json:"provider"`
	Linked         bool          `json:"linked"`
	ProviderEmail  *string       `json:"provider_email,omitempty"`
	LinkedAt       *FlexibleTime `json:"linked_at,omitempty"`
	CanAccessRepos bool          `json:"can_access_repos"`
}

// GitHubRepository represents a GitHub repository from the API
type GitHubRepository struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Private       bool      `json:"private"`
	DefaultBranch string    `json:"default_branch"`
	CloneURL      string    `json:"clone_url"`
	HTMLURL       string    `json:"html_url"`
	Description   string    `json:"description"`
	Language      string    `json:"language"`
	UpdatedAt     time.Time `json:"updated_at"`
	Owner         struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	} `json:"owner"`
}

// GitHubReposResponse is the API response for listing repositories
type GitHubReposResponse struct {
	Repositories []GitHubRepository `json:"repositories"`
	TotalCount   int                `json:"total_count"`
}

// IntegrationStatusResponse is the API response for checking integration status
type IntegrationStatusResponse struct {
	Provider       string  `json:"provider"`
	Linked         bool    `json:"linked"`
	ProviderEmail  *string `json:"provider_email,omitempty"`
	CanAccessRepos bool    `json:"can_access_repos"`
	Message        string  `json:"message,omitempty"`
}

// getJanuaToken retrieves the user's OAuth token for a provider from Janua
func (h *Handler) getJanuaToken(ctx context.Context, provider, jwtToken string) (*JanuaIntegrationToken, error) {
	if h.config.JanuaAPIURL == "" {
		return nil, fmt.Errorf("Janua API URL not configured")
	}

	url := fmt.Sprintf("%s/api/v1/integrations/%s/token", h.config.JanuaAPIURL, provider)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Janua API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("GitHub account not linked. Please connect your GitHub account first")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Janua API error: %d - %s", resp.StatusCode, string(body))
	}

	var tokenResp JanuaIntegrationToken
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

// getJanuaIntegrationStatus checks if a provider is linked in Janua
func (h *Handler) getJanuaIntegrationStatus(ctx context.Context, provider, jwtToken string) (*JanuaIntegrationStatus, error) {
	if h.config.JanuaAPIURL == "" {
		return nil, fmt.Errorf("Janua API URL not configured")
	}

	url := fmt.Sprintf("%s/api/v1/integrations/%s/status", h.config.JanuaAPIURL, provider)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Janua API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Janua API error: %d - %s", resp.StatusCode, string(body))
	}

	var statusResp JanuaIntegrationStatus
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode status response: %w", err)
	}

	return &statusResp, nil
}

// listGitHubRepos fetches repositories from GitHub using the provided access token
func (h *Handler) listGitHubRepos(ctx context.Context, accessToken string) ([]GitHubRepository, error) {
	// Fetch user repos with pagination (up to 100 per page, we'll fetch first page)
	url := "https://api.github.com/user/repos?visibility=all&affiliation=owner,collaborator,organization_member&sort=updated&per_page=100"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call GitHub API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var repos []GitHubRepository
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	return repos, nil
}

// GetGitHubStatus returns the GitHub integration status for the current user
// GET /v1/integrations/github/status
func (h *Handler) GetGitHubStatus(c *gin.Context) {
	ctx := c.Request.Context()

	// Get the IDP token (Janua token) from X-IDP-Token header
	// This is the token issued by Janua that can be used to call Janua APIs
	idpToken := c.GetHeader("X-IDP-Token")
	if idpToken == "" {
		// Fall back to Authorization header (for backwards compatibility or local auth mode)
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}
		idpToken = strings.TrimPrefix(authHeader, "Bearer ")
	}

	status, err := h.getJanuaIntegrationStatus(ctx, "github", idpToken)
	if err != nil {
		h.logger.Error(ctx, "Failed to get GitHub integration status", logging.Error("error", err))

		c.JSON(http.StatusOK, IntegrationStatusResponse{
			Provider:       "github",
			Linked:         false,
			CanAccessRepos: false,
			Message:        "Unable to check GitHub status. Please try again.",
		})
		return
	}

	message := ""
	if !status.Linked {
		message = "GitHub account is not connected. Please link your GitHub account to import repositories."
	} else if !status.CanAccessRepos {
		message = "GitHub is connected but lacks repository access. Please re-authorize with repo permissions."
	}

	c.JSON(http.StatusOK, IntegrationStatusResponse{
		Provider:       "github",
		Linked:         status.Linked,
		ProviderEmail:  status.ProviderEmail,
		CanAccessRepos: status.CanAccessRepos,
		Message:        message,
	})
}

// ListGitHubRepos returns the user's GitHub repositories
// GET /v1/integrations/github/repos
func (h *Handler) ListGitHubRepos(c *gin.Context) {
	ctx := c.Request.Context()

	// Get the IDP token (Janua token) from X-IDP-Token header
	// This is the token issued by Janua that can be used to call Janua APIs
	idpToken := c.GetHeader("X-IDP-Token")
	if idpToken == "" {
		// Fall back to Authorization header (for backwards compatibility or local auth mode)
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}
		idpToken = strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Step 1: Get the user's GitHub access token from Janua
	tokenResp, err := h.getJanuaToken(ctx, "github", idpToken)
	if err != nil {
		h.logger.Error(ctx, "Failed to get GitHub token from Janua", logging.Error("error", err))

		// Return helpful error message based on the error type
		if strings.Contains(err.Error(), "not linked") || strings.Contains(err.Error(), "not connected") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "GitHub account not linked",
				"message": "Please connect your GitHub account first via Settings > Integrations",
				"code":    "GITHUB_NOT_LINKED",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve GitHub credentials",
			"message": "Unable to access your GitHub account. Please try re-linking.",
		})
		return
	}

	// Step 2: Use the GitHub access token to list repositories
	repos, err := h.listGitHubRepos(ctx, tokenResp.AccessToken)
	if err != nil {
		h.logger.Error(ctx, "Failed to list GitHub repositories", logging.Error("error", err))

		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403") {
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "GitHub access denied",
				"message": "Your GitHub token may have expired. Please re-link your GitHub account.",
				"code":    "GITHUB_TOKEN_EXPIRED",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to list repositories",
			"message": "Unable to fetch repositories from GitHub. Please try again.",
		})
		return
	}

	h.logger.Info(ctx, "Listed GitHub repositories", logging.Int("repo_count", len(repos)))

	c.JSON(http.StatusOK, GitHubReposResponse{
		Repositories: repos,
		TotalCount:   len(repos),
	})
}

// LinkGitHubRequest represents the request to initiate GitHub linking
type LinkGitHubRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

// LinkGitHubResponse represents the response with the authorization URL
type LinkGitHubResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
	Provider         string `json:"provider"`
}

// LinkGitHub initiates the GitHub OAuth linking flow via Janua
// POST /v1/integrations/github/link
func (h *Handler) LinkGitHub(c *gin.Context) {
	ctx := c.Request.Context()

	// Get redirect_uri from query or body
	redirectURI := c.Query("redirect_uri")
	if redirectURI == "" {
		var req LinkGitHubRequest
		if err := c.ShouldBindJSON(&req); err == nil {
			redirectURI = req.RedirectURI
		}
	}

	if redirectURI == "" {
		redirectURI = c.GetHeader("Referer")
	}

	// Get the JWT token from the Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
		return
	}

	jwtToken := strings.TrimPrefix(authHeader, "Bearer ")

	// Call Janua's OAuth link endpoint
	linkURL, err := h.initiateJanuaGitHubLink(ctx, jwtToken, redirectURI)
	if err != nil {
		h.logger.Error(ctx, "Failed to initiate GitHub link", logging.Error("error", err))

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to initiate GitHub linking",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, LinkGitHubResponse{
		AuthorizationURL: linkURL.AuthURL,
		State:            linkURL.State,
		Provider:         "github",
	})
}

// JanuaLinkResponse represents the response from Janua's link endpoint
type JanuaLinkResponse struct {
	AuthURL  string `json:"authorization_url"`
	State    string `json:"state"`
	Provider string `json:"provider"`
	Action   string `json:"action"`
}

// initiateJanuaGitHubLink calls Janua's OAuth link endpoint
func (h *Handler) initiateJanuaGitHubLink(ctx context.Context, jwtToken, redirectURI string) (*JanuaLinkResponse, error) {
	if h.config.JanuaAPIURL == "" {
		return nil, fmt.Errorf("Janua API URL not configured")
	}

	url := fmt.Sprintf("%s/api/v1/auth/oauth/link/github", h.config.JanuaAPIURL)
	if redirectURI != "" {
		url = fmt.Sprintf("%s?redirect_uri=%s", url, redirectURI)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Janua API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Janua API error: %d - %s", resp.StatusCode, string(body))
	}

	var linkResp JanuaLinkResponse
	if err := json.Unmarshal(body, &linkResp); err != nil {
		return nil, fmt.Errorf("failed to decode link response: %w", err)
	}

	return &linkResp, nil
}

// AnalyzeRepositoryRequest represents the request to analyze a repository
type AnalyzeRepositoryRequest struct {
	Branch  string `json:"branch"`
	AppPath string `json:"app_path,omitempty"` // Optional: analyze specific subdirectory
}

// AnalyzeRepository scans a GitHub repository for deployable services
// POST /v1/integrations/github/repos/:owner/:repo/analyze
func (h *Handler) AnalyzeRepository(c *gin.Context) {
	ctx := c.Request.Context()
	owner := c.Param("owner")
	repo := c.Param("repo")

	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner and repo are required"})
		return
	}

	// Get branch from query or body
	branch := c.Query("branch")
	if branch == "" {
		var req AnalyzeRepositoryRequest
		if err := c.ShouldBindJSON(&req); err == nil && req.Branch != "" {
			branch = req.Branch
		}
	}
	if branch == "" {
		branch = "main" // Default branch
	}

	// Get the IDP token (Janua token) from X-IDP-Token header
	idpToken := c.GetHeader("X-IDP-Token")
	if idpToken == "" {
		// Fall back to Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}
		idpToken = strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Get the user's GitHub access token from Janua
	tokenResp, err := h.getJanuaToken(ctx, "github", idpToken)
	if err != nil {
		h.logger.Error(ctx, "Failed to get GitHub token from Janua", logging.Error("error", err))

		if strings.Contains(err.Error(), "not linked") || strings.Contains(err.Error(), "not connected") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "GitHub account not linked",
				"message": "Please connect your GitHub account first via Settings > Integrations",
				"code":    "GITHUB_NOT_LINKED",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve GitHub credentials",
			"message": "Unable to access your GitHub account. Please try re-linking.",
		})
		return
	}

	// Create analyzer and analyze the repository
	analyzer := services.NewRepositoryAnalyzer(h.logger)
	result, err := analyzer.AnalyzeRepository(ctx, tokenResp.AccessToken, owner, repo, branch)
	if err != nil {
		h.logger.Error(ctx, "Failed to analyze repository",
			logging.String("owner", owner),
			logging.String("repo", repo),
			logging.Error("error", err),
		)

		if strings.Contains(err.Error(), "404") {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "Repository not found",
				"message": fmt.Sprintf("Repository %s/%s not found or you don't have access", owner, repo),
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to analyze repository",
			"message": err.Error(),
		})
		return
	}

	h.logger.Info(ctx, "Repository analysis completed",
		logging.String("owner", owner),
		logging.String("repo", repo),
		logging.Int("services_found", len(result.Services)),
		logging.Bool("monorepo_detected", result.MonorepoDetected),
	)

	c.JSON(http.StatusOK, result)
}

// GetRepositoryBranches returns the branches for a GitHub repository
// GET /v1/integrations/github/repos/:owner/:repo/branches
func (h *Handler) GetRepositoryBranches(c *gin.Context) {
	ctx := c.Request.Context()
	owner := c.Param("owner")
	repo := c.Param("repo")

	if owner == "" || repo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "owner and repo are required"})
		return
	}

	// Get the IDP token
	idpToken := c.GetHeader("X-IDP-Token")
	if idpToken == "" {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			return
		}
		idpToken = strings.TrimPrefix(authHeader, "Bearer ")
	}

	// Get GitHub token
	tokenResp, err := h.getJanuaToken(ctx, "github", idpToken)
	if err != nil {
		h.logger.Error(ctx, "Failed to get GitHub token from Janua", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve GitHub credentials"})
		return
	}

	// Fetch branches from GitHub
	branches, err := h.listGitHubBranches(ctx, tokenResp.AccessToken, owner, repo)
	if err != nil {
		h.logger.Error(ctx, "Failed to list branches", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list branches"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"branches": branches,
		"count":    len(branches),
	})
}

// GitHubBranch represents a GitHub branch
type GitHubBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

// listGitHubBranches fetches branches for a repository
func (h *Handler) listGitHubBranches(ctx context.Context, accessToken, owner, repo string) ([]GitHubBranch, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches?per_page=100", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var branches []GitHubBranch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, err
	}

	return branches, nil
}
