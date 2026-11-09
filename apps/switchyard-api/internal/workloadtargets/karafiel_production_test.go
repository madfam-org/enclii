package workloadtargets

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ktypes "k8s.io/apimachinery/pkg/types"
)

func productionFleetFixture() []Snapshot {
	base := heldStageFixture()
	base.Namespace.Name = "karafiel"
	base.Environment.Name = "production"
	base.Environment.KubeNamespace = "karafiel"
	app := "karafiel-services"
	base.Registration.ArgocdAppName = &app
	base.Registration.ConfigSnapshot = map[string]any{"namespace": "karafiel", "manifest_path": "infra/k8s/production", "branch": "main"}
	base.Application.SetName(app)
	base.Application.SetAnnotations(nil)
	controller := true
	base.Application.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "argoproj.io/v1alpha1", Kind: "ApplicationSet", Name: "project-applications", UID: "appset:fixture", Controller: &controller}})
	_ = unstructured.SetNestedField(base.Application.Object, "default", "spec", "project")
	_ = unstructured.SetNestedField(base.Application.Object, true, "spec", "syncPolicy", "automated", "enabled")
	_ = unstructured.SetNestedField(base.Application.Object, "karafiel", "spec", "destination", "namespace")
	source := map[string]any{"repoURL": "https://github.com/madfam-org/karafiel.git", "path": "infra/k8s/production", "targetRevision": "main"}
	_ = unstructured.SetNestedMap(base.Application.Object, source, "spec", "source")
	_ = unstructured.SetNestedMap(base.Application.Object, source, "status", "sync", "comparedTo", "source")
	_ = unstructured.SetNestedSlice(base.SourceRoot.Object, []any{map[string]any{"group": "argoproj.io", "kind": "ApplicationSet", "namespace": "argocd", "name": "project-applications", "status": "Synced"}}, "status", "resources")
	base.SourceConfigurationRevision = "cccccccccccccccccccccccccccccccccccccccc"
	_ = unstructured.SetNestedField(base.SourceRoot.Object, base.SourceConfigurationRevision, "status", "sync", "revision")
	base.SourceConfiguration = map[string]string{"name": "karafiel", "partOf": "karafiel", "repoURL": "https://github.com/madfam-org/karafiel.git", "branch": "main", "namespace": "karafiel", "manifestPath": "infra/k8s/production"}
	base.SourceParent = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "ApplicationSet",
		"metadata": map[string]any{"name": "project-applications", "namespace": "argocd", "uid": "appset:fixture", "resourceVersion": "6", "annotations": map[string]any{"argocd.argoproj.io/tracking-id": "enclii-infrastructure:argoproj.io/ApplicationSet:argocd/project-applications"}},
		"spec": map[string]any{"goTemplate": true, "goTemplateOptions": []any{"missingkey=zero"}, "generators": []any{map[string]any{"git": map[string]any{"repoURL": "https://github.com/madfam-org/enclii.git", "revision": "main", "files": []any{map[string]any{"path": "infra/argocd/projects/*/config.json"}}}}},
			"template": map[string]any{"metadata": map[string]any{"name": "{{ .name }}-services"}, "spec": map[string]any{"project": "default", "source": map[string]any{"repoURL": "{{ .repoURL }}", "targetRevision": "{{ .branch }}", "path": "{{ .manifestPath }}"}, "destination": map[string]any{"server": "https://kubernetes.default.svc", "namespace": "{{ .namespace }}"}}}},
	}}
	resources := []any{}
	for _, name := range []string{"karafiel-api", "karafiel-web", "karafiel-admin", "karafiel-worker", "karafiel-beat"} {
		resources = append(resources, map[string]any{"group": "apps", "kind": "Deployment", "namespace": "karafiel", "name": name})
	}
	_ = unstructured.SetNestedSlice(base.Application.Object, resources, "status", "resources")
	fleet := []Snapshot{}
	for _, name := range []string{"karafiel-api", "karafiel-web", "karafiel-admin", "karafiel-worker", "karafiel-beat"} {
		s := base
		service := *base.Service
		s.Service = &service
		ns := "karafiel"
		s.Service.ID = uuid.New()
		s.Service.Name = name
		s.Service.K8sNamespace = &ns
		s.Service.GitRepo = ""
		s.Service.AutoDeploy = true
		s.Service.AutoDeployBranch = "main"
		s.Service.AutoDeployEnv = "production"
		s.Deployment = base.Deployment.DeepCopy()
		s.Deployment.Name = name
		s.Deployment.Namespace = ns
		s.Deployment.UID = ktypes.UID("deployment:" + name)
		s.Deployment.Annotations["argocd.argoproj.io/tracking-id"] = app + ":apps/Deployment:karafiel/" + name
		s.Deployment.Spec.Template.Labels["enclii.dev/service"] = name
		s.Deployment.Spec.Template.Labels["app"] = name
		s.Deployment.Spec.Selector.MatchLabels["app"] = name
		s.Declared = s.Deployment.DeepCopy()
		fleet = append(fleet, s)
	}
	return fleet
}

func TestKarafielProductionFenceBlankRepoCompatibilityPreservesExistingMetadata(t *testing.T) {
	fleet := productionFleetFixture()
	before, _ := json.Marshal(fleet)
	plans, err := ValidateKarafielProductionFleet(fleet)
	require.NoError(t, err)
	require.Len(t, plans, 5)
	after, _ := json.Marshal(fleet)
	require.Equal(t, string(before), string(after))
	for n, p := range plans {
		require.Equal(t, KarafielProductionFenceAuthority, p.Identity.SourceAuthority)
		require.NotEmpty(t, p.Identity.SourceFleetSHA)
		require.Equal(t, plans[0].Identity.SourceFleetSHA, p.Identity.SourceFleetSHA)
		require.NoError(t, p.Identity.Validate())
		require.True(t, fleet[n].Service.AutoDeploy, "enrollment must not pretend flags changed")
		fleet[n].Incumbent = &Binding{ID: uuid.New(), Identity: p.Identity, PlanSHA: p.SHA, Versions: p.Versions}
	}
	_, err = ValidateKarafielProductionFleet(fleet)
	require.NoError(t, err)
	_, err = ValidateSnapshot(fleet[0], "karafiel", "karafiel-api")
	require.Error(t, err, "generic blank repo guard remains")
}

func TestKarafielProductionFenceRefusesForeignOrIncoherentFleet(t *testing.T) {
	for name, mutate := range map[string]func([]Snapshot){
		"foreign-nonempty-repo":   func(f []Snapshot) { f[0].Service.GitRepo = "https://github.com/foreign/repo" },
		"whitespace-repo":         func(f []Snapshot) { f[0].Service.GitRepo = " " },
		"foreign-service":         func(f []Snapshot) { f[0].Service.Name = "unrelated-api" },
		"missing-namespace":       func(f []Snapshot) { f[0].Service.K8sNamespace = nil },
		"foreign-service-project": func(f []Snapshot) { f[0].Service.ProjectID = uuid.New() },
		"foreign-owner":           func(f []Snapshot) { f[0].Namespace.Labels["enclii.dev/project"] = uuid.NewString() },
		"foreign-name-owner":      func(f []Snapshot) { f[0].Namespace.Labels["enclii.dev/project"] = "other-project" },
		"duplicate-uid":           func(f []Snapshot) { f[1].Deployment.UID = f[0].Deployment.UID },
		"foreign-namespace":       func(f []Snapshot) { f[1].Deployment.Namespace = "enclii-karafiel-staging" },
		"service-id-reuse":        func(f []Snapshot) { f[1].Service.ID = f[0].Service.ID },
		"missing-root":            func(f []Snapshot) { f[0].SourceRoot = nil },
		"untracked-root-child":    func(f []Snapshot) { f[0].SourceParent.SetAnnotations(nil) },
		"changed-actual-source": func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].Application.Object, "release/karafiel-production", "spec", "source", "targetRevision")
		},
		"changed-original-intent": func(f []Snapshot) { f[0].Registration.ConfigSnapshot["branch"] = "release/karafiel-production" },
		"stale-compared-source": func(f []Snapshot) {
			_ = unstructured.SetNestedField(f[0].Application.Object, "foreign/path", "status", "sync", "comparedTo", "source", "path")
		},
		"different-revision": func(f []Snapshot) {
			f[1].Application = f[1].Application.DeepCopy()
			_ = unstructured.SetNestedField(f[1].Application.Object, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "status", "sync", "revision")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := productionFleetFixture()
			mutate(f)
			_, err := ValidateKarafielProductionFleet(f)
			require.Error(t, err)
		})
	}
	f := productionFleetFixture()
	_, err := ValidateKarafielProductionFleet(f[:4])
	require.Error(t, err)
}

func TestKarafielProductionFenceFleetUIDsBindReviewAndPendingOwnership(t *testing.T) {
	for _, owner := range []string{"", "karafiel"} {
		t.Run("owner-"+owner, func(t *testing.T) {
			f := productionFleetFixture()
			f[0].Namespace.Labels = map[string]string{"enclii.dev/project": owner}
			plans, err := ValidateKarafielProductionFleet(f)
			require.NoError(t, err)
			for n, p := range plans {
				require.True(t, p.Identity.NamespaceOwnershipPending)
				f[n].Incumbent = &Binding{ID: uuid.New(), Identity: p.Identity, PlanSHA: p.SHA, Versions: p.Versions}
			}
			f[0].Namespace.Labels["enclii.dev/project"] = f[0].Project.ID.String()
			f[0].Namespace.ResourceVersion = "99"
			adopted, err := ValidateKarafielProductionFleet(f)
			require.NoError(t, err)
			require.True(t, adopted[0].Identity.NamespaceOwnershipPending)
			f[4].Deployment.UID = "replacement:beat"
			_, err = ValidateKarafielProductionFleet(f)
			require.Error(t, err, "related replacement cannot silently refresh immutable fleet authority")
		})
	}
	f := productionFleetFixture()
	a, err := ValidateKarafielProductionFleet(f)
	require.NoError(t, err)
	f[4].Deployment.UID = "replacement:beat"
	b, err := ValidateKarafielProductionFleet(f)
	require.NoError(t, err)
	require.NotEqual(t, a[0].SHA, b[0].SHA)
	f = productionFleetFixture()
	f[0].Service.GitRepo = "https://github.com/madfam-org/karafiel.git"
	_, err = ValidateKarafielProductionFleet(f)
	require.NoError(t, err, "canonical existing nonblank repo remains corroborated")
}

func TestKarafielProductionFenceRequiresActualApplicationSetParentChain(t *testing.T) {
	for name, change := range map[string]func(*Snapshot){
		"direct-root-fallback": func(s *Snapshot) {
			s.Application.SetOwnerReferences(nil)
			s.Application.SetAnnotations(map[string]string{"argocd.argoproj.io/tracking-id": "enclii-infrastructure:argoproj.io/Application:argocd/karafiel-services"})
		},
		"foreign-controller-uid": func(s *Snapshot) {
			refs := s.Application.GetOwnerReferences()
			refs[0].UID = "foreign:controller"
			s.Application.SetOwnerReferences(refs)
		},
		"foreign-controller-name": func(s *Snapshot) {
			refs := s.Application.GetOwnerReferences()
			refs[0].Name = "another-appset"
			s.Application.SetOwnerReferences(refs)
		},
		"noncontroller-owner": func(s *Snapshot) {
			refs := s.Application.GetOwnerReferences()
			no := false
			refs[0].Controller = &no
			s.Application.SetOwnerReferences(refs)
		},
		"extra-controller": func(s *Snapshot) {
			refs := s.Application.GetOwnerReferences()
			s.Application.SetOwnerReferences(append(refs, refs[0]))
		},
		"missing-appset":     func(s *Snapshot) { s.SourceParent = nil },
		"missing-appset-uid": func(s *Snapshot) { s.SourceParent.SetUID("") },
		"missing-appset-rv":  func(s *Snapshot) { s.SourceParent.SetResourceVersion("") },
		"foreign-generator-repo": func(s *Snapshot) {
			_ = unstructured.SetNestedSlice(s.SourceParent.Object, []any{map[string]any{"git": map[string]any{"repoURL": "https://github.com/foreign/repo", "revision": "main", "files": []any{map[string]any{"path": "infra/argocd/projects/*/config.json"}}}}}, "spec", "generators")
		},
		"template-extra-source": func(s *Snapshot) {
			_ = unstructured.SetNestedMap(s.SourceParent.Object, map[string]any{"name": "arbitrary-plugin"}, "spec", "template", "spec", "source", "plugin")
		},
		"template-override": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.SourceParent.Object, "arbitrary", "spec", "templatePatch")
		},
		"template-wrong-ref": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.SourceParent.Object, "main", "spec", "template", "spec", "source", "targetRevision")
		},
		"root-missing-appset":    func(s *Snapshot) { unstructured.RemoveNestedField(s.SourceRoot.Object, "status", "resources") },
		"config-foreign-project": func(s *Snapshot) { s.SourceConfiguration["name"] = "foreign" },
		"config-wrong-branch":    func(s *Snapshot) { s.SourceConfiguration["branch"] = "release/karafiel-production" },
		"config-unknown-key":     func(s *Snapshot) { s.SourceConfiguration["command"] = "refused" },
		"config-wrong-revision":  func(s *Snapshot) { s.SourceConfigurationRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" },
	} {
		t.Run(name, func(t *testing.T) {
			f := productionFleetFixture()
			change(&f[0])
			_, err := ValidateKarafielProductionFleet(f)
			require.Error(t, err)
		})
	}
	f := productionFleetFixture()
	plans, err := ValidateKarafielProductionFleet(f)
	require.NoError(t, err)
	require.Equal(t, "appset:fixture", plans[0].Identity.SourceParentUID)
	require.Equal(t, "6", plans[0].Versions.SourceParent)
	require.NotEmpty(t, plans[0].Identity.SourceConfigurationSHA)
	f[0].SourceParent.SetResourceVersion("7")
	changed, err := ValidateKarafielProductionFleet(f)
	require.NoError(t, err)
	require.NotEqual(t, plans[0].SHA, changed[0].SHA)
	stage := heldStageFixture()
	stage.Application.SetOwnerReferences(f[0].Application.GetOwnerReferences())
	_, err = ValidateKarafielStagingSnapshot(stage)
	require.Error(t, err, "stage direct-root proof must not accept generated child")
}
