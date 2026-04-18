package aggregation

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/events"
)

// tracer — aggregation package spans show up as "aggregation.*" in Tempo.
var tracer = otel.Tracer("waybill/aggregation")

// HourlyAggregator handles hourly usage aggregation
type HourlyAggregator struct {
	db        *sql.DB
	collector *events.Collector
	logger    *zap.Logger
}

// NewHourlyAggregator creates a new hourly aggregator
func NewHourlyAggregator(db *sql.DB, collector *events.Collector, logger *zap.Logger) *HourlyAggregator {
	return &HourlyAggregator{
		db:        db,
		collector: collector,
		logger:    logger,
	}
}

// Run performs hourly aggregation for a specific hour.
//
// Wrapped in a single root span "aggregation.hourly" — the DB queries via
// otelsql nest as child spans, so Tempo shows which exact SELECT/INSERT
// dominated the 1-hour cycle.
func (a *HourlyAggregator) Run(ctx context.Context, hour time.Time) error {
	// Normalize to start of hour
	hour = hour.Truncate(time.Hour)
	nextHour := hour.Add(time.Hour)

	ctx, span := tracer.Start(ctx, "aggregation.hourly")
	defer span.End()
	span.SetAttributes(
		attribute.String("aggregation.hour", hour.Format(time.RFC3339)),
	)

	a.logger.Info("starting hourly aggregation",
		zap.Time("hour", hour),
	)

	// Get all projects with events in this hour
	projectsQuery := `
		SELECT DISTINCT project_id
		FROM usage_events
		WHERE timestamp >= $1 AND timestamp < $2
	`

	rows, err := a.db.QueryContext(ctx, projectsQuery, hour, nextHour)
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var projectIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("failed to scan project ID: %w", err)
		}
		projectIDs = append(projectIDs, id)
	}

	// Aggregate each project
	for _, projectID := range projectIDs {
		if err := a.aggregateProject(ctx, projectID, hour, nextHour); err != nil {
			a.logger.Error("failed to aggregate project",
				zap.String("project_id", projectID.String()),
				zap.Error(err),
			)
			continue
		}
	}

	a.logger.Info("hourly aggregation complete",
		zap.Time("hour", hour),
		zap.Int("projects", len(projectIDs)),
	)
	span.SetAttributes(attribute.Int("aggregation.projects_processed", len(projectIDs)))

	return nil
}

func (a *HourlyAggregator) aggregateProject(ctx context.Context, projectID uuid.UUID, start, end time.Time) error {
	ctx, span := tracer.Start(ctx, "aggregation.project")
	defer span.End()
	span.SetAttributes(attribute.String("project.id", projectID.String()))

	eventList, err := a.collector.GetEventsByProject(ctx, projectID, start, end)
	if err != nil {
		span.SetStatus(codes.Error, "get events")
		span.RecordError(err)
		return err
	}
	span.SetAttributes(attribute.Int("events.count", len(eventList)))

	// Calculate metrics
	metrics := a.calculateMetrics(eventList, start, end)

	// Insert hourly usage records
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after successful commit

	insertQuery := `
		INSERT INTO hourly_usage (id, project_id, metric_type, value, hour, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (project_id, metric_type, hour)
		DO UPDATE SET value = EXCLUDED.value
	`

	for metricType, value := range metrics {
		if value == 0 {
			continue
		}

		_, err := tx.ExecContext(ctx, insertQuery,
			uuid.New(),
			projectID,
			metricType,
			value,
			start,
			time.Now(),
		)
		if err != nil {
			return fmt.Errorf("failed to insert hourly usage: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (a *HourlyAggregator) calculateMetrics(eventList []*events.UsageEvent, start, end time.Time) map[events.MetricType]float64 {
	metrics := make(map[events.MetricType]float64)

	// Track active deployments for compute calculation
	activeDeployments := make(map[uuid.UUID]*deploymentState)

	for _, event := range eventList {
		switch event.EventType {
		case events.EventDeploymentStarted:
			activeDeployments[event.ResourceID] = &deploymentState{
				startTime: event.Timestamp,
				replicas:  int(event.Metrics["replicas"]),
				cpuMilli:  int(event.Metrics["cpu_millicores"]),
				memoryMB:  int(event.Metrics["memory_mb"]),
			}

		case events.EventDeploymentStopped:
			if state, ok := activeDeployments[event.ResourceID]; ok {
				gbHours := a.calculateGBHours(state, event.Timestamp)
				metrics[events.MetricComputeGBHours] += gbHours
				delete(activeDeployments, event.ResourceID)
			}

		case events.EventDeploymentScaled:
			// Close current state and open new one
			if state, ok := activeDeployments[event.ResourceID]; ok {
				gbHours := a.calculateGBHours(state, event.Timestamp)
				metrics[events.MetricComputeGBHours] += gbHours
			}
			activeDeployments[event.ResourceID] = &deploymentState{
				startTime: event.Timestamp,
				replicas:  int(event.Metrics["replicas"]),
				cpuMilli:  int(event.Metrics["cpu_millicores"]),
				memoryMB:  int(event.Metrics["memory_mb"]),
			}

		case events.EventBuildCompleted:
			minutes := event.Metrics["duration_seconds"] / 60.0
			metrics[events.MetricBuildMinutes] += minutes

		case events.EventVolumeCreated, events.EventVolumeResized:
			// Storage is tracked at the end of the hour
			// This is simplified - real implementation would track active storage
			sizeGB := event.Metrics["size_gb"]
			metrics[events.MetricStorageGBHours] += sizeGB // 1 hour

		case events.EventBandwidthUsage:
			metrics[events.MetricBandwidthGB] += event.Metrics["egress_gb"]

		case events.EventDomainAdded:
			metrics[events.MetricCustomDomains]++
		case events.EventDomainRemoved:
			metrics[events.MetricCustomDomains]--
		}
	}

	// Close any still-active deployments at end of hour
	for _, state := range activeDeployments {
		gbHours := a.calculateGBHours(state, end)
		metrics[events.MetricComputeGBHours] += gbHours
	}

	return metrics
}

type deploymentState struct {
	startTime time.Time
	replicas  int
	cpuMilli  int
	memoryMB  int
}

func (a *HourlyAggregator) calculateGBHours(state *deploymentState, endTime time.Time) float64 {
	duration := endTime.Sub(state.startTime).Hours()

	// Calculate GB equivalent
	memoryGB := float64(state.memoryMB) / 1024.0
	cpuGB := float64(state.cpuMilli) / 1000.0

	gbEquivalent := memoryGB
	if cpuGB > memoryGB {
		gbEquivalent = cpuGB
	}

	return gbEquivalent * float64(state.replicas) * duration
}

// RunForRange runs aggregation for a range of hours (backfill)
func (a *HourlyAggregator) RunForRange(ctx context.Context, start, end time.Time) error {
	current := start.Truncate(time.Hour)
	end = end.Truncate(time.Hour)

	for current.Before(end) {
		if err := a.Run(ctx, current); err != nil {
			return fmt.Errorf("failed at hour %v: %w", current, err)
		}
		current = current.Add(time.Hour)
	}

	return nil
}
