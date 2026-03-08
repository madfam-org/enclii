package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

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

// Record stores a usage event
func (c *Collector) Record(ctx context.Context, req *EventRequest) (*UsageEvent, error) {
	event := &UsageEvent{
		ID:           uuid.New(),
		ProjectID:    req.ProjectID,
		TeamID:       req.TeamID,
		EventType:    req.EventType,
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		ResourceName: req.ResourceName,
		Metrics:      req.Metrics,
		Metadata:     req.Metadata,
		CreatedAt:    time.Now(),
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

	query := `
		INSERT INTO usage_events (
			id, project_id, team_id, event_type, resource_type,
			resource_id, resource_name, metrics, metadata, timestamp, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err = c.db.ExecContext(ctx, query,
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
	)

	if err != nil {
		return nil, fmt.Errorf("failed to insert event: %w", err)
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
			resource_id, resource_name, metrics, metadata, timestamp, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
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
