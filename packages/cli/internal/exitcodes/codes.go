package exitcodes

import "errors"

// Exit codes matching CLAUDE.md documentation.
const (
	Success    = 0
	Validation = 10
	BuildFail  = 20
	DeployFail = 30
	Timeout    = 40
	AuthError  = 50
)

// ValidationError indicates invalid input or configuration.
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

// BuildError indicates a build failure.
type BuildError struct{ Err error }

func (e *BuildError) Error() string { return e.Err.Error() }
func (e *BuildError) Unwrap() error { return e.Err }

// DeployError indicates a deployment failure.
type DeployError struct{ Err error }

func (e *DeployError) Error() string { return e.Err.Error() }
func (e *DeployError) Unwrap() error { return e.Err }

// TimeoutError indicates an operation timed out.
type TimeoutError struct{ Err error }

func (e *TimeoutError) Error() string { return e.Err.Error() }
func (e *TimeoutError) Unwrap() error { return e.Err }

// AuthenticationError indicates an authentication failure.
type AuthenticationError struct{ Err error }

func (e *AuthenticationError) Error() string { return e.Err.Error() }
func (e *AuthenticationError) Unwrap() error { return e.Err }

// FromError maps typed errors to exit codes, defaulting to 1.
func FromError(err error) int {
	if err == nil {
		return Success
	}
	var v *ValidationError
	if errors.As(err, &v) {
		return Validation
	}
	var b *BuildError
	if errors.As(err, &b) {
		return BuildFail
	}
	var d *DeployError
	if errors.As(err, &d) {
		return DeployFail
	}
	var t *TimeoutError
	if errors.As(err, &t) {
		return Timeout
	}
	var a *AuthenticationError
	if errors.As(err, &a) {
		return AuthError
	}
	return 1
}
