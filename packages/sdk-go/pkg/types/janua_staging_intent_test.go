package types_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func heldIntentWireReceipt(t *testing.T) types.JanuaStagingIntent {
	t.Helper()
	wire := `{
		"id":"00000000-0000-4000-8000-000000000004",
		"project_id":"00000000-0000-4000-8000-000000000001",
		"environment_id":"00000000-0000-4000-8000-000000000002",
		"repository_registration_id":"00000000-0000-4000-8000-000000000003",
		"repository_registration_scope":"REPOSITORY_OWNERSHIP_ONLY","state":"HELD_PREREGISTRATION_ONLY",
		"profile":"janua-staging-preregistration-v1","proposed_service_name":"janua-api-staging",
		"proposed_deployment_name":"janua-api-staging","proposed_repository":"https://github.com/madfam-org/janua.git",
		"proposed_app_path":"apps/api","proposed_namespace":"janua-staging",
		"proposed_manifest_path":"k8s/overlays/staging","proposed_ref":"main",
		"activation_held":true,"service_materialized":false,"native_binding_proven":false,
		"materialization_supported":false,"recorded":false,"plan_sha256":"` + strings.Repeat("b", 64) + `",
		"unresolved_gates":["SOURCE_STAGE_REGISTRATION_NOT_PROVEN","STAGING_SOURCE_ALIGNMENT_NOT_QUALIFIED",
		"SERVICE_MATERIALIZATION_NOT_IMPLEMENTED","BACKGROUND_WRITER_EXCLUSION_NOT_QUALIFIED",
		"NATIVE_043_BINDING_NOT_PROVEN","RUNTIME_CUSTODY_MIGRATION_AND_SOAK_NOT_QUALIFIED"]}`
	var r types.JanuaStagingIntent
	if err := json.Unmarshal([]byte(wire), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestJanuaHeldIntentWireContract(t *testing.T) {
	r := heldIntentWireReceipt(t)
	if err := types.ValidateJanuaStagingIntent(r, r.ID); err != nil {
		t.Fatal(err)
	}
	recorded := true
	r.Recorded, r.RecordedAt = &recorded, "2026-10-20T19:00:00.123Z"
	for i, j := 0, len(r.Gates)-1; i < j; i, j = i+1, j-1 {
		r.Gates[i], r.Gates[j] = r.Gates[j], r.Gates[i]
	}
	if err := types.ValidateJanuaStagingIntent(r, r.ID); err != nil {
		t.Fatalf("recording remains held and does not materialize a service: %v", err)
	}
}

func TestJanuaHeldIntentRefusesAuthorityAndSourceDrift(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*types.JanuaStagingIntent, *uuid.UUID)
	}{
		{"unbound request", func(_ *types.JanuaStagingIntent, id *uuid.UUID) { *id = uuid.Nil }},
		{"different request", func(_ *types.JanuaStagingIntent, id *uuid.UUID) { *id = uuid.New() }},
		{"missing project", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.ProjectID = uuid.Nil }},
		{"missing environment", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.EnvironmentID = uuid.Nil }},
		{"missing ownership registration", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.RepositoryRegistrationID = uuid.Nil }},
		{"project environment alias", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.EnvironmentID = r.ProjectID }},
		{"project registration alias", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.RepositoryRegistrationID = r.ProjectID }},
		{"environment registration alias", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.RepositoryRegistrationID = r.EnvironmentID }},
		{"claimed source authority", func(r *types.JanuaStagingIntent, _ *uuid.UUID) {
			r.RepositoryRegistrationScope = "STAGING_SOURCE_AUTHORITY"
		}},
		{"claimed activation", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.State = "ACTIVE" }},
		{"different profile", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.Profile = "untrusted-upstream-value" }},
		{"production service", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.ProposedService = "janua-api" }},
		{"production deployment", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.ProposedDeployment = "janua-api" }},
		{"different repository", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.Repository = "https://example.com/other.git" }},
		{"different app path", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.AppPath = "apps/other" }},
		{"production namespace", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.Namespace = "janua" }},
		{"production manifest", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.ManifestPath = "k8s/overlays/production" }},
		{"different ref", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.Ref = "other" }},
		{"missing hold", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.ActivationHeld = nil }},
		{"released hold", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { v := false; r.ActivationHeld = &v }},
		{"missing materialization flag", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.ServiceMaterialized = nil }},
		{"materialized service", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { v := true; r.ServiceMaterialized = &v }},
		{"missing binding flag", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.NativeBindingProven = nil }},
		{"claimed native binding", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { v := true; r.NativeBindingProven = &v }},
		{"missing capability flag", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.MaterializationSupported = nil }},
		{"claimed materialization support", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { v := true; r.MaterializationSupported = &v }},
		{"missing recording flag", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.Recorded = nil }},
		{"malformed plan hash", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.PlanSHA256 = "sha256:" + strings.Repeat("b", 64) }},
		{"missing recorded timestamp", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { v := true; r.Recorded = &v }},
		{"invalid recorded timestamp", func(r *types.JanuaStagingIntent, _ *uuid.UUID) {
			v := true
			r.Recorded = &v
			r.RecordedAt = "yesterday"
		}},
		{"zero recorded timestamp", func(r *types.JanuaStagingIntent, _ *uuid.UUID) {
			v := true
			r.Recorded = &v
			r.RecordedAt = "0001-01-01T00:00:00Z"
		}},
		{"noncanonical recorded UTC", func(r *types.JanuaStagingIntent, _ *uuid.UUID) {
			v := true
			r.Recorded = &v
			r.RecordedAt = "2026-10-20T19:00:00+00:00"
		}},
		{"unrecorded timestamp", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.RecordedAt = "2026-10-20T19:00:00Z" }},
		{"missing prerequisite", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.Gates = r.Gates[1:] }},
		{"duplicate prerequisite", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.Gates[0] = r.Gates[1] }},
		{"unknown prerequisite", func(r *types.JanuaStagingIntent, _ *uuid.UUID) { r.Gates[0] = "untrusted-upstream-value" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := heldIntentWireReceipt(t)
			id := r.ID
			tc.mutate(&r, &id)
			err := types.ValidateJanuaStagingIntent(r, id)
			if err == nil || err.Error() != "closed held staging intent unavailable; no activation authority" {
				t.Fatalf("must refuse with bounded error, got %v", err)
			}
		})
	}
}

func TestJanuaHeldIntentConstructorRemainsInertAndIndependent(t *testing.T) {
	want := heldIntentWireReceipt(t)
	r := types.NewJanuaStagingIntent()
	// The constructor is an unbound proposal, never an existing service or a
	// persisted receipt. Assigning identities/hash is still separately required.
	if r.ID != uuid.Nil || r.ProjectID != uuid.Nil || r.EnvironmentID != uuid.Nil || r.RepositoryRegistrationID != uuid.Nil || r.PlanSHA256 != "" || r.RecordedAt != "" {
		t.Fatal("constructor fabricated persisted identity or observation")
	}
	if err := types.ValidateJanuaStagingIntent(r, want.ID); err == nil {
		t.Fatal("unbound proposal accepted as a receipt")
	}
	r.ID, r.ProjectID, r.EnvironmentID, r.RepositoryRegistrationID, r.PlanSHA256 = want.ID, want.ProjectID, want.EnvironmentID, want.RepositoryRegistrationID, want.PlanSHA256
	if err := types.ValidateJanuaStagingIntent(r, r.ID); err != nil {
		t.Fatal(err)
	}
	r.Gates[0] = "overwritten"
	*r.ActivationHeld = false
	fresh := types.NewJanuaStagingIntent()
	if !*fresh.ActivationHeld || *fresh.ServiceMaterialized || *fresh.NativeBindingProven || *fresh.MaterializationSupported || *fresh.Recorded || fresh.Gates[0] != "SOURCE_STAGE_REGISTRATION_NOT_PROVEN" || len(fresh.Gates) != 6 {
		t.Fatal("caller mutation changed future inert proposals")
	}
	gates := types.JanuaStagingIntentGates()
	gates[0] = "overwritten"
	if types.JanuaStagingIntentGates()[0] != "SOURCE_STAGE_REGISTRATION_NOT_PROVEN" {
		t.Fatal("caller mutation changed the closed prerequisite set")
	}
}
