package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// tracer — events package emits spans under this name.
var tracer = otel.Tracer("waybill/events")

// Collector handles event ingestion
type Collector struct {
	db     *sql.DB
	logger *zap.Logger
}

// NewCollector creates a new event collector
func NewCollector(db *sql.DB, logger *zap.Logger) *Collector {
	return &Collector{
		db:     db,
		logger: logger,
	}
}

// Record stores a usage event.
//
// Span "events.record" so we can attribute billing-event ingest latency
// separately from the surrounding HTTP handler. event_type is a low-
// cardinality attribute (well-known enum) — safe to tag.
func (c *Collector) Record(ctx context.Context, req *EventRequest) (*UsageEvent, error) {
	ctx, span := tracer.Start(ctx, "events.record")
	defer span.End()
	span.SetAttributes(
		attribute.String("event.type", string(req.EventType)),
		attribute.String("event.resource_type", string(req.ResourceType)),
	)

	event := &UsageEvent{
		ID:             uuid.New(),
		ProjectID:      req.ProjectID,
		TeamID:         req.TeamID,
		EventType:      req.EventType,
		ResourceType:   req.ResourceType,
		ResourceID:     req.ResourceID,
		ResourceName:   req.ResourceName,
		Metrics:        req.Metrics,
		Metadata:       req.Metadata,
		IdempotencyKey: req.IdempotencyKey,
		CreatedAt:      time.Now(),
	}

	if req.Timestamp != nil {
		event.Timestamp = *req.Timestamp
	} else {
		event.Timestamp = time.Now()
	}

	metricsJSON, err := json.Marshal(event.Metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metrics: %w", err)
	}

	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// ON CONFLICT DO NOTHING is what makes a retried delivery safe.
	//
	// The partial unique index (migration 040) covers idempotency_key WHERE
	// NOT NULL, so an emitter that sends no key still inserts unconditionally
	// — NULLs do not collide — and every pre-existing caller keeps its exact
	// behaviour. `idempotencyKey()` below is what turns "" into NULL; writing
	// the empty string instead would make the FIRST keyless event claim the
	// index and silently swallow every keyless event after it, which is the
	// bug this shape is written to avoid.
	query := `
		INSERT INTO usage_events (
			id, project_id, team_id, event_type, resource_type,
			resource_id, resource_name, metrics, metadata, timestamp, created_at,
			idempotency_key
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
	`

	res, err := c.db.ExecContext(ctx, query,
		event.ID,
		event.ProjectID,
		event.TeamID,
		event.EventType,
		event.ResourceType,
		event.ResourceID,
		event.ResourceName,
		metricsJSON,
		metadataJSON,
		event.Timestamp,
		event.CreatedAt,
		idempotencyKey(event.IdempotencyKey),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to insert event: %w", err)
	}

	// A conflict is a SUCCESS, not an error: the transition is already
	// recorded, and the caller's retry has achieved what it wanted. Reported
	// separately so a flood of duplicates is visible rather than looking like
	// healthy traffic. RowsAffected is advisory (some drivers do not implement
	// it); an error there is treated as "inserted", because guessing
	// "duplicate" would hide a real write.
	if n, rowsErr := res.RowsAffected(); rowsErr == nil && n == 0 {
		c.logger.Info("event already recorded, ignoring duplicate delivery",
			zap.String("event_type", string(event.EventType)),
			zap.String("idempotency_key", event.IdempotencyKey),
			zap.String("project_id", event.ProjectID.String()),
		)
		span.SetAttributes(attribute.Bool("event.duplicate", true))
		return event, nil
	}

	c.logger.Info("event recorded",
		zap.String("event_id", event.ID.String()),
		zap.String("event_type", string(event.EventType)),
		zap.String("project_id", event.ProjectID.String()),
		zap.String("resource_type", event.ResourceType),
	)

	return event, nil
}

// RecordBatch stores multiple events in a transaction
func (c *Collector) RecordBatch(ctx context.Context, events []*EventRequest) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after successful commit

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO usage_events (
			id, project_id, team_id, event_type, resource_type,
			resource_id, resource_name, metrics, metadata, timestamp, created_at,
			idempotency_key
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now()
	for _, req := range events {
		metricsJSON, _ := json.Marshal(req.Metrics)
		metadataJSON, _ := json.Marshal(req.Metadata)

		timestamp := now
		if req.Timestamp != nil {
			timestamp = *req.Timestamp
		}

		_, err = stmt.ExecContext(ctx,
			uuid.New(),
			req.ProjectID,
			req.TeamID,
			req.EventType,
			req.ResourceType,
			req.ResourceID,
			req.ResourceName,
			metricsJSON,
			metadataJSON,
			timestamp,
			now,
			idempotencyKey(req.IdempotencyKey),
		)
		if err != nil {
			return fmt.Errorf("failed to insert event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	c.logger.Info("batch events recorded", zap.Int("count", len(events)))
	return nil
}

// GetUnprocessedEvents retrieves events that haven't been aggregated
func (c *Collector) GetUnprocessedEvents(ctx context.Context, limit int) ([]*UsageEvent, error) {
	query := `
		SELECT id, project_id, team_id, event_type, resource_type,
		       resource_id, resource_name, metrics, metadata, timestamp, created_at
		FROM usage_events
		WHERE processed_at IS NULL
		ORDER BY timestamp ASC
		LIMIT $1
	`

	rows, err := c.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []*UsageEvent
	for rows.Next() {
		var event UsageEvent
		var metricsJSON, metadataJSON []byte

		err := rows.Scan(
			&event.ID,
			&event.ProjectID,
			&event.TeamID,
			&event.EventType,
			&event.ResourceType,
			&event.ResourceID,
			&event.ResourceName,
			&metricsJSON,
			&metadataJSON,
			&event.Timestamp,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		_ = json.Unmarshal(metricsJSON, &event.Metrics)   // best-effort: fields default to zero on error
		_ = json.Unmarshal(metadataJSON, &event.Metadata) // best-effort: fields default to zero on error

		events = append(events, &event)
	}

	return events, nil
}

// MarkProcessed marks events as processed
func (c *Collector) MarkProcessed(ctx context.Context, eventIDs []uuid.UUID) error {
	if len(eventIDs) == 0 {
		return nil
	}

	query := `UPDATE usage_events SET processed_at = $1 WHERE id = ANY($2)`

	ids := make([]string, len(eventIDs))
	for i, id := range eventIDs {
		ids[i] = id.String()
	}

	_, err := c.db.ExecContext(ctx, query, time.Now(), ids)
	if err != nil {
		return fmt.Errorf("failed to mark events processed: %w", err)
	}

	return nil
}

// GetEventsByProject retrieves events for a project within a time range
func (c *Collector) GetEventsByProject(ctx context.Context, projectID uuid.UUID, start, end time.Time) ([]*UsageEvent, error) {
	query := `
		SELECT id, project_id, team_id, event_type, resource_type,
		       resource_id, resource_name, metrics, metadata, timestamp, created_at
		FROM usage_events
		WHERE project_id = $1 AND timestamp >= $2 AND timestamp < $3
		ORDER BY timestamp ASC
	`

	rows, err := c.db.QueryContext(ctx, query, projectID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []*UsageEvent
	for rows.Next() {
		var event UsageEvent
		var metricsJSON, metadataJSON []byte

		err := rows.Scan(
			&event.ID,
			&event.ProjectID,
			&event.TeamID,
			&event.EventType,
			&event.ResourceType,
			&event.ResourceID,
			&event.ResourceName,
			&metricsJSON,
			&metadataJSON,
			&event.Timestamp,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		_ = json.Unmarshal(metricsJSON, &event.Metrics)   // best-effort: fields default to zero on error
		_ = json.Unmarshal(metadataJSON, &event.Metadata) // best-effort: fields default to zero on error

		events = append(events, &event)
	}

	return events, nil
}

// idempotencyKey maps the empty string to a SQL NULL.
//
// This is the whole reason keyless emitters are unaffected by the unique
// index: NULLs never collide. Writing "" instead would let the first keyless
// event take the index and cause every keyless event afterwards to be
// silently discarded as a duplicate — a data-loss bug that would look exactly
// like "billing events stopped arriving" and nothing else.
func idempotencyKey(key string) interface{} {
	if key == "" {
		return nil
	}
	return key
}
