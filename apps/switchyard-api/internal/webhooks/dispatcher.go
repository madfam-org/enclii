package webhooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Dispatcher owns the fan-out path from a single lifecycle event to N
// subscription deliveries. It is called by the api handler goroutine
// that emits lifecycle events; the actual HTTP POST happens later in
// the Worker pool.
type Dispatcher struct {
	repos *db.Repositories
	log   Logger
}

// Logger is the minimal interface both logging.Logger and a no-op
// stdout logger satisfy. Kept here to avoid a hard dependency on the
// switchyard logging package (useful for unit tests).
type Logger interface {
	Info(ctx context.Context, msg string, fields ...any)
	Error(ctx context.Context, msg string, fields ...any)
	Warn(ctx context.Context, msg string, fields ...any)
}

// NewDispatcher wires the component. The logger can be nil; it will
// then be replaced with a no-op.
func NewDispatcher(repos *db.Repositories, log Logger) *Dispatcher {
	if log == nil {
		log = noopLogger{}
	}
	return &Dispatcher{repos: repos, log: log}
}

// Dispatch fans a single lifecycle event out to every matching active
// subscription by inserting pending delivery rows. It is fail-soft:
// per-subscription errors are logged but do not abort the batch.
//
// `projectID` may be uuid.Nil for platform-wide events; in that case
// we skip dispatch (no project → no subscription to fan out to).
func (d *Dispatcher) Dispatch(
	ctx context.Context,
	projectID uuid.UUID,
	lifecycleEventID *uuid.UUID,
	eventType types.OutboundWebhookEventType,
	data map[string]any,
) error {
	if d == nil || d.repos == nil || d.repos.OutboundWebhooks == nil {
		// Dispatcher not configured (e.g. unit-test Handler) — no-op.
		return nil
	}
	if projectID == uuid.Nil {
		return nil
	}

	subs, err := d.repos.OutboundWebhooks.ListActiveSubscriptionsForEvent(ctx, projectID, eventType)
	if err != nil {
		return fmt.Errorf("list active subscriptions: %w", err)
	}
	if len(subs) == 0 {
		return nil
	}

	envelope, payloadBytes, payloadSHA, err := BuildEnvelope(eventType, data, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("build envelope: %w", err)
	}

	if len(payloadBytes) > types.OutboundWebhookMaxPayloadBytes {
		return fmt.Errorf(
			"webhook payload exceeds %d bytes (%d) — event %s dropped",
			types.OutboundWebhookMaxPayloadBytes, len(payloadBytes), eventType,
		)
	}

	for _, sub := range subs {
		now := time.Now().UTC()
		d.log.Info(ctx, "enqueue outbound webhook delivery",
			"subscription_id", sub.ID.String(),
			"event_id", envelope.ID,
			"event_type", string(eventType),
		)

		// Re-marshal from the envelope so the stored payload reflects
		// what the HTTP body will actually contain (stable JSON field
		// ordering per encoding/json).
		delivery := &types.OutboundWebhookDelivery{
			SubscriptionID:   sub.ID,
			LifecycleEventID: lifecycleEventID,
			EventID:          envelope.ID,
			EventType:        eventType,
			Payload:          envelope.Data,
			PayloadSHA256:    payloadSHA,
			AttemptNumber:    1,
			Status:           types.OutboundDeliveryPending,
			NextRetryAt:      &now, // immediately eligible for pickup
		}
		if err := d.repos.OutboundWebhooks.CreateDelivery(ctx, delivery); err != nil {
			d.log.Error(ctx, "failed to create webhook delivery row",
				"subscription_id", sub.ID.String(),
				"error", err.Error(),
			)
			continue
		}
	}
	return nil
}

// BuildEnvelope produces the canonical on-the-wire JSON body, its
// marshaled bytes, and the SHA-256 hex digest (used for dedup/audit).
// The event ID is "evt_" + hex(unix_nanos) + first-12-hex of UUIDv4, so
// IDs sort lexicographically by time and have 48 bits of randomness for
// collision resistance within the same nanosecond.
func BuildEnvelope(
	eventType types.OutboundWebhookEventType,
	data map[string]any,
	now time.Time,
) (*types.OutboundWebhookEnvelope, []byte, string, error) {
	if data == nil {
		data = map[string]any{}
	}
	envelope := &types.OutboundWebhookEnvelope{
		ID:         makeEventID(now),
		Type:       eventType,
		CreatedAt:  now,
		APIVersion: types.OutboundWebhookAPIVersion,
		Data:       data,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, "", err
	}
	sum := sha256.Sum256(body)
	return envelope, body, hex.EncodeToString(sum[:]), nil
}

// makeEventID returns "evt_" + 16-hex-of-nanos + 12-hex-of-random.
// Lexicographic ordering matches time ordering within the same nano;
// 48 bits of randomness keep two events at the same nanosecond apart
// with overwhelming probability.
func makeEventID(now time.Time) string {
	var sb strings.Builder
	sb.WriteString("evt_")
	// 8-byte unix nanos = 16 hex chars
	nanos := now.UnixNano()
	for i := 60; i >= 0; i -= 4 {
		sb.WriteByte("0123456789abcdef"[(nanos>>i)&0xF])
	}
	// 12 hex chars from uuid4
	id := uuid.New()
	sb.WriteString(hex.EncodeToString(id[:6]))
	return sb.String()
}

// ---------------------------------------------------------------------------
// DispatchError + noop logger
// ---------------------------------------------------------------------------

// ErrPayloadTooLarge is surfaced when an envelope exceeds the 64 KB cap.
var ErrPayloadTooLarge = errors.New("webhook payload exceeds maximum size")

type noopLogger struct{}

func (noopLogger) Info(ctx context.Context, msg string, fields ...any)  {}
func (noopLogger) Error(ctx context.Context, msg string, fields ...any) {}
func (noopLogger) Warn(ctx context.Context, msg string, fields ...any)  {}
