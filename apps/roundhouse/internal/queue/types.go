package queue

import (
	"time"

	"github.com/google/uuid"
)

// BuildJob represents a build job in the queue
type BuildJob struct {
	ID          uuid.UUID   `json:"id"`
	ReleaseID   uuid.UUID   `json:"release_id"`
	ServiceID   uuid.UUID   `json:"service_id"`
	ServiceName string      `json:"service_name"` // Human-readable service name for image tagging
	ProjectID   uuid.UUID   `json:"project_id"`
	ProjectSlug string      `json:"project_slug"` // Project slug for scoped image naming
	GitRepo     string      `json:"git_repo"`
	GitSHA      string      `json:"git_sha"`
	GitBranch   string      `json:"git_branch"`
	BuildConfig BuildConfig `json:"build_config"`
	CallbackURL string      `json:"callback_url"`
	CreatedAt   time.Time   `json:"created_at"`
	Priority    int         `json:"priority"` // Higher = more urgent
}

// BuildConfig specifies how to build the image
type BuildConfig struct {
	Type       string            `json:"type"`       // dockerfile, buildpack, auto
	Dockerfile string            `json:"dockerfile"` // Path to Dockerfile
	Buildpack  string            `json:"buildpack"`  // Buildpack URL
	Context    string            `json:"context"`    // Build context path
	BuildArgs  map[string]string `json:"build_args"` // Build arguments
	Target     string            `json:"target"`     // Multi-stage target
}

// JobStatus represents the current state of a build job
type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusBuilding  JobStatus = "building"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
	StatusCancelled JobStatus = "cancelled"
)

// BuildResult contains the outcome of a build
type BuildResult struct {
	JobID          uuid.UUID `json:"job_id"`
	ReleaseID      uuid.UUID `json:"release_id"`
	Success        bool      `json:"success"`
	ImageURI       string    `json:"image_uri"`
	ImageDigest    string    `json:"image_digest"`
	ImageSizeMB    float64   `json:"image_size_mb"`
	SBOM           string    `json:"sbom"`
	SBOMFormat     string    `json:"sbom_format"`
	ImageSignature string    `json:"image_signature"`
	DurationSecs   float64   `json:"duration_secs"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	LogsURL        string    `json:"logs_url"`
}

// WebhookPayload represents incoming webhook data
type WebhookPayload struct {
	Provider   string `json:"provider"` // github, gitlab, bitbucket
	Event      string `json:"event"`    // push, pull_request, merge_request
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	CommitSHA  string `json:"commit_sha"`
	Author     string `json:"author"`
	Message    string `json:"message"`
	PRURL      string `json:"pr_url,omitempty"`
	PRNumber   int    `json:"pr_number,omitempty"`
}

// EnqueueRequest is the request to enqueue a new build
type EnqueueRequest struct {
	ReleaseID   uuid.UUID   `json:"release_id" binding:"required"`
	ServiceID   uuid.UUID   `json:"service_id" binding:"required"`
	ServiceName string      `json:"service_name" binding:"required"` // Human-readable service name for image tagging
	ProjectID   uuid.UUID   `json:"project_id" binding:"required"`
	ProjectSlug string      `json:"project_slug"` // Project slug for scoped image naming
	GitRepo     string      `json:"git_repo" binding:"required"`
	GitSHA      string      `json:"git_sha" binding:"required"`
	GitBranch   string      `json:"git_branch"`
	BuildConfig BuildConfig `json:"build_config" binding:"required"`
	CallbackURL string      `json:"callback_url"`
	Priority    int         `json:"priority"`
}

// EnqueueResponse is the response after enqueueing a build
type EnqueueResponse struct {
	JobID          uuid.UUID `json:"job_id"`
	Position       int       `json:"position"`
	EstimatedStart time.Time `json:"estimated_start,omitempty"`
}

// FailedCallback represents a callback that needs to be retried
type FailedCallback struct {
	ID          uuid.UUID    `json:"id"`
	JobID       uuid.UUID    `json:"job_id"`
	URL         string       `json:"url"`
	Result      *BuildResult `json:"result"`
	Attempts    int          `json:"attempts"`
	MaxAttempts int          `json:"max_attempts"`
	NextRetry   time.Time    `json:"next_retry"`
	LastError   string       `json:"last_error"`
	CreatedAt   time.Time    `json:"created_at"`
}

// CallbackRetryConfig configures callback retry behavior
type CallbackRetryConfig struct {
	MaxAttempts     int           // Maximum retry attempts (default: 5)
	InitialInterval time.Duration // Initial retry interval (default: 10s)
	MaxInterval     time.Duration // Maximum retry interval (default: 5m)
	Multiplier      float64       // Backoff multiplier (default: 2.0)
}
