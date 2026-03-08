package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// RoundhouseClient is an HTTP client for communicating with the Roundhouse build worker
type RoundhouseClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewRoundhouseClient creates a new Roundhouse API client
func NewRoundhouseClient(baseURL, apiKey string) *RoundhouseClient {
	return &RoundhouseClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// RoundhouseBuildConfig matches Roundhouse's queue.BuildConfig
type RoundhouseBuildConfig struct {
	Type       string            `json:"type"`
	Dockerfile string            `json:"dockerfile"`
	Buildpack  string            `json:"buildpack"`
	Context    string            `json:"context"`
	BuildArgs  map[string]string `json:"build_args"`
	Target     string            `json:"target"`
}

// EnqueueRequest is the request body for enqueueing a build job
type EnqueueRequest struct {
	ReleaseID   uuid.UUID             `json:"release_id"`
	ServiceID   uuid.UUID             `json:"service_id"`
	ServiceName string                `json:"service_name"` // Human-readable service name for image tagging
	ProjectID   uuid.UUID             `json:"project_id"`
	ProjectSlug string                `json:"project_slug"` // Project slug for scoped image naming
	GitRepo     string                `json:"git_repo"`
	GitSHA      string                `json:"git_sha"`
	GitBranch   string                `json:"git_branch"`
	BuildConfig RoundhouseBuildConfig `json:"build_config"`
	CallbackURL string                `json:"callback_url"`
	Priority    int                   `json:"priority"`
}

// EnqueueResponse is the response from enqueueing a build job
type EnqueueResponse struct {
	JobID          uuid.UUID  `json:"job_id"`
	Position       int        `json:"position"`
	EstimatedStart *time.Time `json:"estimated_start,omitempty"`
}

// BuildServiceConfigToRoundhouse converts SDK BuildConfig to Roundhouse format
func BuildServiceConfigToRoundhouse(cfg types.BuildConfig) RoundhouseBuildConfig {
	context := cfg.Context
	if context == "" {
		context = "." // Default context if not specified
	}
	return RoundhouseBuildConfig{
		Type:       string(cfg.Type),
		Dockerfile: cfg.Dockerfile,
		Buildpack:  cfg.Buildpack,
		Context:    context,
		BuildArgs:  cfg.BuildArgs,
		Target:     cfg.Target,
	}
}

// Enqueue sends a build job to the Roundhouse queue
func (c *RoundhouseClient) Enqueue(ctx context.Context, req *EnqueueRequest) (*EnqueueResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal enqueue request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/enqueue", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to roundhouse: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("roundhouse returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var enqueueResp EnqueueResponse
	if err := json.Unmarshal(respBody, &enqueueResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &enqueueResp, nil
}

// GetJobStatus retrieves the status of a build job
func (c *RoundhouseClient) GetJobStatus(ctx context.Context, jobID uuid.UUID) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/jobs/"+jobID.String()+"/status", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("roundhouse returned status %d", resp.StatusCode)
	}

	var statusResp struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return statusResp.Status, nil
}

// CancelJob cancels a queued or running build job
func (c *RoundhouseClient) CancelJob(ctx context.Context, jobID uuid.UUID) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/jobs/"+jobID.String()+"/cancel", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("roundhouse returned status %d", resp.StatusCode)
	}

	return nil
}

// HealthCheck verifies connectivity to Roundhouse
func (c *RoundhouseClient) HealthCheck(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to connect to roundhouse: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("roundhouse health check failed with status %d", resp.StatusCode)
	}

	return nil
}
