package exitcodes

import (
	"errors"
	"fmt"
	"testing"
)

func TestConstants(t *testing.T) {
	tests := []struct {
		name string
		code int
		want int
	}{
		{"Success", Success, 0},
		{"Validation", Validation, 10},
		{"BuildFail", BuildFail, 20},
		{"DeployFail", DeployFail, 30},
		{"Timeout", Timeout, 40},
		{"AuthError", AuthError, 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.want)
			}
		})
	}
}

func TestFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "nil error returns Success",
			err:  nil,
			want: Success,
		},
		{
			name: "ValidationError returns Validation code",
			err:  &ValidationError{Err: errors.New("bad input")},
			want: Validation,
		},
		{
			name: "BuildError returns BuildFail code",
			err:  &BuildError{Err: errors.New("compile failed")},
			want: BuildFail,
		},
		{
			name: "DeployError returns DeployFail code",
			err:  &DeployError{Err: errors.New("deployment crashed")},
			want: DeployFail,
		},
		{
			name: "TimeoutError returns Timeout code",
			err:  &TimeoutError{Err: errors.New("exceeded 5m")},
			want: Timeout,
		},
		{
			name: "AuthenticationError returns AuthError code",
			err:  &AuthenticationError{Err: errors.New("invalid token")},
			want: AuthError,
		},
		{
			name: "generic error returns 1",
			err:  errors.New("unknown error"),
			want: 1,
		},
		{
			name: "wrapped ValidationError still detected",
			err:  fmt.Errorf("context: %w", &ValidationError{Err: errors.New("bad")}),
			want: Validation,
		},
		{
			name: "wrapped BuildError still detected",
			err:  fmt.Errorf("build step: %w", &BuildError{Err: errors.New("npm failed")}),
			want: BuildFail,
		},
		{
			name: "wrapped DeployError still detected",
			err:  fmt.Errorf("deployment: %w", &DeployError{Err: errors.New("pod crash")}),
			want: DeployFail,
		},
		{
			name: "wrapped TimeoutError still detected",
			err:  fmt.Errorf("operation: %w", &TimeoutError{Err: errors.New("5m")}),
			want: Timeout,
		},
		{
			name: "wrapped AuthenticationError still detected",
			err:  fmt.Errorf("login: %w", &AuthenticationError{Err: errors.New("expired")}),
			want: AuthError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromError(tt.err)
			if got != tt.want {
				t.Errorf("FromError() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestErrorTypes_Error(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "ValidationError.Error()",
			err:  &ValidationError{Err: errors.New("field required")},
			want: "field required",
		},
		{
			name: "BuildError.Error()",
			err:  &BuildError{Err: errors.New("npm install failed")},
			want: "npm install failed",
		},
		{
			name: "DeployError.Error()",
			err:  &DeployError{Err: errors.New("container OOMKilled")},
			want: "container OOMKilled",
		},
		{
			name: "TimeoutError.Error()",
			err:  &TimeoutError{Err: errors.New("exceeded 10 minutes")},
			want: "exceeded 10 minutes",
		},
		{
			name: "AuthenticationError.Error()",
			err:  &AuthenticationError{Err: errors.New("token expired")},
			want: "token expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorTypes_Unwrap(t *testing.T) {
	inner := errors.New("root cause")

	tests := []struct {
		name string
		err  error
	}{
		{"ValidationError", &ValidationError{Err: inner}},
		{"BuildError", &BuildError{Err: inner}},
		{"DeployError", &DeployError{Err: inner}},
		{"TimeoutError", &TimeoutError{Err: inner}},
		{"AuthenticationError", &AuthenticationError{Err: inner}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unwrapped := errors.Unwrap(tt.err)
			if unwrapped != inner {
				t.Errorf("Unwrap() = %v, want %v", unwrapped, inner)
			}
			if !errors.Is(tt.err, inner) {
				t.Error("errors.Is() should find inner error")
			}
		})
	}
}
