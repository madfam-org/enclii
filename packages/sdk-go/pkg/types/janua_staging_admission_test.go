package types_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func admissionWireReceipt(t *testing.T) types.JanuaStagingAdmissionReceipt {
	t.Helper()
	// A wire observation is not an apply receipt, a capacity claim or a runtime
	// qualification. Keep those unresolved prerequisites visible to consumers.
	wire := `{
		"classification":"ADMITTED_DRY_RUN_ONLY","dry_run_only":true,"apply_supported":false,
		"profile":"janua-api-staging-v1","reviewed_image":"` + types.JanuaStagingPostgresImage + `",
		"project_slug":"janua","environment_name":"staging",
		"project_id":"00000000-0000-4000-8000-000000000001",
		"environment_id":"00000000-0000-4000-8000-000000000002",
		"service_id":"00000000-0000-4000-8000-000000000003",
		"catalog_plan":"standard-0","objects_dry_admitted":4,"exact_api_destination_pairs":2,
		"observation_fingerprint":"sha256:` + strings.Repeat("a", 64) + `",
		"observed_at":"2026-10-20T19:00:00Z",
		"unresolved_gates":["DURABLE_TYPED_APPLY_NOT_IMPLEMENTED","ACTUAL_CAPACITY_AND_SCHEDULING_NOT_EXERCISED",
		"ACTUAL_CNPG_PODS_AND_CNI_PATHS_NOT_EXERCISED","RUNTIME_OWNER_AND_PUBLIC_CA_CUSTODY_NOT_PROVISIONED",
		"STANDARD_IMAGE_BACKUP_PLUGIN_AND_RECOVERY_NOT_QUALIFIED","FIXED_JANUA_STAGE_MIGRATOR_NOT_IMPLEMENTED",
		"ACTUAL_RUNTIME_SOAK_AND_PRODUCTION_PROMOTION_NOT_QUALIFIED"]}`
	var r types.JanuaStagingAdmissionReceipt
	if err := json.Unmarshal([]byte(wire), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestJanuaAdmissionWireContract(t *testing.T) {
	r := admissionWireReceipt(t)
	for _, peers := range []int{1, 2, 258} {
		r.APIPeers = peers
		if err := types.ValidateJanuaStagingAdmissionReceipt(r, r.ServiceID, "standard-0"); err != nil {
			t.Fatalf("bounded metadata observation rejected: %v", err)
		}
	}
	// Set ordering is immaterial, but omission, duplication and unknown gates
	// must never turn unresolved qualification into an apparently green receipt.
	for i, j := 0, len(r.Gates)-1; i < j; i, j = i+1, j-1 {
		r.Gates[i], r.Gates[j] = r.Gates[j], r.Gates[i]
	}
	if err := types.ValidateJanuaStagingAdmissionReceipt(r, r.ServiceID, "standard-0"); err != nil {
		t.Fatal(err)
	}
}

func TestJanuaAdmissionRefusesAuthorityAndBindingDrift(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*types.JanuaStagingAdmissionReceipt, *uuid.UUID, *string)
	}{
		{"unbound request", func(_ *types.JanuaStagingAdmissionReceipt, s *uuid.UUID, _ *string) { *s = uuid.Nil }},
		{"invalid requested plan", func(_ *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, p *string) { *p = "../../prod" }},
		{"different requested plan", func(_ *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, p *string) { *p = "standard-1" }},
		{"apply classification", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.Classification = "APPLIED" }},
		{"missing dry mode", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.DryRunOnly = nil }},
		{"live mode", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { v := false; r.DryRunOnly = &v }},
		{"missing apply support", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.ApplySupported = nil }},
		{"apply enabled", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { v := true; r.ApplySupported = &v }},
		{"different profile", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) {
			r.Profile = "untrusted-upstream-value"
		}},
		{"floating image", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.Image = "postgres:latest" }},
		{"other project", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.ProjectSlug = "other" }},
		{"production environment", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.EnvironmentName = "production" }},
		{"missing project identity", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.ProjectID = uuid.Nil }},
		{"missing environment identity", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.EnvironmentID = uuid.Nil }},
		{"aliased identities", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.EnvironmentID = r.ProjectID }},
		{"project aliases service", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.ProjectID = r.ServiceID }},
		{"environment aliases service", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.EnvironmentID = r.ServiceID }},
		{"different service", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.ServiceID = uuid.New() }},
		{"partial admission", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.ObjectsAdmitted = 3 }},
		{"no API destination", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.APIPeers = 0 }},
		{"unbounded API destinations", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.APIPeers = 259 }},
		{"short fingerprint", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.Fingerprint = "sha256:abc" }},
		{"noncanonical fingerprint", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) {
			r.Fingerprint = "sha256:" + strings.Repeat("A", 64)
		}},
		{"invalid timestamp", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.ObservedAt = "yesterday" }},
		{"zero timestamp", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) {
			r.ObservedAt = "0001-01-01T00:00:00Z"
		}},
		{"noncanonical UTC", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) {
			r.ObservedAt = "2026-10-20T19:00:00+00:00"
		}},
		{"missing prerequisite", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.Gates = r.Gates[1:] }},
		{"duplicate prerequisite", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) { r.Gates[0] = r.Gates[1] }},
		{"unknown prerequisite", func(r *types.JanuaStagingAdmissionReceipt, _ *uuid.UUID, _ *string) {
			r.Gates[0] = "untrusted-upstream-value"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := admissionWireReceipt(t)
			service, plan := r.ServiceID, "standard-0"
			tc.mutate(&r, &service, &plan)
			err := types.ValidateJanuaStagingAdmissionReceipt(r, service, plan)
			if err == nil || err.Error() != "closed dry-admission receipt incomplete or incompatible; no apply authority" {
				t.Fatalf("must refuse with bounded error, got %v", err)
			}
		})
	}
}

func TestJanuaAdmissionGateSlicesAreIndependent(t *testing.T) {
	first := types.JanuaStagingAdmissionGates()
	first[0] = "overwritten"
	if got := types.JanuaStagingAdmissionGates(); got[0] != "DURABLE_TYPED_APPLY_NOT_IMPLEMENTED" || len(got) != 7 {
		t.Fatal("caller mutation changed the closed prerequisite set")
	}
}
