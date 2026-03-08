package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ==================== Functions API ====================

// FunctionInvokeResult represents the result of invoking a function
type FunctionInvokeResult struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
	ColdStart  bool   `json:"cold_start"`
	Duration   string `json:"duration"`
}

// ListFunctions returns all functions for a project
func (c *APIClient) ListFunctions(ctx context.Context, projectSlug string) ([]*types.Function, error) {
	var response struct {
		Functions []*types.Function `json:"functions"`
	}

	if err := c.get(ctx, fmt.Sprintf("/v1/projects/%s/functions", projectSlug), &response); err != nil {
		return nil, fmt.Errorf("failed to list functions: %w", err)
	}

	return response.Functions, nil
}

// ListAllFunctions returns all functions the user has access to
func (c *APIClient) ListAllFunctions(ctx context.Context) ([]*types.Function, error) {
	var response struct {
		Functions []*types.Function `json:"functions"`
	}

	if err := c.get(ctx, "/v1/functions", &response); err != nil {
		return nil, fmt.Errorf("failed to list all functions: %w", err)
	}

	return response.Functions, nil
}

// GetFunction returns a function by name or ID
func (c *APIClient) GetFunction(ctx context.Context, nameOrID string) (*types.Function, error) {
	var fn types.Function
	if err := c.get(ctx, fmt.Sprintf("/v1/functions/%s", nameOrID), &fn); err != nil {
		return nil, fmt.Errorf("failed to get function: %w", err)
	}

	return &fn, nil
}

// CreateFunction creates a new function
func (c *APIClient) CreateFunction(ctx context.Context, projectSlug, name string, config types.FunctionConfig) (*types.Function, error) {
	payload := map[string]interface{}{
		"name":   name,
		"config": config,
	}

	var fn types.Function
	if err := c.post(ctx, fmt.Sprintf("/v1/projects/%s/functions", projectSlug), payload, &fn); err != nil {
		return nil, fmt.Errorf("failed to create function: %w", err)
	}

	return &fn, nil
}

// DeleteFunction deletes a function by name or ID
func (c *APIClient) DeleteFunction(ctx context.Context, nameOrID string) error {
	resp, err := c.makeRequest(ctx, "DELETE", fmt.Sprintf("/v1/functions/%s", nameOrID), nil)
	if err != nil {
		return fmt.Errorf("failed to delete function: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete function (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// InvokeFunction invokes a function with optional data
func (c *APIClient) InvokeFunction(ctx context.Context, nameOrID, data string) (*FunctionInvokeResult, error) {
	var payload interface{}
	if data != "" {
		// Parse the JSON data
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			// If it's not valid JSON, send as raw string
			payload = map[string]string{"body": data}
		}
	}

	var result FunctionInvokeResult
	if err := c.post(ctx, fmt.Sprintf("/v1/functions/%s/invoke", nameOrID), payload, &result); err != nil {
		return nil, fmt.Errorf("failed to invoke function: %w", err)
	}

	return &result, nil
}

// GetFunctionLogs returns recent logs for a function
func (c *APIClient) GetFunctionLogs(ctx context.Context, nameOrID string, lines int) ([]string, error) {
	path := fmt.Sprintf("/v1/functions/%s/logs?lines=%d", nameOrID, lines)

	var response struct {
		Logs []string `json:"logs"`
	}

	if err := c.get(ctx, path, &response); err != nil {
		return nil, fmt.Errorf("failed to get function logs: %w", err)
	}

	return response.Logs, nil
}

// ==================== Admin API ====================

// OnboardProject sends a full onboarding request including optional provisioning specs.
func (c *APIClient) OnboardProject(ctx context.Context, req *types.OnboardingRequest, result interface{}) error {
	return c.post(ctx, "/v1/admin/onboard", req, result)
}

func (c *APIClient) PreflightOnboard(ctx context.Context, req *types.OnboardingRequest, result *types.PreflightResult) error {
	return c.post(ctx, "/v1/admin/onboard/preflight", req, result)
}
