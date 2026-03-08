package client

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CreateEnvironment creates a new environment in a project.
func (c *Client) CreateEnvironment(ctx context.Context, projectSlug, envName string) (*types.Environment, error) {
	payload := map[string]string{"name": envName}
	var env types.Environment
	if err := c.post(ctx, fmt.Sprintf("/v1/projects/%s/environments", projectSlug), payload, &env); err != nil {
		return nil, fmt.Errorf("create environment: %w", err)
	}
	return &env, nil
}

// ListEnvironments returns all environments for a project.
func (c *Client) ListEnvironments(ctx context.Context, projectSlug string) ([]*types.Environment, error) {
	var resp struct {
		Environments []*types.Environment `json:"environments"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/projects/%s/environments", projectSlug), &resp); err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	return resp.Environments, nil
}
