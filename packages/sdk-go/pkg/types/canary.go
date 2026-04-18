package types

import (
	"time"

	"github.com/google/uuid"
)

// CanaryRolloutState enumerates the states of a canary rollout lifecycle.
//
// Transitions (enforced by reconciler/canary.go):
//
//	pending → running → validating → promoting → succeeded
//	                  ↘ auto_rolled_back
//	                  ↘ manual_rolled_back
//	    ↘ failed (from any non-terminal state on reconciler error)
//
// Terminal states (no further transitions): succeeded, auto_rolled_back,
// manual_rolled_back, failed.
type CanaryRolloutState string

const (
	CanaryStatePending          CanaryRolloutState = "pending"
	CanaryStateRunning          CanaryRolloutState = "running"
	CanaryStateValidating       CanaryRolloutState = "validating"
	CanaryStatePromoting        CanaryRolloutState = "promoting"
	CanaryStateSucceeded        CanaryRolloutState = "succeeded"
	CanaryStateAutoRolledBack   CanaryRolloutState = "auto_rolled_back"
	CanaryStateManualRolledBack CanaryRolloutState = "manual_rolled_back"
	CanaryStateFailed           CanaryRolloutState = "failed"
)

// IsTerminal reports whether the rollout has reached an end state.
func (s CanaryRolloutState) IsTerminal() bool {
	switch s {
	case CanaryStateSucceeded, CanaryStateAutoRolledBack, CanaryStateManualRolledBack, CanaryStateFailed:
		return true
	}
	return false
}

// IsActive reports whether the rollout is currently in flight (non-terminal).
func (s CanaryRolloutState) IsActive() bool {
	return !s.IsTerminal()
}

// CanaryRollout represents an in-flight or historical canary release.
//
// The two-Deployment pattern is realized in K8s as:
//   - <service>         — the existing stable Deployment (continues to serve traffic)
//   - <service>-canary  — a new Deployment spun up at `CanaryReplicas` count,
//     selected by the same Service via the shared `app=<svc>` label.
//
// Traffic split is purely replica-count proportion (no service mesh). For a
// 20% canary at 5 total replicas: 4 stable + 1 canary.
//
// On auto-promote, a new Deployment `<service>-stable-new` is created from
// the canary digest, scaled up, health-gated, then the old stable is scaled
// to 0 and deleted after a 15-minute soak — see Reconciler.Promote.
type CanaryRollout struct {
	ID            uuid.UUID `json:"id" db:"id"`
	ServiceID     uuid.UUID `json:"service_id" db:"service_id"`
	EnvironmentID uuid.UUID `json:"environment_id" db:"environment_id"`

	StableDeploymentID    uuid.UUID  `json:"stable_deployment_id" db:"stable_deployment_id"`
	CanaryDeploymentID    uuid.UUID  `json:"canary_deployment_id" db:"canary_deployment_id"`
	NewStableDeploymentID *uuid.UUID `json:"new_stable_deployment_id,omitempty" db:"new_stable_deployment_id"`

	// Rollout spec (frozen at start)
	CanaryDigest            string  `json:"canary_digest" db:"canary_digest"`
	CanaryPercentage        int     `json:"canary_percentage" db:"canary_percentage"`
	TotalReplicas           int     `json:"total_replicas" db:"total_replicas"`
	CanaryReplicas          int     `json:"canary_replicas" db:"canary_replicas"`
	StableReplicas          int     `json:"stable_replicas" db:"stable_replicas"`
	ValidationWindowSeconds int     `json:"validation_window_seconds" db:"validation_window_seconds"`
	SmokeEndpoint           string  `json:"smoke_endpoint,omitempty" db:"smoke_endpoint"`
	ErrorRateThreshold      float64 `json:"error_rate_threshold" db:"error_rate_threshold"`

	// State machine
	State               CanaryRolloutState `json:"state" db:"state"`
	StartedAt           *time.Time         `json:"started_at,omitempty" db:"started_at"`
	ValidatingStartedAt *time.Time         `json:"validating_started_at,omitempty" db:"validating_started_at"`
	PromotingStartedAt  *time.Time         `json:"promoting_started_at,omitempty" db:"promoting_started_at"`
	TerminalAt          *time.Time         `json:"terminal_at,omitempty" db:"terminal_at"`

	// Audit / diagnostics
	InitiatedBy     *uuid.UUID `json:"initiated_by,omitempty" db:"initiated_by"`
	ChangeTicketURL string     `json:"change_ticket_url,omitempty" db:"change_ticket_url"`
	LastError       string     `json:"last_error,omitempty" db:"last_error"`
	RollbackReason  string     `json:"rollback_reason,omitempty" db:"rollback_reason"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// CanaryRolloutSpec is the user-supplied configuration for a rollout.
// Validation lives on this struct so it can be reused by API + CLI.
type CanaryRolloutSpec struct {
	// ImageDigest of the candidate (must already be built — reuse an existing
	// Release). The caller is responsible for resolving a service.yaml deploy
	// into a concrete digest before calling the API.
	ImageDigest string `json:"digest"`
	// Percentage of traffic to route to the canary (5-50).
	Percentage int `json:"percentage"`
	// ValidationWindowMinutes is how long the canary must stay healthy before
	// auto-promotion triggers. Range: 1-60.
	ValidationWindowMinutes int `json:"validation_window_minutes"`
	// SmokeEndpoint (optional) is a URL Path on the canary Service (e.g.
	// "/health/deep") that must return 200 during the validation window.
	SmokeEndpoint string `json:"smoke_endpoint,omitempty"`
	// ErrorRateThreshold allows at most this fraction of 5xx responses during
	// validation. Defaults to 0.05 (5%). Range: 0.0-0.5.
	ErrorRateThreshold float64 `json:"error_rate_threshold,omitempty"`
	// ChangeTicketURL is required for production environments (matches the
	// DeployService / InstantRollback HITL gate).
	ChangeTicketURL string `json:"change_ticket_url,omitempty"`
}
