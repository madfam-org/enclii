package events

import (
	"time"

	"github.com/google/uuid"
)

// EventType represents the type of usage event
type EventType string

const (
	// Deployment events
	EventDeploymentStarted EventType = "deployment.started"
	EventDeploymentStopped EventType = "deployment.stopped"
	EventDeploymentScaled  EventType = "deployment.scaled"

	// Build events
	EventBuildStarted   EventType = "build.started"
	EventBuildCompleted EventType = "build.completed"
	EventBuildFailed    EventType = "build.failed"

	// Storage events
	EventVolumeCreated EventType = "volume.created"
	EventVolumeDeleted EventType = "volume.deleted"
	EventVolumeResized EventType = "volume.resized"

	// Network events
	EventBandwidthUsage EventType = "bandwidth.usage"

	// Domain events
	EventDomainAdded   EventType = "domain.added"
	EventDomainRemoved EventType = "domain.removed"

	// Managed-database addon events.
	//
	// THE STRINGS ARE THE VOCABULARY THE PLATFORM ALREADY SPEAKS. They are
	// copied verbatim from the append-only lifecycle ledger in switchyard-api
	// (internal/db/managed_db_addon_event_repository.go), which has written
	// these exact values on every addon transition since migration 014. A
	// second, prettier vocabulary here — `addon.created`, `addon.deleted` —
	// would mean two names for one transition and a translation table that
	// eventually disagrees with itself.
	//
	// These are RECORDS OF TRANSITIONS, not meter readings. Nothing in this
	// package aggregates them, no MetricType is defined for a database, and
	// no price is attached anywhere: metering DB usage needs an hourly sampler
	// over CNPG volume usage that does not exist yet. Emitting the events
	// first is what makes that sampler possible to write and to check.
	EventAddonReady       EventType = "addon.ready"
	EventAddonPlanChanged EventType = "addon.plan.changed"
	EventAddonDestroyed   EventType = "addon.destroyed"

	// Emitted by the addon reconciler when CloudNativePG reports a newly
	// completed backup. Not part of the ledger vocabulary (the ledger records
	// operator- and API-initiated transitions; this one is observed from
	// cluster status), so it is named to sit alongside it.
	EventAddonBackupCompleted EventType = "addon.backup.completed"
)

// MetricType represents billable metric types
type MetricType string

const (
	MetricComputeGBHours MetricType = "compute_gb_hours"
	MetricBuildMinutes   MetricType = "build_minutes"
	MetricStorageGBHours MetricType = "storage_gb_hours"
	MetricBandwidthGB    MetricType = "bandwidth_gb"
	MetricCustomDomains  MetricType = "custom_domains"
)

// Metric types for the runner, database and content surfaces.
//
// A METRIC TYPE IS A UNIT, NOT A PRICE. Nothing in this package, and nothing
// in internal/metering, attaches a rate, a tier or a currency to any of the
// names below. The metering calculator's rate table is unchanged and none of
// these appears in it, so each one costs exactly zero until somebody writes a
// rate and says so in a reviewable diff — which is the point of naming the
// units before anyone can bill them. metering.TestNewMetricTypesAreNotPriced
// pins that.
//
// THREE OF THEM HAVE NO PRODUCER AND NO AGGREGATION CASE. They are declared
// here so the vocabulary is settled in one place instead of being invented
// twice; see the aggregation package for exactly which ones aggregate and
// which deliberately aggregate to nothing.
const (
	// MetricRunnerSlotMinutes is wall-clock minutes during which a CI runner
	// slot was held by a job. Derived from the `slot_seconds` metric on a
	// build.completed event. Distinct from MetricBuildMinutes, which measures
	// the build itself: a job that holds a slot for 10 minutes and spends 6 of
	// them building consumes 10 slot-minutes and 6 build-minutes, and the pool
	// capacity that has to be paid for is the former.
	MetricRunnerSlotMinutes MetricType = "runner_slot_minutes"

	// MetricCacheGBHours is build-cache residency. See the aggregation
	// package for the exact unit convention used to derive it — bytes are not
	// GB-hours on their own and the conversion is a stated convention, not a
	// measurement.
	MetricCacheGBHours MetricType = "cache_gb_hours"

	// MetricDBStorageGBHours and MetricDBComputeGBHours are managed-database
	// footprint over time. DERIVED FROM DECLARED SIZE, NOT FROM A READING:
	// the addon lifecycle events carry the plan's declared storage and
	// compute at the moment of a transition, so these say "a database of this
	// declared shape existed for this long" and never "this much disk was
	// actually occupied". A measured reading needs an hourly sampler over
	// CNPG volume usage, which does not exist.
	MetricDBStorageGBHours MetricType = "db_storage_gb_hours"
	MetricDBComputeGBHours MetricType = "db_compute_gb_hours"

	// MetricDBEgressGB is egress attributed to a managed database rather than
	// to the project's general bandwidth. Kept separate from
	// MetricBandwidthGB on purpose: folding a database's egress into the
	// project total makes the two impossible to tell apart afterwards.
	MetricDBEgressGB MetricType = "db_egress_gb"

	// NO PRODUCER TODAY. Named so the three surfaces the roadmap describes
	// have one agreed unit each before anything starts emitting, and so a
	// second, differently-spelled name cannot appear alongside them later.
	// None of these has an aggregation case, and
	// aggregation.TestCalculateMetrics_TypesWithoutProducersAggregateToNothing
	// asserts they stay at nothing rather than quietly acquiring a wrong
	// number from a lookalike field.
	MetricDataAPIRequests     MetricType = "data_api_requests"
	MetricRealtimeSocketHours MetricType = "realtime_socket_hours"
	MetricCMSSites            MetricType = "cms_sites"
)

// UsageEvent represents a single usage event
type UsageEvent struct {
	ID           uuid.UUID          `json:"id" db:"id"`
	ProjectID    uuid.UUID          `json:"project_id" db:"project_id"`
	TeamID       *uuid.UUID         `json:"team_id,omitempty" db:"team_id"`
	EventType    EventType          `json:"event_type" db:"event_type"`
	ResourceType string             `json:"resource_type" db:"resource_type"`
	ResourceID   uuid.UUID          `json:"resource_id" db:"resource_id"`
	ResourceName string             `json:"resource_name,omitempty" db:"resource_name"`
	Metrics      map[string]float64 `json:"metrics" db:"metrics"`
	Metadata     map[string]string  `json:"metadata,omitempty" db:"metadata"`
	// IdempotencyKey names the real-world transition this event records.
	// Empty means the emitter opted out of dedup. See migration 040.
	//
	// Write path only today: the read helpers below (GetUnprocessedEvents,
	// GetEventsByProject) do not select the column, because nothing downstream
	// reads it and widening their SELECT would change every caller's row
	// shape for no gain. It is therefore zero on any event loaded from the
	// database — do not branch on it after a read.
	IdempotencyKey string     `json:"idempotency_key,omitempty" db:"idempotency_key"`
	Timestamp      time.Time  `json:"timestamp" db:"timestamp"`
	ProcessedAt    *time.Time `json:"processed_at,omitempty" db:"processed_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// EventRequest is the request to record an event
type EventRequest struct {
	EventType    EventType          `json:"event_type" binding:"required"`
	ProjectID    uuid.UUID          `json:"project_id" binding:"required"`
	TeamID       *uuid.UUID         `json:"team_id,omitempty"`
	ResourceType string             `json:"resource_type" binding:"required"`
	ResourceID   uuid.UUID          `json:"resource_id" binding:"required"`
	ResourceName string             `json:"resource_name,omitempty"`
	Metrics      map[string]float64 `json:"metrics" binding:"required"`
	Metadata     map[string]string  `json:"metadata,omitempty"`
	// IdempotencyKey, when set, makes this request safe to retry: a second
	// POST carrying the same key records nothing and reports the event that
	// already exists. Optional — omitting it preserves the historical
	// insert-always behaviour for every emitter that predates it.
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
	Timestamp      *time.Time `json:"timestamp,omitempty"`
}

// HourlyUsage represents aggregated hourly usage
type HourlyUsage struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	ProjectID  uuid.UUID  `json:"project_id" db:"project_id"`
	MetricType MetricType `json:"metric_type" db:"metric_type"`
	Value      float64    `json:"value" db:"value"`
	Hour       time.Time  `json:"hour" db:"hour"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

// DailyUsage represents aggregated daily usage
type DailyUsage struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	ProjectID  uuid.UUID  `json:"project_id" db:"project_id"`
	MetricType MetricType `json:"metric_type" db:"metric_type"`
	Value      float64    `json:"value" db:"value"`
	Date       time.Time  `json:"date" db:"date"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}

// UsageSummary represents a usage summary for a project
type UsageSummary struct {
	ProjectID        uuid.UUID              `json:"project_id"`
	PeriodStart      time.Time              `json:"period_start"`
	PeriodEnd        time.Time              `json:"period_end"`
	Metrics          map[MetricType]float64 `json:"metrics"`
	Costs            map[MetricType]float64 `json:"costs"`
	TotalCost        float64                `json:"total_cost"`
	EstimatedMonthly float64                `json:"estimated_monthly"`
}

// DeploymentMetrics captures compute resource metrics
type DeploymentMetrics struct {
	Replicas      int `json:"replicas"`
	CPUMillicores int `json:"cpu_millicores"`
	MemoryMB      int `json:"memory_mb"`
	// Calculated
	GBEquivalent float64 `json:"gb_equivalent"`
}

// BuildMetrics captures build resource metrics
type BuildMetrics struct {
	DurationSeconds float64 `json:"duration_seconds"`
	ImageSizeMB     float64 `json:"image_size_mb"`
	// Calculated
	Minutes float64 `json:"minutes"`
}

// StorageMetrics captures storage resource metrics
type StorageMetrics struct {
	SizeGB float64 `json:"size_gb"`
}

// BandwidthMetrics captures network resource metrics
type BandwidthMetrics struct {
	EgressGB  float64 `json:"egress_gb"`
	IngressGB float64 `json:"ingress_gb"`
}

// CalculateGBEquivalent converts CPU + Memory to GB-equivalent
// Using a simplified model: 1 GB = 1 GB RAM or 1 vCPU
func (m *DeploymentMetrics) CalculateGBEquivalent() float64 {
	// Memory component (MB to GB)
	memoryGB := float64(m.MemoryMB) / 1024.0

	// CPU component (millicores to cores, 1 core = 1 GB equivalent)
	cpuGB := float64(m.CPUMillicores) / 1000.0

	// Take the larger of the two (similar to Railway's pricing)
	if memoryGB > cpuGB {
		return memoryGB * float64(m.Replicas)
	}
	return cpuGB * float64(m.Replicas)
}
