package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// WebhookRepository handles webhook destination CRUD operations
type WebhookRepository struct {
	db DBTX
}

// NewWebhookRepository creates a new webhook repository
func NewWebhookRepository(db DBTX) *WebhookRepository {
	return &WebhookRepository{db: db}
}

// NewWebhookRepositoryWithTx creates a repository using a transaction
func NewWebhookRepositoryWithTx(tx DBTX) *WebhookRepository {
	return &WebhookRepository{db: tx}
}

// Create creates a new webhook destination
func (r *WebhookRepository) Create(ctx context.Context, webhook *types.WebhookDestination) error {
	webhook.ID = uuid.New()
	webhook.CreatedAt = time.Now()
	webhook.UpdatedAt = time.Now()

	eventsJSON, err := json.Marshal(webhook.Events)
	if err != nil {
		return fmt.Errorf("failed to marshal events: %w", err)
	}

	headersJSON, err := json.Marshal(webhook.CustomHeaders)
	if err != nil {
		return fmt.Errorf("failed to marshal custom headers: %w", err)
	}

	query := `
		INSERT INTO webhook_destinations (
			id, project_id, name, type, webhook_url,
			telegram_bot_token, telegram_chat_id, custom_headers, signing_secret,
			events, enabled, consecutive_failures,
			created_by, created_by_email, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`
	_, err = r.db.ExecContext(ctx, query,
		webhook.ID, webhook.ProjectID, webhook.Name, webhook.Type, webhook.WebhookURL,
		nullString(webhook.TelegramBotToken), nullString(webhook.TelegramChatID),
		headersJSON, nullString(webhook.SigningSecret),
		eventsJSON, webhook.Enabled, 0,
		webhook.CreatedBy, nullString(webhook.CreatedByEmail), webhook.CreatedAt, webhook.UpdatedAt,
	)
	return err
}

// GetByID retrieves a webhook destination by ID
func (r *WebhookRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.WebhookDestination, error) {
	webhook := &types.WebhookDestination{}
	var eventsJSON, headersJSON []byte
	var createdBy sql.NullString
	var telegramToken, telegramChatID, signingSecret, createdByEmail sql.NullString
	var lastDeliveryAt, autoDisabledAt sql.NullTime
	var lastDeliveryStatus, lastDeliveryError sql.NullString

	query := `
		SELECT id, project_id, name, type, webhook_url,
		       telegram_bot_token, telegram_chat_id, custom_headers, signing_secret,
		       events, enabled, last_delivery_at, last_delivery_status, last_delivery_error,
		       consecutive_failures, auto_disabled_at,
		       created_by, created_by_email, created_at, updated_at
		FROM webhook_destinations WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&webhook.ID, &webhook.ProjectID, &webhook.Name, &webhook.Type, &webhook.WebhookURL,
		&telegramToken, &telegramChatID, &headersJSON, &signingSecret,
		&eventsJSON, &webhook.Enabled, &lastDeliveryAt, &lastDeliveryStatus, &lastDeliveryError,
		&webhook.ConsecutiveFailures, &autoDisabledAt,
		&createdBy, &createdByEmail, &webhook.CreatedAt, &webhook.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse JSON fields
	if err := json.Unmarshal(eventsJSON, &webhook.Events); err != nil {
		return nil, fmt.Errorf("failed to unmarshal events: %w", err)
	}
	if len(headersJSON) > 0 {
		if err := json.Unmarshal(headersJSON, &webhook.CustomHeaders); err != nil {
			return nil, fmt.Errorf("failed to unmarshal custom headers: %w", err)
		}
	}

	// Parse nullable fields
	if telegramToken.Valid {
		webhook.TelegramBotToken = telegramToken.String
	}
	if telegramChatID.Valid {
		webhook.TelegramChatID = telegramChatID.String
	}
	if signingSecret.Valid {
		webhook.SigningSecret = signingSecret.String
	}
	if createdBy.Valid {
		parsed, _ := uuid.Parse(createdBy.String)
		webhook.CreatedBy = &parsed
	}
	if createdByEmail.Valid {
		webhook.CreatedByEmail = createdByEmail.String
	}
	if lastDeliveryAt.Valid {
		webhook.LastDeliveryAt = &lastDeliveryAt.Time
	}
	if lastDeliveryStatus.Valid {
		webhook.LastDeliveryStatus = lastDeliveryStatus.String
	}
	if lastDeliveryError.Valid {
		webhook.LastDeliveryError = lastDeliveryError.String
	}
	if autoDisabledAt.Valid {
		webhook.AutoDisabledAt = &autoDisabledAt.Time
	}

	return webhook, nil
}

// ListByProject retrieves all webhook destinations for a project
func (r *WebhookRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]*types.WebhookDestination, error) {
	query := `
		SELECT id, project_id, name, type, webhook_url,
		       telegram_bot_token, telegram_chat_id, custom_headers, signing_secret,
		       events, enabled, last_delivery_at, last_delivery_status, last_delivery_error,
		       consecutive_failures, auto_disabled_at,
		       created_by, created_by_email, created_at, updated_at
		FROM webhook_destinations WHERE project_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var webhooks []*types.WebhookDestination
	for rows.Next() {
		webhook := &types.WebhookDestination{}
		var eventsJSON, headersJSON []byte
		var createdBy sql.NullString
		var telegramToken, telegramChatID, signingSecret, createdByEmail sql.NullString
		var lastDeliveryAt, autoDisabledAt sql.NullTime
		var lastDeliveryStatus, lastDeliveryError sql.NullString

		err := rows.Scan(
			&webhook.ID, &webhook.ProjectID, &webhook.Name, &webhook.Type, &webhook.WebhookURL,
			&telegramToken, &telegramChatID, &headersJSON, &signingSecret,
			&eventsJSON, &webhook.Enabled, &lastDeliveryAt, &lastDeliveryStatus, &lastDeliveryError,
			&webhook.ConsecutiveFailures, &autoDisabledAt,
			&createdBy, &createdByEmail, &webhook.CreatedAt, &webhook.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Parse JSON fields
		if err := json.Unmarshal(eventsJSON, &webhook.Events); err != nil {
			return nil, fmt.Errorf("failed to unmarshal events: %w", err)
		}
		if len(headersJSON) > 0 {
			_ = json.Unmarshal(headersJSON, &webhook.CustomHeaders) // best-effort: optional field
		}

		// Parse nullable fields
		if telegramToken.Valid {
			webhook.TelegramBotToken = telegramToken.String
		}
		if telegramChatID.Valid {
			webhook.TelegramChatID = telegramChatID.String
		}
		if signingSecret.Valid {
			webhook.SigningSecret = signingSecret.String
		}
		if createdBy.Valid {
			parsed, _ := uuid.Parse(createdBy.String)
			webhook.CreatedBy = &parsed
		}
		if createdByEmail.Valid {
			webhook.CreatedByEmail = createdByEmail.String
		}
		if lastDeliveryAt.Valid {
			webhook.LastDeliveryAt = &lastDeliveryAt.Time
		}
		if lastDeliveryStatus.Valid {
			webhook.LastDeliveryStatus = lastDeliveryStatus.String
		}
		if lastDeliveryError.Valid {
			webhook.LastDeliveryError = lastDeliveryError.String
		}
		if autoDisabledAt.Valid {
			webhook.AutoDisabledAt = &autoDisabledAt.Time
		}

		webhooks = append(webhooks, webhook)
	}

	return webhooks, rows.Err()
}

// ListEnabledByEvent retrieves all enabled webhooks subscribed to an event for a project
func (r *WebhookRepository) ListEnabledByEvent(ctx context.Context, projectID uuid.UUID, eventType types.WebhookEventType) ([]*types.WebhookDestination, error) {
	// Use JSONB contains operator to check if event is in the events array
	query := `
		SELECT id, project_id, name, type, webhook_url,
		       telegram_bot_token, telegram_chat_id, custom_headers, signing_secret,
		       events, enabled, last_delivery_at, last_delivery_status, last_delivery_error,
		       consecutive_failures, auto_disabled_at,
		       created_by, created_by_email, created_at, updated_at
		FROM webhook_destinations
		WHERE project_id = $1
		  AND enabled = true
		  AND auto_disabled_at IS NULL
		  AND events @> $2::jsonb
		ORDER BY created_at ASC
	`

	eventJSON, _ := json.Marshal([]string{string(eventType)})

	rows, err := r.db.QueryContext(ctx, query, projectID, eventJSON)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var webhooks []*types.WebhookDestination
	for rows.Next() {
		webhook := &types.WebhookDestination{}
		var eventsJSON, headersJSON []byte
		var createdBy sql.NullString
		var telegramToken, telegramChatID, signingSecret, createdByEmail sql.NullString
		var lastDeliveryAt, autoDisabledAt sql.NullTime
		var lastDeliveryStatus, lastDeliveryError sql.NullString

		err := rows.Scan(
			&webhook.ID, &webhook.ProjectID, &webhook.Name, &webhook.Type, &webhook.WebhookURL,
			&telegramToken, &telegramChatID, &headersJSON, &signingSecret,
			&eventsJSON, &webhook.Enabled, &lastDeliveryAt, &lastDeliveryStatus, &lastDeliveryError,
			&webhook.ConsecutiveFailures, &autoDisabledAt,
			&createdBy, &createdByEmail, &webhook.CreatedAt, &webhook.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Parse JSON fields
		_ = json.Unmarshal(eventsJSON, &webhook.Events) // best-effort: optional field
		if len(headersJSON) > 0 {
			_ = json.Unmarshal(headersJSON, &webhook.CustomHeaders) // best-effort: optional field
		}

		// Parse nullable fields
		if telegramToken.Valid {
			webhook.TelegramBotToken = telegramToken.String
		}
		if telegramChatID.Valid {
			webhook.TelegramChatID = telegramChatID.String
		}
		if signingSecret.Valid {
			webhook.SigningSecret = signingSecret.String
		}
		if createdBy.Valid {
			parsed, _ := uuid.Parse(createdBy.String)
			webhook.CreatedBy = &parsed
		}
		if createdByEmail.Valid {
			webhook.CreatedByEmail = createdByEmail.String
		}
		if lastDeliveryAt.Valid {
			webhook.LastDeliveryAt = &lastDeliveryAt.Time
		}
		if lastDeliveryStatus.Valid {
			webhook.LastDeliveryStatus = lastDeliveryStatus.String
		}
		if lastDeliveryError.Valid {
			webhook.LastDeliveryError = lastDeliveryError.String
		}
		if autoDisabledAt.Valid {
			webhook.AutoDisabledAt = &autoDisabledAt.Time
		}

		webhooks = append(webhooks, webhook)
	}

	return webhooks, rows.Err()
}

// Update updates a webhook destination
func (r *WebhookRepository) Update(ctx context.Context, webhook *types.WebhookDestination) error {
	webhook.UpdatedAt = time.Now()

	eventsJSON, err := json.Marshal(webhook.Events)
	if err != nil {
		return fmt.Errorf("failed to marshal events: %w", err)
	}

	headersJSON, err := json.Marshal(webhook.CustomHeaders)
	if err != nil {
		return fmt.Errorf("failed to marshal custom headers: %w", err)
	}

	query := `
		UPDATE webhook_destinations SET
			name = $2, webhook_url = $3,
			telegram_bot_token = $4, telegram_chat_id = $5,
			custom_headers = $6, events = $7, enabled = $8,
			updated_at = $9
		WHERE id = $1
	`
	_, err = r.db.ExecContext(ctx, query,
		webhook.ID, webhook.Name, webhook.WebhookURL,
		nullString(webhook.TelegramBotToken), nullString(webhook.TelegramChatID),
		headersJSON, eventsJSON, webhook.Enabled,
		webhook.UpdatedAt,
	)
	return err
}

// UpdateDeliveryStatus updates the delivery tracking fields
func (r *WebhookRepository) UpdateDeliveryStatus(ctx context.Context, id uuid.UUID, status string, errorMsg string, incrementFailures bool) error {
	now := time.Now()

	var query string
	var args []interface{}

	if incrementFailures {
		query = `
			UPDATE webhook_destinations SET
				last_delivery_at = $2,
				last_delivery_status = $3,
				last_delivery_error = $4,
				consecutive_failures = consecutive_failures + 1,
				auto_disabled_at = CASE WHEN consecutive_failures >= 4 THEN $2 ELSE NULL END,
				updated_at = $2
			WHERE id = $1
		`
		args = []interface{}{id, now, status, nullString(errorMsg)}
	} else {
		query = `
			UPDATE webhook_destinations SET
				last_delivery_at = $2,
				last_delivery_status = $3,
				last_delivery_error = $4,
				consecutive_failures = 0,
				updated_at = $2
			WHERE id = $1
		`
		args = []interface{}{id, now, status, nullString(errorMsg)}
	}

	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// Delete deletes a webhook destination
func (r *WebhookRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM webhook_destinations WHERE id = $1", id)
	return err
}

// ResetFailures resets the failure count and re-enables a webhook
func (r *WebhookRepository) ResetFailures(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE webhook_destinations SET
			consecutive_failures = 0,
			auto_disabled_at = NULL,
			enabled = true,
			updated_at = $2
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id, time.Now())
	return err
}

// ============================================================================
// Webhook Delivery Repository
// ============================================================================

// CreateDelivery creates a new webhook delivery record
func (r *WebhookRepository) CreateDelivery(ctx context.Context, delivery *types.WebhookDelivery) error {
	delivery.ID = uuid.New()
	delivery.AttemptedAt = time.Now()

	payloadJSON, err := json.Marshal(delivery.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	query := `
		INSERT INTO webhook_deliveries (
			id, webhook_id, event_type, event_id, payload,
			status, status_code, response_body, error_message,
			attempted_at, completed_at, duration_ms, attempt_number
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err = r.db.ExecContext(ctx, query,
		delivery.ID, delivery.WebhookID, delivery.EventType, delivery.EventID, payloadJSON,
		delivery.Status, delivery.StatusCode, nullString(delivery.ResponseBody), nullString(delivery.ErrorMessage),
		delivery.AttemptedAt, delivery.CompletedAt, delivery.DurationMs, delivery.AttemptNumber,
	)
	return err
}

// UpdateDelivery updates a webhook delivery record
func (r *WebhookRepository) UpdateDelivery(ctx context.Context, delivery *types.WebhookDelivery) error {
	query := `
		UPDATE webhook_deliveries SET
			status = $2, status_code = $3, response_body = $4, error_message = $5,
			completed_at = $6, duration_ms = $7
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query,
		delivery.ID, delivery.Status, delivery.StatusCode,
		nullString(delivery.ResponseBody), nullString(delivery.ErrorMessage),
		delivery.CompletedAt, delivery.DurationMs,
	)
	return err
}

// ListDeliveries retrieves recent deliveries for a webhook
func (r *WebhookRepository) ListDeliveries(ctx context.Context, webhookID uuid.UUID, limit int) ([]*types.WebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, webhook_id, event_type, event_id, payload,
		       status, status_code, response_body, error_message,
		       attempted_at, completed_at, duration_ms, attempt_number
		FROM webhook_deliveries
		WHERE webhook_id = $1
		ORDER BY attempted_at DESC
		LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deliveries []*types.WebhookDelivery
	for rows.Next() {
		delivery := &types.WebhookDelivery{}
		var payloadJSON []byte
		var eventID sql.NullString
		var statusCode sql.NullInt64
		var responseBody, errorMessage sql.NullString
		var completedAt sql.NullTime
		var durationMs sql.NullInt64

		err := rows.Scan(
			&delivery.ID, &delivery.WebhookID, &delivery.EventType, &eventID, &payloadJSON,
			&delivery.Status, &statusCode, &responseBody, &errorMessage,
			&delivery.AttemptedAt, &completedAt, &durationMs, &delivery.AttemptNumber,
		)
		if err != nil {
			return nil, err
		}

		// Parse JSON payload
		_ = json.Unmarshal(payloadJSON, &delivery.Payload) // best-effort: optional field

		// Parse nullable fields
		if eventID.Valid {
			parsed, _ := uuid.Parse(eventID.String)
			delivery.EventID = &parsed
		}
		if statusCode.Valid {
			code := int(statusCode.Int64)
			delivery.StatusCode = &code
		}
		if responseBody.Valid {
			delivery.ResponseBody = responseBody.String
		}
		if errorMessage.Valid {
			delivery.ErrorMessage = errorMessage.String
		}
		if completedAt.Valid {
			delivery.CompletedAt = &completedAt.Time
		}
		if durationMs.Valid {
			ms := int(durationMs.Int64)
			delivery.DurationMs = &ms
		}

		deliveries = append(deliveries, delivery)
	}

	return deliveries, rows.Err()
}

// GetDelivery retrieves a specific delivery by ID
func (r *WebhookRepository) GetDelivery(ctx context.Context, id uuid.UUID) (*types.WebhookDelivery, error) {
	query := `
		SELECT id, webhook_id, event_type, event_id, payload, status, status_code,
		       response_body, error_message, attempted_at, completed_at, duration_ms, attempt_number
		FROM webhook_deliveries
		WHERE id = $1`

	var delivery types.WebhookDelivery
	var eventID sql.NullString
	var statusCode sql.NullInt64
	var completedAt sql.NullTime
	var durationMs sql.NullInt64
	var payloadBytes []byte

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&delivery.ID, &delivery.WebhookID, &delivery.EventType, &eventID, &payloadBytes,
		&delivery.Status, &statusCode, &delivery.ResponseBody, &delivery.ErrorMessage,
		&delivery.AttemptedAt, &completedAt, &durationMs, &delivery.AttemptNumber,
	)
	if err != nil {
		return nil, err
	}

	// Parse JSON payload
	if len(payloadBytes) > 0 {
		if err := json.Unmarshal(payloadBytes, &delivery.Payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
		}
	}

	// Handle nullable fields
	if eventID.Valid {
		eid, _ := uuid.Parse(eventID.String)
		delivery.EventID = &eid
	}
	if statusCode.Valid {
		code := int(statusCode.Int64)
		delivery.StatusCode = &code
	}
	if completedAt.Valid {
		delivery.CompletedAt = &completedAt.Time
	}
	if durationMs.Valid {
		ms := int(durationMs.Int64)
		delivery.DurationMs = &ms
	}

	return &delivery, nil
}
