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
	SecretName   string  `json:"secret_name,omitempty"` // K8s Secret name (default: <project>-credentials)

	// Inline provisioning (all optional)
	ProvisionPostgres *PostgresProvisionSpec `json:"provision_postgres,omitempty"`
	ProvisionSecrets  []SecretEntry          `json:"provision_secrets,omitempty"`
	ProvisionR2       *R2ProvisionSpec       `json:"provision_r2,omitempty"`

	// ProvisionAppRole creates (or keeps) a runtime database role that
	// row-level security applies to, with a server-generated password whose
	// connection URL is written into the project Secret.
	ProvisionAppRole *AppRoleSpec `json:"provision_app_role,omitempty"`
	// GenerateSecrets lists project-Secret keys whose values the server
	// generates. Values are never returned or logged.
	GenerateSecrets []GeneratedSecretSpec `json:"generate_secrets,omitempty"`
}

// PreflightResult is the response from the preflight validation endpoint
type PreflightResult struct {
	Pass       bool             `json:"pass"`
	Violations []PreflightIssue `json:"violations,omitempty"`
}

// PreflightIssue describes a single manifest validation failure
type PreflightIssue struct {
	File    string `json:"file"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// PostgresProvisionSpec defines parameters for creating a Postgres database and role.
//
// The owner role's password is either caller-chosen (RolePassword) or
// generated by the server (GeneratePassword). Exactly one of the two must be
// set; the server rejects a request with both or neither.
type PostgresProvisionSpec struct {
	DatabaseName string `json:"database_name" binding:"required"`
	RoleName     string `json:"role_name,omitempty"`
	RolePassword string `json:"role_password,omitempty"`
	// GeneratePassword asks the server to generate the owner password and to
	// write the owner connection URL into the project Secret under
	// URLSecretKey. The value never leaves the server.
	GeneratePassword bool `json:"generate_password,omitempty"`
	// RotatePassword replaces the generated password of an existing role.
	// Without it a re-run keeps the existing role and Secret value.
	RotatePassword bool `json:"rotate_password,omitempty"`
	// URLSecretKey is the Secret key for the owner URL (default DATABASE_URL).
	// Only used with GeneratePassword.
	URLSecretKey string `json:"url_secret_key,omitempty"`
	// ConnectionLimit sets CONNECTION LIMIT on the owner role (0 leaves it as is).
	ConnectionLimit int      `json:"connection_limit,omitempty"`
	Extensions      []string `json:"extensions,omitempty"`
}

// AppRoleSpec defines a runtime database role: LOGIN, no superuser, no
// BYPASSRLS, no inheritance, a connection limit, and CONNECT on one database.
// Table grants stay with the application's own migrations.
type AppRoleSpec struct {
	// RoleName must start with "<database>_" (for example "pravara_app").
	RoleName string `json:"role_name"`
	// DatabaseName defaults to provision_postgres.database_name.
	DatabaseName string `json:"database_name,omitempty"`
	// ConnectionLimit defaults to 5.
	ConnectionLimit int `json:"connection_limit,omitempty"`
	// URLSecretKey is the Secret key for the role's pooled connection URL
	// (default APP_DATABASE_URL).
	URLSecretKey string `json:"url_secret_key,omitempty"`
	// PasswordSecretKey optionally also stores the bare password, for
	// services that take split connection settings.
	PasswordSecretKey string `json:"password_secret_key,omitempty"`
	// RotatePassword replaces the password of an existing role.
	RotatePassword bool `json:"rotate_password,omitempty"`
}

// GeneratedSecretSpec asks the server to generate a random value for one key
// of the project Secret: crypto/rand bytes, base64url without padding. // pragma: allowlist secret -- doc comment, not a value
type GeneratedSecretSpec struct {
	Key string `json:"key"`
	// Bytes is the number of random bytes, 16-128 (default 32).
	Bytes int `json:"bytes,omitempty"`
	// Rotate replaces an existing value. Without it an existing key is kept.
	Rotate bool `json:"rotate,omitempty"`
}

// SecretEntry is a key-value pair for K8s secret provisioning
type SecretEntry struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value" binding:"required"`
}

// R2ProvisionSpec defines parameters for creating a Cloudflare R2 bucket
type R2ProvisionSpec struct {
	BucketName string `json:"bucket_name" binding:"required"`
}

// ProvisionPostgresRequest is the standalone request for Postgres provisioning
type ProvisionPostgresRequest struct {
	Namespace string                `json:"namespace" binding:"required"`
	Spec      PostgresProvisionSpec `json:"spec" binding:"required"`
}

// ProvisionSecretsRequest is the standalone request for K8s secret provisioning
type ProvisionSecretsRequest struct {
	Namespace  string        `json:"namespace" binding:"required"`
	SecretName string        `json:"secret_name,omitempty"`
	Secrets    []SecretEntry `json:"secrets" binding:"required"`
}

// ProvisionR2Request is the standalone request for R2 bucket provisioning
type ProvisionR2Request struct {
	Namespace  string `json:"namespace" binding:"required"`
	BucketName string `json:"bucket_name" binding:"required"`
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
