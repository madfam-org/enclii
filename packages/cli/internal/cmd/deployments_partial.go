package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// serviceDeploymentLister is the slice of *client.APIClient the listing
// commands need (an interface so tests can stub it).
type serviceDeploymentLister interface {
	ListServiceDeployments(ctx context.Context, serviceID string) ([]*types.Deployment, error)
}

// listServiceDeploymentsForDisplay lists a service's deployments for a
// listing command. A partial list (the API could not read some releases)
// is shown rather than failing the command, with a warning on warn, and
// truncated=true. Commands that act on the history, such as rollback, call
// ListServiceDeployments directly so a partial list stays an error there.
func listServiceDeploymentsForDisplay(ctx context.Context, api serviceDeploymentLister, serviceID string, warn io.Writer) (deployments []*types.Deployment, truncated bool, err error) {
	deployments, err = api.ListServiceDeployments(ctx, serviceID)
	var partial *client.PartialDeploymentListError
	if errors.As(err, &partial) {
		_, _ = fmt.Fprintf(warn, "warning: %v; older deployments may be missing below\n", partial)
		return deployments, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return deployments, false, nil
}
