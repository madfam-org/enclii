package api

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"net/http"
	"sigs.k8s.io/yaml"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func productionBudgetFleetFixture() []workloadtargets.Snapshot {
	base := planSnapshot(workloadtargets.Request{ServiceID: uuid.New(), Project: "karafiel", Namespace: "karafiel", Deployment: "karafiel-api", Environment: "production"})
	base.Registration.RepoFullName = "madfam-org/karafiel"
	base.Application.SetAPIVersion("argoproj.io/v1alpha1")
	base.Application.SetKind("Application")
	_ = unstructured.SetNestedField(base.Application.Object, "Synced", "status", "sync", "status")
	base.SourceRoot = &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": "enclii-infrastructure", "namespace": "argocd", "uid": "root:fixture", "resourceVersion": "5"},
		"spec":     map[string]any{"source": map[string]any{"repoURL": "https://github.com/madfam-org/enclii.git", "path": "infra/argocd/apps", "targetRevision": "main"}},
		"status":   map[string]any{"sync": map[string]any{"status": "Synced"}},
	}}
	rootSource, _, _ := unstructured.NestedMap(base.SourceRoot.Object, "spec", "source")
	_ = unstructured.SetNestedMap(base.SourceRoot.Object, rootSource, "status", "sync", "comparedTo", "source")
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
	fleet := []workloadtargets.Snapshot{}
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

func productionBudgetExpectSQL(mock sqlmock.Sqlmock, fleet []workloadtargets.Snapshot, otherClaim bool) {
	base := fleet[0]
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`FROM projects WHERE slug = \$1`).WithArgs("karafiel").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(base.Project.ID, "fixture", "karafiel", "", now, now))
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).WithArgs(base.Project.ID, "production").WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).AddRow(base.Environment.ID, base.Project.ID, "production", "karafiel", now, now))
	raw, _ := json.Marshal(base.Registration.ConfigSnapshot)
	mock.ExpectQuery(`FROM onboarding_registrations WHERE repo_full_name = \$1`).WithArgs("madfam-org/karafiel").WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_full_name", "webhook_id", "webhook_secret", "argocd_app_name", "onboard_status", "config_snapshot", "error_message", "created_at", "updated_at"}).AddRow(base.Registration.ID, base.Project.ID, "madfam-org/karafiel", nil, nil, "karafiel-services", "completed", raw, nil, now, now))
	columns := make([]string, 29)
	for n := range columns {
		columns[n] = fmt.Sprintf("column_%d", n)
	}
	rows := sqlmock.NewRows(columns)
	for _, s := range fleet {
		rows.AddRow([]driver.Value{s.Service.ID, s.Project.ID, s.Service.Name, "", "", []byte(`{}`), true, "main", "production", "karafiel", "healthy", "ready", 1, 1, "", nil, nil, nil, nil, nil, nil, nil, nil, []byte(`[]`), now, now, []byte(`[]`), "web", ""}...)
	}
	mock.ExpectQuery(`FROM services s WHERE s.project_id = \$1`).WithArgs(base.Project.ID).WillReturnRows(rows)
	mock.ExpectQuery(`SELECT EXISTS`).WithArgs("karafiel", base.Project.ID).WillReturnRows(sqlmock.NewRows([]string{"other"}).AddRow(otherClaim))
	if !otherClaim {
		for _, s := range fleet {
			mock.ExpectQuery(`SELECT payload FROM service_deployment_targets WHERE service_id=\$1 AND environment_id=\$2`).WithArgs(s.Service.ID, s.Environment.ID).WillReturnRows(sqlmock.NewRows([]string{"payload"}))
		}
	}
}

func TestProductionImmutableSourceReuseKeepsSQLAndNativeObservationsFresh(t *testing.T) {
	for _, scenario := range []string{"stable", "deployment-rv", "namespace-claim"} {
		t.Run(scenario, func(t *testing.T) {
			fleet := productionBudgetFleetFixture()
			_, err := workloadtargets.ValidateKarafielProductionFleet(fleet)
			require.NoError(t, err, "native authority fixture must begin valid")
			files, listing := productionGitFixture()
			for _, s := range fleet {
				d := s.Declared.DeepCopy()
				d.APIVersion = "apps/v1"
				d.Kind = "Deployment"
				raw, err := yaml.Marshal(d)
				require.NoError(t, err)
				files[d.Name+".yaml"] = string(raw)
			}
			for n, e := range listing {
				if content, ok := files[e.Name]; ok {
					listing[n] = productionGitFile(karafielProductionManifestPath+"/"+e.Name, content).karafielProductionGitEntry
				}
			}
			var githubReads atomic.Int64
			source := productionGitReader(func(req *http.Request) (*http.Response, error) {
				githubReads.Add(1)
				if strings.Contains(req.URL.Path, "/enclii/") {
					raw, _ := json.Marshal(fleet[0].SourceConfiguration)
					return productionGitJSON(productionGitFile(karafielProductionConfigPath, string(raw))), nil
				}
				if strings.HasSuffix(req.URL.Path, "/production") {
					return productionGitJSON(listing), nil
				}
				name := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
				return productionGitJSON(productionGitFile(karafielProductionManifestPath+"/"+name, files[name])), nil
			})
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer database.Close()
			base := fleet[0]
			objects := []runtime.Object{base.Namespace.DeepCopy(), base.ArgoConfig.DeepCopy()}
			for _, s := range fleet {
				objects = append(objects, s.Deployment.DeepCopy())
			}
			kube := kubefake.NewSimpleClientset(objects...)
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), base.Application.DeepCopy(), base.SourceRoot.DeepCopy(), base.SourceParent.DeepCopy())
			h := &Handler{repos: db.NewRepositories(database), config: &config.Config{GitHubToken: "synthetic-fixture"}, k8sClient: &k8s.Client{KubeClient: kube, DynamicClient: dynamic}}
			productionBudgetExpectSQL(mock, fleet, false)
			first, err := h.readKarafielProductionTargetSnapshotsWithSource(context.Background(), nil, source)
			require.NoError(t, err)
			before, err := workloadtargets.ValidateKarafielProductionFleet(first)
			require.NoError(t, err)
			require.EqualValues(t, 39, githubReads.Load())
			// These are genuine fake-client updates between separate observations; no
			// result snapshot is injected and no SQL/native call is memoized.
			if scenario == "deployment-rv" {
				d := fleet[0].Deployment.DeepCopy()
				d.ResourceVersion = "changed-fixture-rv"
				_, err = kube.AppsV1().Deployments("karafiel").Update(context.Background(), d, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			kube.ClearActions()
			dynamic.ClearActions()
			productionBudgetExpectSQL(mock, fleet, scenario == "namespace-claim")
			second, err := h.readKarafielProductionTargetSnapshotsWithSource(context.Background(), nil, source)
			require.EqualValues(t, 39, githubReads.Load(), "only exact immutable Git bytes may be reused")
			if scenario == "namespace-claim" {
				require.Equal(t, workloadtargets.ErrIntent, err)
				require.Nil(t, second)
			} else {
				require.NoError(t, err)
				after, err := workloadtargets.ValidateKarafielProductionFleet(second)
				require.NoError(t, err)
				if scenario == "stable" {
					require.Equal(t, before, after)
				} else {
					require.NotEqual(t, before[0].SHA, after[0].SHA, "reviewed plan must become stale when fresh native resourceVersion changes")
				}
				require.Len(t, kube.Actions(), 7, "fresh Namespace/ConfigMap/five Deployment reads required")
				require.Len(t, dynamic.Actions(), 3, "fresh production/root/ApplicationSet reads required")
				for _, a := range kube.Actions() {
					require.Equal(t, "get", a.GetVerb())
				}
				for _, a := range dynamic.Actions() {
					require.Equal(t, "get", a.GetVerb())
				}
			}
			require.NoError(t, mock.ExpectationsWereMet(), "all registry/namespace-claim/incumbent SQL reads must repeat")
		})
	}
}
