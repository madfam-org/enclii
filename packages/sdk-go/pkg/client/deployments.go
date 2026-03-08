package client

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DeployRequest contains parameters for deploying a service.
type DeployRequest struct {
	ReleaseID       string            `json:"release_id"`
	EnvironmentName string            `json:"environment_name"`
	Environment     map[string]string `json:"environment,omitempty"`
	Replicas        int               `json:"replicas,omitempty"`
}

// RollbackRequest contains parameters for rolling back a deployment.
type RollbackRequest struct {
	ToRelease string `json:"to_release,omitempty"`
}

// DeployService triggers a deployment for a service.
func (c *Client) DeployService(ctx context.Context, serviceID string, req DeployRequest) (*types.Deployment, error) {
	var deployment types.Deployment
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/deploy", serviceID), req, &deployment); err != nil {
		return nil, fmt.Errorf("deploy service: %w", err)
	}
	return &deployment, nil
}

// GetDeployment retrieves a deployment by ID.
func (c *Client) GetDeployment(ctx context.Context, deploymentID string) (*types.Deployment, error) {
	var deployment types.Deployment
	if err := c.get(ctx, fmt.Sprintf("/v1/deployments/%s", deploymentID), &deployment); err != nil {
		return nil, fmt.Errorf("get deployment: %w", err)
	}
	return &deployment, nil
}

// ListDeployments returns all deployments for a service.
func (c *Client) ListDeployments(ctx context.Context, serviceID string) ([]*types.Deployment, error) {
	var resp struct {
		Deployments []*types.Deployment `json:"deployments"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/deployments", serviceID), &resp); err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	return resp.Deployments, nil
}

// RollbackDeployment rolls back a deployment.
func (c *Client) RollbackDeployment(ctx context.Context, deploymentID string, req RollbackRequest) error {
	if err := c.post(ctx, fmt.Sprintf("/v1/deployments/%s/rollback", deploymentID), req, nil); err != nil {
		return fmt.Errorf("rollback deployment: %w", err)
	}
	return nil
}

// BuildService triggers a build for a service.
func (c *Client) BuildService(ctx context.Context, serviceID, gitSHA string) (*types.Release, error) {
	payload := map[string]string{"git_sha": gitSHA}
	var release types.Release
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/build", serviceID), payload, &release); err != nil {
		return nil, fmt.Errorf("build service: %w", err)
	}
	return &release, nil
}

// ListReleases returns all releases for a service.
func (c *Client) ListReleases(ctx context.Context, serviceID string) ([]*types.Release, error) {
	var resp struct {
		Releases []*types.Release `json:"releases"`
	}
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/releases", serviceID), &resp); err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}
	return resp.Releases, nil
}
