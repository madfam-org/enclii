package client

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// StartCanaryRequest is the body for POST /v1/services/{id}/canary.
type StartCanaryRequest struct {
	Digest                  string  `json:"digest"`
	Percentage              int     `json:"percentage"`
	ValidationWindowMinutes int     `json:"validation_window_minutes,omitempty"`
	SmokeEndpoint           string  `json:"smoke_endpoint,omitempty"`
	ErrorRateThreshold      float64 `json:"error_rate_threshold,omitempty"`
	EnvironmentName         string  `json:"environment_name,omitempty"`
	ChangeTicketURL         string  `json:"change_ticket_url,omitempty"`
	TotalReplicas           int     `json:"total_replicas,omitempty"`
}

// CanaryRolloutResponse mirrors the API response body.
type CanaryRolloutResponse struct {
	*types.CanaryRollout
	ActualPercentage float64 `json:"actual_percentage"`
}

// StartCanary kicks off a canary rollout for a service.
func (c *APIClient) StartCanary(ctx context.Context, serviceID string, req StartCanaryRequest) (*CanaryRolloutResponse, error) {
	var resp CanaryRolloutResponse
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/canary", serviceID), req, &resp); err != nil {
		return nil, fmt.Errorf("start canary: %w", err)
	}
	return &resp, nil
}

// GetCanary fetches the current state of a rollout.
func (c *APIClient) GetCanary(ctx context.Context, serviceID, rolloutID string) (*CanaryRolloutResponse, error) {
	var resp CanaryRolloutResponse
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/canary/%s", serviceID, rolloutID), &resp); err != nil {
		return nil, fmt.Errorf("get canary: %w", err)
	}
	return &resp, nil
}

// PromoteCanary short-circuits validation and promotes now.
func (c *APIClient) PromoteCanary(ctx context.Context, serviceID, rolloutID string) error {
	return c.post(ctx, fmt.Sprintf("/v1/services/%s/canary/%s/promote", serviceID, rolloutID), struct{}{}, nil)
}

// RollbackCanary aborts the rollout.
func (c *APIClient) RollbackCanary(ctx context.Context, serviceID, rolloutID, reason string) error {
	return c.post(ctx, fmt.Sprintf("/v1/services/%s/canary/%s/rollback", serviceID, rolloutID),
		map[string]string{"reason": reason}, nil)
}
