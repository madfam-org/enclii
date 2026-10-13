package workloadtargets

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func targetFixture() Snapshot {
	id := uuid.MustParse("11000000-0000-4000-8000-000000000001")
	ns, app := "fixture", "fixture-services"
	s := Snapshot{
		Project:     &types.Project{ID: id, Slug: ns},
		Service:     &types.Service{ID: uuid.New(), ProjectID: id, Name: "fixture", GitRepo: "https://github.com/madfam-org/fixture"},
		Environment: &types.Environment{ID: uuid.New(), ProjectID: id, Name: "production", KubeNamespace: ns},
		Registration: &types.OnboardingRegistration{ID: uuid.New(), ProjectID: id, RepoFullName: "madfam-org/fixture", ArgocdAppName: &app, OnboardStatus: "completed",
			ConfigSnapshot: map[string]interface{}{"namespace": ns, "manifest_path": "infra/k8s/production", "branch": "main"}},
		Namespace:  &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns, UID: "namespace:fixture", ResourceVersion: "1"}},
		ArgoConfig: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: "argocd", UID: "config:fixture", ResourceVersion: "4"}, Data: map[string]string{"application.resourceTrackingMethod": "annotation"}}, NamespaceIntentExclusive: true,
	}
	s.Deployment = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "fixture-web", Namespace: ns, UID: "deployment:fixture", ResourceVersion: "2",
		Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "fixture-services:apps/Deployment:fixture/fixture-web"}},
		Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "fixture"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "fixture", "enclii.dev/service": "fixture"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "ghcr.io/madfam-org/fixture/web@sha256:" + strings.Repeat("a", 64)}}}}}}
	s.Declared = s.Deployment.DeepCopy()
	s.Application = &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": app, "namespace": "argocd", "uid": "application:fixture", "resourceVersion": "3"},
		"spec": map[string]interface{}{"source": map[string]interface{}{"repoURL": s.Service.GitRepo + ".git", "path": "infra/k8s/production", "targetRevision": "main"},
			"destination": map[string]interface{}{"namespace": ns, "server": "https://kubernetes.default.svc"}},
		"status": map[string]interface{}{"sync": map[string]interface{}{"revision": strings.Repeat("b", 40)},
			"resources": []interface{}{map[string]interface{}{"group": "apps", "kind": "Deployment", "namespace": ns, "name": "fixture-web"}}},
	}}
	source, _, _ := unstructured.NestedMap(s.Application.Object, "spec", "source")
	_ = unstructured.SetNestedMap(s.Application.Object, source, "status", "sync", "comparedTo", "source")
	return s
}

func TestExplicitNamesMayDifferFromRegisteredService(t *testing.T) {
	s := targetFixture()
	plan, err := ValidateSnapshot(s, "fixture", "fixture-web")
	if err != nil || plan.Identity.Deployment != s.Deployment.Name || plan.Identity.NamespaceUID != "namespace:fixture" {
		t.Fatalf("explicit source target refused: %v", err)
	}
	if _, err = ValidateSnapshot(s, "fixture", s.Service.Name); err == nil {
		t.Fatal("service alias silently selected Deployment")
	}
	first := plan.SHA
	s.Deployment.ResourceVersion = "4"
	plan, err = ValidateSnapshot(s, "fixture", "fixture-web")
	if err != nil || first == plan.SHA {
		t.Fatal("review fingerprint did not change with resourceVersion")
	}
}

func TestMissingOrConflictingPersistedIntentNeverAdopts(t *testing.T) {
	for name, change := range map[string]func(*Snapshot){
		"partial registration":         func(s *Snapshot) { s.Registration.OnboardStatus = "failed" },
		"conflicting namespace intent": func(s *Snapshot) { s.NamespaceIntentExclusive = false },
		"registration":                 func(s *Snapshot) { s.Registration = nil },
		"registration namespace":       func(s *Snapshot) { delete(s.Registration.ConfigSnapshot, "namespace") },
		"environment namespace":        func(s *Snapshot) { s.Environment.KubeNamespace = "" },
		"another project":              func(s *Snapshot) { s.Service.ProjectID = uuid.New() },
		"foreign repo":                 func(s *Snapshot) { s.Registration.RepoFullName = "madfam-org/other" },
		"missing ref":                  func(s *Snapshot) { delete(s.Registration.ConfigSnapshot, "branch") },
		"service namespace conflicts":  func(s *Snapshot) { other := "other"; s.Service.K8sNamespace = &other },
	} {
		t.Run(name, func(t *testing.T) {
			s := targetFixture()
			change(&s)
			if !errors.Is(func() error { _, e := ValidateSnapshot(s, "fixture", "fixture-web"); return e }(), ErrIntent) {
				t.Fatal("missing intent adopted")
			}
		})
	}
}

func TestActualOwnershipAndSourceDriftRefuse(t *testing.T) {
	for name, change := range map[string]func(*Snapshot){
		"absent configured tracking": func(s *Snapshot) { delete(s.ArgoConfig.Data, "application.resourceTrackingMethod") },
		"no config UID":              func(s *Snapshot) { s.ArgoConfig.UID = "" },
		"no config RV":               func(s *Snapshot) { s.ArgoConfig.ResourceVersion = "" },
		"no namespace UID":           func(s *Snapshot) { s.Namespace.UID = "" },
		"stale compared source": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "stale/path", "status", "sync", "comparedTo", "source", "path")
		},
		"foreign namespace label": func(s *Snapshot) { s.Namespace.Labels = map[string]string{"enclii.dev/project": uuid.NewString()} },
		"foreign service label":   func(s *Snapshot) { s.Deployment.Spec.Template.Labels["enclii.dev/service"] = "other" },
		"no tracking":             func(s *Snapshot) { s.Deployment.Annotations = nil },
		"nonself tracking": func(s *Snapshot) {
			s.Deployment.Annotations["argocd.argoproj.io/tracking-id"] = "fixture-services:apps/Deployment:other/fixture-web"
		},
		"another installation": func(s *Snapshot) { s.ArgoConfig.Data["installationID"] = "different" },
		"label-only tracking":  func(s *Snapshot) { s.ArgoConfig.Data["application.resourceTrackingMethod"] = "label" },
		"foreign app source": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "https://github.com/other/repo", "spec", "source", "repoURL")
		},
		"multiple sources": func(s *Snapshot) {
			_ = unstructured.SetNestedSlice(s.Application.Object, []interface{}{}, "spec", "sources")
		},
		"resource missing": func(s *Snapshot) {
			_ = unstructured.SetNestedSlice(s.Application.Object, []interface{}{}, "status", "resources")
		},
		"resource duplicated": func(s *Snapshot) {
			rows, _, _ := unstructured.NestedSlice(s.Application.Object, "status", "resources")
			_ = unstructured.SetNestedSlice(s.Application.Object, append(rows, rows[0]), "status", "resources")
		},
		"selector drift":      func(s *Snapshot) { s.Deployment.Spec.Selector.MatchLabels["app"] = "other" },
		"deleting deployment": func(s *Snapshot) { now := metav1.Now(); s.Deployment.DeletionTimestamp = &now },
		"mutable source ref as revision": func(s *Snapshot) {
			_ = unstructured.SetNestedField(s.Application.Object, "main", "status", "sync", "revision")
		},
		"container identity": func(s *Snapshot) { s.Deployment.Spec.Template.Spec.Containers[0].Name = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			s := targetFixture()
			change(&s)
			if _, err := ValidateSnapshot(s, "fixture", "fixture-web"); err == nil {
				t.Fatal("unproven target accepted")
			}
		})
	}
}

func TestStoredBindingDoesNotAdoptRecreatedUID(t *testing.T) {
	s := targetFixture()
	p, err := ValidateSnapshot(s, "fixture", "fixture-web")
	if err != nil {
		t.Fatal(err)
	}
	s.Incumbent = &Binding{ID: uuid.New(), Identity: p.Identity, PlanSHA: p.SHA, Versions: p.Versions}
	_ = unstructured.SetNestedField(s.Application.Object, strings.Repeat("c", 40), "status", "sync", "revision")
	if _, err = ValidateSnapshot(s, "fixture", "fixture-web"); err != nil {
		t.Fatal("normal same-target revision refused")
	}
	s.Deployment.UID = "replacement"
	if _, err = ValidateSnapshot(s, "fixture", "fixture-web"); !errors.Is(err, ErrConflict) {
		t.Fatal("recreated target adopted")
	}
}

func TestStoreOnlyReturnsExactValidatedMetadata(t *testing.T) {
	for _, scenario := range []string{"absent", "unavailable", "valid", "foreign"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			s := targetFixture()
			p, err := ValidateSnapshot(s, "fixture", "fixture-web")
			if err != nil {
				t.Fatal(err)
			}
			b := Binding{ID: uuid.New(), Identity: p.Identity, PlanSHA: p.SHA, Versions: p.Versions}
			if scenario == "foreign" {
				b.Identity.ServiceID = uuid.New()
			}
			query := mock.ExpectQuery(`SELECT payload FROM service_deployment_targets`).WithArgs(s.Service.ID, s.Environment.ID)
			if scenario == "absent" {
				query.WillReturnError(sql.ErrNoRows)
			} else if scenario == "unavailable" {
				query.WillReturnError(errors.New("missing migration"))
			} else {
				raw, _ := json.Marshal(b)
				query.WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(raw))
			}
			got, err := (Store{DB: database}).Get(context.Background(), s.Service.ID, s.Environment.ID)
			if scenario == "valid" && (err != nil || got == nil) {
				t.Fatal("valid metadata unavailable")
			}
			if scenario == "absent" && (err != nil || got != nil) {
				t.Fatal("absence misrepresented")
			}
			if scenario == "foreign" && !errors.Is(err, ErrConflict) {
				t.Fatal("foreign metadata accepted")
			}
			if scenario == "unavailable" && !errors.Is(err, ErrUnavailable) {
				t.Fatal("missing migration became empty healthy metadata")
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNamespaceIntentCheckNeverAdoptsConflictingClaims(t *testing.T) {
	for _, scenario := range []string{"exclusive", "foreign-claim", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			project := uuid.New()
			query := mock.ExpectQuery(`SELECT EXISTS`).WithArgs("fixture", project)
			if scenario == "unavailable" {
				query.WillReturnError(errors.New("read failed"))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(scenario == "foreign-claim"))
			}
			verified, err := (Store{DB: database}).NamespaceIntentExclusive(context.Background(), project, "fixture")
			if scenario == "exclusive" && (err != nil || !verified) {
				t.Fatal("exclusive persisted claim rejected")
			}
			if scenario == "foreign-claim" && (err != nil || verified) {
				t.Fatal("foreign persisted claim adopted")
			}
			if scenario == "unavailable" && (verified || !errors.Is(err, ErrUnavailable)) {
				t.Fatal("unavailable proof became authority")
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestArgoAuthorityIdentityAndReviewedVersionAreSeparate(t *testing.T) {
	s := targetFixture()
	plan, err := ValidateSnapshot(s, "fixture", "fixture-web")
	if err != nil {
		t.Fatal(err)
	}
	s.Incumbent = &Binding{ID: uuid.New(), Identity: plan.Identity, PlanSHA: plan.SHA, Versions: plan.Versions}
	s.ArgoConfig.ResourceVersion = "new-version"
	fresh, err := ValidateSnapshot(s, "fixture", "fixture-web")
	if err != nil || fresh.SHA == plan.SHA || !SameOwner(fresh.Identity, plan.Identity) {
		t.Fatal("unrelated config RV disabled current same authority or failed to change plan hash")
	}
	for _, change := range []func(*Snapshot){
		func(s *Snapshot) { s.ArgoConfig.UID = "new-config" },
		func(s *Snapshot) { s.ArgoConfig.Data["application.resourceTrackingMethod"] = "annotation+label" },
		func(s *Snapshot) {
			s.ArgoConfig.Data["installationID"] = "different"
			s.Deployment.Annotations["argocd.argoproj.io/installation-id"] = "different"
		},
	} {
		s = targetFixture()
		s.Incumbent = &Binding{ID: uuid.New(), Identity: plan.Identity, PlanSHA: plan.SHA, Versions: plan.Versions}
		change(&s)
		if _, err = ValidateSnapshot(s, "fixture", "fixture-web"); !errors.Is(err, ErrConflict) {
			t.Fatal("changed configured authority silently adopted")
		}
	}
}
