package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	defaultBaseURL = "https://api.cloudflare.com/client/v4"
	defaultTimeout = 30 * time.Second
)

// Client is a Cloudflare API client
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiToken   string
	accountID  string
	zoneID     string
	tunnelID   string
}

// NewClient creates a new Cloudflare API client
func NewClient(cfg *Config) (*Client, error) {
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("cloudflare: API token is required")
	}
	if cfg.AccountID == "" {
		return nil, fmt.Errorf("cloudflare: account ID is required")
	}
	if cfg.ZoneID == "" {
		return nil, fmt.Errorf("cloudflare: zone ID is required")
	}

	baseURL := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	client := &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		baseURL:   baseURL,
		apiToken:  cfg.APIToken,
		accountID: cfg.AccountID,
		zoneID:    cfg.ZoneID,
		tunnelID:  cfg.TunnelID,
	}

	logrus.WithFields(logrus.Fields{
		"account_id": cfg.AccountID,
		"zone_id":    cfg.ZoneID,
		"tunnel_id":  cfg.TunnelID,
	}).Info("Cloudflare API client initialized")

	return client, nil
}

// doRequest performs an authenticated HTTP request to the Cloudflare API
func (c *Client) doRequest(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Response, error) {
	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "enclii-switchyard/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: request failed: %w", err)
	}

	return resp, nil
}

// get performs a GET request and decodes the response
func (c *Client) get(ctx context.Context, path string, query url.Values, result interface{}) error {
	resp, err := c.doRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return c.handleResponse(resp, result)
}

// put performs a PUT request and decodes the response
func (c *Client) put(ctx context.Context, path string, body io.Reader, result interface{}) error {
	resp, err := c.doRequest(ctx, http.MethodPut, path, nil, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return c.handleResponse(resp, result)
}

// patch performs a PATCH request and decodes the response.
//
// Zone settings are the reason this exists: Cloudflare's
// /zones/{id}/settings/{setting} endpoint takes PATCH, and PUT is not an
// accepted substitute there.
func (c *Client) patch(ctx context.Context, path string, body io.Reader, result interface{}) error {
	resp, err := c.doRequest(ctx, http.MethodPatch, path, nil, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return c.handleResponse(resp, result)
}

// post performs a POST request and decodes the response
func (c *Client) post(ctx context.Context, path string, body io.Reader, result interface{}) error {
	resp, err := c.doRequest(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return c.handleResponse(resp, result)
}

// delete performs a DELETE request and decodes the response
func (c *Client) httpDelete(ctx context.Context, path string, result interface{}) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	return c.handleResponse(resp, result)
}

// handleResponse processes the API response
func (c *Client) handleResponse(resp *http.Response, result interface{}) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cloudflare: failed to read response: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		var apiResp APIResponse[interface{}]
		if err := json.Unmarshal(body, &apiResp); err == nil && len(apiResp.Errors) > 0 {
			// Typed so callers can errors.As() the Cloudflare error code.
			// Renders identically to the previous fmt.Errorf.
			return &APIError{
				Code:    apiResp.Errors[0].Code,
				Message: apiResp.Errors[0].Message,
			}
		}
		return fmt.Errorf("cloudflare: HTTP error %d: %s", resp.StatusCode, string(body))
	}

	// Decode response
	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("cloudflare: failed to decode response: %w", err)
		}
	}

	return nil
}

// GetAccountID returns the configured account ID
func (c *Client) GetAccountID() string {
	return c.accountID
}

// GetZoneID returns the configured zone ID
func (c *Client) GetZoneID() string {
	return c.zoneID
}

// GetTunnelID returns the configured tunnel ID
func (c *Client) GetTunnelID() string {
	return c.tunnelID
}

// VerifyToken tests the API token by making a simple request
func (c *Client) VerifyToken(ctx context.Context) error {
	var resp APIResponse[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}]

	err := c.get(ctx, "/user/tokens/verify", nil, &resp)
	if err != nil {
		return fmt.Errorf("cloudflare: token verification failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("cloudflare: token verification failed")
	}

	logrus.Info("Cloudflare API token verified successfully")
	return nil
}
