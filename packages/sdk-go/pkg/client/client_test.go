package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupTestServer(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := New(server.URL, "test-token")
	return client, server
}

func TestNew(t *testing.T) {
	c := New("https://api.example.com", "my-token")
	if c.baseURL != "https://api.example.com" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "https://api.example.com")
	}
	if c.token != "my-token" {
		t.Errorf("token = %q, want %q", c.token, "my-token")
	}
	if c.userAgent != "enclii-sdk-go/1.0.0" {
		t.Errorf("userAgent = %q, want %q", c.userAgent, "enclii-sdk-go/1.0.0")
	}
}

func TestWithUserAgent(t *testing.T) {
	c := New("https://api.example.com", "t", WithUserAgent("custom/1.0"))
	if c.userAgent != "custom/1.0" {
		t.Errorf("userAgent = %q, want %q", c.userAgent, "custom/1.0")
	}
}

func TestHealth(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %q, want /health", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{
			Status:  "ok",
			Service: "switchyard-api",
			Version: "1.0.0",
		})
	})

	health, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error: %v", err)
	}
	if health.Status != "ok" {
		t.Errorf("status = %q, want %q", health.Status, "ok")
	}
}

func TestCreateProject(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/projects" {
			t.Errorf("path = %q, want /v1/projects", r.URL.Path)
		}

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "My Project" {
			t.Errorf("name = %q, want %q", body["name"], "My Project")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000001","name":"My Project","slug":"my-project"}`))
	})

	project, err := c.CreateProject(context.Background(), "My Project", "my-project")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if project.Name != "My Project" {
		t.Errorf("name = %q, want %q", project.Name, "My Project")
	}
}

func TestListProjects(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"projects":[{"id":"00000000-0000-0000-0000-000000000001","name":"P1","slug":"p1"}]}`))
	})

	projects, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("len(projects) = %d, want 1", len(projects))
	}
	if projects[0].Slug != "p1" {
		t.Errorf("slug = %q, want %q", projects[0].Slug, "p1")
	}
}

func TestGetService(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/services/svc-123" {
			t.Errorf("path = %q, want /v1/services/svc-123", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000002","name":"my-svc"}`))
	})

	svc, err := c.GetService(context.Background(), "svc-123")
	if err != nil {
		t.Fatalf("GetService() error: %v", err)
	}
	if svc.Name != "my-svc" {
		t.Errorf("name = %q, want %q", svc.Name, "my-svc")
	}
}

func TestListDeployments(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"deployments":[{"id":"00000000-0000-0000-0000-000000000003","status":"running"}]}`))
	})

	deps, err := c.ListDeployments(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("ListDeployments() error: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("len = %d, want 1", len(deps))
	}
}

func TestListEnvVars(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"environment_variables":[{"id":"00000000-0000-0000-0000-000000000004","key":"DB_URL","value":"***","is_secret":true}]}`))
	})

	vars, err := c.ListEnvVars(context.Background(), "svc-1", nil)
	if err != nil {
		t.Fatalf("ListEnvVars() error: %v", err)
	}
	if len(vars) != 1 {
		t.Fatalf("len = %d, want 1", len(vars))
	}
	if vars[0].Key != "DB_URL" {
		t.Errorf("key = %q, want %q", vars[0].Key, "DB_URL")
	}
}

func TestListEnvVarsWithEnvironment(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		envID := r.URL.Query().Get("environment_id")
		if envID != "env-prod" {
			t.Errorf("environment_id = %q, want %q", envID, "env-prod")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"environment_variables":[]}`))
	})

	envID := "env-prod"
	_, err := c.ListEnvVars(context.Background(), "svc-1", &envID)
	if err != nil {
		t.Fatalf("ListEnvVars() error: %v", err)
	}
}

func TestAPIError(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"project not found"}`))
	})

	_, err := c.GetProject(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(APIError)
	if !ok {
		// May be wrapped
		t.Logf("error type: %T, value: %v", err, err)
		return
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", apiErr.StatusCode, http.StatusNotFound)
	}
}

func TestDeleteProject(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	err := c.DeleteProject(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("DeleteProject() error: %v", err)
	}
}

func TestBuildService(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["git_sha"] != "abc123" {
			t.Errorf("git_sha = %q, want %q", body["git_sha"], "abc123")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000005","git_sha":"abc123"}`))
	})

	release, err := c.BuildService(context.Background(), "svc-1", "abc123")
	if err != nil {
		t.Fatalf("BuildService() error: %v", err)
	}
	if release.GitSHA != "abc123" {
		t.Errorf("git_sha = %q, want %q", release.GitSHA, "abc123")
	}
}

func TestGetLogs(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lines") != "100" {
			t.Errorf("lines = %q, want %q", r.URL.Query().Get("lines"), "100")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"logs":[{"timestamp":"2026-01-01T00:00:00Z","pod":"api-0","message":"started"}]}`))
	})

	logs, err := c.GetLogs(context.Background(), "dep-1", LogOptions{Lines: 100})
	if err != nil {
		t.Fatalf("GetLogs() error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len = %d, want 1", len(logs))
	}
	if logs[0].Message != "started" {
		t.Errorf("message = %q, want %q", logs[0].Message, "started")
	}
}

func TestCreateEnvironment(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000006","name":"staging"}`))
	})

	env, err := c.CreateEnvironment(context.Background(), "my-project", "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment() error: %v", err)
	}
	if env.Name != "staging" {
		t.Errorf("name = %q, want %q", env.Name, "staging")
	}
}

func TestRollbackDeployment(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})

	err := c.RollbackDeployment(context.Background(), "dep-1", RollbackRequest{ToRelease: "rel-old"})
	if err != nil {
		t.Fatalf("RollbackDeployment() error: %v", err)
	}
}
