package types

import (
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Closed source contract shared by the native API and CLI. These descriptors
// are neither deployed-image proof nor authority to apply resources.
const JanuaStagingAdmissionProfile = "janua-api-staging-v1"
const JanuaStagingAdmissionClassification = "ADMITTED_DRY_RUN_ONLY"
const JanuaStagingPostgresImage = "ghcr.io/cloudnative-pg/postgresql:18.6-202610190957-standard-trixie@sha256:d72ace96f5803b12c341bb5dac48532e8c1f92f69e8a717a59d779bfc038dea9"

type JanuaStagingAdmissionReceipt struct {
	Classification  string    `json:"classification"`
	DryRunOnly      *bool     `json:"dry_run_only"`
	ApplySupported  *bool     `json:"apply_supported"`
	Profile         string    `json:"profile"`
	Image           string    `json:"reviewed_image"`
	ProjectSlug     string    `json:"project_slug"`
	EnvironmentName string    `json:"environment_name"`
	ProjectID       uuid.UUID `json:"project_id"`
	EnvironmentID   uuid.UUID `json:"environment_id"`
	ServiceID       uuid.UUID `json:"service_id"`
	CatalogPlan     string    `json:"catalog_plan"`
	ObjectsAdmitted int       `json:"objects_dry_admitted"`
	APIPeers        int       `json:"exact_api_destination_pairs"`
	Fingerprint     string    `json:"observation_fingerprint"`
	ObservedAt      string    `json:"observed_at"`
	Gates           []string  `json:"unresolved_gates"`
}

func JanuaStagingAdmissionGates() []string {
	return []string{"DURABLE_TYPED_APPLY_NOT_IMPLEMENTED", "ACTUAL_CAPACITY_AND_SCHEDULING_NOT_EXERCISED", "ACTUAL_CNPG_PODS_AND_CNI_PATHS_NOT_EXERCISED", "RUNTIME_OWNER_AND_PUBLIC_CA_CUSTODY_NOT_PROVISIONED", "STANDARD_IMAGE_BACKUP_PLUGIN_AND_RECOVERY_NOT_QUALIFIED", "FIXED_JANUA_STAGE_MIGRATOR_NOT_IMPLEMENTED", "ACTUAL_RUNTIME_SOAK_AND_PRODUCTION_PROMOTION_NOT_QUALIFIED"}
}

var januaStageAdmissionHash = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var januaStageAdmissionPlan = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// ValidateJanuaStagingAdmissionReceipt validates a closed nonpersistent wire
// observation, not project authorization or readiness. The server must prove
// persisted ownership; the CLI binds its fixed route, requested service/plan and
// complete safe receipt shape without printing unreviewed free-form fields.
func ValidateJanuaStagingAdmissionReceipt(r JanuaStagingAdmissionReceipt, service uuid.UUID, plan string) error {
	refused := fmt.Errorf("closed dry-admission receipt incomplete or incompatible; no apply authority")
	if service == uuid.Nil || !januaStageAdmissionPlan.MatchString(plan) || r.Classification != JanuaStagingAdmissionClassification || r.DryRunOnly == nil || !*r.DryRunOnly || r.ApplySupported == nil || *r.ApplySupported || r.Profile != JanuaStagingAdmissionProfile || r.Image != JanuaStagingPostgresImage || r.ProjectSlug != "janua" || r.EnvironmentName != "staging" || r.ProjectID == uuid.Nil || r.EnvironmentID == uuid.Nil || r.ProjectID == r.EnvironmentID || r.ProjectID == service || r.EnvironmentID == service || r.ServiceID != service || r.CatalogPlan != plan || r.ObjectsAdmitted != 4 || r.APIPeers < 1 || r.APIPeers > 258 || !januaStageAdmissionHash.MatchString(r.Fingerprint) {
		return refused
	}
	observed, err := time.Parse(time.RFC3339Nano, r.ObservedAt)
	if err != nil || observed.IsZero() || observed.UTC().Format(time.RFC3339Nano) != r.ObservedAt {
		return refused
	}
	wanted := JanuaStagingAdmissionGates()
	if len(r.Gates) != len(wanted) {
		return refused
	}
	allowed := make(map[string]bool, len(wanted))
	for _, gate := range wanted {
		allowed[gate] = true
	}
	for _, gate := range r.Gates {
		if !allowed[gate] {
			return refused
		}
		delete(allowed, gate)
	}
	if len(allowed) != 0 {
		return refused
	}
	return nil
}
