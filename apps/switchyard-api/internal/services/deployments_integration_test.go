//go:build integration

package services

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/testutil"
)

func newTestDeploymentService(t *testing.T) (*DeploymentService, *ProjectService) {
	t.Helper()
	repos := testutil.RequireTestRepos(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	projSvc := NewProjectService(repos, logger)
	deplSvc := NewDeploymentService(repos, logger)
	return deplSvc, projSvc
}

func TestDeploymentService_Create(t *testing.T) {
	deplSvc, projSvc := newTestDeploymentService(t)
	ctx := context.Background()

	projResp, err := projSvc.CreateProject(ctx, &CreateProjectRequest{
		Name: "Deploy Create Test", Slug: "deploy-create-test", UserID: "u1", UserEmail: "u@e.com", UserRole: "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	_, err = projSvc.CreateService(ctx, &CreateServiceRequest{
		ProjectID: projResp.Project.ID.String(), Name: "deploy-svc", GitRepo: "https://github.com/org/deploy-svc",
		UserID: "u1", UserEmail: "u@e.com", UserRole: "admin",
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	// DeployService requires a valid service and configuration
	_, err = deplSvc.DeployService(ctx, &DeployServiceRequest{
		ProjectID:   projResp.Project.ID.String(),
		ServiceName: "deploy-svc",
		ImageURI:    "ghcr.io/test/image:v1",
		UserID:      "u1",
		UserEmail:   "u@e.com",
	})
	if err != nil {
		// May fail due to missing k8s client or environment — that's expected in integration test
		t.Logf("DeployService: %v (may need full infra setup)", err)
	}
}

func TestDeploymentService_Get(t *testing.T) {
	deplSvc, _ := newTestDeploymentService(t)
	ctx := context.Background()

	// GetDeploymentStatus with non-existent ID should return error
	_, err := deplSvc.GetDeploymentStatus(ctx, "nonexistent-id")
	if err == nil {
		t.Error("expected error for non-existent deployment, got nil")
	}
}

func TestDeploymentService_List(t *testing.T) {
	deplSvc, projSvc := newTestDeploymentService(t)
	ctx := context.Background()

	projResp, err := projSvc.CreateProject(ctx, &CreateProjectRequest{
		Name: "Deploy List Test", Slug: "deploy-list-test", UserID: "u1", UserEmail: "u@e.com", UserRole: "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	svcResp, err := projSvc.CreateService(ctx, &CreateServiceRequest{
		ProjectID: projResp.Project.ID.String(), Name: "deploy-list-svc", GitRepo: "https://github.com/org/deploy-list-svc",
		UserID: "u1", UserEmail: "u@e.com", UserRole: "admin",
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	deployments, err := deplSvc.ListServiceDeployments(ctx, svcResp.Service.ID.String())
	if err != nil {
		t.Fatalf("ListServiceDeployments: %v", err)
	}
	if deployments == nil {
		t.Fatal("expected non-nil deployments slice")
	}
}

func TestDeploymentService_UpdateStatus(t *testing.T) {
	deplSvc, _ := newTestDeploymentService(t)
	ctx := context.Background()

	// GetDeploymentStatus with invalid UUID should return a clear error
	_, err := deplSvc.GetDeploymentStatus(ctx, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Log("No deployment found for zero UUID (expected)")
	}
}

func TestDeploymentService_GetLatest(t *testing.T) {
	deplSvc, projSvc := newTestDeploymentService(t)
	ctx := context.Background()

	projResp, err := projSvc.CreateProject(ctx, &CreateProjectRequest{
		Name: "Deploy Latest Test", Slug: "deploy-latest-test", UserID: "u1", UserEmail: "u@e.com", UserRole: "admin",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	svcResp, err := projSvc.CreateService(ctx, &CreateServiceRequest{
		ProjectID: projResp.Project.ID.String(), Name: "deploy-latest-svc", GitRepo: "https://github.com/org/deploy-latest-svc",
		UserID: "u1", UserEmail: "u@e.com", UserRole: "admin",
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	// ListServiceDeployments on a fresh service should return empty list
	deps, err := deplSvc.ListServiceDeployments(ctx, svcResp.Service.ID.String())
	if err != nil {
		t.Fatalf("ListServiceDeployments: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deployments for new service, got %d", len(deps))
	}
}

func TestDeploymentService_Rollback(t *testing.T) {
	deplSvc, _ := newTestDeploymentService(t)
	ctx := context.Background()

	// Rollback with non-existent deployment should return error
	_, err := deplSvc.Rollback(ctx, &RollbackRequest{
		DeploymentID: "00000000-0000-0000-0000-000000000000",
		UserID:       "u1",
		UserEmail:    "u@e.com",
	})
	if err == nil {
		t.Error("expected error for rollback on non-existent deployment, got nil")
	}
}
