package types

import (
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// OUTBOUND LIFECYCLE WEBHOOK TYPES (P2.3)
//
// Customer-configurable HTTPS endpoints that receive signed JSON payloads
// whenever a lifecycle event (deploy/rollback/scale/secret.rotated) occurs.
//
// Distinct from WebhookDestination (Slack/Discord/Email). These webhooks:
//   - Require HTTPS
//   - Are HMAC-SHA256 signed per Stripe convention
//   - Have individual per-subscription signing secrets
//   - Ship with retry/DLQ semantics for at-least-once delivery
// ============================================================================

// OutboundWebhookAPIVersion is emitted in every webhook envelope so
// subscribers can gate behavior on schema evolution. Bump only on
// breaking payload changes.
const OutboundWebhookAPIVersion = "2026-04-01"

// OutboundWebhookEventType is the catalogue of lifecycle events that
// outbound webhooks can subscribe to.
type OutboundWebhookEventType string

const (
	OutboundEventDeployStarted   OutboundWebhookEventType = "deploy.started"
	OutboundEventDeploySucceeded OutboundWebhookEventType = "deploy.succeeded"
	OutboundEventDeployFailed    OutboundWebhookEventType = "deploy.failed"
	OutboundEventRollbackSuccess OutboundWebhookEventType = "rollback.succeeded"
	OutboundEventSecretRotated   OutboundWebhookEventType = "secret.rotated"
	OutboundEventServiceScaled   OutboundWebhookEventType = "service.scaled"
	OutboundEventTestPing        OutboundWebhookEventType = "test.ping"
)

// AllOutboundWebhookEventTypes returns the canonical list used by the
// /webhooks/event-types endpoint and CLI validation. test.ping is
// reserved for the `enclii webhooks test` command and should not be
// subscribable like a real event.
func AllOutboundWebhookEventTypes() []OutboundWebhookEventType {
	return []OutboundWebhookEventType{
		OutboundEventDeployStarted,
		OutboundEventDeploySucceeded,
		OutboundEventDeployFailed,
		OutboundEventRollbackSuccess,
		OutboundEventSecretRotated,
		OutboundEventServiceScaled,
	}
}

// IsValidOutboundEventType reports whether s is a recognized subscribable
// lifecycle event. test.ping is intentionally excluded — it is a control
// event fired only by operator tooling.
func IsValidOutboundEventType(s string) bool {
	for _, et := range AllOutboundWebhookEventTypes() {
		if string(et) == s {
			return true
		}
	}
	return false
}

// OutboundWebhookDeliveryStatus is the machine state of a single delivery
// row. `delivering` is transient (owned by a worker); `delivered` is
// terminal-happy; `failed` means retriable; `dlq` means gave up.
type OutboundWebhookDeliveryStatus string

const (
	OutboundDeliveryPending    OutboundWebhookDeliveryStatus = "pending"
	OutboundDeliveryDelivering OutboundWebhookDeliveryStatus = "delivering"
	OutboundDeliveryDelivered  OutboundWebhookDeliveryStatus = "delivered"
	OutboundDeliveryFailed     OutboundWebhookDeliveryStatus = "failed"
	OutboundDeliveryDLQ        OutboundWebhookDeliveryStatus = "dlq"
)

// OutboundWebhookMaxAttempts is the retry ceiling. After the 5th attempt
// the delivery transitions to `dlq` and no further retries occur.
const OutboundWebhookMaxAttempts = 5

// OutboundWebhookMaxPayloadBytes is the envelope-size cap. Payloads
// larger than this are rejected at enqueue time with a clear error.
const OutboundWebhookMaxPayloadBytes = 64 * 1024

// OutboundWebhookMaxResponseSnippetBytes is how much of the subscriber's
// response body we persist for debugging. Anything past this is truncated
// to avoid inadvertently storing user data.
const OutboundWebhookMaxResponseSnippetBytes = 500

// OutboundWebhookAutoDisableThreshold — when a subscription crosses this
// many consecutive failures it is automatically disabled and an audit
// event is emitted for the operator.
const OutboundWebhookAutoDisableThreshold = 20

// OutboundWebhookSignatureTolerance is the maximum allowable skew between
// subscriber clock and signature timestamp. Mirrors Stripe's default.
const OutboundWebhookSignatureTolerance = 5 * time.Minute

// OutboundWebhookSignatureHeader is the HTTP header that carries the
// t=<ts>,v1=<hex> tuple. Kept in a constant so SDK consumers can import it.
const OutboundWebhookSignatureHeader = "X-Enclii-Signature"

// OutboundWebhookEventHeader surfaces the event type as a plaintext
// header so subscribers can route without parsing the body.
const OutboundWebhookEventHeader = "X-Enclii-Event"

// OutboundWebhookDeliveryIDHeader gives subscribers a stable idempotency
// key for deduping retries.
const OutboundWebhookDeliveryIDHeader = "X-Enclii-Delivery-Id"

// OutboundWebhookSubscription is a single customer-configured endpoint.
// The raw signing_secret is never returned — callers see it once at
// create/rotate time and must persist it themselves.
type OutboundWebhookSubscription struct {
	ID                  uuid.UUID                  `json:"id"`
	ProjectID           uuid.UUID                  `json:"project_id"`
	Name                string                     `json:"name"`
	URL                 string                     `json:"url"`
	SecretSHA256Prefix  string                     `json:"secret_sha256_prefix"`
	EventTypes          []OutboundWebhookEventType `json:"event_types"`
	Active              bool                       `json:"active"`
	CreatedBy           string                     `json:"created_by"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
	LastSuccessAt       *time.Time                 `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time                 `json:"last_failure_at,omitempty"`
	ConsecutiveFailures int                        `json:"consecutive_failures"`
	AutoDisabledAt      *time.Time                 `json:"auto_disabled_at,omitempty"`
}

// OutboundWebhookSubscriptionCreateRequest is the API input for POST.
type OutboundWebhookSubscriptionCreateRequest struct {
	Name       string                     `json:"name" binding:"required"`
	URL        string                     `json:"url" binding:"required"`
	EventTypes []OutboundWebhookEventType `json:"event_types"`
}

// OutboundWebhookSubscriptionUpdateRequest is the API input for PATCH.
// Only non-nil fields are applied — allows toggling `active` without
// clobbering url/events.
type OutboundWebhookSubscriptionUpdateRequest struct {
	Name       *string                     `json:"name,omitempty"`
	URL        *string                     `json:"url,omitempty"`
	EventTypes *[]OutboundWebhookEventType `json:"event_types,omitempty"`
	Active     *bool                       `json:"active,omitempty"`
}

// OutboundWebhookSubscriptionCreateResponse is returned exactly once at
// creation or secret rotation. The SigningSecret is plaintext and must
// be saved by the caller — the server will never return it again.
type OutboundWebhookSubscriptionCreateResponse struct {
	Subscription  OutboundWebhookSubscription `json:"subscription"`
	SigningSecret string                      `json:"signing_secret"`
	Note          string                      `json:"note"`
}

// OutboundWebhookDelivery is a single delivery attempt row. Retries
// produce additional rows with the same event_id but incremented
// attempt_number.
type OutboundWebhookDelivery struct {
	ID               uuid.UUID                     `json:"id"`
	SubscriptionID   uuid.UUID                     `json:"subscription_id"`
	LifecycleEventID *uuid.UUID                    `json:"lifecycle_event_id,omitempty"`
	EventID          string                        `json:"event_id"`
	EventType        OutboundWebhookEventType      `json:"event_type"`
	Payload          map[string]any                `json:"payload,omitempty"`
	PayloadSHA256    string                        `json:"payload_sha256"`
	AttemptNumber    int                           `json:"attempt_number"`
	Status           OutboundWebhookDeliveryStatus `json:"status"`
	HTTPStatus       *int                          `json:"http_status,omitempty"`
	ResponseSnippet  string                        `json:"response_snippet,omitempty"`
	ErrorMessage     string                        `json:"error_message,omitempty"`
	AttemptedAt      *time.Time                    `json:"attempted_at,omitempty"`
	DeliveredAt      *time.Time                    `json:"delivered_at,omitempty"`
	DurationMs       *int                          `json:"duration_ms,omitempty"`
	NextRetryAt      *time.Time                    `json:"next_retry_at,omitempty"`
	CreatedAt        time.Time                     `json:"created_at"`
}

// OutboundWebhookEnvelope is the canonical JSON body posted to the
// subscriber. All events share this shape; event-specific fields live
// under `data`.
type OutboundWebhookEnvelope struct {
	ID         string                   `json:"id"`
	Type       OutboundWebhookEventType `json:"type"`
	CreatedAt  time.Time                `json:"created_at"`
	APIVersion string                   `json:"api_version"`
	Data       map[string]any           `json:"data"`
}
