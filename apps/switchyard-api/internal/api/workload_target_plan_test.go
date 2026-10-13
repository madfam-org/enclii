package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func planSnapshot(r workloadtargets.Request) workloadtargets.Snapshot {
	project := uuid.MustParse("11000000-0000-4000-8000-000000000001")
	app := "fixture-services"
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: r.Deployment, Namespace: r.Namespace, UID: "deployment:fixture", ResourceVersion: "2",
		Annotations: map[string]string{"argocd.argoproj.io/tracking-id": app + ":apps/Deployment:" + r.Namespace + "/" + r.Deployment}},
		Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "fixture"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "fixture", "enclii.dev/service": "fixture"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "ghcr.io/madfam-org/fixture/web@sha256:" + strings.Repeat("a", 64)}}}}}}
	snapshot := workloadtargets.Snapshot{Project: &types.Project{ID: project, Slug: r.Project}, Service: &types.Service{ID: r.ServiceID, ProjectID: project, Name: "fixture", GitRepo: "https://github.com/madfam-org/fixture"},
		Environment: &types.Environment{ID: uuid.New(), ProjectID: project, Name: r.Environment, KubeNamespace: r.Namespace},
		Registration: &types.OnboardingRegistration{ID: uuid.New(), ProjectID: project, RepoFullName: "madfam-org/fixture", ArgocdAppName: &app, OnboardStatus: "completed",
			ConfigSnapshot: map[string]interface{}{"namespace": r.Namespace, "manifest_path": "infra/k8s/production", "branch": "main"}},
		Namespace:  &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, UID: "namespace:fixture", ResourceVersion: "1"}},
		Deployment: d, Declared: d.DeepCopy(), ArgoConfig: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: "argocd", UID: "config:fixture", ResourceVersion: "4"}, Data: map[string]string{"application.resourceTrackingMethod": "annotation"}}, NamespaceIntentExclusive: true,
		Application: &unstructured.Unstructured{Object: map[string]interface{}{
			"metadata": map[string]interface{}{"name": app, "namespace": "argocd", "uid": "application:fixture", "resourceVersion": "3"},
			"spec":     map[string]interface{}{"source": map[string]interface{}{"repoURL": "https://github.com/madfam-org/fixture.git", "path": "infra/k8s/production", "targetRevision": "main"}, "destination": map[string]interface{}{"namespace": r.Namespace, "server": "https://kubernetes.default.svc"}},
			"status":   map[string]interface{}{"sync": map[string]interface{}{"revision": strings.Repeat("b", 40)}, "resources": []interface{}{map[string]interface{}{"group": "apps", "kind": "Deployment", "namespace": r.Namespace, "name": r.Deployment}}},
		}},
	}
	source, _, _ := unstructured.NestedMap(snapshot.Application.Object, "spec", "source")
	_ = unstructured.SetNestedMap(snapshot.Application.Object, source, "status", "sync", "comparedTo", "source")
	return snapshot
}

func TestTargetPlanRouteRequiresActualOperatorRegardlessOfFlag(t *testing.T) {
	for _, flag := range []string{"false", "unset"} {
		t.Run(flag, func(t *testing.T) {
			if flag == "unset" {
				old, exists := os.LookupEnv("ENCLII_TENANT_SCOPE_ENFORCE")
				require.NoError(t, os.Unsetenv("ENCLII_TENANT_SCOPE_ENFORCE"))
				t.Cleanup(func() {
					if exists {
						_ = os.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", old)
					} else {
						_ = os.Unsetenv("ENCLII_TENANT_SCOPE_ENFORCE")
					}
				})
			} else {
				t.Setenv("ENCLII_TENANT_SCOPE_ENFORCE", flag)
			}
			for _, operator := range []bool{false, true} {
				t.Run(map[bool]string{false: "tenant-admin", true: "actual-operator"}[operator], func(t *testing.T) {
					h, mock, cleanup := setupTenantScopeHandler(t)
					defer cleanup()
					h.auth = &auth.JWTManager{}
					user := uuid.New()
					mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(operator))
					reads := 0
					h.workloadTargetSnapshotReader = func(_ context.Context, r workloadtargets.Request) (workloadtargets.Snapshot, error) {
						reads++
						return planSnapshot(r), nil
					}
					router := gin.New()
					router.Use(withUserContext(user, "admin"))
					registerOperatorRoutes(router.Group("/v1"), h)
					body := `{"dry_run":true,"scope":{"project":"fixture","service":"11000000-0000-4000-8000-000000000002","environment":"production"},"args":{"namespace":"fixture","deployment":"fixture-web"}}`
					request := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/bind-target", strings.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					if operator {
						require.Equal(t, http.StatusOK, response.Code, response.Body.String())
						require.Equal(t, 1, reads)
						var result map[string]interface{}
						require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
						require.Equal(t, "planned", result["status"])
						require.True(t, result["dry_run"].(bool))
					} else {
						require.Equal(t, http.StatusForbidden, response.Code)
						require.Zero(t, reads)
					}
					require.NoError(t, mock.ExpectationsWereMet())
				})
			}
		})
	}
}

func TestTargetPlanApplyAndInvalidBodiesNeverReachReaders(t *testing.T) {
	valid := `{"dry_run":true,"scope":{"project":"fixture","service":"11000000-0000-4000-8000-000000000002","environment":"production"},"args":{"namespace":"fixture","deployment":"fixture-web"}}`
	for name, body := range map[string]string{
		"apply":                   strings.Replace(valid, `"dry_run":true`, `"dry_run":false`, 1),
		"duplicate":               strings.Replace(valid, `"dry_run":true`, `"dry_run":true,"dry_run":false`, 1),
		"body operation mismatch": strings.Replace(valid, `{"dry_run"`, `{"operation":"ops.jobs.suspend","dry_run"`, 1),
		"label selector":          strings.Replace(valid, `"deployment":"fixture-web"`, `"selector":"app=fixture"`, 1),
		"unknown outer field":     strings.Replace(valid, `{"dry_run"`, `{"raw_sql":"invalid","dry_run"`, 1),
		"missing dry run":         strings.Replace(valid, `"dry_run":true,`, "", 1),
	} {
		t.Run(name, func(t *testing.T) {
			h, mock, cleanup := setupTenantScopeHandler(t)
			defer cleanup()
			h.auth = &auth.JWTManager{}
			user := uuid.New()
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
			h.workloadTargetSnapshotReader = func(context.Context, workloadtargets.Request) (workloadtargets.Snapshot, error) {
				t.Fatal("reader reached by apply/invalid input")
				return workloadtargets.Snapshot{}, nil
			}
			router := gin.New()
			router.Use(withUserContext(user, "admin"))
			registerOperatorRoutes(router.Group("/v1"), h)
			request := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/bind-target", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			want := http.StatusBadRequest
			if name == "apply" {
				want = http.StatusBadRequest
			}
			require.Equal(t, want, response.Code, response.Body.String())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestTargetSourceOnlyUsesExplicitFlatResources(t *testing.T) {
	kustomization := checks.ManifestFile{Path: "kustomization.yaml", Content: "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nnamespace: fixture\nresources: [deployment.yaml]\n"}
	deployment := checks.ManifestFile{Path: "deployment.yaml", Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: fixture-web, namespace: fixture}\nspec:\n  selector: {matchLabels: {app: fixture}}\n  template:\n    metadata: {labels: {app: fixture}}\n    spec:\n      containers: [{name: web, image: 'ghcr.io/madfam-org/fixture/web@sha256:" + strings.Repeat("a", 64) + "'}]\n"}
	deferred := deployment
	deferred.Path = "deferred.yaml"
	target, err := declaredTarget([]checks.ManifestFile{kustomization, deployment, deferred}, "fixture", "fixture-web")
	require.NoError(t, err)
	require.Equal(t, "fixture-web", target.Name)
	for name, change := range map[string]func(*checks.ManifestFile){
		"duplicate referenced target": func(k *checks.ManifestFile) {
			k.Content = strings.Replace(k.Content, "[deployment.yaml]", "[deployment.yaml, deferred.yaml]", 1)
		},
		"remote resource": func(k *checks.ManifestFile) {
			k.Content = strings.Replace(k.Content, "[deployment.yaml]", "[../deployment.yaml]", 1)
		},
		"unknown transform": func(k *checks.ManifestFile) { k.Content += "nameSuffix: -other\n" },
	} {
		t.Run(name, func(t *testing.T) {
			k := kustomization
			change(&k)
			_, err := declaredTarget([]checks.ManifestFile{k, deployment, deferred}, "fixture", "fixture-web")
			require.Error(t, err)
		})
	}
}
