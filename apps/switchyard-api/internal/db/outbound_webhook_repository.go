package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// OutboundWebhookRepository owns both outbound_webhook_subscriptions and
// outbound_webhook_deliveries — they are tightly coupled and always
// accessed together by the dispatcher/worker.
type OutboundWebhookRepository struct {
	db DBTX
}

// NewOutboundWebhookRepository wires the repo against the root *sql.DB.
func NewOutboundWebhookRepository(db DBTX) *OutboundWebhookRepository {
	return &OutboundWebhookRepository{db: db}
}

// NewOutboundWebhookRepositoryWithTx wires the repo within a transaction.
func NewOutboundWebhookRepositoryWithTx(tx DBTX) *OutboundWebhookRepository {
	return &OutboundWebhookRepository{db: tx}
}

// ---------------------------------------------------------------------------
// Subscription CRUD
// ---------------------------------------------------------------------------

// CreateSubscription persists a new subscription row. The caller is
// responsible for generating the signing secret, computing its SHA-256
// prefix, and encrypting the raw bytes before calling.
func (r *OutboundWebhookRepository) CreateSubscription(
	ctx context.Context,
	sub *types.OutboundWebhookSubscription,
	secretEncrypted []byte,
) error {
	if sub.ID == uuid.Nil {
		sub.ID = uuid.New()
	}
	now := time.Now().UTC()
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = now
	}
	sub.UpdatedAt = now

	eventTypes := eventTypesToStrings(sub.EventTypes)

	query := `
		INSERT INTO outbound_webhook_subscriptions (
			id, project_id, name, url,
			secret_sha256_prefix, secret_encrypted,
			event_types, active, created_by,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := r.db.ExecContext(ctx, query,
		sub.ID, sub.ProjectID, sub.Name, sub.URL,
		sub.SecretSHA256Prefix, secretEncrypted,
		pq.Array(eventTypes), sub.Active, sub.CreatedBy,
		sub.CreatedAt, sub.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert outbound_webhook_subscription: %w", err)
	}
	return nil
}

// GetSubscription fetches a non-deleted subscription by ID. Returns
// sql.ErrNoRows when the row is missing or soft-deleted.
func (r *OutboundWebhookRepository) GetSubscription(
	ctx context.Context, id uuid.UUID,
) (*types.OutboundWebhookSubscription, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, project_id, name, url, secret_sha256_prefix,
			event_types, active, created_by,
			created_at, updated_at,
			last_success_at, last_failure_at, consecutive_failures, auto_disabled_at
		FROM outbound_webhook_subscriptions
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	return scanSubscription(row)
}

// GetSubscriptionSecret returns the encrypted signing-secret blob. It is
// isolated from GetSubscription so handler code cannot accidentally leak
// it in a JSON response — callers must explicitly ask.
func (r *OutboundWebhookRepository) GetSubscriptionSecret(
	ctx context.Context, id uuid.UUID,
) ([]byte, error) {
	var b []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT secret_encrypted
		FROM outbound_webhook_subscriptions
		WHERE id = $1 AND deleted_at IS NULL
	`, id).Scan(&b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListSubscriptionsByProject returns all non-deleted subscriptions for
// the given project, most-recently-created first.
func (r *OutboundWebhookRepository) ListSubscriptionsByProject(
	ctx context.Context, projectID uuid.UUID,
) ([]*types.OutboundWebhookSubscription, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, project_id, name, url, secret_sha256_prefix,
			event_types, active, created_by,
			created_at, updated_at,
			last_success_at, last_failure_at, consecutive_failures, auto_disabled_at
		FROM outbound_webhook_subscriptions
		WHERE project_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*types.OutboundWebhookSubscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListActiveSubscriptionsForEvent returns subscriptions that should
// receive the given event type for the given project. A subscription
// with an empty event_types array matches *every* event.
func (r *OutboundWebhookRepository) ListActiveSubscriptionsForEvent(
	ctx context.Context, projectID uuid.UUID, eventType types.OutboundWebhookEventType,
) ([]*types.OutboundWebhookSubscription, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, project_id, name, url, secret_sha256_prefix,
			event_types, active, created_by,
			created_at, updated_at,
			last_success_at, last_failure_at, consecutive_failures, auto_disabled_at
		FROM outbound_webhook_subscriptions
		WHERE project_id = $1
		  AND active = TRUE
		  AND deleted_at IS NULL
		  AND auto_disabled_at IS NULL
		  AND (array_length(event_types, 1) IS NULL OR $2 = ANY(event_types))
	`, projectID, string(eventType))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*types.OutboundWebhookSubscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpdateSubscription applies a sparse update. Only fields with non-nil
// values in the request mutate.
func (r *OutboundWebhookRepository) UpdateSubscription(
	ctx context.Context,
	id uuid.UUID,
	req *types.OutboundWebhookSubscriptionUpdateRequest,
) error {
	sets := []string{"updated_at = NOW()"}
	args := []interface{}{}
	idx := 1

	if req.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", idx))
		args = append(args, *req.Name)
		idx++
	}
	if req.URL != nil {
		sets = append(sets, fmt.Sprintf("url = $%d", idx))
		args = append(args, *req.URL)
		idx++
	}
	if req.EventTypes != nil {
		sets = append(sets, fmt.Sprintf("event_types = $%d", idx))
		args = append(args, pq.Array(eventTypesToStrings(*req.EventTypes)))
		idx++
	}
	if req.Active != nil {
		sets = append(sets, fmt.Sprintf("active = $%d", idx))
		args = append(args, *req.Active)
		idx++
		// Re-activating clears the auto-disabled flag so the worker
		// starts considering this subscription again.
		if *req.Active {
			sets = append(sets, "auto_disabled_at = NULL", "consecutive_failures = 0")
		}
	}

	args = append(args, id)
	query := fmt.Sprintf(
		`UPDATE outbound_webhook_subscriptions SET %s WHERE id = $%d AND deleted_at IS NULL`,
		strings.Join(sets, ", "), idx,
	)
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update outbound_webhook_subscription: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RotateSubscriptionSecret atomically swaps the stored secret (prefix +
// encrypted blob). The caller computes both values from the new plaintext.
func (r *OutboundWebhookRepository) RotateSubscriptionSecret(
	ctx context.Context, id uuid.UUID, newPrefix string, newEncrypted []byte,
) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE outbound_webhook_subscriptions
		SET secret_sha256_prefix = $1,
		    secret_encrypted = $2,
		    updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL
	`, newPrefix, newEncrypted, id)
	if err != nil {
		return fmt.Errorf("rotate secret: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteSubscription is soft. Deliveries still cascade from FK on hard
// deletes but we preserve history here by flipping deleted_at so the
// operator can still inspect past deliveries.
func (r *OutboundWebhookRepository) DeleteSubscription(
	ctx context.Context, id uuid.UUID,
) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE outbound_webhook_subscriptions
		SET deleted_at = NOW(), active = FALSE, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecordDeliverySuccess updates the denormalized stats. Called by the
// worker after a 2xx response.
func (r *OutboundWebhookRepository) RecordDeliverySuccess(
	ctx context.Context, subID uuid.UUID,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbound_webhook_subscriptions
		SET last_success_at = NOW(),
		    consecutive_failures = 0,
		    updated_at = NOW()
		WHERE id = $1
	`, subID)
	return err
}

// RecordDeliveryFailure increments the failure counter. When it crosses
// AutoDisableThreshold, auto_disabled_at is set in the same statement so
// the dispatcher stops enqueuing. Returns true if auto-disable tripped.
func (r *OutboundWebhookRepository) RecordDeliveryFailure(
	ctx context.Context, subID uuid.UUID, threshold int,
) (autoDisabled bool, err error) {
	err = r.db.QueryRowContext(ctx, `
		UPDATE outbound_webhook_subscriptions
		SET last_failure_at = NOW(),
		    consecutive_failures = consecutive_failures + 1,
		    auto_disabled_at = CASE
		        WHEN consecutive_failures + 1 >= $2 AND auto_disabled_at IS NULL
		            THEN NOW()
		        ELSE auto_disabled_at
		    END,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING auto_disabled_at IS NOT NULL
	`, subID, threshold).Scan(&autoDisabled)
	return
}

// ---------------------------------------------------------------------------
// Delivery CRUD
// ---------------------------------------------------------------------------

// CreateDelivery enqueues a new delivery row. Workers consume via
// ClaimPendingDeliveries.
func (r *OutboundWebhookRepository) CreateDelivery(
	ctx context.Context, d *types.OutboundWebhookDelivery,
) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if d.AttemptNumber == 0 {
		d.AttemptNumber = 1
	}
	if d.Status == "" {
		d.Status = types.OutboundDeliveryPending
	}

	payloadJSON, err := json.Marshal(d.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	query := `
		INSERT INTO outbound_webhook_deliveries (
			id, subscription_id, lifecycle_event_id,
			event_id, event_type, payload, payload_sha256,
			attempt_number, status, next_retry_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = r.db.ExecContext(ctx, query,
		d.ID, d.SubscriptionID, d.LifecycleEventID,
		d.EventID, string(d.EventType), payloadJSON, d.PayloadSHA256,
		d.AttemptNumber, string(d.Status), d.NextRetryAt, d.CreatedAt,
	)
	return err
}

// ClaimPendingDeliveries atomically transitions up to `limit` deliveries
// from pending/failed → delivering using SELECT FOR UPDATE SKIP LOCKED.
// This is the classic multi-worker queue pattern — each worker only
// sees rows no one else has claimed.
func (r *OutboundWebhookRepository) ClaimPendingDeliveries(
	ctx context.Context, limit int,
) ([]*types.OutboundWebhookDelivery, error) {
	if limit <= 0 {
		limit = 10
	}
	// We need the underlying *sql.DB for a transaction — this method
	// isn't safe inside the WithTransaction wrapper (nested txn).
	rows, err := r.db.QueryContext(ctx, `
		WITH claimed AS (
			SELECT id
			FROM outbound_webhook_deliveries
			WHERE status IN ('pending', 'failed')
			  AND (next_retry_at IS NULL OR next_retry_at <= NOW())
			ORDER BY created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbound_webhook_deliveries d
		SET status = 'delivering',
		    attempted_at = NOW()
		FROM claimed
		WHERE d.id = claimed.id
		RETURNING d.id, d.subscription_id, d.lifecycle_event_id,
		          d.event_id, d.event_type, d.payload, d.payload_sha256,
		          d.attempt_number, d.status, d.http_status,
		          d.response_snippet, d.error_message,
		          d.attempted_at, d.delivered_at, d.duration_ms,
		          d.next_retry_at, d.created_at
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*types.OutboundWebhookDelivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkDelivered transitions a row to `delivered` and records response metadata.
func (r *OutboundWebhookRepository) MarkDelivered(
	ctx context.Context, id uuid.UUID, httpStatus int, snippet string, durationMs int,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbound_webhook_deliveries
		SET status = 'delivered',
		    http_status = $2,
		    response_snippet = $3,
		    duration_ms = $4,
		    delivered_at = NOW()
		WHERE id = $1
	`, id, httpStatus, truncateSnippet(snippet), durationMs)
	return err
}

// MarkFailed records a non-terminal failure and schedules the next retry.
// If attemptNumber has reached the ceiling the caller should use MarkDLQ
// instead.
func (r *OutboundWebhookRepository) MarkFailed(
	ctx context.Context,
	id uuid.UUID,
	httpStatus *int,
	errMsg string,
	snippet string,
	durationMs int,
	nextRetryAt time.Time,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbound_webhook_deliveries
		SET status = 'failed',
		    http_status = $2,
		    error_message = $3,
		    response_snippet = $4,
		    duration_ms = $5,
		    next_retry_at = $6
		WHERE id = $1
	`, id, nullInt(httpStatus), truncateErr(errMsg), truncateSnippet(snippet), durationMs, nextRetryAt)
	return err
}

// MarkDLQ is the terminal-failure transition. next_retry_at is cleared so
// the worker's claim query never picks it up again.
func (r *OutboundWebhookRepository) MarkDLQ(
	ctx context.Context,
	id uuid.UUID,
	httpStatus *int,
	errMsg string,
	snippet string,
	durationMs int,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbound_webhook_deliveries
		SET status = 'dlq',
		    http_status = $2,
		    error_message = $3,
		    response_snippet = $4,
		    duration_ms = $5,
		    next_retry_at = NULL
		WHERE id = $1
	`, id, nullInt(httpStatus), truncateErr(errMsg), truncateSnippet(snippet), durationMs)
	return err
}

// GetDelivery returns a single delivery row (used by the redeliver endpoint).
func (r *OutboundWebhookRepository) GetDelivery(
	ctx context.Context, id uuid.UUID,
) (*types.OutboundWebhookDelivery, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, subscription_id, lifecycle_event_id,
			event_id, event_type, payload, payload_sha256,
			attempt_number, status, http_status,
			response_snippet, error_message,
			attempted_at, delivered_at, duration_ms,
			next_retry_at, created_at
		FROM outbound_webhook_deliveries
		WHERE id = $1
	`, id)
	return scanDelivery(row)
}

// ListDeliveriesBySubscription returns the most recent deliveries for a
// given subscription, newest-first, bounded by `limit`.
func (r *OutboundWebhookRepository) ListDeliveriesBySubscription(
	ctx context.Context, subID uuid.UUID, limit, offset int,
) ([]*types.OutboundWebhookDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, subscription_id, lifecycle_event_id,
			event_id, event_type, payload, payload_sha256,
			attempt_number, status, http_status,
			response_snippet, error_message,
			attempted_at, delivered_at, duration_ms,
			next_retry_at, created_at
		FROM outbound_webhook_deliveries
		WHERE subscription_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, subID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []*types.OutboundWebhookDelivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// scan helpers (work with both *sql.Row and *sql.Rows)
// ---------------------------------------------------------------------------

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanSubscription(s scanner) (*types.OutboundWebhookSubscription, error) {
	var sub types.OutboundWebhookSubscription
	var eventTypes []string
	var lastSuccess, lastFailure, autoDisabled sql.NullTime

	err := s.Scan(
		&sub.ID, &sub.ProjectID, &sub.Name, &sub.URL, &sub.SecretSHA256Prefix,
		pq.Array(&eventTypes), &sub.Active, &sub.CreatedBy,
		&sub.CreatedAt, &sub.UpdatedAt,
		&lastSuccess, &lastFailure, &sub.ConsecutiveFailures, &autoDisabled,
	)
	if err != nil {
		return nil, err
	}

	sub.EventTypes = stringsToEventTypes(eventTypes)
	if lastSuccess.Valid {
		t := lastSuccess.Time
		sub.LastSuccessAt = &t
	}
	if lastFailure.Valid {
		t := lastFailure.Time
		sub.LastFailureAt = &t
	}
	if autoDisabled.Valid {
		t := autoDisabled.Time
		sub.AutoDisabledAt = &t
	}
	return &sub, nil
}

func scanDelivery(s scanner) (*types.OutboundWebhookDelivery, error) {
	var d types.OutboundWebhookDelivery
	var lifecycleEventID sql.NullString
	var httpStatus sql.NullInt64
	var snippet, errMsg sql.NullString
	var attemptedAt, deliveredAt, nextRetryAt sql.NullTime
	var durationMs sql.NullInt64
	var payloadBytes []byte
	var eventType, status string

	err := s.Scan(
		&d.ID, &d.SubscriptionID, &lifecycleEventID,
		&d.EventID, &eventType, &payloadBytes, &d.PayloadSHA256,
		&d.AttemptNumber, &status, &httpStatus,
		&snippet, &errMsg,
		&attemptedAt, &deliveredAt, &durationMs,
		&nextRetryAt, &d.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	d.EventType = types.OutboundWebhookEventType(eventType)
	d.Status = types.OutboundWebhookDeliveryStatus(status)

	if lifecycleEventID.Valid {
		id, perr := uuid.Parse(lifecycleEventID.String)
		if perr == nil {
			d.LifecycleEventID = &id
		}
	}
	if httpStatus.Valid {
		n := int(httpStatus.Int64)
		d.HTTPStatus = &n
	}
	if snippet.Valid {
		d.ResponseSnippet = snippet.String
	}
	if errMsg.Valid {
		d.ErrorMessage = errMsg.String
	}
	if attemptedAt.Valid {
		t := attemptedAt.Time
		d.AttemptedAt = &t
	}
	if deliveredAt.Valid {
		t := deliveredAt.Time
		d.DeliveredAt = &t
	}
	if nextRetryAt.Valid {
		t := nextRetryAt.Time
		d.NextRetryAt = &t
	}
	if durationMs.Valid {
		n := int(durationMs.Int64)
		d.DurationMs = &n
	}

	if len(payloadBytes) > 0 {
		_ = json.Unmarshal(payloadBytes, &d.Payload)
	}
	return &d, nil
}

// ---------------------------------------------------------------------------
// misc helpers
// ---------------------------------------------------------------------------

func eventTypesToStrings(xs []types.OutboundWebhookEventType) []string {
	if len(xs) == 0 {
		return []string{}
	}
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = string(x)
	}
	return out
}

func stringsToEventTypes(xs []string) []types.OutboundWebhookEventType {
	if len(xs) == 0 {
		return nil
	}
	out := make([]types.OutboundWebhookEventType, len(xs))
	for i, x := range xs {
		out[i] = types.OutboundWebhookEventType(x)
	}
	return out
}

func truncateSnippet(s string) string {
	if len(s) > types.OutboundWebhookMaxResponseSnippetBytes {
		return s[:types.OutboundWebhookMaxResponseSnippetBytes]
	}
	return s
}

func truncateErr(s string) string {
	// Use the same cap as the snippet — error messages should fit.
	return truncateSnippet(s)
}

func nullInt(n *int) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Valid: true, Int64: int64(*n)}
}
