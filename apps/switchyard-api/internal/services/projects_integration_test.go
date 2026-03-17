//go:build integration

package services

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/testutil"
)

func newTestProjectService(t *testing.T) *ProjectService {
	t.Helper()
	repos := testutil.RequireTestRepos(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return NewProjectService(repos, logger)
}

func TestProjectService_CreateProject(t *testing.T) {
	svc := newTestProjectService(t)
	ctx := context.Background()

	resp, err := svc.CreateProject(ctx, &CreateProjectRequest{
		Name:      "Integration Test Project",
		Slug:      "integ-test-proj",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	if resp.Project == nil {
		t.Fatal("expected non-nil project in response")
	}
	if resp.Project.Slug != "integ-test-proj" {
		t.Errorf("expected slug 'integ-test-proj', got %q", resp.Project.Slug)
	}
}

func TestProjectService_CreateProject_DuplicateSlug(t *testing.T) {
	svc := newTestProjectService(t)
	ctx := context.Background()

	_, err := svc.CreateProject(ctx, &CreateProjectRequest{
		Name:      "Dup Project",
		Slug:      "dup-slug-test",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err != nil {
		t.Fatalf("first CreateProject failed: %v", err)
	}

	_, err = svc.CreateProject(ctx, &CreateProjectRequest{
		Name:      "Dup Project 2",
		Slug:      "dup-slug-test",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err == nil {
		t.Fatal("expected error for duplicate slug, got nil")
	}
}

func TestProjectService_GetProject(t *testing.T) {
	svc := newTestProjectService(t)
	ctx := context.Background()

	resp, err := svc.CreateProject(ctx, &CreateProjectRequest{
		Name:      "Get Test Project",
		Slug:      "get-test-proj",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	got, err := svc.GetProject(ctx, resp.Project.Slug)
	if err != nil {
		t.Fatalf("GetProject failed: %v", err)
	}
	if got.ID != resp.Project.ID {
		t.Errorf("expected project ID %s, got %s", resp.Project.ID, got.ID)
	}
}

func TestProjectService_ListProjects(t *testing.T) {
	svc := newTestProjectService(t)
	ctx := context.Background()

	projects, err := svc.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	// Should return at least 0 projects (depends on DB state)
	if projects == nil {
		t.Fatal("expected non-nil projects slice")
	}
}

func TestProjectService_CreateService(t *testing.T) {
	svc := newTestProjectService(t)
	ctx := context.Background()

	projResp, err := svc.CreateProject(ctx, &CreateProjectRequest{
		Name:      "Svc Test Project",
		Slug:      "svc-test-proj",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	svcResp, err := svc.CreateService(ctx, &CreateServiceRequest{
		ProjectID: projResp.Project.ID.String(),
		Name:      "test-api",
		GitRepo:   "https://github.com/test-org/test-api",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}
	if svcResp.Service == nil {
		t.Fatal("expected non-nil service in response")
	}
	if svcResp.Service.Name != "test-api" {
		t.Errorf("expected service name 'test-api', got %q", svcResp.Service.Name)
	}
}

func TestProjectService_GetService(t *testing.T) {
	svc := newTestProjectService(t)
	ctx := context.Background()

	projResp, err := svc.CreateProject(ctx, &CreateProjectRequest{
		Name:      "Get Svc Project",
		Slug:      "get-svc-proj",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	svcResp, err := svc.CreateService(ctx, &CreateServiceRequest{
		ProjectID: projResp.Project.ID.String(),
		Name:      "get-test-svc",
		GitRepo:   "https://github.com/test-org/get-test-svc",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}

	got, err := svc.GetService(ctx, svcResp.Service.ID.String())
	if err != nil {
		t.Fatalf("GetService failed: %v", err)
	}
	if got.Name != "get-test-svc" {
		t.Errorf("expected service name 'get-test-svc', got %q", got.Name)
	}
}

func TestProjectService_ListServices(t *testing.T) {
	svc := newTestProjectService(t)
	ctx := context.Background()

	projResp, err := svc.CreateProject(ctx, &CreateProjectRequest{
		Name:      "List Svc Project",
		Slug:      "list-svc-proj",
		UserID:    "test-user-id",
		UserEmail: "test@example.com",
		UserRole:  "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	services, err := svc.ListServices(ctx, projResp.Project.Slug)
	if err != nil {
		t.Fatalf("ListServices failed: %v", err)
	}
	if services == nil {
		t.Fatal("expected non-nil services slice")
	}
}
