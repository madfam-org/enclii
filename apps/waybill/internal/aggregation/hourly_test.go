package aggregation

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/waybill/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// newTestAggregator creates an HourlyAggregator with a nil db and collector
// suitable for testing pure calculation methods.
func newTestAggregator(t *testing.T) *HourlyAggregator {
	t.Helper()
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	return &HourlyAggregator{
		db:        nil,
		collector: nil,
		logger:    logger,
	}
}

func TestCalculateGBHours(t *testing.T) {
	agg := newTestAggregator(t)

	tests := []struct {
		name     string
		state    *deploymentState
		endTime  time.Time
		expected float64
	}{
		{
			name: "one_hour_1GB_1_replica",
			state: &deploymentState{
				startTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				replicas:  1,
				cpuMilli:  500,
				memoryMB:  1024, // 1 GB, dominates over 0.5 CPU
			},
			endTime:  time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
			expected: 1.0, // 1 GB * 1 replica * 1 hour
		},
		{
			name: "half_hour_2GB_2_replicas",
			state: &deploymentState{
				startTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				replicas:  2,
				cpuMilli:  1000,
				memoryMB:  2048, // 2 GB, dominates over 1.0 CPU
			},
			endTime:  time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC),
			expected: 2.0, // 2 GB * 2 replicas * 0.5 hours
		},
		{
			name: "cpu_dominant_one_hour",
			state: &deploymentState{
				startTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				replicas:  1,
				cpuMilli:  4000, // 4.0 GB equivalent
				memoryMB:  512,  // 0.5 GB
			},
			endTime:  time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
			expected: 4.0, // 4.0 GB * 1 replica * 1 hour
		},
		{
			name: "zero_duration",
			state: &deploymentState{
				startTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				replicas:  1,
				cpuMilli:  1000,
				memoryMB:  1024,
			},
			endTime:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: 0.0,
		},
		{
			name: "zero_replicas",
			state: &deploymentState{
				startTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				replicas:  0,
				cpuMilli:  1000,
				memoryMB:  1024,
			},
			endTime:  time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agg.calculateGBHours(tt.state, tt.endTime)
			assert.InDelta(t, tt.expected, got, 1e-9, "GB-hours mismatch")
		})
	}
}

func TestCalculateMetrics_DeploymentStartAndStop(t *testing.T) {
	agg := newTestAggregator(t)

	resourceID := uuid.New()
	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventDeploymentStarted,
			ResourceID: resourceID,
			Timestamp:  hourStart.Add(10 * time.Minute),
			Metrics: map[string]float64{
				"replicas":       2,
				"cpu_millicores": 1000,
				"memory_mb":      2048,
			},
		},
		{
			EventType:  events.EventDeploymentStopped,
			ResourceID: resourceID,
			Timestamp:  hourStart.Add(40 * time.Minute),
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// Duration: 30 minutes = 0.5 hours
	// Memory: 2048/1024 = 2.0 GB (dominates over 1.0 CPU)
	// GB-hours: 2.0 * 2 replicas * 0.5 hours = 2.0
	assert.InDelta(t, 2.0, metrics[events.MetricComputeGBHours], 1e-9)
}

func TestCalculateMetrics_DeploymentRunsThroughEntireHour(t *testing.T) {
	agg := newTestAggregator(t)

	resourceID := uuid.New()
	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	// Deployment started before the hour and runs through entire hour (no stop event)
	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventDeploymentStarted,
			ResourceID: resourceID,
			Timestamp:  hourStart, // Started at beginning of hour
			Metrics: map[string]float64{
				"replicas":       1,
				"cpu_millicores": 1000,
				"memory_mb":      1024,
			},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// Runs full hour: 1.0 GB * 1 replica * 1.0 hour = 1.0
	assert.InDelta(t, 1.0, metrics[events.MetricComputeGBHours], 1e-9)
}

func TestCalculateMetrics_DeploymentScaled(t *testing.T) {
	agg := newTestAggregator(t)

	resourceID := uuid.New()
	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventDeploymentStarted,
			ResourceID: resourceID,
			Timestamp:  hourStart,
			Metrics: map[string]float64{
				"replicas":       1,
				"cpu_millicores": 1000,
				"memory_mb":      1024,
			},
		},
		{
			EventType:  events.EventDeploymentScaled,
			ResourceID: resourceID,
			Timestamp:  hourStart.Add(30 * time.Minute), // Scale at 30 min mark
			Metrics: map[string]float64{
				"replicas":       3,
				"cpu_millicores": 1000,
				"memory_mb":      1024,
			},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// Phase 1: 1.0 GB * 1 replica * 0.5 hours = 0.5
	// Phase 2: 1.0 GB * 3 replicas * 0.5 hours = 1.5
	// Total: 2.0 GB-hours
	assert.InDelta(t, 2.0, metrics[events.MetricComputeGBHours], 1e-9)
}

func TestCalculateMetrics_BuildCompleted(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventBuildCompleted,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(15 * time.Minute),
			Metrics: map[string]float64{
				"duration_seconds": 300, // 5 minutes
			},
		},
		{
			EventType:  events.EventBuildCompleted,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(45 * time.Minute),
			Metrics: map[string]float64{
				"duration_seconds": 180, // 3 minutes
			},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// 300/60 + 180/60 = 5.0 + 3.0 = 8.0 minutes
	assert.InDelta(t, 8.0, metrics[events.MetricBuildMinutes], 1e-9)
}

func TestCalculateMetrics_StorageAndBandwidth(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventVolumeCreated,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(5 * time.Minute),
			Metrics: map[string]float64{
				"size_gb": 10.0,
			},
		},
		{
			EventType:  events.EventBandwidthUsage,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(20 * time.Minute),
			Metrics: map[string]float64{
				"egress_gb": 2.5,
			},
		},
		{
			EventType:  events.EventBandwidthUsage,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(50 * time.Minute),
			Metrics: map[string]float64{
				"egress_gb": 1.5,
			},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	assert.InDelta(t, 10.0, metrics[events.MetricStorageGBHours], 1e-9)
	assert.InDelta(t, 4.0, metrics[events.MetricBandwidthGB], 1e-9)
}

func TestCalculateMetrics_DomainAddedAndRemoved(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventDomainAdded,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(5 * time.Minute),
		},
		{
			EventType:  events.EventDomainAdded,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(10 * time.Minute),
		},
		{
			EventType:  events.EventDomainRemoved,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(30 * time.Minute),
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// 2 added - 1 removed = 1
	assert.InDelta(t, 1.0, metrics[events.MetricCustomDomains], 1e-9)
}

func TestCalculateMetrics_EmptyEventList(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	metrics := agg.calculateMetrics(nil, hourStart, hourEnd)

	assert.Empty(t, metrics, "empty event list should produce empty metrics")
}

func TestCalculateMetrics_StopWithoutStart(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

	// Stop event without a preceding start event should be ignored gracefully
	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventDeploymentStopped,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(30 * time.Minute),
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// No compute should be recorded since there was no start event
	assert.InDelta(t, 0.0, metrics[events.MetricComputeGBHours], 1e-9)
}

func TestRunForRange_EmptyRange(t *testing.T) {
	// RunForRange with start == end should be a no-op
	agg := newTestAggregator(t)

	ts := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	// This should not call Run at all because current.Before(end) is false
	// Since db is nil, if Run were called it would panic on DB access
	// An empty range (start == end) simply returns nil without DB calls
	err := agg.RunForRange(t.Context(), ts, ts)
	assert.NoError(t, err, "empty range should succeed without DB calls")
}

// --- Runner, database and content metric types (SKU roadmap Wave 1) ---------

func TestCalculateMetrics_RunnerSlotMinutes(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := hourStart.Add(time.Hour)

	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventBuildCompleted,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(10 * time.Minute),
			Metrics: map[string]float64{
				"duration_seconds": 300, // 5 min of building
				"slot_seconds":     600, // 10 min of slot held
			},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// The two are independent readings of the same job and must not collapse
	// into each other: the slot was held twice as long as the build ran.
	assert.InDelta(t, 5.0, metrics[events.MetricBuildMinutes], 1e-9)
	assert.InDelta(t, 10.0, metrics[events.MetricRunnerSlotMinutes], 1e-9)
}

// An emitter that cannot observe slot time omits the field. The build minutes
// must still land, and the slot metric must stay at nothing rather than
// borrowing duration_seconds.
func TestCalculateMetrics_SlotSecondsAbsentDoesNotFallBackToDuration(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := hourStart.Add(time.Hour)

	eventList := []*events.UsageEvent{
		{
			EventType:  events.EventBuildCompleted,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(10 * time.Minute),
			Metrics:    map[string]float64{"duration_seconds": 300},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	assert.InDelta(t, 5.0, metrics[events.MetricBuildMinutes], 1e-9)
	assert.Zero(t, metrics[events.MetricRunnerSlotMinutes])
}

func TestCacheGBHours(t *testing.T) {
	tests := []struct {
		name     string
		metrics  map[string]float64
		expected float64
	}{
		{
			name:     "no_cache_counters",
			metrics:  map[string]float64{"duration_seconds": 3600},
			expected: 0,
		},
		{
			name: "one_gib_held_for_one_hour",
			metrics: map[string]float64{
				"duration_seconds":  3600,
				"cache_bytes_read":  1024 * 1024 * 1024,
				"cache_bytes_write": 0,
			},
			expected: 1.0,
		},
		{
			// max, not sum: the two counters overlap on the same bytes.
			name: "read_and_written_take_the_larger",
			metrics: map[string]float64{
				"duration_seconds":    3600,
				"cache_bytes_read":    2 * 1024 * 1024 * 1024,
				"cache_bytes_written": 3 * 1024 * 1024 * 1024,
			},
			expected: 3.0,
		},
		{
			name: "half_hour_halves_the_residency",
			metrics: map[string]float64{
				"duration_seconds": 1800,
				"cache_bytes_read": 4 * 1024 * 1024 * 1024,
			},
			expected: 2.0,
		},
		{
			// Cache bytes with no runtime is not a residency of anything.
			name: "zero_duration_yields_nothing",
			metrics: map[string]float64{
				"cache_bytes_read": 8 * 1024 * 1024 * 1024,
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.expected, cacheGBHours(tt.metrics), 1e-9)
		})
	}
}

func TestCalculateMetrics_AddonReadyThenDestroyed(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := hourStart.Add(time.Hour)
	addonID := uuid.New()

	eventList := []*events.UsageEvent{
		{
			EventType:    events.EventAddonReady,
			ResourceType: "database_addon",
			ResourceID:   addonID,
			Timestamp:    hourStart,
			Metrics: map[string]float64{
				"storage_gb":     10,
				"cpu_millicores": 500,
				"memory_mb":      2048, // 2 GB, dominates 0.5 CPU-GB
				"instances":      1,
			},
		},
		{
			EventType:    events.EventAddonDestroyed,
			ResourceType: "database_addon",
			ResourceID:   addonID,
			Timestamp:    hourStart.Add(30 * time.Minute),
			Metrics: map[string]float64{
				"storage_gb":     10,
				"cpu_millicores": 500,
				"memory_mb":      2048,
				"instances":      1,
			},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// Declared 10 GB for half an hour, declared 2 GB-equivalent for half an
	// hour. Declared, not measured — see the case comments in hourly.go.
	assert.InDelta(t, 5.0, metrics[events.MetricDBStorageGBHours], 1e-9)
	assert.InDelta(t, 1.0, metrics[events.MetricDBComputeGBHours], 1e-9)
}

// An addon still alive at the end of the hour is closed at the hour boundary,
// exactly as a still-running deployment is.
func TestCalculateMetrics_AddonStillAliveAtEndOfHour(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := hourStart.Add(time.Hour)

	eventList := []*events.UsageEvent{
		{
			EventType:    events.EventAddonReady,
			ResourceType: "database_addon",
			ResourceID:   uuid.New(),
			Timestamp:    hourStart,
			Metrics: map[string]float64{
				"storage_gb":     20,
				"cpu_millicores": 1000,
				"memory_mb":      1024,
				"instances":      3, // HA: three pods, one volume's worth of data
			},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// Storage is per cluster (20 GB-hours), compute is per instance
	// (1 GB-equivalent × 3 × 1h).
	assert.InDelta(t, 20.0, metrics[events.MetricDBStorageGBHours], 1e-9)
	assert.InDelta(t, 3.0, metrics[events.MetricDBComputeGBHours], 1e-9)
}

func TestCalculateMetrics_AddonPlanChangedClosesAndReopens(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := hourStart.Add(time.Hour)
	addonID := uuid.New()

	eventList := []*events.UsageEvent{
		{
			EventType:    events.EventAddonReady,
			ResourceType: "database_addon",
			ResourceID:   addonID,
			Timestamp:    hourStart,
			Metrics:      map[string]float64{"storage_gb": 10, "memory_mb": 1024, "instances": 1},
		},
		{
			EventType:    events.EventAddonPlanChanged,
			ResourceType: "database_addon",
			ResourceID:   addonID,
			Timestamp:    hourStart.Add(30 * time.Minute),
			Metrics:      map[string]float64{"storage_gb": 30, "memory_mb": 1024, "instances": 1},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	// 10 GB for the first half hour, 30 GB for the second: 5 + 15.
	assert.InDelta(t, 20.0, metrics[events.MetricDBStorageGBHours], 1e-9)
}

// Egress splits on resource_type so a database's bytes never become
// indistinguishable from a service's.
func TestCalculateMetrics_BandwidthSplitsByResourceType(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := hourStart.Add(time.Hour)

	eventList := []*events.UsageEvent{
		{
			EventType:    events.EventBandwidthUsage,
			ResourceType: "service",
			ResourceID:   uuid.New(),
			Timestamp:    hourStart.Add(time.Minute),
			Metrics:      map[string]float64{"egress_gb": 7},
		},
		{
			EventType:    events.EventBandwidthUsage,
			ResourceType: "database_addon",
			ResourceID:   uuid.New(),
			Timestamp:    hourStart.Add(2 * time.Minute),
			Metrics:      map[string]float64{"egress_gb": 4},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	assert.InDelta(t, 7.0, metrics[events.MetricBandwidthGB], 1e-9)
	assert.InDelta(t, 4.0, metrics[events.MetricDBEgressGB], 1e-9)
}

// The three metric types with no producer must aggregate to NOTHING.
//
// This is the whole point of the test: a declared unit with no source is
// honest, and a declared unit that quietly picks up a plausible number from a
// lookalike field is a billing defect that nobody goes looking for. The event
// list below deliberately carries fields whose names invite exactly that
// mistake — `requests`, `socket_hours`, `sites`.
func TestCalculateMetrics_TypesWithoutProducersAggregateToNothing(t *testing.T) {
	agg := newTestAggregator(t)

	hourStart := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	hourEnd := hourStart.Add(time.Hour)

	eventList := []*events.UsageEvent{
		{
			EventType:    events.EventAddonReady,
			ResourceType: "database_addon",
			ResourceID:   uuid.New(),
			Timestamp:    hourStart,
			Metrics: map[string]float64{
				"storage_gb":   1,
				"instances":    1,
				"requests":     1000,
				"socket_hours": 24,
				"sites":        3,
			},
		},
		{
			EventType:  events.EventBuildCompleted,
			ResourceID: uuid.New(),
			Timestamp:  hourStart.Add(time.Minute),
			Metrics: map[string]float64{
				"duration_seconds": 60,
				"requests":         5,
			},
		},
	}

	metrics := agg.calculateMetrics(eventList, hourStart, hourEnd)

	for _, mt := range []events.MetricType{
		events.MetricDataAPIRequests,
		events.MetricRealtimeSocketHours,
		events.MetricCMSSites,
	} {
		assert.Zerof(t, metrics[mt], "%s has no producer and must aggregate to nothing", mt)
	}
}
