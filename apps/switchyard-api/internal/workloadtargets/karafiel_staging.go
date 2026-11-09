package workloadtargets

import (
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
)

const KarafielStagingHeldAuthority = "karafiel-staging-held-v1"
const KarafielStagingNamespace = "enclii-karafiel-staging"
const KarafielStagingServiceName = "karafiel-api-staging"

func KarafielStagingServiceID(project, operation uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(project, []byte(KarafielStagingHeldAuthority+":"+operation.String()))
}

func ValidateKarafielStagingService(service *types.Service, project uuid.UUID) error {
	if service == nil || service.ID == uuid.Nil || service.ProjectID != project ||
		service.Name != KarafielStagingServiceName || service.Type != types.ServiceTypeWeb ||
		service.GitRepo != "https://github.com/madfam-org/karafiel" || service.AppPath != "apps/api" ||
		service.AutoDeploy || service.AutoDeployBranch != "release/karafiel-staging" || service.AutoDeployEnv != "staging" ||
		service.K8sNamespace == nil || *service.K8sNamespace != KarafielStagingNamespace || len(service.Jobs) != 0 || len(service.Volumes) != 0 {
		return ErrIntent
	}
	if service.BuildConfig.Type != types.BuildTypeDockerfile || service.BuildConfig.Dockerfile != "apps/api/Dockerfile" || service.BuildConfig.Context != "." ||
		service.BuildConfig.BuildOnly || len(service.BuildConfig.BuildArgs) != 0 || service.BuildConfig.Target != "" || service.BuildConfig.Buildpack != "" {
		return ErrIntent
	}
	return nil
}

// The original repository registration remains authoritative and unchanged.
// Only this closed profile declares the separate held staging source intent;
// ordinary target plans cannot manufacture or refresh that authority.
func ValidateKarafielStagingSnapshot(s Snapshot) (Plan, error) {
	if s.Project == nil || s.Project.Slug != "karafiel" || s.Environment == nil ||
		s.Environment.Name != "staging" || s.Environment.KubeNamespace != KarafielStagingNamespace ||
		s.Registration == nil || s.Registration.RepoFullName != "madfam-org/karafiel" ||
		ValidateKarafielStagingService(s.Service, s.Project.ID) != nil || s.Application == nil || s.Namespace == nil {
		return Plan{}, ErrIntent
	}
	owner := s.Namespace.Labels["enclii.dev/project"]
	if owner != "" && owner != "karafiel" && owner != s.Project.ID.String() {
		return Plan{}, ErrOwnership
	}
	pending := owner != s.Project.ID.String()
	if err := validateKarafielSourceRoot(s); err != nil {
		return Plan{}, err
	}
	status, found, e := unstructured.NestedString(s.Application.Object, "status", "sync", "status")
	if e != nil || !found || status != "Synced" {
		return Plan{}, ErrOwnership
	}
	project, found, e := unstructured.NestedString(s.Application.Object, "spec", "project")
	if e != nil || !found || project != "karafiel-staging" {
		return Plan{}, ErrOwnership
	}
	if s.Declared == nil || s.Declared.Spec.Replicas == nil || *s.Declared.Spec.Replicas != 0 ||
		s.Deployment == nil || s.Deployment.Spec.Replicas == nil || *s.Deployment.Spec.Replicas != 0 {
		return Plan{}, ErrOwnership
	}
	enabled, found, err := unstructured.NestedBool(s.Application.Object, "spec", "syncPolicy", "automated", "enabled")
	if err != nil || !found || enabled {
		return Plan{}, ErrOwnership
	}
	registration := *s.Registration
	application := "karafiel-staging"
	registration.ArgocdAppName = &application
	registration.ConfigSnapshot = map[string]any{"namespace": KarafielStagingNamespace, "manifest_path": "infra/k8s/overlays/staging", "branch": "release/karafiel-staging"}
	s.Registration = &registration
	// Native043 normally refuses legacy labels. This closed metadata-only plan
	// may record that exact pending state without declaring namespace ownership
	// or changing the actual object. UID and resourceVersion remain authoritative.
	if pending {
		s.Namespace = s.Namespace.DeepCopy()
		delete(s.Namespace.Labels, "enclii.dev/project")
	}
	incumbent := s.Incumbent
	s.Incumbent = nil
	plan, err := ValidateSnapshot(s, KarafielStagingNamespace, "karafiel-api")
	if err != nil {
		return Plan{}, err
	}
	plan.Identity.SourceAuthority = KarafielStagingHeldAuthority
	plan.Identity.SourceRootUID = string(s.SourceRoot.GetUID())
	plan.Identity.NamespaceOwnershipPending = pending
	plan.Versions.SourceRoot = s.SourceRoot.GetResourceVersion()
	if err := plan.Identity.Validate(); err != nil {
		return Plan{}, err
	}
	if incumbent != nil {
		// The pending flag is a historical registration fact, not an activation
		// permit. A later verified same-UID namespace adoption must not rewrite it.
		plan.Identity.NamespaceOwnershipPending = incumbent.Identity.NamespaceOwnershipPending
		if incumbent.Validate() != nil || !SameOwner(incumbent.Identity, plan.Identity) {
			return Plan{}, ErrConflict
		}
		plan.Incumbent = incumbent
	}
	plan.SHA, err = plan.Fingerprint()
	return plan, err
}

// ValidateKarafielStagingRootAuthority rechecks current direct-root custody.
// Consumers must additionally compare the observed native identities and Argo
// authority configuration to their immutable enrollment before claiming READY.
func ValidateKarafielStagingRootAuthority(s Snapshot) error {
	return validateKarafielSourceRoot(s)
}

func validateKarafielSourceRoot(s Snapshot) error {
	return validateKarafielRootChild(s, s.Application, "Application", "karafiel-staging")
}

func validateKarafielRootChild(s Snapshot, a *unstructured.Unstructured, kind, name string) error {
	r, config := s.SourceRoot, s.ArgoConfig
	if a == nil || a.GetName() != name || a.GetNamespace() != "argocd" ||
		r == nil || config == nil || r.GetName() != "enclii-infrastructure" || r.GetNamespace() != "argocd" ||
		r.GetUID() == "" || r.GetResourceVersion() == "" || r.GetDeletionTimestamp() != nil {
		return ErrOwnership
	}
	for _, f := range []struct {
		path     []string
		expected string
	}{
		{[]string{"spec", "source", "repoURL"}, "https://github.com/madfam-org/enclii.git"},
		{[]string{"spec", "source", "path"}, "infra/argocd/apps"},
		{[]string{"spec", "source", "targetRevision"}, "main"},
		{[]string{"status", "sync", "status"}, "Synced"},
	} {
		value, found, err := unstructured.NestedString(r.Object, f.path...)
		if err != nil || !found || value != f.expected {
			return ErrOwnership
		}
	}
	if _, found, err := unstructured.NestedFieldNoCopy(r.Object, "spec", "sources"); err != nil || found {
		return ErrOwnership
	}
	for _, key := range []string{"repoURL", "path", "targetRevision"} {
		actual, found, err := unstructured.NestedString(r.Object, "status", "sync", "comparedTo", "source", key)
		declared, _, _ := unstructured.NestedString(r.Object, "spec", "source", key)
		if err != nil || !found || actual != declared {
			return ErrOwnership
		}
	}
	for _, owner := range a.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller {
			return ErrOwnership
		}
	}
	switch config.Data["application.resourceTrackingMethod"] {
	case "annotation", "annotation+label":
		if a.GetAnnotations()["argocd.argoproj.io/tracking-id"] != "enclii-infrastructure:argoproj.io/"+kind+":argocd/"+name ||
			a.GetAnnotations()["argocd.argoproj.io/installation-id"] != config.Data["installationID"] {
			return ErrOwnership
		}
	case "label":
		key := config.Data["application.instanceLabelKey"]
		if key == "" {
			key = "app.kubernetes.io/instance"
		}
		if len(validation.IsQualifiedName(key)) != 0 || a.GetLabels()[key] != "enclii-infrastructure" {
			return ErrOwnership
		}
	default:
		return ErrTrackingMethod
	}
	resources, found, err := unstructured.NestedSlice(r.Object, "status", "resources")
	if err != nil || !found {
		return ErrOwnership
	}
	count := 0
	for _, raw := range resources {
		m, ok := raw.(map[string]any)
		if ok && m["group"] == "argoproj.io" && m["kind"] == kind && m["namespace"] == "argocd" && m["name"] == name {
			if m["status"] != "Synced" {
				return ErrOwnership
			}
			count++
		}
	}
	if count != 1 {
		return ErrOwnership
	}
	return nil
}
