package cmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type stubDeploymentLister struct {
	deployments []*types.Deployment
	err         error
}

func (s stubDeploymentLister) ListServiceDeployments(context.Context, string) ([]*types.Deployment, error) {
	return s.deployments, s.err
}

func TestListServiceDeploymentsForDisplay(t *testing.T) {
	rows := []*types.Deployment{{Status: types.DeploymentStatusRunning}}

	t.Run("partial list is shown with a warning", func(t *testing.T) {
		var warn bytes.Buffer
		got, truncated, err := listServiceDeploymentsForDisplay(context.Background(), stubDeploymentLister{
			deployments: rows,
			err:         &client.PartialDeploymentListError{SkippedReleaseIDs: []string{"rel-2"}},
		}, "svc", &warn)
		require.NoError(t, err)
		assert.True(t, truncated)
		assert.Equal(t, rows, got)
		assert.Contains(t, warn.String(), "warning: the API returned a partial deployment list")
		assert.Contains(t, warn.String(), "rel-2")
	})

	t.Run("complete list, no warning", func(t *testing.T) {
		var warn bytes.Buffer
		got, truncated, err := listServiceDeploymentsForDisplay(context.Background(), stubDeploymentLister{deployments: rows}, "svc", &warn)
		require.NoError(t, err)
		assert.False(t, truncated)
		assert.Equal(t, rows, got)
		assert.Empty(t, warn.String())
	})

	t.Run("other errors still fail", func(t *testing.T) {
		var warn bytes.Buffer
		_, _, err := listServiceDeploymentsForDisplay(context.Background(), stubDeploymentLister{err: errors.New("boom")}, "svc", &warn)
		require.EqualError(t, err, "boom")
		assert.Empty(t, warn.String())
	})
}
