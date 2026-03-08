package helpers

import (
	"errors"
	"fmt"
	"testing"
)

func TestWrapError(t *testing.T) {
	tests := []struct {
		name     string
		action   Action
		resource string
		err      error
		want     string
		wantNil  bool
	}{
		{
			name:     "wraps error with action and resource",
			action:   ActionParse,
			resource: "service.yaml",
			err:      errors.New("file not found"),
			want:     "failed to parse service.yaml: file not found",
		},
		{
			name:     "returns nil when error is nil",
			action:   ActionParse,
			resource: "service.yaml",
			err:      nil,
			wantNil:  true,
		},
		{
			name:     "works with deploy action",
			action:   ActionDeploy,
			resource: "my-service",
			err:      errors.New("timeout"),
			want:     "failed to deploy my-service: timeout",
		},
		{
			name:     "works with delete action",
			action:   ActionDelete,
			resource: "secret/api-key",
			err:      errors.New("permission denied"),
			want:     "failed to delete secret/api-key: permission denied",
		},
		{
			name:     "preserves error chain via wrapping",
			action:   ActionGet,
			resource: "project",
			err:      fmt.Errorf("network error: %w", errors.New("connection refused")),
			want:     "failed to get project: network error: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapError(tt.action, tt.resource, tt.err)
			if tt.wantNil {
				if got != nil {
					t.Errorf("WrapError() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("WrapError() = nil, want error")
			}
			if got.Error() != tt.want {
				t.Errorf("WrapError().Error() = %q, want %q", got.Error(), tt.want)
			}
		})
	}
}

func TestWrapError_UnwrapChain(t *testing.T) {
	originalErr := errors.New("root cause")
	wrapped := WrapError(ActionBuild, "image", originalErr)

	if !errors.Is(wrapped, originalErr) {
		t.Error("WrapError result should unwrap to original error via errors.Is")
	}
}

func TestWrapErrorf(t *testing.T) {
	tests := []struct {
		name        string
		action      Action
		err         error
		resourceFmt string
		args        []any
		want        string
		wantNil     bool
	}{
		{
			name:        "formats resource with single argument",
			action:      ActionFind,
			err:         errors.New("not found"),
			resourceFmt: "environment %s",
			args:        []any{"staging"},
			want:        "failed to find environment staging: not found",
		},
		{
			name:        "formats resource with multiple arguments",
			action:      ActionFind,
			err:         errors.New("not found"),
			resourceFmt: "environment %s in project %s",
			args:        []any{"staging", "myproject"},
			want:        "failed to find environment staging in project myproject: not found",
		},
		{
			name:        "returns nil when error is nil",
			action:      ActionFind,
			err:         nil,
			resourceFmt: "service %s",
			args:        []any{"api"},
			wantNil:     true,
		},
		{
			name:        "works with no format arguments",
			action:      ActionCreate,
			err:         errors.New("already exists"),
			resourceFmt: "default project",
			args:        nil,
			want:        "failed to create default project: already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapErrorf(tt.action, tt.err, tt.resourceFmt, tt.args...)
			if tt.wantNil {
				if got != nil {
					t.Errorf("WrapErrorf() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("WrapErrorf() = nil, want error")
			}
			if got.Error() != tt.want {
				t.Errorf("WrapErrorf().Error() = %q, want %q", got.Error(), tt.want)
			}
		})
	}
}

func TestWrapErrorf_UnwrapChain(t *testing.T) {
	originalErr := errors.New("root cause")
	wrapped := WrapErrorf(ActionUpdate, originalErr, "resource %s", "foo")

	if !errors.Is(wrapped, originalErr) {
		t.Error("WrapErrorf result should unwrap to original error via errors.Is")
	}
}

func TestNewNotFoundError(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		resourceName string
		scopeType    string
		scopeName    string
		want         string
	}{
		{
			name:         "service not found in project",
			resourceType: "service",
			resourceName: "my-api",
			scopeType:    "project",
			scopeName:    "default",
			want:         "service my-api not found in project default",
		},
		{
			name:         "environment not found in project",
			resourceType: "environment",
			resourceName: "staging",
			scopeType:    "project",
			scopeName:    "acme",
			want:         "environment staging not found in project acme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewNotFoundError(tt.resourceType, tt.resourceName, tt.scopeType, tt.scopeName)
			if got.Error() != tt.want {
				t.Errorf("NewNotFoundError().Error() = %q, want %q", got.Error(), tt.want)
			}
		})
	}
}

func TestNewValidationError(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		reason string
		want   string
	}{
		{
			name:   "invalid format",
			value:  "KEY=VALUE",
			reason: "invalid format, expected KEY=VALUE",
			want:   `invalid value "KEY=VALUE": invalid format, expected KEY=VALUE`,
		},
		{
			name:   "empty value",
			value:  "",
			reason: "must not be empty",
			want:   `invalid value "": must not be empty`,
		},
		{
			name:   "special characters in value",
			value:  "abc\"def",
			reason: "contains invalid characters",
			want:   `invalid value "abc\"def": contains invalid characters`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewValidationError(tt.value, tt.reason)
			if got.Error() != tt.want {
				t.Errorf("NewValidationError().Error() = %q, want %q", got.Error(), tt.want)
			}
		})
	}
}

func TestNewRequiredError(t *testing.T) {
	tests := []struct {
		name  string
		field string
		want  string
	}{
		{
			name:  "service name required",
			field: "service name",
			want:  "service name is required",
		},
		{
			name:  "project slug required",
			field: "project slug",
			want:  "project slug is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewRequiredError(tt.field)
			if got.Error() != tt.want {
				t.Errorf("NewRequiredError().Error() = %q, want %q", got.Error(), tt.want)
			}
		})
	}
}

func TestNewTimeoutError(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		duration  string
		want      string
	}{
		{
			name:      "build timeout",
			operation: "build",
			duration:  "10 minutes",
			want:      "build timeout after 10 minutes",
		},
		{
			name:      "deployment timeout",
			operation: "deployment",
			duration:  "5 minutes",
			want:      "deployment timeout after 5 minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewTimeoutError(tt.operation, tt.duration)
			if got.Error() != tt.want {
				t.Errorf("NewTimeoutError().Error() = %q, want %q", got.Error(), tt.want)
			}
		})
	}
}

func TestActionConstants(t *testing.T) {
	// Verify all action constants have the expected string values.
	// This prevents accidental renaming that would break error messages.
	actions := map[Action]string{
		ActionParse:    "parse",
		ActionFind:     "find",
		ActionGet:      "get",
		ActionList:     "list",
		ActionCreate:   "create",
		ActionUpdate:   "update",
		ActionDelete:   "delete",
		ActionBuild:    "build",
		ActionDeploy:   "deploy",
		ActionRollback: "rollback",
		ActionVerify:   "verify",
		ActionSet:      "set",
		ActionReveal:   "reveal",
		ActionEnsure:   "ensure",
	}

	for action, expected := range actions {
		t.Run(expected, func(t *testing.T) {
			if string(action) != expected {
				t.Errorf("Action constant = %q, want %q", string(action), expected)
			}
		})
	}
}
