package client

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CreateProject creates a new project.
func (c *Client) CreateProject(ctx context.Context, name, slug string) (*types.Project, error) {
	payload := map[string]string{"name": name, "slug": slug}
	var project types.Project
	if err := c.post(ctx, "/v1/projects", payload, &project); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return &project, nil
}

// GetProject retrieves a project by slug.
func (c *Client) GetProject(ctx context.Context, slug string) (*types.Project, error) {
	var project types.Project
	if err := c.get(ctx, fmt.Sprintf("/v1/projects/%s", slug), &project); err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return &project, nil
}

// ListProjects returns all projects accessible to the authenticated user.
func (c *Client) ListProjects(ctx context.Context) ([]*types.Project, error) {
	var resp struct {
		Projects []*types.Project `json:"projects"`
	}
	if err := c.get(ctx, "/v1/projects", &resp); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return resp.Projects, nil
}

// DeleteProject deletes a project by slug.
func (c *Client) DeleteProject(ctx context.Context, slug string) error {
	if err := c.delete(ctx, fmt.Sprintf("/v1/projects/%s", slug)); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}
