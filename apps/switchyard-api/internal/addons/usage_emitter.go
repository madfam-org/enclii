package addons

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// addon.* lifecycle events into Waybill.
//
// WHAT THIS IS
// ============
// Waybill's event set has been deployment/build/volume/bandwidth/domain since
// it was written; there was no addon event of any kind, so the
// managed_db_addon_events ledger — which has recorded every addon transition
// since migration 014 — was read by nobody outside the timeline UI. This
// forwards the lifecycle transitions that bracket a database's existence to
// `POST /internal/events`, which is the only ingest Waybill exposes.
//
// WHAT THIS IS NOT
// ================
// It is not a meter, and it computes no money. No price, rate, tier or
// currency appears in this file; no MetricType is introduced in Waybill; the
// hourly aggregator has no case for these event types, so they aggregate to
// nothing. Metering a database needs an hourly sampler over CNPG volume usage
// which does not exist yet, and cannot exist until the addon pods are actually
// scraped. The events are the substrate that sampler will be checked against —
// "when did this addon exist, on which plan, at which size" — and nothing more.
//
// The numbers under `metrics` are DIMENSIONS, not readings: the declared
// storage/compute of the plan at the moment of the transition, normalised to
// the same units Waybill already uses for deployments (millicores, MB). They
// describe the shape of the thing, not its consumption.
//
// FAILURE POSTURE
// ===============
// Emission is best-effort and never blocks a lifecycle transition, matching
// the ledger write it hangs off: a billing pipeline outage must not stop a
// customer from deleting a database. Which is exactly why delivery is
// idempotent — see the idempotency key below.

const (
	// AddonUsageResourceType is the `resource_type` every addon event carries.
	// Waybill stores it verbatim, so it is a join key: pick one string and
	// never drift.
	AddonUsageResourceType = "database_addon"

	// addonUsageTimeout bounds a single delivery. Short on purpose: this call
	// sits on the lifecycle path, and a slow biller must not become a slow
	// provision.
	addonUsageTimeout = 5 * time.Second
)

// UsageEmitter forwards an addon lifecycle transition to the usage pipeline.
// An interface so AddonService can be constructed without one (the emitter is
// optional; a nil emitter is a no-op) and so tests can assert what would have
// been sent without an HTTP server.
type UsageEmitter interface {
	EmitAddonEvent(ctx context.Context, ev AddonUsageEvent) error
}

// AddonUsageEvent is one transition, ready to send.
type AddonUsageEvent struct {
	// IdempotencyKey names the TRANSITION, not the delivery attempt. For
	// ledger-derived events it is the id of the managed_db_addon_events row,
	// which is minted exactly once per transition and is therefore stable
	// across any number of retries, restarts and duplicate reconcile passes.
	IdempotencyKey string
	EventType      string
	Addon          *types.DatabaseAddon
	OccurredAt     time.Time
	// Extra metadata merged into the event's metadata map (string values
	// only, to match Waybill's schema).
	Extra map[string]string
}

// ledgerEventToUsageEvent maps the platform's existing ledger vocabulary onto
// the subset Waybill records.
//
// DELIBERATELY PARTIAL. The ledger carries a dozen event types — requested,
// provisioning-started, failed, binding created/deleted, credentials rotated,
// data-API enabled/disabled. Only the transitions that bracket a database's
// existence or change its declared size belong in a usage stream; forwarding
// the rest would fill the table with rows nothing will ever read and make the
// real signal harder to find. An unmapped type returns "" and is dropped.
//
// `addon.plan.changed` HAS NO PRODUCER TODAY: the constant exists in the
// ledger and no code path emits it, because there is no resize/scale API for
// addons yet. It is mapped here so that when that path is written the usage
// event follows automatically instead of being a second thing to remember.
func ledgerEventToUsageEvent(t db.ManagedDBAddonEventType) string {
	switch t {
	case db.EventAddonReady:
		return "addon.ready"
	case db.EventAddonPlanChanged:
		return "addon.plan.changed"
	case db.EventAddonDestroyed:
		return "addon.destroyed"
	default:
		return ""
	}
}

// usageEventPayload is the wire shape of Waybill's EventRequest. Written out
// here rather than imported: switchyard-api and waybill are separate Go
// modules, and a shared struct would couple their release cycles for four
// fields.
type usageEventPayload struct {
	EventType      string             `json:"event_type"`
	ProjectID      uuid.UUID          `json:"project_id"`
	ResourceType   string             `json:"resource_type"`
	ResourceID     uuid.UUID          `json:"resource_id"`
	ResourceName   string             `json:"resource_name,omitempty"`
	Metrics        map[string]float64 `json:"metrics"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
	IdempotencyKey string             `json:"idempotency_key,omitempty"`
	Timestamp      *time.Time         `json:"timestamp,omitempty"`
}

// buildUsageEventPayload is pure so the exact fields the data plan's metering
// section asks for — addon id, plan, storage class, compute class — can be
// asserted without a server.
func buildUsageEventPayload(ev AddonUsageEvent) (*usageEventPayload, error) {
	if ev.Addon == nil {
		return nil, fmt.Errorf("addon usage event has no addon")
	}
	if ev.EventType == "" {
		return nil, fmt.Errorf("addon usage event has no event type")
	}

	cfg := ev.Addon.Config
	ts := ev.OccurredAt
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	instances := cfg.Replicas
	if instances == 0 {
		instances = 1
	}

	metadata := map[string]string{
		// The addon id is already resource_id, but repeating it in metadata
		// makes a metrics query that groups by a metadata label work without
		// a join, and costs one short string per row.
		"addon_id":      ev.Addon.ID.String(),
		"addon_type":    string(ev.Addon.Type),
		"plan":          ev.Addon.Plan,
		"storage_class": storageClassFor(ev.Addon),
		"compute_class": computeClassFor(cfg),
		"ha":            fmt.Sprintf("%t", cfg.HAEnabled),
	}
	if ev.Addon.EnvironmentID != nil {
		metadata["environment_id"] = ev.Addon.EnvironmentID.String()
	}
	for k, v := range ev.Extra {
		metadata[k] = v
	}

	// NOTE: no namespace, resource name or host from the cluster is put in
	// metadata beyond the addon's own name — this payload crosses a service
	// boundary and node/topology identity does not belong in it.

	return &usageEventPayload{
		EventType:    ev.EventType,
		ProjectID:    ev.Addon.ProjectID,
		ResourceType: AddonUsageResourceType,
		ResourceID:   ev.Addon.ID,
		ResourceName: ev.Addon.Name,
		Metrics: map[string]float64{
			"storage_gb":     float64(cfg.StorageGB),
			"cpu_millicores": float64(milliCores(cfg.CPU)),
			"memory_mb":      float64(memoryMB(cfg.Memory)),
			"instances":      float64(instances),
		},
		Metadata:       metadata,
		IdempotencyKey: ev.IdempotencyKey,
		Timestamp:      &ts,
	}, nil
}

// storageClassFor reports the StorageClass the addon's volumes actually land
// on. Managed-DB PVCs are pinned to the Retain class so a torn-down cluster
// leaves recoverable volumes (addons/types.go RetainStorageClass); shared
// legacy addons have no PVC of their own at all.
func storageClassFor(addon *types.DatabaseAddon) string {
	if addon.Plan == "shared-discovered" {
		return "shared"
	}
	return RetainStorageClass
}

// computeClassFor is the declared CPU/memory pair, as a single label. The plan
// code alone is not enough: config carries operator overrides that the plan
// row does not.
func computeClassFor(cfg types.DatabaseAddonConfig) string {
	cpu := cfg.CPU
	if cpu == "" {
		cpu = DefaultCPU
	}
	mem := cfg.Memory
	if mem == "" {
		mem = DefaultMemory
	}
	return cpu + "/" + mem
}

// milliCores parses a Kubernetes CPU quantity ("100m", "1", "1500m") into
// millicores. UNIT NORMALISATION, NOT METERING: it converts a declared
// quantity into the unit Waybill's deployment events already use so the two
// are comparable. An unparseable value yields 0 rather than an error — a
// malformed config must not stop a delete event from being recorded.
func milliCores(cpu string) int64 {
	cpu = strings.TrimSpace(cpu)
	if cpu == "" {
		return 0
	}
	q, err := resource.ParseQuantity(cpu)
	if err != nil {
		return 0
	}
	return q.MilliValue()
}

// memoryMB parses a Kubernetes memory quantity ("256Mi", "1Gi") into whole
// mebibytes. Same posture as milliCores.
func memoryMB(mem string) int64 {
	mem = strings.TrimSpace(mem)
	if mem == "" {
		return 0
	}
	q, err := resource.ParseQuantity(mem)
	if err != nil {
		return 0
	}
	return q.Value() / (1024 * 1024)
}

// WaybillUsageEmitter posts events to Waybill's internal ingest.
type WaybillUsageEmitter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewWaybillUsageEmitter returns nil when no base URL is configured, so the
// caller's `if emitter != nil` is the single switch for the whole feature and
// a deployment without Waybill behaves exactly as it did before.
func NewWaybillUsageEmitter(baseURL, apiKey string) *WaybillUsageEmitter {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &WaybillUsageEmitter{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: addonUsageTimeout},
	}
}

// EmitAddonEvent delivers one event. Safe to call twice with the same
// IdempotencyKey: Waybill's partial unique index refuses the second write and
// answers 201 either way (migration 040).
func (e *WaybillUsageEmitter) EmitAddonEvent(ctx context.Context, ev AddonUsageEvent) error {
	payload, err := buildUsageEventPayload(ev)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal usage event: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, addonUsageTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/internal/events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build usage event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("X-API-Key", e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("post usage event: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The status only. A response body from an authenticated internal
		// endpoint can echo the request, and this error is logged.
		return fmt.Errorf("usage event rejected: HTTP %d", resp.StatusCode)
	}
	return nil
}
