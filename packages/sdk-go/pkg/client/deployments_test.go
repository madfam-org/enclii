package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestDeployService(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/services/svc-1/deploy" {
			t.Errorf("path = %q, want /v1/services/svc-1/deploy", r.URL.Path)
		}

		var body DeployRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.ReleaseID != "rel-1" {
			t.Errorf("release_id = %q, want %q", body.ReleaseID, "rel-1")
		}
		if body.EnvironmentName != "staging" {
			t.Errorf("environment_name = %q, want %q", body.EnvironmentName, "staging")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000020","status":"pending","replicas":2}`))
	})

	dep, err := c.DeployService(context.Background(), "svc-1", DeployRequest{
		ReleaseID:       "rel-1",
		EnvironmentName: "staging",
		Replicas:        2,
	})
	if err != nil {
		t.Fatalf("DeployService() error: %v", err)
	}
	if dep.Status != "pending" {
		t.Errorf("status = %q, want %q", dep.Status, "pending")
	}
	if dep.Replicas != 2 {
		t.Errorf("replicas = %d, want 2", dep.Replicas)
	}
}

func TestGetDeployment(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/deployments/dep-42" {
			t.Errorf("path = %q, want /v1/deployments/dep-42", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000020","status":"running","health":"healthy"}`))
	})

	dep, err := c.GetDeployment(context.Background(), "dep-42")
	if err != nil {
		t.Fatalf("GetDeployment() error: %v", err)
	}
	if dep.Status != "running" {
		t.Errorf("status = %q, want %q", dep.Status, "running")
	}
	if dep.Health != "healthy" {
		t.Errorf("health = %q, want %q", dep.Health, "healthy")
	}
}

func TestListReleases(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/services/svc-1/releases" {
			t.Errorf("path = %q, want /v1/services/svc-1/releases", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"releases":[{"id":"00000000-0000-0000-0000-000000000030","version":"v1.0.0","status":"ready","git_sha":"abc123"}]}`))
	})

	releases, err := c.ListReleases(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("ListReleases() error: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len(releases) = %d, want 1", len(releases))
	}
	if releases[0].Version != "v1.0.0" {
		t.Errorf("version = %q, want %q", releases[0].Version, "v1.0.0")
	}
	if releases[0].Status != "ready" {
		t.Errorf("status = %q, want %q", releases[0].Status, "ready")
	}
}

func TestDeployServiceError(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid release ID"}`))
	})

	_, err := c.DeployService(context.Background(), "svc-1", DeployRequest{ReleaseID: "bad"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetDeploymentNotFound(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"deployment not found"}`))
	})

	_, err := c.GetDeployment(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
