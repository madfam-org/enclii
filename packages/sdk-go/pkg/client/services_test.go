package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateService(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/projects/my-project/services" {
			t.Errorf("path = %q, want /v1/projects/my-project/services", r.URL.Path)
		}

		var body CreateServiceRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.Name != "api" {
			t.Errorf("name = %q, want %q", body.Name, "api")
		}
		if body.GitRepo != "https://github.com/org/repo" {
			t.Errorf("git_repo = %q, want %q", body.GitRepo, "https://github.com/org/repo")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000010","name":"api","git_repo":"https://github.com/org/repo"}`))
	})

	svc, err := c.CreateService(context.Background(), "my-project", CreateServiceRequest{
		Name:    "api",
		GitRepo: "https://github.com/org/repo",
	})
	if err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}
	if svc.Name != "api" {
		t.Errorf("name = %q, want %q", svc.Name, "api")
	}
	if svc.GitRepo != "https://github.com/org/repo" {
		t.Errorf("git_repo = %q, want %q", svc.GitRepo, "https://github.com/org/repo")
	}
}

func TestListServices(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/my-project/services" {
			t.Errorf("path = %q, want /v1/projects/my-project/services", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"services":[{"id":"00000000-0000-0000-0000-000000000010","name":"api"},{"id":"00000000-0000-0000-0000-000000000011","name":"web"}]}`))
	})

	services, err := c.ListServices(context.Background(), "my-project")
	if err != nil {
		t.Fatalf("ListServices() error: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("len(services) = %d, want 2", len(services))
	}
	if services[0].Name != "api" {
		t.Errorf("services[0].name = %q, want %q", services[0].Name, "api")
	}
	if services[1].Name != "web" {
		t.Errorf("services[1].name = %q, want %q", services[1].Name, "web")
	}
}

func TestDeleteService(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if r.URL.Path != "/v1/services/svc-456" {
			t.Errorf("path = %q, want /v1/services/svc-456", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	err := c.DeleteService(context.Background(), "svc-456")
	if err != nil {
		t.Fatalf("DeleteService() error: %v", err)
	}
}

func TestCreateServiceError(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"service already exists"}`))
	})

	_, err := c.CreateService(context.Background(), "proj", CreateServiceRequest{Name: "dup"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetProjectSuccess(t *testing.T) {
	c, _ := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/my-slug" {
			t.Errorf("path = %q, want /v1/projects/my-slug", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000001","name":"My Project","slug":"my-slug"}`))
	})

	project, err := c.GetProject(context.Background(), "my-slug")
	if err != nil {
		t.Fatalf("GetProject() error: %v", err)
	}
	if project.Slug != "my-slug" {
		t.Errorf("slug = %q, want %q", project.Slug, "my-slug")
	}
}
