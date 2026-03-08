package helpers

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/cli/internal/client"
)

// mockAPIClient is not possible to inject here because FindServiceByName and
// FindEnvironmentByName take a concrete *client.APIClient. We test the logic
// that does NOT require a live API: the not-found error generation from empty
// results and the ServiceContext struct construction.

func TestNewNotFoundError_ForService(t *testing.T) {
	err := NewNotFoundError("service", "web-api", "project", "acme")
	want := "service web-api not found in project acme"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestNewNotFoundError_ForEnvironment(t *testing.T) {
	err := NewNotFoundError("environment", "staging", "project", "acme")
	want := "environment staging not found in project acme"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestServiceContext_FieldAccess(t *testing.T) {
	svcID := uuid.New()
	projID := uuid.New()

	sctx := &ServiceContext{
		Service: &client.ServiceInfo{
			ID:        svcID,
			ProjectID: projID,
			Name:      "my-api",
		},
		ProjectSlug: "acme",
	}

	if sctx.Service.Name != "my-api" {
		t.Errorf("Service.Name = %q, want %q", sctx.Service.Name, "my-api")
	}
	if sctx.ProjectSlug != "acme" {
		t.Errorf("ProjectSlug = %q, want %q", sctx.ProjectSlug, "acme")
	}
	if sctx.Service.ID != svcID {
		t.Errorf("Service.ID = %v, want %v", sctx.Service.ID, svcID)
	}
	if sctx.Service.ProjectID != projID {
		t.Errorf("Service.ProjectID = %v, want %v", sctx.Service.ProjectID, projID)
	}
}

func TestResolveEnvironmentID_EmptyName(t *testing.T) {
	// When envName is empty, ResolveEnvironmentID returns nil, nil
	// without calling the API. This is testable without a real client.
	ctx := context.Background()
	id, err := ResolveEnvironmentID(ctx, nil, "any-project", "")
	if err != nil {
		t.Errorf("ResolveEnvironmentID() error = %v, want nil", err)
	}
	if id != nil {
		t.Errorf("ResolveEnvironmentID() = %v, want nil", id)
	}
}

func TestWrapError_ServiceContextErrors(t *testing.T) {
	// Test the error wrapping patterns used within ResolveService
	tests := []struct {
		name     string
		action   Action
		resource string
		errMsg   string
		want     string
	}{
		{
			name:     "parse service.yaml error",
			action:   ActionParse,
			resource: "service.yaml",
			errMsg:   "no such file",
			want:     "failed to parse service.yaml: no such file",
		},
		{
			name:     "find service error",
			action:   ActionFind,
			resource: "service web-api",
			errMsg:   "connection refused",
			want:     "failed to find service web-api: connection refused",
		},
		{
			name:     "find environment error",
			action:   ActionFind,
			resource: "environment staging",
			errMsg:   "not found",
			want:     "failed to find environment staging: not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WrapError(tt.action, tt.resource, errFromMsg(tt.errMsg))
			if err.Error() != tt.want {
				t.Errorf("got %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

// errFromMsg is a test helper that creates a simple error from a string.
func errFromMsg(msg string) error {
	return &simpleError{msg: msg}
}

type simpleError struct {
	msg string
}

func (e *simpleError) Error() string { return e.msg }
