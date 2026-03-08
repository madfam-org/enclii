package client

import (
	"context"
	"fmt"
)

// HealthResponse contains the API health check response.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// Health checks the API health endpoint.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var health HealthResponse
	if err := c.get(ctx, "/health", &health); err != nil {
		return nil, fmt.Errorf("health check: %w", err)
	}
	return &health, nil
}
