package workloadtargets

import (
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const KarafielProductionFenceAuthority = "karafiel-production-writer-fence-v1"

func KarafielProductionDeployment(name string) bool {
	switch name {
	case "karafiel-api", "karafiel-web", "karafiel-admin", "karafiel-worker", "karafiel-beat":
		return true
	}
	return false
}

// ValidateKarafielProductionFleet is only a writer-exclusion enrollment
// profile. Existing Service/config/env/namespace metadata is never repaired.
// The sole legacy compatibility is a blank Service.git_repo corroborated by
// the completed exact repository registration and actual tracked source.
func ValidateKarafielProductionFleet(snapshots []Snapshot) ([]Plan, error) {
	return ValidateKarafielProductionFleetWithDiagnostic(snapshots, nil)
}

// ValidateKarafielProductionFleetWithDiagnostic adds only a bounded dry-plan trace.
func ValidateKarafielProductionFleetWithDiagnostic(snapshots []Snapshot, diagnostic *ProductionFenceDiagnostic) ([]Plan, error) {
	if len(snapshots) != 5 {
		return nil, diagnostic.ValidationFailure("fleet_identity", ErrIntent)
	}
	plans := make([]Plan, 0, 5)
	names := map[string]bool{}
	ids := map[uuid.UUID]bool{}
	uids := map[string]bool{}
	fleet := map[string]any{}
	var common Identity
	var commonVersions ReviewedVersions
	for _, s := range snapshots {
		if s.Project == nil || s.Project.Slug != "karafiel" || s.Service == nil || s.Environment == nil || s.Environment.Name != "production" || s.Environment.KubeNamespace != "karafiel" ||
			s.Registration == nil || s.Registration.RepoFullName != "madfam-org/karafiel" || s.Registration.ArgocdAppName == nil || *s.Registration.ArgocdAppName != "karafiel-services" ||
			s.Service.K8sNamespace == nil || *s.Service.K8sNamespace != "karafiel" || !KarafielProductionDeployment(s.Service.Name) || names[s.Service.Name] || ids[s.Service.ID] ||
			s.Namespace == nil || s.Application == nil {
			return nil, diagnostic.ValidationFailure("fleet_identity", ErrIntent)
		}
		if s.Service.GitRepo != "" {
			repo, _, err := CanonicalRepo(s.Service.GitRepo)
			if err != nil || repo != "https://github.com/madfam-org/karafiel" {
				return nil, diagnostic.ValidationFailure("repository", ErrIntent)
			}
		}
		configurationSHA, parentErr := validateKarafielProductionParent(s, diagnostic)
		if parentErr != nil {
			return nil, diagnostic.ValidationFailure("root_authority", ErrOwnership)
		}
		status, found, err := unstructured.NestedString(s.Application.Object, "status", "sync", "status")
		if err != nil || !found || status != "Synced" {
			return nil, diagnostic.ValidationFailure("application_sync", ErrOwnership)
		}
		owner := s.Namespace.Labels["enclii.dev/project"]
		pending := owner != s.Project.ID.String()
		if owner != "" && owner != "karafiel" && pending {
			return nil, diagnostic.ValidationFailure("namespace_owner", ErrOwnership)
		}
		actualService := s.Service
		service := *actualService
		service.GitRepo = "https://github.com/madfam-org/karafiel"
		s.Service = &service
		if pending {
			s.Namespace = s.Namespace.DeepCopy()
			delete(s.Namespace.Labels, "enclii.dev/project")
		}
		incumbent := s.Incumbent
		s.Incumbent = nil
		plan, err := validateSnapshotWithDiagnostic(s, "karafiel", actualService.Name, diagnostic)
		if err != nil {
			return nil, diagnostic.ValidationFailure("declared_identity", err)
		}
		if plan.Identity.Path != "infra/k8s/production" || plan.Identity.Ref != "main" || plan.Identity.Application != "karafiel-services" || uids[plan.Identity.DeploymentUID] {
			return nil, diagnostic.ValidationFailure("declared_identity", ErrOwnership)
		}
		if len(plans) > 0 && (plan.Identity.ProjectID != common.ProjectID || plan.Identity.EnvironmentID != common.EnvironmentID || plan.Identity.RegistrationID != common.RegistrationID ||
			plan.Identity.NamespaceUID != common.NamespaceUID || plan.Identity.ApplicationUID != common.ApplicationUID || plan.Identity.Revision != common.Revision ||
			plan.Identity.ArgoConfigUID != common.ArgoConfigUID || plan.Identity.TrackingMethod != common.TrackingMethod || plan.Identity.InstallationID != common.InstallationID ||
			plan.Versions.Namespace != commonVersions.Namespace || plan.Versions.Application != commonVersions.Application || plan.Versions.ArgoConfig != commonVersions.ArgoConfig) {
			return nil, diagnostic.ValidationFailure("fleet_coherence", ErrConflict)
		}
		if len(plans) > 0 && (plan.Identity.InstanceLabelKey != common.InstanceLabelKey || string(s.SourceRoot.GetUID()) != plans[0].Identity.SourceRootUID || s.SourceRoot.GetResourceVersion() != plans[0].Versions.SourceRoot ||
			string(s.SourceParent.GetUID()) != plans[0].Identity.SourceParentUID || s.SourceParent.GetResourceVersion() != plans[0].Versions.SourceParent || configurationSHA != plans[0].Identity.SourceConfigurationSHA || s.SourceConfigurationRevision != plans[0].Versions.SourceConfigurationRevision) {
			return nil, diagnostic.ValidationFailure("fleet_coherence", ErrConflict)
		}
		common, commonVersions = plan.Identity, plan.Versions
		plan.Identity.SourceAuthority = KarafielProductionFenceAuthority
		plan.Identity.SourceRootUID = string(s.SourceRoot.GetUID())
		plan.Identity.SourceParentUID = string(s.SourceParent.GetUID())
		plan.Identity.SourceConfigurationSHA = configurationSHA
		plan.Identity.NamespaceOwnershipPending = pending
		plan.Versions.SourceRoot = s.SourceRoot.GetResourceVersion()
		plan.Versions.SourceParent = s.SourceParent.GetResourceVersion()
		plan.Versions.SourceConfigurationRevision = s.SourceConfigurationRevision
		plan.Incumbent = incumbent
		fleet[actualService.Name] = struct {
			ServiceID                  uuid.UUID
			DeploymentUID, DeclaredSHA string
		}{actualService.ID, plan.Identity.DeploymentUID, plan.Identity.DeclaredIdentitySHA}
		names[actualService.Name] = true
		ids[actualService.ID] = true
		uids[plan.Identity.DeploymentUID] = true
		plans = append(plans, plan)
	}
	fleetSHA, err := hash(fleet)
	if err != nil {
		return nil, diagnostic.ValidationFailure("plan_fingerprint", err)
	}
	for n := range plans {
		p := &plans[n]
		p.Identity.SourceFleetSHA = fleetSHA
		if p.Incumbent != nil {
			p.Identity.NamespaceOwnershipPending = p.Incumbent.Identity.NamespaceOwnershipPending
			if p.Incumbent.Validate() != nil || !SameOwner(p.Incumbent.Identity, p.Identity) {
				return nil, diagnostic.ValidationFailure("incumbent_binding", ErrConflict)
			}
		}
		if p.Identity.Validate() != nil {
			return nil, diagnostic.ValidationFailure("plan_identity", ErrIntent)
		}
		p.SHA, err = p.Fingerprint()
		if err != nil {
			return nil, diagnostic.ValidationFailure("plan_fingerprint", err)
		}
	}
	return plans, nil
}
