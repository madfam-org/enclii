package types

import (
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// A held intent is outside services and all activation consumers. Its repository
// registration proves repository ownership only, never staging Argo authority.
type JanuaStagingIntent struct {
	ID                          uuid.UUID `json:"id"`
	ProjectID                   uuid.UUID `json:"project_id"`
	EnvironmentID               uuid.UUID `json:"environment_id"`
	RepositoryRegistrationID    uuid.UUID `json:"repository_registration_id"`
	RepositoryRegistrationScope string    `json:"repository_registration_scope"`
	State                       string    `json:"state"`
	Profile                     string    `json:"profile"`
	ProposedService             string    `json:"proposed_service_name"`
	ProposedDeployment          string    `json:"proposed_deployment_name"`
	Repository                  string    `json:"proposed_repository"`
	AppPath                     string    `json:"proposed_app_path"`
	Namespace                   string    `json:"proposed_namespace"`
	ManifestPath                string    `json:"proposed_manifest_path"`
	Ref                         string    `json:"proposed_ref"`
	ActivationHeld              *bool     `json:"activation_held"`
	ServiceMaterialized         *bool     `json:"service_materialized"`
	NativeBindingProven         *bool     `json:"native_binding_proven"`
	MaterializationSupported    *bool     `json:"materialization_supported"`
	Recorded                    *bool     `json:"recorded"`
	RecordedAt                  string    `json:"recorded_at,omitempty"`
	PlanSHA256                  string    `json:"plan_sha256"`
	Gates                       []string  `json:"unresolved_gates"`
}

func JanuaStagingIntentGates() []string {
	return []string{"SOURCE_STAGE_REGISTRATION_NOT_PROVEN", "STAGING_SOURCE_ALIGNMENT_NOT_QUALIFIED", "SERVICE_MATERIALIZATION_NOT_IMPLEMENTED", "BACKGROUND_WRITER_EXCLUSION_NOT_QUALIFIED", "NATIVE_043_BINDING_NOT_PROVEN", "RUNTIME_CUSTODY_MIGRATION_AND_SOAK_NOT_QUALIFIED"}
}

func NewJanuaStagingIntent() JanuaStagingIntent {
	held, no := true, false
	return JanuaStagingIntent{RepositoryRegistrationScope: "REPOSITORY_OWNERSHIP_ONLY", State: "HELD_PREREGISTRATION_ONLY", Profile: "janua-staging-preregistration-v1", ProposedService: "janua-api-staging", ProposedDeployment: "janua-api-staging", Repository: "https://github.com/madfam-org/janua.git", AppPath: "apps/api", Namespace: "janua-staging", ManifestPath: "k8s/overlays/staging", Ref: "main", ActivationHeld: &held, ServiceMaterialized: &no, NativeBindingProven: &no, MaterializationSupported: &no, Recorded: &no, Gates: JanuaStagingIntentGates()}
}

var januaIntentHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateJanuaStagingIntent(r JanuaStagingIntent, id uuid.UUID) error {
	bad := fmt.Errorf("closed held staging intent unavailable; no activation authority")
	want := NewJanuaStagingIntent()
	if id == uuid.Nil || r.ID != id || r.ProjectID == uuid.Nil || r.EnvironmentID == uuid.Nil || r.RepositoryRegistrationID == uuid.Nil || r.ProjectID == r.EnvironmentID || r.ProjectID == r.RepositoryRegistrationID || r.EnvironmentID == r.RepositoryRegistrationID || r.RepositoryRegistrationScope != want.RepositoryRegistrationScope || r.State != want.State || r.Profile != want.Profile || r.ProposedService != want.ProposedService || r.ProposedDeployment != want.ProposedDeployment || r.Repository != want.Repository || r.AppPath != want.AppPath || r.Namespace != want.Namespace || r.ManifestPath != want.ManifestPath || r.Ref != want.Ref || r.ActivationHeld == nil || !*r.ActivationHeld || r.ServiceMaterialized == nil || *r.ServiceMaterialized || r.NativeBindingProven == nil || *r.NativeBindingProven || r.MaterializationSupported == nil || *r.MaterializationSupported || r.Recorded == nil || !januaIntentHash.MatchString(r.PlanSHA256) {
		return bad
	}
	if *r.Recorded {
		t, err := time.Parse(time.RFC3339Nano, r.RecordedAt)
		if err != nil || t.IsZero() || t.UTC().Format(time.RFC3339Nano) != r.RecordedAt {
			return bad
		}
	} else if r.RecordedAt != "" {
		return bad
	}
	allowed := map[string]bool{}
	for _, gate := range want.Gates {
		allowed[gate] = true
	}
	if len(r.Gates) != len(allowed) {
		return bad
	}
	for _, gate := range r.Gates {
		if !allowed[gate] {
			return bad
		}
		delete(allowed, gate)
	}
	if len(allowed) != 0 {
		return bad
	}
	return nil
}
