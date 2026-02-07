package types

import (
	"time"

	"github.com/google/uuid"
)

// Lifecycle event type constants
const (
	LifecyclePushReceived     = "push_received"
	LifecycleBuildStarted     = "build_started"
	LifecycleBuildSucceeded   = "build_succeeded"
	LifecycleBuildFailed      = "build_failed"
	LifecycleImagePushed      = "image_pushed"
	LifecycleDigestCommitted  = "digest_committed"
	LifecycleDeployStarted    = "deploy_started"
	LifecycleDeploySynced     = "deploy_synced"
	LifecycleDeployHealthy    = "deploy_healthy"
	LifecycleDeployDegraded   = "deploy_degraded"
	LifecycleDeployFailed     = "deploy_failed"
	LifecyclePreviewCreated   = "preview_created"
	LifecyclePreviewDestroyed = "preview_destroyed"
)

// Lifecycle event source constants
const (
	SourceGitHubWebhook  = "github_webhook"
	SourceCICallback     = "ci_callback"
	SourceArgocdCallback = "argocd_callback"
	SourcePlatform       = "platform"
	SourceManual         = "manual"
)

// DeploymentLifecycleEvent represents a single event in the deployment pipeline
type DeploymentLifecycleEvent struct {
	ID           uuid.UUID              `json:"id"`
	DeploymentID *uuid.UUID             `json:"deployment_id,omitempty"`
	ReleaseID    *uuid.UUID             `json:"release_id,omitempty"`
	CIRunID      *uuid.UUID             `json:"ci_run_id,omitempty"`
	ProjectID    *uuid.UUID             `json:"project_id,omitempty"`
	ServiceID    *uuid.UUID             `json:"service_id,omitempty"`
	RepoFullName string                 `json:"repo_full_name"`
	CommitSHA    string                 `json:"commit_sha"`
	Branch       string                 `json:"branch"`
	Ref          string                 `json:"ref"`
	TargetEnv    *string                `json:"target_env,omitempty"`
	EventType    string                 `json:"event_type"`
	Source       string                 `json:"source"`
	Message      *string                `json:"message,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

// LifecycleEventCreate is the input for creating a new lifecycle event
type LifecycleEventCreate struct {
	DeploymentID *uuid.UUID             `json:"deployment_id,omitempty"`
	ReleaseID    *uuid.UUID             `json:"release_id,omitempty"`
	CIRunID      *uuid.UUID             `json:"ci_run_id,omitempty"`
	ProjectID    *uuid.UUID             `json:"project_id,omitempty"`
	ServiceID    *uuid.UUID             `json:"service_id,omitempty"`
	RepoFullName string                 `json:"repo_full_name" binding:"required"`
	CommitSHA    string                 `json:"commit_sha" binding:"required"`
	Branch       string                 `json:"branch" binding:"required"`
	Ref          string                 `json:"ref" binding:"required"`
	TargetEnv    *string                `json:"target_env,omitempty"`
	EventType    string                 `json:"event_type" binding:"required"`
	Source       string                 `json:"source" binding:"required"`
	Message      *string                `json:"message,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// LifecycleTimelineQuery supports flexible filtering of lifecycle events
type LifecycleTimelineQuery struct {
	RepoFullName *string
	Branch       *string
	CommitSHA    *string
	ProjectID    *uuid.UUID
	TargetEnv    *string
	EventTypes   []string
	Since        *time.Time
	Limit        int
}

// OnboardingRegistration tracks self-service repo onboarding
type OnboardingRegistration struct {
	ID             uuid.UUID              `json:"id"`
	ProjectID      uuid.UUID              `json:"project_id"`
	RepoFullName   string                 `json:"repo_full_name"`
	WebhookID      *int64                 `json:"webhook_id,omitempty"`
	WebhookSecret  *string                `json:"-"`
	ArgocdAppName  *string                `json:"argocd_app_name,omitempty"`
	OnboardStatus  string                 `json:"onboard_status"`
	ConfigSnapshot map[string]interface{} `json:"config_snapshot,omitempty"`
	ErrorMessage   *string                `json:"error_message,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// OnboardingRequest is the input for the onboarding API
type OnboardingRequest struct {
	RepoFullName string  `json:"repo_full_name" binding:"required"`
	ProjectName  string  `json:"project_name" binding:"required"`
	Namespace    string  `json:"namespace,omitempty"`
	ManifestPath string  `json:"manifest_path,omitempty"`
	Branch       *string `json:"branch,omitempty"`
}

// DeriveTargetEnv maps a branch name to a target environment
func DeriveTargetEnv(branch string) string {
	switch {
	case branch == "main" || branch == "master":
		return "production"
	case branch == "staging" || startsWith(branch, "staging/"):
		return "staging"
	case startsWith(branch, "feature/") || startsWith(branch, "fix/") || startsWith(branch, "feat/"):
		return "preview"
	case branch == "dev" || branch == "develop" || startsWith(branch, "dev/"):
		return "dev"
	default:
		return "preview"
	}
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
