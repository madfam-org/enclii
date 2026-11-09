package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/client"
	"github.com/madfam-org/enclii/packages/cli/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestPsRequiresUniqueEnvironmentNamespaceProof(t *testing.T) {
	project := uuid.New()
	valid := client.EnvironmentInfo{ID: uuid.New(), ProjectID: project, Name: "staging", KubeNamespace: "fixture-stage"}
	for _, tc := range []struct {
		name string
		envs []*client.EnvironmentInfo
	}{
		{"absent", nil},
		{"missing namespace", []*client.EnvironmentInfo{{ID: valid.ID, ProjectID: project, Name: "staging"}}},
		{"missing project", []*client.EnvironmentInfo{{ID: valid.ID, Name: "staging", KubeNamespace: "fixture-stage"}}},
		{"missing identity", []*client.EnvironmentInfo{{ProjectID: project, Name: "staging", KubeNamespace: "fixture-stage"}}},
		{"duplicate environment", []*client.EnvironmentInfo{&valid, &valid}},
		{"shared namespace", []*client.EnvironmentInfo{&valid, {ID: uuid.New(), ProjectID: project, Name: "production", KubeNamespace: valid.KubeNamespace}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolvePsEnvironment(tc.envs, "staging")
			require.Error(t, err)
		})
	}
	_, err := resolvePsEnvironment([]*client.EnvironmentInfo{&valid}, "staging")
	require.NoError(t, err)
}

func TestPsEnvironmentCannotInheritProductionHealth(t *testing.T) {
	for _, tc := range []struct {
		name, requested, serviceNamespace, healthNamespace, healthProject, healthStatus string
		pods, ready                                                                     int
		missingBinding, missingHealth, duplicateHealth, wantError                       bool
	}{
		{name: "production-only service is absent from staging", requested: "staging", serviceNamespace: "fixture-prod", healthNamespace: "fixture-prod", healthProject: "fixture", healthStatus: "healthy", pods: 2, ready: 2, wantError: true},
		{name: "production observation cannot prove staging", requested: "staging", serviceNamespace: "fixture-stage", healthNamespace: "fixture-prod", healthProject: "fixture", healthStatus: "healthy", pods: 2, ready: 2, wantError: true},
		{name: "older API has no namespace proof", requested: "staging", serviceNamespace: "fixture-stage", healthProject: "fixture", healthStatus: "healthy", pods: 2, ready: 2, wantError: true},
		{name: "missing service binding", requested: "production", missingBinding: true, healthNamespace: "fixture-prod", healthProject: "fixture", healthStatus: "healthy", pods: 2, ready: 2, wantError: true},
		{name: "foreign project observation", requested: "staging", serviceNamespace: "fixture-stage", healthNamespace: "fixture-stage", healthProject: "other", healthStatus: "healthy", pods: 2, ready: 2, wantError: true},
		{name: "zero staging pods", requested: "staging", serviceNamespace: "fixture-stage", healthNamespace: "fixture-stage", healthProject: "fixture", healthStatus: "unhealthy", wantError: true},
		{name: "false healthy zero counts", requested: "staging", serviceNamespace: "fixture-stage", healthNamespace: "fixture-stage", healthProject: "fixture", healthStatus: "healthy", wantError: true},
		{name: "duplicate observations", requested: "staging", serviceNamespace: "fixture-stage", healthNamespace: "fixture-stage", healthProject: "fixture", healthStatus: "healthy", pods: 2, ready: 2, duplicateHealth: true, wantError: true},
		{name: "missing live observation never falls back", requested: "production", serviceNamespace: "fixture-prod", missingHealth: true, wantError: true},
		{name: "scoped staging is healthy but latest production version is excluded", requested: "staging", serviceNamespace: "fixture-stage", healthNamespace: "fixture-stage", healthProject: "fixture", healthStatus: "healthy", pods: 2, ready: 2},
		{name: "scoped production retains native health", requested: "production", serviceNamespace: "fixture-prod", healthNamespace: "fixture-prod", healthProject: "fixture", healthStatus: "healthy", pods: 2, ready: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project, serviceID, prod, stage := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			service := &types.Service{ID: serviceID, ProjectID: project, Name: "api", K8sNamespace: &tc.serviceNamespace,
				Status: "running", Health: types.HealthStatusHealthy, ReadyReplicas: 2, DesiredReplicas: 2}
			if tc.missingBinding {
				service.K8sNamespace = nil
			}
			healthCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/projects/fixture/services":
					_ = json.NewEncoder(w).Encode(map[string]any{"services": []*types.Service{service}})
				case "/v1/projects/fixture/registered-environments":
					_ = json.NewEncoder(w).Encode(map[string]any{"environments": []*client.EnvironmentInfo{
						{ID: prod, ProjectID: project, Name: "production", KubeNamespace: "fixture-prod"},
						{ID: stage, ProjectID: project, Name: "staging", KubeNamespace: "fixture-stage"},
					}})
				case "/v1/observability/health":
					healthCalls++
					if tc.missingHealth {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					health := client.ServiceHealth{ServiceID: serviceID.String(), ProjectSlug: tc.healthProject, Namespace: tc.healthNamespace,
						Status: tc.healthStatus, PodCount: tc.pods, ReadyPods: tc.ready, LastChecked: time.Now(), DeploymentName: "api-web"}
					items := []client.ServiceHealth{health}
					if tc.duplicateHealth {
						items = append(items, health)
					}
					_ = json.NewEncoder(w).Encode(client.ServiceHealthResponse{Services: items})
				case "/v1/services/" + serviceID.String() + "/deployments/latest":
					_ = json.NewEncoder(w).Encode(client.DeploymentWithRelease{
						Deployment: &types.Deployment{EnvironmentID: prod, Replicas: 2, Status: types.DeploymentStatusRunning, Health: types.HealthStatusHealthy, CreatedAt: time.Now()},
						Release:    &types.Release{ServiceID: serviceID, Version: "production-marker", GitSHA: "abcdef0123456789"},
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var err error
			out := captureStdoutStderr(t, func() {
				err = listServices(&config.Config{APIEndpoint: server.URL, APIToken: "synthetic-token"}, tc.requested, "fixture")
			})
			if tc.wantError {
				require.Error(t, err)
				require.NotContains(t, out, "2/2")
				if tc.requested == "staging" {
					require.NotContains(t, out, "production-marker")
				}
			} else {
				require.NoError(t, err)
				require.Contains(t, out, "2/2")
				require.Contains(t, out, "healthy")
				if tc.requested == "staging" {
					require.NotContains(t, out, "production-marker")
				}
				if tc.requested == "production" {
					require.Contains(t, out, "production-marker")
				}
			}
			if tc.name == "production-only service is absent from staging" {
				require.Zero(t, healthCalls)
			}
		})
	}
}

func TestPsNeverFallsBackToLegacyEnvironmentCreation(t *testing.T) {
	legacyCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/projects/fixture/environments" {
			legacyCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/v1/projects/fixture/services" {
			_, _ = w.Write([]byte(`{"services":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	var err error
	captureStdoutStderr(t, func() {
		err = listServices(&config.Config{APIEndpoint: server.URL, APIToken: "synthetic"}, "staging", "fixture")
	})
	require.Error(t, err)
	require.Zero(t, legacyCalls)
}
