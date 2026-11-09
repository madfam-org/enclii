package k8s

import "errors"

type initialJobHoldError struct {
	stage string
	cause error
}

func (e *initialJobHoldError) Error() string { return e.cause.Error() }
func (e *initialJobHoldError) Unwrap() error { return e.cause }

// InitialJobHoldStage exposes only a server-selected closed stage, never an
// underlying Kubernetes/admission error or any credential/environment value.
func InitialJobHoldStage(err error) string {
	var held *initialJobHoldError
	if errors.As(err, &held) {
		return held.stage
	}
	return "executor"
}
