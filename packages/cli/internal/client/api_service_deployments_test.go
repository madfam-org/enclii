package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serveDeployments(t *testing.T, body string) *APIClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/services/svc-1/deployments", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewAPIClient(server.URL, "test-token")
}

func TestListServiceDeployments_CompleteList(t *testing.T) {
	c := serveDeployments(t, `{"service_id":"svc-1","count":1,"truncated":false,
		"deployments":[{"id":"6f1c2c1e-0000-4000-8000-000000000001","status":"running"}]}`)
	deployments, err := c.ListServiceDeployments(context.Background(), "svc-1")
	require.NoError(t, err)
	assert.Len(t, deployments, 1)
}

func TestListServiceDeployments_ServerWithoutTruncatedField(t *testing.T) {
	// A switchyard-api that predates the field sends no truncated key.
	c := serveDeployments(t, `{"service_id":"svc-1","count":0,"deployments":null}`)
	deployments, err := c.ListServiceDeployments(context.Background(), "svc-1")
	require.NoError(t, err)
	assert.Empty(t, deployments)
}

func TestListServiceDeployments_TruncatedIsAPartialListError(t *testing.T) {
	c := serveDeployments(t, `{"service_id":"svc-1","count":1,"truncated":true,
		"skipped_release_ids":["rel-2"],
		"deployments":[{"id":"6f1c2c1e-0000-4000-8000-000000000001","status":"running"}]}`)
	deployments, err := c.ListServiceDeployments(context.Background(), "svc-1")
	var partial *PartialDeploymentListError
	require.True(t, errors.As(err, &partial), "err = %v", err)
	assert.Equal(t, []string{"rel-2"}, partial.SkippedReleaseIDs)
	assert.Contains(t, err.Error(), "rel-2")
	assert.Len(t, deployments, 1, "the rows that were read are still returned")
}
