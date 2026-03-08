package client

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// EnvVarRequest contains parameters for creating or updating an environment variable.
type EnvVarRequest struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

// EnvVar represents an environment variable in API responses.
type EnvVar struct {
	ID            uuid.UUID  `json:"id"`
	ServiceID     uuid.UUID  `json:"service_id"`
	EnvironmentID *uuid.UUID `json:"environment_id,omitempty"`
	Key           string     `json:"key"`
	Value         string     `json:"value"`
	IsSecret      bool       `json:"is_secret"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ListEnvVars returns all environment variables for a service.
func (c *Client) ListEnvVars(ctx context.Context, serviceID string, environmentID *string) ([]EnvVar, error) {
	endpoint := fmt.Sprintf("/v1/services/%s/env-vars", serviceID)
	if environmentID != nil && *environmentID != "" {
		endpoint += "?environment_id=" + url.QueryEscape(*environmentID)
	}

	var resp struct {
		EnvVars []EnvVar `json:"environment_variables"`
	}
	if err := c.get(ctx, endpoint, &resp); err != nil {
		return nil, fmt.Errorf("list env vars: %w", err)
	}
	return resp.EnvVars, nil
}

// CreateEnvVar creates a new environment variable.
func (c *Client) CreateEnvVar(ctx context.Context, serviceID string, req EnvVarRequest, environmentID *string) (*EnvVar, error) {
	payload := map[string]interface{}{
		"key":       req.Key,
		"value":     req.Value,
		"is_secret": req.IsSecret,
	}
	if environmentID != nil && *environmentID != "" {
		payload["environment_id"] = *environmentID
	}

	var result EnvVar
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/env-vars", serviceID), payload, &result); err != nil {
		return nil, fmt.Errorf("create env var: %w", err)
	}
	return &result, nil
}

// BulkCreateEnvVars creates multiple environment variables at once.
func (c *Client) BulkCreateEnvVars(ctx context.Context, serviceID string, vars []EnvVarRequest, environmentID *string) ([]EnvVar, error) {
	payload := map[string]interface{}{"variables": vars}
	if environmentID != nil && *environmentID != "" {
		payload["environment_id"] = *environmentID
	}

	var resp struct {
		EnvVars []EnvVar `json:"environment_variables"`
	}
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/env-vars/bulk", serviceID), payload, &resp); err != nil {
		return nil, fmt.Errorf("bulk create env vars: %w", err)
	}
	return resp.EnvVars, nil
}

// DeleteEnvVar deletes an environment variable.
func (c *Client) DeleteEnvVar(ctx context.Context, serviceID, envVarID string) error {
	if err := c.delete(ctx, fmt.Sprintf("/v1/services/%s/env-vars/%s", serviceID, envVarID)); err != nil {
		return fmt.Errorf("delete env var: %w", err)
	}
	return nil
}

// RevealEnvVar reveals the actual value of a secret environment variable.
func (c *Client) RevealEnvVar(ctx context.Context, serviceID, envVarID string) (string, error) {
	var resp struct {
		Value string `json:"value"`
	}
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/env-vars/%s/reveal", serviceID, envVarID), nil, &resp); err != nil {
		return "", fmt.Errorf("reveal env var: %w", err)
	}
	return resp.Value, nil
}
