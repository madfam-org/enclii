package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestResolveReleaseServiceIDSearchesAllProjectsWhenProjectUnset(t *testing.T) {
	targetID := uuid.New()
	apiClient := newReleaseLookupTestClient(t, []*types.Project{
		{Slug: "forgesight"},
		{Slug: "enclii"},
	}, map[string][]*types.Service{
		"forgesight": {
			{ID: uuid.New(), Name: "forgesight-api"},
		},
		"enclii": {
			{ID: targetID, Name: "switchyard-api"},
		},
	})

	gotID, label, err := resolveReleaseServiceID(context.Background(), apiClient, "", "switchyard-api")
	require.NoError(t, err)
	require.Equal(t, targetID.String(), gotID)
	require.Contains(t, label, "enclii/switchyard-api")
}

func TestResolveReleaseServiceIDHonorsProjectScope(t *testing.T) {
	targetID := uuid.New()
	apiClient := newReleaseLookupTestClient(t, []*types.Project{
		{Slug: "other"},
	}, map[string][]*types.Service{
		"enclii": {
			{ID: targetID, Name: "switchyard-api"},
		},
	})

	gotID, label, err := resolveReleaseServiceID(context.Background(), apiClient, "enclii", "switchyard-api")
	require.NoError(t, err)
	require.Equal(t, targetID.String(), gotID)
	require.Contains(t, label, "enclii/switchyard-api")
}

func TestResolveReleaseServiceIDRejectsAmbiguousNames(t *testing.T) {
	apiClient := newReleaseLookupTestClient(t, []*types.Project{
		{Slug: "enclii"},
		{Slug: "staging"},
	}, map[string][]*types.Service{
		"enclii": {
			{ID: uuid.New(), Name: "switchyard-api"},
		},
		"staging": {
			{ID: uuid.New(), Name: "switchyard-api"},
		},
	})

	_, _, err := resolveReleaseServiceID(context.Background(), apiClient, "", "switchyard-api")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")
	require.Contains(t, err.Error(), "--project")
}

func newReleaseLookupTestClient(t *testing.T, projects []*types.Project, servicesByProject map[string][]*types.Service) *client.APIClient {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && r.URL.Path == "/v1/projects" {
			_ = json.NewEncoder(w).Encode(struct {
				Projects []*types.Project `json:"projects"`
			}{Projects: projects})
			return
		}

		for project, services := range servicesByProject {
			if r.Method == http.MethodGet && r.URL.Path == "/v1/projects/"+project+"/services" {
				_ = json.NewEncoder(w).Encode(struct {
					Services []*types.Service `json:"services"`
				}{Services: services})
				return
			}
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	return client.NewAPIClient(server.URL, "test-token")
}
