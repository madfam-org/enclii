package client

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CreateServiceRequest contains parameters for creating a service.
type CreateServiceRequest struct {
	Name        string                 `json:"name"`
	GitRepo     string                 `json:"git_repo"`
	BuildConfig map[string]interface{} `json:"build_config,omitempty"`
}

// CreateService creates a new service in a project.
func (c *Client) CreateService(ctx context.Context, projectSlug string, req CreateServiceRequest) (*types.Service, error) {
	var svc types.Service
	if err := c.post(ctx, fmt.Sprintf("/v1/projects/%s/services", projectSlug), req, &svc); err != nil {
		return nil, fmt.Errorf("create service: %w", err)
	}
	return &svc, nil
}

// GetService retrieves a service by ID.
func (c *Client) GetService(ctx context.Context, serviceID string) (*types.Service, error) {
	var svc types.Service
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s", serviceID), &svc); err != nil {
		return nil, fmt.Errorf("get service: %w", err)
	}
	return &svc, nil
}

// ListServices returns all services in a project.
func (c *Client) ListServices(ctx context.Context, projectSlug string) ([]*types.Service, error) {
	var resp struct {
		Services []*types.Service `json:"services"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/projects/%s/services", projectSlug), &resp); err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	return resp.Services, nil
}

// DeleteService deletes a service by ID.
func (c *Client) DeleteService(ctx context.Context, serviceID string) error {
	if err := c.delete(ctx, fmt.Sprintf("/v1/services/%s", serviceID)); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}
