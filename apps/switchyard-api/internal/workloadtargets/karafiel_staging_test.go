package workloadtargets

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func heldStageFixture() Snapshot {
	s := targetFixture()
	s.Project.Slug = "karafiel"
	op := uuid.MustParse("22000000-0000-4000-8000-000000000001")
	ns := KarafielStagingNamespace
	s.Service = &types.Service{ID: KarafielStagingServiceID(s.Project.ID, op), ProjectID: s.Project.ID, Name: KarafielStagingServiceName, Type: types.ServiceTypeWeb, GitRepo: "https://github.com/madfam-org/karafiel", AppPath: "apps/api", AutoDeploy: false, AutoDeployBranch: "release/karafiel-staging", AutoDeployEnv: "staging", K8sNamespace: &ns, BuildConfig: types.BuildConfig{Type: types.BuildTypeDockerfile, Dockerfile: "apps/api/Dockerfile", Context: "."}}
	s.Environment.Name = "staging"
	s.Environment.KubeNamespace = ns
	s.Registration.RepoFullName = "madfam-org/karafiel"
	// Production registration intent deliberately remains unchanged.
	s.Namespace.Name = ns
	s.Namespace.Labels = map[string]string{"enclii.dev/project": s.Project.ID.String(), "enclii.dev/verify-signatures": "true"}
	s.Deployment.Name = "karafiel-api"
	zero := int32(0)
	s.Deployment.Spec.Replicas = &zero
	s.Deployment.Namespace = ns
	s.Deployment.Annotations["argocd.argoproj.io/tracking-id"] = "karafiel-staging:apps/Deployment:" + ns + "/karafiel-api"
	s.Deployment.Spec.Template.Labels["enclii.dev/service"] = KarafielStagingServiceName
	s.Declared = s.Deployment.DeepCopy()
	s.Application.SetName("karafiel-staging")
	_ = unstructured.SetNestedField(s.Application.Object, "karafiel-staging", "spec", "project")
	s.Application.SetAnnotations(map[string]string{"argocd.argoproj.io/tracking-id": "enclii-infrastructure:argoproj.io/Application:argocd/karafiel-staging"})
	_ = unstructured.SetNestedMap(s.Application.Object, map[string]any{"repoURL": "https://github.com/madfam-org/karafiel.git", "path": "infra/k8s/overlays/staging", "targetRevision": "release/karafiel-staging"}, "spec", "source")
	_ = unstructured.SetNestedField(s.Application.Object, ns, "spec", "destination", "namespace")
	_ = unstructured.SetNestedField(s.Application.Object, false, "spec", "syncPolicy", "automated", "enabled")
	source, _, _ := unstructured.NestedMap(s.Application.Object, "spec", "source")
	_ = unstructured.SetNestedMap(s.Application.Object, source, "status", "sync", "comparedTo", "source")
	_ = unstructured.SetNestedField(s.Application.Object, "Synced", "status", "sync", "status")
	_ = unstructured.SetNestedSlice(s.Application.Object, []any{map[string]any{"group": "apps", "kind": "Deployment", "namespace": ns, "name": "karafiel-api"}}, "status", "resources")
	s.SourceRoot = &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "enclii-infrastructure", "namespace": "argocd", "uid": "root:fixture", "resourceVersion": "5"},
		"spec":     map[string]any{"source": map[string]any{"repoURL": "https://github.com/madfam-org/enclii.git", "path": "infra/argocd/apps", "targetRevision": "main"}},
		"status":   map[string]any{"sync": map[string]any{"status": "Synced"}, "resources": []any{map[string]any{"group": "argoproj.io", "kind": "Application", "namespace": "argocd", "name": "karafiel-staging", "status": "Synced"}}},
	}}
	rootSource, _, _ := unstructured.NestedMap(s.SourceRoot.Object, "spec", "source")
	_ = unstructured.SetNestedMap(s.SourceRoot.Object, rootSource, "status", "sync", "comparedTo", "source")
	return s
}

func TestKarafielHeldStagePlanUses043WithoutChangingProductionIntent(t *testing.T) {
	s := heldStageFixture()
	before, _ := json.Marshal(s.Registration)
	plan, err := ValidateKarafielStagingSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Identity.SourceAuthority != KarafielStagingHeldAuthority || plan.Identity.Deployment != "karafiel-api" || plan.Identity.ServiceID != s.Service.ID || plan.Identity.Ref != "release/karafiel-staging" {
		t.Fatal("wrong closed authority")
	}
	after, _ := json.Marshal(s.Registration)
	if string(before) != string(after) {
		t.Fatal("production repository registration changed")
	}
	s.Incumbent = &Binding{ID: uuid.New(), Identity: plan.Identity, PlanSHA: plan.SHA, Versions: plan.Versions}
	if _, err = ValidateKarafielStagingSnapshot(s); err != nil {
		t.Fatal("same immutable profile refused", err)
	}
	if _, err = ValidateSnapshot(s, KarafielStagingNamespace, "karafiel-api"); err == nil {
		t.Fatal("generic source intent adopted closed stage authority")
	}
}

func TestKarafielHeldStageProfileRefusesForeignOrUnheldMetadata(t *testing.T) {
	for name, change := range map[string]func(*Snapshot){
		"prod-name":               func(s *Snapshot) { s.Service.Name = "karafiel-api" },
		"auto-deploy":             func(s *Snapshot) { s.Service.AutoDeploy = true },
		"main-source":             func(s *Snapshot) { s.Service.AutoDeployBranch = "main" },
		"production-environment":  func(s *Snapshot) { s.Environment.Name = "production" },
		"foreign-namespace":       func(s *Snapshot) { s.Environment.KubeNamespace = "karafiel" },
		"foreign-repository":      func(s *Snapshot) { s.Service.GitRepo = "https://github.com/fixture/foreign" },
		"foreign-project":         func(s *Snapshot) { s.Service.ProjectID = uuid.New() },
		"foreign-owner":           func(s *Snapshot) { s.Namespace.Labels["enclii.dev/project"] = uuid.NewString() },
		"missing-held-automation": func(s *Snapshot) { unstructured.RemoveNestedField(s.Application.Object, "spec", "syncPolicy") },
		"enabled-automation": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, true, "spec", "syncPolicy", "automated", "enabled")
		},
		"foreign-application-source": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "main", "spec", "source", "targetRevision")
		},
		"foreign-service-label":  func(s *Snapshot) { s.Deployment.Spec.Template.Labels["enclii.dev/service"] = "karafiel-api" },
		"build-command-override": func(s *Snapshot) { s.Service.BuildConfig.BuildArgs = map[string]string{"FIXTURE": "override"} },
		"foreign-name-claim":     func(s *Snapshot) { s.Namespace.Labels["enclii.dev/project"] = "foreign-project" },
		"missing-root":           func(s *Snapshot) { s.SourceRoot = nil },
		"root-uid":               func(s *Snapshot) { s.SourceRoot.SetUID("") },
		"root-rv":                func(s *Snapshot) { s.SourceRoot.SetResourceVersion("") },
		"root-source": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.SourceRoot.Object, "foreign", "spec", "source", "repoURL")
		},
		"root-resource-missing": func(s *Snapshot) { unstructured.RemoveNestedField(s.SourceRoot.Object, "status", "resources") },
		"root-child-tracking":   func(s *Snapshot) { s.Application.SetAnnotations(nil) },
		"root-child-controller": func(s *Snapshot) {
			yes := true
			s.Application.SetOwnerReferences([]metav1.OwnerReference{{Controller: &yes, UID: "foreign"}})
		},
		"root-source-not-synced": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.SourceRoot.Object, "OutOfSync", "status", "sync", "status")
		},
		"declared-active": func(s *Snapshot) { one := int32(1); s.Declared.Spec.Replicas = &one },
		"actual-active":   func(s *Snapshot) { one := int32(1); s.Deployment.Spec.Replicas = &one },
	} {
		t.Run(name, func(t *testing.T) {
			s := heldStageFixture()
			change(&s)
			if _, err := ValidateKarafielStagingSnapshot(s); err == nil {
				t.Fatal("unsafe profile accepted")
			}
		})
	}
}

func TestKarafielHeldPendingNamespaceIsHistoricalAndNotOwnership(t *testing.T) {
	for _, owner := range []string{"", "karafiel"} {
		t.Run("pending-"+owner, func(t *testing.T) {
			s := heldStageFixture()
			s.Namespace.Labels = map[string]string{"enclii.dev/project": owner}
			before, _ := json.Marshal(s.Namespace)
			plan, err := ValidateKarafielStagingSnapshot(s)
			if err != nil || !plan.Identity.NamespaceOwnershipPending {
				t.Fatalf("pending metadata refused: %v", err)
			}
			after, _ := json.Marshal(s.Namespace)
			if string(before) != string(after) {
				t.Fatal("actual namespace mutated")
			}
			if plan.Identity.SourceRootUID != "root:fixture" || plan.Versions.SourceRoot != "5" {
				t.Fatal("root evidence not bound")
			}
			s.Incumbent = &Binding{ID: uuid.New(), Identity: plan.Identity, PlanSHA: plan.SHA, Versions: plan.Versions}
			s.Namespace.Labels = map[string]string{"enclii.dev/project": s.Project.ID.String(), "enclii.dev/verify-signatures": "true"}
			s.Namespace.ResourceVersion = "6"
			adopted, err := ValidateKarafielStagingSnapshot(s)
			if err != nil || !adopted.Identity.NamespaceOwnershipPending || adopted.SHA == plan.SHA {
				t.Fatalf("same UID adoption self-blocked/lost historical fact: %v", err)
			}
			s.Namespace.UID = "other:namespace"
			if _, err = ValidateKarafielStagingSnapshot(s); err == nil {
				t.Fatal("replacement namespace adopted")
			}
		})
	}
}

func TestKarafielHeldRootResourceVersionChangesPlanAndUIDCannotMove(t *testing.T) {
	s := heldStageFixture()
	plan, err := ValidateKarafielStagingSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	s.SourceRoot.SetResourceVersion("7")
	changed, err := ValidateKarafielStagingSnapshot(s)
	if err != nil || changed.SHA == plan.SHA {
		t.Fatal("root version did not bind plan")
	}
	s.Incumbent = &Binding{ID: uuid.New(), Identity: plan.Identity, PlanSHA: plan.SHA, Versions: plan.Versions}
	s.SourceRoot.SetUID("other:root")
	if _, err = ValidateKarafielStagingSnapshot(s); err == nil {
		t.Fatal("replacement root adopted")
	}
}

func TestKarafielHeldAuthorityCannotBeMovedToAnotherTarget(t *testing.T) {
	plan, err := ValidateKarafielStagingSnapshot(heldStageFixture())
	if err != nil {
		t.Fatal(err)
	}
	i := plan.Identity
	i.Ref = "main"
	if i.Validate() == nil {
		t.Fatal("held authority moved to main")
	}
	i = plan.Identity
	i.SourceAuthority = "arbitrary-authority"
	if i.Validate() == nil {
		t.Fatal("unknown source authority")
	}
	old, err := ValidateSnapshot(targetFixture(), "fixture", "fixture-web")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(old.Identity)
	if strings.Contains(string(raw), "source_authority") {
		t.Fatal("legacy identity encoding changed")
	}
}

func TestKarafielHeldStageFreshRootAuthorityWrapperRetainsStrictDirectTracking(t *testing.T) {
	s := heldStageFixture()
	if err := ValidateKarafielStagingRootAuthority(s); err != nil {
		t.Fatal(err)
	}
	s.Application.SetAnnotations(nil)
	if ValidateKarafielStagingRootAuthority(s) == nil {
		t.Fatal("missing exact child tracking accepted")
	}
	s = heldStageFixture()
	delete(s.ArgoConfig.Data, "application.resourceTrackingMethod")
	if ValidateKarafielStagingRootAuthority(s) == nil {
		t.Fatal("absent tracking method inferred")
	}
	s = heldStageFixture()
	_ = unstructured.SetNestedField(s.SourceRoot.Object, "foreign", "status", "sync", "comparedTo", "source", "repoURL")
	if ValidateKarafielStagingRootAuthority(s) == nil {
		t.Fatal("stale root source accepted")
	}
}
