package switchyard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Client is a client for the Switchyard API
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewClient creates a new Switchyard API client
func NewClient(baseURL, apiKey string, logger *zap.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// CreatePreviewRequest represents a request to create a preview environment
type CreatePreviewRequest struct {
	ServiceID    string `json:"service_id"`
	PRNumber     int    `json:"pr_number"`
	PRTitle      string `json:"pr_title,omitempty"`
	PRURL        string `json:"pr_url,omitempty"`
	PRAuthor     string `json:"pr_author,omitempty"`
	PRBranch     string `json:"pr_branch"`
	PRBaseBranch string `json:"pr_base_branch,omitempty"`
	CommitSHA    string `json:"commit_sha"`
}

// PreviewResponse represents a preview environment response
type PreviewResponse struct {
	Preview struct {
		ID               string `json:"id"`
		PreviewURL       string `json:"preview_url"`
		PreviewSubdomain string `json:"preview_subdomain"`
		Status           string `json:"status"`
	} `json:"preview"`
	Message string `json:"message"`
	Action  string `json:"action"` // "created" or "updated"
}

// ServiceByRepoResponse represents a service lookup response
type ServiceByRepoResponse struct {
	Services []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		ProjectID string `json:"project_id"`
	} `json:"services"`
}

// CreatePreview creates or updates a preview environment for a PR
func (c *Client) CreatePreview(ctx context.Context, req *CreatePreviewRequest) (*PreviewResponse, error) {
	logger := c.logger.With(
		zap.Int("pr_number", req.PRNumber),
		zap.String("branch", req.PRBranch),
		zap.String("commit", req.CommitSHA[:8]),
	)

	logger.Info("Creating preview environment")

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/previews", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		logger.Error("Failed to create preview",
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(respBody)),
		)
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var previewResp PreviewResponse
	if err := json.Unmarshal(respBody, &previewResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	logger.Info("Preview environment created/updated",
		zap.String("preview_id", previewResp.Preview.ID),
		zap.String("preview_url", previewResp.Preview.PreviewURL),
		zap.String("action", previewResp.Action),
	)

	return &previewResp, nil
}

// ClosePreviewByPR closes a preview environment by service ID and PR number
func (c *Client) ClosePreviewByPR(ctx context.Context, serviceID string, prNumber int) error {
	logger := c.logger.With(
		zap.String("service_id", serviceID),
		zap.Int("pr_number", prNumber),
	)

	logger.Info("Closing preview environment")

	// First, find the preview by service and PR number
	url := fmt.Sprintf("%s/v1/services/%s/previews?pr_number=%d", c.baseURL, serviceID, prNumber)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		logger.Info("No preview found to close")
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var listResp struct {
		Previews []struct {
			ID string `json:"id"`
		} `json:"previews"`
	}
	if err := json.Unmarshal(respBody, &listResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Close each preview found
	for _, preview := range listResp.Previews {
		if err := c.closePreview(ctx, preview.ID); err != nil {
			logger.Error("Failed to close preview",
				zap.String("preview_id", preview.ID),
				zap.Error(err),
			)
			// Continue with other previews
		}
	}

	return nil
}

// closePreview closes a specific preview environment by ID
func (c *Client) closePreview(ctx context.Context, previewID string) error {
	url := fmt.Sprintf("%s/v1/previews/%s/close", c.baseURL, previewID)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	c.logger.Info("Preview environment closed", zap.String("preview_id", previewID))
	return nil
}

// GetServicesByRepo finds services that use a specific git repository
func (c *Client) GetServicesByRepo(ctx context.Context, repoURL string) (*ServiceByRepoResponse, error) {
	url := fmt.Sprintf("%s/v1/services?git_repo=%s", c.baseURL, repoURL)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result ServiceByRepoResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}
