package addons

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func usageAddon() *types.DatabaseAddon {
	envID := uuid.New()
	return &types.DatabaseAddon{
		ID:            uuid.New(),
		ProjectID:     uuid.New(),
		EnvironmentID: &envID,
		Type:          types.DatabaseAddonTypePostgres,
		Name:          "map-db",
		Plan:          "standard-1",
		Config: types.DatabaseAddonConfig{
			StorageGB: 20,
			CPU:       "500m",
			Memory:    "1Gi",
			Replicas:  1,
		},
	}
}

// The data plan's metering section asks for addon id, plan and storage/compute
// class. If any of those stops being emitted, the sampler that will read these
// events cannot attribute what it samples.
func TestPayloadCarriesTheFieldsMeteringWillNeed(t *testing.T) {
	addon := usageAddon()
	p, err := buildUsageEventPayload(AddonUsageEvent{
		IdempotencyKey: "ledger-1",
		EventType:      "addon.ready",
		Addon:          addon,
		OccurredAt:     time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("buildUsageEventPayload: %v", err)
	}

	if p.ResourceType != AddonUsageResourceType {
		t.Errorf("resource_type = %q, want %q", p.ResourceType, AddonUsageResourceType)
	}
	if p.ResourceID != addon.ID || p.ProjectID != addon.ProjectID {
		t.Error("payload does not identify the addon and its project")
	}
	if p.IdempotencyKey != "ledger-1" {
		t.Errorf("idempotency_key = %q, want ledger-1", p.IdempotencyKey)
	}
	for _, k := range []string{"addon_id", "plan", "storage_class", "compute_class", "addon_type", "environment_id"} {
		if p.Metadata[k] == "" {
			t.Errorf("metadata is missing %q", k)
		}
	}
	if p.Metadata["plan"] != "standard-1" {
		t.Errorf("plan = %q, want standard-1", p.Metadata["plan"])
	}
	if p.Metadata["compute_class"] != "500m/1Gi" {
		t.Errorf("compute_class = %q, want 500m/1Gi", p.Metadata["compute_class"])
	}
	if p.Metadata["storage_class"] != RetainStorageClass {
		t.Errorf("storage_class = %q, want %q", p.Metadata["storage_class"], RetainStorageClass)
	}
	want := map[string]float64{"storage_gb": 20, "cpu_millicores": 500, "memory_mb": 1024, "instances": 1}
	for k, v := range want {
		if p.Metrics[k] != v {
			t.Errorf("metrics[%q] = %v, want %v", k, p.Metrics[k], v)
		}
	}
}

// The legacy shared-pod plan has no volume of its own. Reporting it as
// longhorn-replicated would be the "selling isolation while the tenant lands
// on the shared pod" mistake, in data form.
func TestSharedDiscoveredIsNotReportedAsDedicatedStorage(t *testing.T) {
	addon := usageAddon()
	addon.Plan = "shared-discovered"
	p, err := buildUsageEventPayload(AddonUsageEvent{EventType: "addon.ready", Addon: addon})
	if err != nil {
		t.Fatalf("buildUsageEventPayload: %v", err)
	}
	if p.Metadata["storage_class"] != "shared" {
		t.Errorf("storage_class = %q, want \"shared\"", p.Metadata["storage_class"])
	}
}

// A malformed CPU or memory string must not stop a delete event from being
// recorded — losing the end of a billable span is worse than losing a
// dimension on it.
func TestUnparseableQuantitiesDegradeToZeroNotError(t *testing.T) {
	addon := usageAddon()
	addon.Config.CPU = "not-a-quantity"
	addon.Config.Memory = ""
	p, err := buildUsageEventPayload(AddonUsageEvent{EventType: "addon.destroyed", Addon: addon})
	if err != nil {
		t.Fatalf("buildUsageEventPayload: %v", err)
	}
	if p.Metrics["cpu_millicores"] != 0 || p.Metrics["memory_mb"] != 0 {
		t.Errorf("want zeroed dimensions, got %v", p.Metrics)
	}
}

func TestQuantityNormalisation(t *testing.T) {
	for in, want := range map[string]int64{"100m": 100, "1": 1000, "1500m": 1500, "": 0, "bad": 0} {
		if got := milliCores(in); got != want {
			t.Errorf("milliCores(%q) = %d, want %d", in, got, want)
		}
	}
	for in, want := range map[string]int64{"256Mi": 256, "1Gi": 1024, "": 0, "bad": 0} {
		if got := memoryMB(in); got != want {
			t.Errorf("memoryMB(%q) = %d, want %d", in, got, want)
		}
	}
}

// Only the transitions that bracket a database's existence or change its
// declared size belong in a usage stream.
func TestOnlyBracketingLedgerEventsAreForwarded(t *testing.T) {
	forwarded := map[db.ManagedDBAddonEventType]string{
		db.EventAddonReady:       "addon.ready",
		db.EventAddonPlanChanged: "addon.plan.changed",
		db.EventAddonDestroyed:   "addon.destroyed",
	}
	for in, want := range forwarded {
		if got := ledgerEventToUsageEvent(in); got != want {
			t.Errorf("ledgerEventToUsageEvent(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []db.ManagedDBAddonEventType{
		db.EventAddonCreateRequested,
		db.EventAddonProvisioningStarted,
		db.EventAddonFailed,
		db.EventAddonDestroyRequested,
		db.EventAddonBindingCreated,
		db.EventAddonBindingDeleted,
		db.EventAddonCredentialsRotated,
		db.EventAddonDataAPIEnabled,
		db.EventAddonDataAPIDisabled,
	} {
		if got := ledgerEventToUsageEvent(in); got != "" {
			t.Errorf("ledgerEventToUsageEvent(%q) = %q, want dropped", in, got)
		}
	}
}

// A retry must carry the same key, so the second delivery is refused by the
// unique index rather than recorded twice.
func TestEmitSendsTheKeyAndTheAPIKeyHeader(t *testing.T) {
	var gotPath, gotKeyHeader string
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKeyHeader = r.Header.Get("X-API-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"recorded":true}`))
	}))
	defer srv.Close()

	e := NewWaybillUsageEmitter(srv.URL+"/", "internal-key")
	if e == nil {
		t.Fatal("emitter is nil for a configured base URL")
	}
	if err := e.EmitAddonEvent(context.Background(), AddonUsageEvent{
		IdempotencyKey: "ledger-42",
		EventType:      "addon.destroyed",
		Addon:          usageAddon(),
	}); err != nil {
		t.Fatalf("EmitAddonEvent: %v", err)
	}

	if gotPath != "/internal/events" {
		t.Errorf("path = %q, want /internal/events", gotPath)
	}
	if gotKeyHeader != "internal-key" {
		t.Errorf("X-API-Key = %q, want internal-key", gotKeyHeader)
	}
	if gotBody["idempotency_key"] != "ledger-42" {
		t.Errorf("idempotency_key = %v, want ledger-42", gotBody["idempotency_key"])
	}
	if gotBody["event_type"] != "addon.destroyed" {
		t.Errorf("event_type = %v", gotBody["event_type"])
	}
}

// The whole feature is switched by one nil check, so an unconfigured
// deployment behaves exactly as it did before the emitter existed.
func TestNoBaseURLMeansNoEmitter(t *testing.T) {
	if e := NewWaybillUsageEmitter("", "k"); e != nil {
		t.Error("an unconfigured Waybill produced an emitter")
	}
	if e := NewWaybillUsageEmitter("   ", "k"); e != nil {
		t.Error("a blank Waybill URL produced an emitter")
	}
}

// The error must name the status and nothing else: the response body of an
// authenticated internal endpoint can echo the request, and this error is
// logged.
func TestRejectionErrorDoesNotEchoTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key: internal-key-value"}`))
	}))
	defer srv.Close()

	err := NewWaybillUsageEmitter(srv.URL, "internal-key-value").
		EmitAddonEvent(context.Background(), AddonUsageEvent{EventType: "addon.ready", Addon: usageAddon()})
	if err == nil {
		t.Fatal("a 401 was reported as success")
	}
	if got := err.Error(); got != "usage event rejected: HTTP 401" {
		t.Errorf("error = %q, want the status only", got)
	}
}

func TestPayloadRefusesAnEventWithNoAddon(t *testing.T) {
	if _, err := buildUsageEventPayload(AddonUsageEvent{EventType: "addon.ready"}); err == nil {
		t.Error("an event with no addon was accepted")
	}
	if _, err := buildUsageEventPayload(AddonUsageEvent{Addon: usageAddon()}); err == nil {
		t.Error("an event with no type was accepted")
	}
}
