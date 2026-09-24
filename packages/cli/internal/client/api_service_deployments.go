package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ListServiceDeployments returns a service's deployments, newest first
// (GET /v1/services/{id}/deployments). When the API marks the list truncated
// it returns the rows it got together with a *PartialDeploymentListError.
func (c *APIClient) ListServiceDeployments(ctx context.Context, serviceID string) ([]*types.Deployment, error) {
	var response struct {
		Deployments       []*types.Deployment `json:"deployments"`
		Truncated         bool                `json:"truncated"`
		SkippedReleaseIDs []string            `json:"skipped_release_ids"`
	}

	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/deployments", serviceID), &response); err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}
	if response.Truncated {
		return response.Deployments, &PartialDeploymentListError{SkippedReleaseIDs: response.SkippedReleaseIDs}
	}
	return response.Deployments, nil
}

// PartialDeploymentListError is returned by ListServiceDeployments, together
// with the deployments that were read, when the API reports the list as
// truncated: it could not read the deployments of some releases
// (GET /v1/services/{id}/deployments answers truncated=true and names them in
// skipped_release_ids).
//
// Callers that need the complete history, such as choosing a rollback target,
// must treat it as a failure; listing commands can show the partial rows with
// a warning. Use errors.As to tell it apart from other errors.
type PartialDeploymentListError struct {
	SkippedReleaseIDs []string
}

func (e *PartialDeploymentListError) Error() string {
	msg := "the API returned a partial deployment list"
	if len(e.SkippedReleaseIDs) > 0 {
		msg += fmt.Sprintf(" (could not read the deployments of %d release(s): %s)",
			len(e.SkippedReleaseIDs), strings.Join(e.SkippedReleaseIDs, ", "))
	}
	return msg + "; retry the command"
}
