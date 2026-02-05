package types

import (
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// NOTIFICATION WEBHOOK TYPES
// Slack, Discord, and Telegram notifications for deployment events
// Platform webhook functionality
// ============================================================================

// WebhookType represents the type of webhook destination
type WebhookType string

const (
	WebhookTypeSlack    WebhookType = "slack"
	WebhookTypeDiscord  WebhookType = "discord"
	WebhookTypeTelegram WebhookType = "telegram"
	WebhookTypeCustom   WebhookType = "custom"
)

// WebhookDeliveryStatus represents the status of a webhook delivery
type WebhookDeliveryStatus string

const (
	WebhookDeliveryStatusPending WebhookDeliveryStatus = "pending"
	WebhookDeliveryStatusSuccess WebhookDeliveryStatus = "success"
	WebhookDeliveryStatusFailed  WebhookDeliveryStatus = "failed"
)

// WebhookEventType defines the events that can trigger webhooks
type WebhookEventType string

const (
	// Deployment events
	WebhookEventDeploymentStarted   WebhookEventType = "deployment.started"
	WebhookEventDeploymentSucceeded WebhookEventType = "deployment.succeeded"
	WebhookEventDeploymentFailed    WebhookEventType = "deployment.failed"
	WebhookEventDeploymentCancelled WebhookEventType = "deployment.cancelled"

	// Build events
	WebhookEventBuildStarted   WebhookEventType = "build.started"
	WebhookEventBuildSucceeded WebhookEventType = "build.succeeded"
	WebhookEventBuildFailed    WebhookEventType = "build.failed"

	// Service events
	WebhookEventServiceCreated   WebhookEventType = "service.created"
	WebhookEventServiceDeleted   WebhookEventType = "service.deleted"
	WebhookEventServiceStarted   WebhookEventType = "service.started"
	WebhookEventServiceStopped   WebhookEventType = "service.stopped"
	WebhookEventServiceUnhealthy WebhookEventType = "service.unhealthy"

	// Database addon events
	WebhookEventDatabaseReady  WebhookEventType = "database.ready"
	WebhookEventDatabaseFailed WebhookEventType = "database.failed"
)

// WebhookDestination represents a configured webhook endpoint
type WebhookDestination struct {
	ID        uuid.UUID   `json:"id" db:"id"`
	ProjectID uuid.UUID   `json:"project_id" db:"project_id"`
	Name      string      `json:"name" db:"name"`
	Type      WebhookType `json:"type" db:"type"`

	// Webhook URL (for Slack, Discord, Custom)
	WebhookURL string `json:"webhook_url,omitempty" db:"webhook_url"`

	// Telegram-specific fields
	TelegramBotToken string `json:"telegram_bot_token,omitempty" db:"telegram_bot_token"` // Encrypted
	TelegramChatID   string `json:"telegram_chat_id,omitempty" db:"telegram_chat_id"`

	// Custom webhook fields
	CustomHeaders map[string]string `json:"custom_headers,omitempty" db:"custom_headers"`
	SigningSecret string            `json:"-" db:"signing_secret"` // Never exposed in API

	// Event subscriptions
	Events []WebhookEventType `json:"events" db:"events"`

	// Status
	Enabled bool `json:"enabled" db:"enabled"`

	// Delivery tracking
	LastDeliveryAt      *time.Time `json:"last_delivery_at,omitempty" db:"last_delivery_at"`
	LastDeliveryStatus  string     `json:"last_delivery_status,omitempty" db:"last_delivery_status"`
	LastDeliveryError   string     `json:"last_delivery_error,omitempty" db:"last_delivery_error"`
	ConsecutiveFailures int        `json:"consecutive_failures" db:"consecutive_failures"`
	AutoDisabledAt      *time.Time `json:"auto_disabled_at,omitempty" db:"auto_disabled_at"`

	// Audit fields
	CreatedBy      *uuid.UUID `json:"created_by,omitempty" db:"created_by"`
	CreatedByEmail string     `json:"created_by_email,omitempty" db:"created_by_email"`

	// Timestamps
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// WebhookDelivery represents a single webhook delivery attempt
type WebhookDelivery struct {
	ID            uuid.UUID             `json:"id" db:"id"`
	WebhookID     uuid.UUID             `json:"webhook_id" db:"webhook_id"`
	EventType     WebhookEventType      `json:"event_type" db:"event_type"`
	EventID       *uuid.UUID            `json:"event_id,omitempty" db:"event_id"`
	Payload       map[string]any        `json:"payload" db:"payload"`
	Status        WebhookDeliveryStatus `json:"status" db:"status"`
	StatusCode    *int                  `json:"status_code,omitempty" db:"status_code"`
	ResponseBody  string                `json:"response_body,omitempty" db:"response_body"`
	ErrorMessage  string                `json:"error_message,omitempty" db:"error_message"`
	AttemptedAt   time.Time             `json:"attempted_at" db:"attempted_at"`
	CompletedAt   *time.Time            `json:"completed_at,omitempty" db:"completed_at"`
	DurationMs    *int                  `json:"duration_ms,omitempty" db:"duration_ms"`
	AttemptNumber int                   `json:"attempt_number" db:"attempt_number"`
}

// WebhookCreateRequest is the API request for creating a webhook
type WebhookCreateRequest struct {
	Name             string             `json:"name" binding:"required"`
	Type             WebhookType        `json:"type" binding:"required"`
	WebhookURL       string             `json:"webhook_url,omitempty"`
	TelegramBotToken string             `json:"telegram_bot_token,omitempty"`
	TelegramChatID   string             `json:"telegram_chat_id,omitempty"`
	Events           []WebhookEventType `json:"events" binding:"required"`
	Enabled          *bool              `json:"enabled,omitempty"` // Defaults to true
}

// WebhookUpdateRequest is the API request for updating a webhook
type WebhookUpdateRequest struct {
	Name             *string            `json:"name,omitempty"`
	WebhookURL       *string            `json:"webhook_url,omitempty"`
	TelegramBotToken *string            `json:"telegram_bot_token,omitempty"`
	TelegramChatID   *string            `json:"telegram_chat_id,omitempty"`
	Events           []WebhookEventType `json:"events,omitempty"`
	Enabled          *bool              `json:"enabled,omitempty"`
}

// WebhookTestRequest is the API request for testing a webhook
type WebhookTestRequest struct {
	EventType WebhookEventType `json:"event_type" binding:"required"`
}

// WebhookEvent represents an event payload sent to webhooks
type WebhookEvent struct {
	ID        uuid.UUID          `json:"id"`
	Type      WebhookEventType   `json:"type"`
	Timestamp time.Time          `json:"timestamp"`
	ProjectID uuid.UUID          `json:"project_id"`
	Project   WebhookProjectInfo `json:"project"`

	// Event-specific data (one of these will be populated)
	Deployment *WebhookDeploymentInfo `json:"deployment,omitempty"`
	Build      *WebhookBuildInfo      `json:"build,omitempty"`
	Service    *WebhookServiceInfo    `json:"service,omitempty"`
	Database   *WebhookDatabaseInfo   `json:"database,omitempty"`
}

// WebhookProjectInfo contains project info included in webhook payloads
type WebhookProjectInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

// WebhookDeploymentInfo contains deployment info for webhook payloads
type WebhookDeploymentInfo struct {
	ID            uuid.UUID `json:"id"`
	ServiceName   string    `json:"service_name"`
	Environment   string    `json:"environment"`
	Status        string    `json:"status"`
	CommitSHA     string    `json:"commit_sha,omitempty"`
	CommitMessage string    `json:"commit_message,omitempty"`
	Branch        string    `json:"branch,omitempty"`
	URL           string    `json:"url,omitempty"`
	Duration      *int      `json:"duration_seconds,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// WebhookBuildInfo contains build info for webhook payloads
type WebhookBuildInfo struct {
	ID          uuid.UUID `json:"id"`
	ServiceName string    `json:"service_name"`
	Status      string    `json:"status"`
	CommitSHA   string    `json:"commit_sha,omitempty"`
	Duration    *int      `json:"duration_seconds,omitempty"`
	ImageTag    string    `json:"image_tag,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// WebhookServiceInfo contains service info for webhook payloads
type WebhookServiceInfo struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	URL    string    `json:"url,omitempty"`
	Error  string    `json:"error,omitempty"`
}

// WebhookDatabaseInfo contains database addon info for webhook payloads
type WebhookDatabaseInfo struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Type   string    `json:"type"`
	Status string    `json:"status"`
	Error  string    `json:"error,omitempty"`
}

// CIRunStatus represents the status of a CI workflow run
type CIRunStatus string

const (
	CIRunStatusQueued     CIRunStatus = "queued"
	CIRunStatusInProgress CIRunStatus = "in_progress"
	CIRunStatusCompleted  CIRunStatus = "completed"
)

// CIRunConclusion represents the final result of a completed CI run
type CIRunConclusion string

const (
	CIRunConclusionSuccess        CIRunConclusion = "success"
	CIRunConclusionFailure        CIRunConclusion = "failure"
	CIRunConclusionCancelled      CIRunConclusion = "cancelled"
	CIRunConclusionSkipped        CIRunConclusion = "skipped"
	CIRunConclusionTimedOut       CIRunConclusion = "timed_out"
	CIRunConclusionActionRequired CIRunConclusion = "action_required"
)

// CIRun represents a GitHub Actions workflow run for tracking CI status
type CIRun struct {
	ID           uuid.UUID        `json:"id" db:"id"`
	ServiceID    uuid.UUID        `json:"service_id" db:"service_id"`
	CommitSHA    string           `json:"commit_sha" db:"commit_sha"`
	WorkflowName string           `json:"workflow_name" db:"workflow_name"`
	WorkflowID   int64            `json:"workflow_id" db:"workflow_id"`
	RunID        int64            `json:"run_id" db:"run_id"`
	RunNumber    int              `json:"run_number" db:"run_number"`
	Status       CIRunStatus      `json:"status" db:"status"`
	Conclusion   *CIRunConclusion `json:"conclusion,omitempty" db:"conclusion"`
	HTMLURL      string           `json:"html_url,omitempty" db:"html_url"`
	Branch       string           `json:"branch,omitempty" db:"branch"`
	EventType    string           `json:"event_type,omitempty" db:"event_type"`
	Actor        string           `json:"actor,omitempty" db:"actor"`
	StartedAt    *time.Time       `json:"started_at,omitempty" db:"started_at"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt    time.Time        `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at" db:"updated_at"`
}
