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

	// Track managed-database addons the same way deployments are tracked. See
	// the addon cases below for what these numbers do and do not mean.
	activeAddons := make(map[uuid.UUID]*addonState)

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

			// Slot-minutes: how long a runner slot was HELD, which is the
			// quantity pool capacity is sized against. duration_seconds is
			// how long the build RAN. They differ by job setup, checkout,
			// upload and teardown, so one cannot be derived from the other
			// and both are carried. An emitter that does not know its slot
			// time omits the field and contributes nothing here rather than
			// having duration_seconds substituted for it.
			metrics[events.MetricRunnerSlotMinutes] += event.Metrics["slot_seconds"] / 60.0

			metrics[events.MetricCacheGBHours] += cacheGBHours(event.Metrics)

		case events.EventVolumeCreated, events.EventVolumeResized:
			// Storage is tracked at the end of the hour
			// This is simplified - real implementation would track active storage
			sizeGB := event.Metrics["size_gb"]
			metrics[events.MetricStorageGBHours] += sizeGB // 1 hour

		case events.EventBandwidthUsage:
			// One event type, two buckets, chosen by what the egress came
			// out of. A managed database's egress is a different product
			// line from a service's, and once the two are summed together
			// they can never be separated again — so the split happens here,
			// at the only point where the resource type is still known.
			//
			// `database_addon` is the resource_type string switchyard-api
			// stamps on every addon event (addons.AddonUsageResourceType).
			// Duplicated as a literal rather than imported: switchyard-api
			// and waybill are separate Go modules and the string is the
			// contract between them.
			//
			// NO PRODUCER TODAY. Nothing emits a bandwidth event for an
			// addon; the DB egress sampler does not exist. The case is here
			// so that when it is written its events land in their own bucket
			// instead of silently inflating bandwidth_gb.
			if event.ResourceType == addonResourceType {
				metrics[events.MetricDBEgressGB] += event.Metrics["egress_gb"]
			} else {
				metrics[events.MetricBandwidthGB] += event.Metrics["egress_gb"]
			}

		// Managed-database addon lifecycle (emitted by switchyard-api since
		// the addon usage emitter landed). These carry DECLARED DIMENSIONS —
		// the plan's storage_gb / cpu_millicores / memory_mb / instances at
		// the moment of the transition — not readings, so what comes out is
		// "a database of this declared shape existed for this long".
		//
		// TWO THINGS THIS IS NOT, both worth stating before anyone bills on
		// it:
		//
		//  1. It is not measured occupancy. A 100 GB volume holding 3 GB of
		//     rows reports 100. Measured occupancy needs an hourly sampler
		//     over CNPG volume usage, which does not exist yet.
		//
		//  2. It only sees the part of the hour it has an event for. An addon
		//     created last month and still running emits nothing this hour, so
		//     it contributes nothing this hour. That undercounts, and it is
		//     the same gap the deployment cases above already have — the
		//     aggregator reads one hour of events, not a state snapshot.
		//     Closing it means a periodic "still exists" event or a snapshot
		//     read, and is deliberately not smuggled in here.
		case events.EventAddonReady:
			activeAddons[event.ResourceID] = newAddonState(event)

		case events.EventAddonPlanChanged:
			// Close the old shape, open the new one — same shape-change
			// handling as EventDeploymentScaled.
			if state, ok := activeAddons[event.ResourceID]; ok {
				addAddonUsage(metrics, state, event.Timestamp)
			}
			activeAddons[event.ResourceID] = newAddonState(event)

		case events.EventAddonDestroyed:
			if state, ok := activeAddons[event.ResourceID]; ok {
				addAddonUsage(metrics, state, event.Timestamp)
				delete(activeAddons, event.ResourceID)
			}

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

	// Same for addons still open at the end of the hour.
	for _, state := range activeAddons {
		addAddonUsage(metrics, state, end)
	}

	// DELIBERATELY ABSENT: data_api_requests, realtime_socket_hours and
	// cms_sites. Each is a declared MetricType with no event and no metric
	// field anywhere in this estate, so there is nothing to key a case on.
	// Writing one anyway would mean inventing a source — counting addon
	// events as "requests", say — and a plausible wrong number is worse than
	// an obviously missing one, because only the missing one gets fixed.
	// TestCalculateMetrics_TypesWithoutProducersAggregateToNothing holds this.

	return metrics
}

// addonResourceType is the `resource_type` switchyard-api stamps on every
// managed-database addon event (addons.AddonUsageResourceType). A literal, not
// an import: the two services are separate Go modules and this string is the
// wire contract between them.
const addonResourceType = "database_addon"

// bytesPerGB is the binary gigabyte (GiB). Stated because "GB" is ambiguous
// and the two conventions differ by 7%, which is a real number on an invoice.
// Binary is chosen to match the MB convention already used on the wire by the
// addon usage emitter (memoryMB = bytes / 1024 / 1024).
const bytesPerGB = 1024 * 1024 * 1024

// cacheGBHours converts a completed build's cache byte counters into
// GB-hours.
//
// THE UNIT CONVENTION, STATED IN FULL, because bytes moved are not GB-hours
// and no arithmetic can make them so on its own:
//
//	cache_gb_hours = max(cache_bytes_read, cache_bytes_written) / GiB
//	                 × duration_seconds / 3600
//
// Read as: the build's cache FOOTPRINT, held for as long as the build ran.
//
//   - max, not sum. A build that restores 2 GiB and saves 2.1 GiB touched a
//     ~2.1 GiB cache, not 4.1 GiB; the two counters overlap almost entirely,
//     and summing them double-counts the same bytes.
//   - This is an UPPER BOUND on footprint, not a measured volume-hour. The
//     real quantity is the size of the cache volume over the time it was
//     mounted; nothing in the estate measures that today.
//   - duration_seconds is the build's own runtime, so a build that reads a
//     large cache and finishes in seconds costs almost nothing here, which is
//     the intended behaviour: the cache is charged for being held, not for
//     being big once.
//
// Both counters absent — the case for every emitter that exists today —
// yields 0, and a zero metric is dropped before it is written.
func cacheGBHours(m map[string]float64) float64 {
	bytes := m["cache_bytes_read"]
	if w := m["cache_bytes_written"]; w > bytes {
		bytes = w
	}
	if bytes <= 0 {
		return 0
	}
	hours := m["duration_seconds"] / 3600.0
	if hours <= 0 {
		return 0
	}
	return (bytes / float64(bytesPerGB)) * hours
}

// addonState is one managed-database addon's DECLARED shape, opened at a
// lifecycle transition. Mirrors deploymentState.
type addonState struct {
	startTime time.Time
	storageGB float64
	cpuMilli  float64
	memoryMB  float64
	instances float64
}

// newAddonState reads the declared dimensions the addon usage emitter puts on
// every addon event. A missing `instances` means one instance — the emitter
// normalises 0 to 1, and this keeps a hand-written or older event from
// erasing the addon's compute entirely.
func newAddonState(event *events.UsageEvent) *addonState {
	instances := event.Metrics["instances"]
	if instances <= 0 {
		instances = 1
	}
	return &addonState{
		startTime: event.Timestamp,
		storageGB: event.Metrics["storage_gb"],
		cpuMilli:  event.Metrics["cpu_millicores"],
		memoryMB:  event.Metrics["memory_mb"],
		instances: instances,
	}
}

// addAddonUsage credits declared-size × elapsed-hours to the two addon
// metrics.
//
// Storage is per cluster: a replicated database's replicas hold copies of the
// same volume, so multiplying storage by instance count would bill a customer
// for MADFAM's redundancy decision. Compute is per instance, because every
// replica is a running pod with its own CPU and memory reservation. The
// GB-equivalent for compute follows the same max(memory, cpu) model
// DeploymentMetrics.CalculateGBEquivalent already uses, so a database-hour and
// a service-hour mean the same thing.
func addAddonUsage(metrics map[events.MetricType]float64, state *addonState, endTime time.Time) {
	hours := endTime.Sub(state.startTime).Hours()
	if hours <= 0 {
		return
	}
	metrics[events.MetricDBStorageGBHours] += state.storageGB * hours

	memoryGB := state.memoryMB / 1024.0
	cpuGB := state.cpuMilli / 1000.0
	gbEquivalent := memoryGB
	if cpuGB > memoryGB {
		gbEquivalent = cpuGB
	}
	metrics[events.MetricDBComputeGBHours] += gbEquivalent * state.instances * hours
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
