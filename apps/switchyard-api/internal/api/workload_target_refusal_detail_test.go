package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func targetSourceFixture() (checks.ManifestFile, checks.ManifestFile) {
	kustomization := checks.ManifestFile{Path: "kustomization.yaml", Content: "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nnamespace: fixture\nresources: [deployment.yaml]\n"}
	deployment := checks.ManifestFile{Path: "deployment.yaml", Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: fixture-web, namespace: fixture}\nspec:\n  selector: {matchLabels: {app: fixture}}\n  template:\n    metadata: {labels: {app: fixture}}\n    spec:\n      containers: [{name: web, image: 'ghcr.io/madfam-org/fixture/web@sha256:" + strings.Repeat("a", 64) + "'}]\n"}
	return kustomization, deployment
}

// Each declared-source refusal names its check. The configMapGenerator case is
// the 2026-10-14 regression: a generator added to a project's production
// kustomization refused every Deployment in it with no detail.
func TestTargetSourceRefusalsNameTheirCheck(t *testing.T) {
	cases := map[string]struct {
		change func(k, d *checks.ManifestFile) []checks.ManifestFile
		want   workloadtargets.Reason
	}{
		"config map generator": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			k.Content += "configMapGenerator:\n- name: script\n  files: [scripts/probe.py]\n"
			return nil
		}, workloadtargets.ReasonKustomizationTransform},
		"name suffix": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			k.Content += "nameSuffix: -other\n"
			return nil
		}, workloadtargets.ReasonKustomizationTransform},
		"kustomization namespace": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			k.Content = strings.Replace(k.Content, "namespace: fixture", "namespace: other", 1)
			return nil
		}, workloadtargets.ReasonKustomizationNamespace},
		"nonlocal resource": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			k.Content = strings.Replace(k.Content, "[deployment.yaml]", "[../deployment.yaml]", 1)
			return nil
		}, workloadtargets.ReasonManifestLayout},
		"no kustomization": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			return []checks.ManifestFile{*d}
		}, workloadtargets.ReasonManifestLayout},
		"unparsable kustomization": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			k.Content = "resources: [\n"
			return nil
		}, workloadtargets.ReasonManifestInvalid},
		"unparsable resource": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			d.Content = "kind: [\n"
			return nil
		}, workloadtargets.ReasonManifestInvalid},
		"target not declared": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			d.Content = strings.Replace(d.Content, "name: fixture-web", "name: fixture-api", 1)
			return nil
		}, workloadtargets.ReasonDeclaredManifestMissing},
		"target declared twice": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			d.Content += "---\n" + d.Content
			return nil
		}, workloadtargets.ReasonDeclaredDeploymentRepeat},
		"target in another namespace": {func(k, d *checks.ManifestFile) []checks.ManifestFile {
			k.Content = strings.Replace(k.Content, "namespace: fixture\n", "", 1)
			d.Content = strings.Replace(d.Content, "namespace: fixture", "namespace: other", 1)
			return nil
		}, workloadtargets.ReasonDeclaredDeploymentShape},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			k, d := targetSourceFixture()
			files := tc.change(&k, &d)
			if files == nil {
				files = []checks.ManifestFile{k, d}
			}
			_, err := declaredTarget(files, "fixture", "fixture-web")
			require.ErrorIs(t, err, workloadtargets.ErrOwnership)
			require.Equal(t, workloadtargets.ErrOwnership.Error(), err.Error())
			got, ok := workloadtargets.RefusalReason(err)
			require.True(t, ok, "refusal carries no detail")
			require.Equal(t, tc.want, got)
		})
	}
	k, d := targetSourceFixture()
	target, err := declaredTarget([]checks.ManifestFile{k, d}, "fixture", "fixture-web")
	require.NoError(t, err)
	require.Equal(t, "fixture-web", target.Name)
}

func nativeTargetObjects(revision string) (*corev1.Namespace, *appsv1.Deployment, *unstructured.Unstructured, *corev1.ConfigMap) {
	app := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]interface{}{"name": "fixture-services", "namespace": "argocd"},
		"status":   map[string]interface{}{"sync": map[string]interface{}{"revision": revision}},
	}}
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fixture"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "fixture-web", Namespace: "fixture"}},
		app,
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: "argocd"}}
}

// The four fixed reads each name themselves; the API server error stays in the
// logged-only cause (here: an RBAC refusal of argocd-cm).
func TestTargetNativeReadsNameTheirRead(t *testing.T) {
	request := workloadtargets.Request{Namespace: "fixture", Deployment: "fixture-web"}
	gvrs := map[schema.GroupVersionResource]string{argoApplicationGVR: "ApplicationList"}
	sha := strings.Repeat("b", 40)
	for name, tc := range map[string]struct {
		present  int
		revision string
		forbid   bool
		want     workloadtargets.Reason
	}{
		"namespace missing":     {present: 0, revision: sha, want: workloadtargets.ReasonNamespaceRead},
		"deployment missing":    {present: 1, revision: sha, want: workloadtargets.ReasonDeploymentRead},
		"application missing":   {present: 2, revision: sha, want: workloadtargets.ReasonApplicationRead},
		"argo config missing":   {present: 3, revision: sha, want: workloadtargets.ReasonArgoConfigRead},
		"argo config forbidden": {present: 4, revision: sha, forbid: true, want: workloadtargets.ReasonArgoConfigRead},
		"mutable sync revision": {present: 4, revision: "main", want: workloadtargets.ReasonSyncRevision},
		"all readable":          {present: 4, revision: sha},
	} {
		t.Run(name, func(t *testing.T) {
			ns, dep, app, cm := nativeTargetObjects(tc.revision)
			typed := []k8sruntime.Object{}
			var dynamicObjects []k8sruntime.Object
			if tc.present > 0 {
				typed = append(typed, ns)
			}
			if tc.present > 1 {
				typed = append(typed, dep)
			}
			if tc.present > 2 {
				dynamicObjects = append(dynamicObjects, app)
			}
			if tc.present > 3 {
				typed = append(typed, cm)
			}
			kube := fake.NewSimpleClientset(typed...)
			if tc.forbid {
				kube.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
					return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "argocd-cm", errors.New("no role"))
				})
			}
			dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(k8sruntime.NewScheme(), gvrs, dynamicObjects...)
			s := workloadtargets.Snapshot{}
			revision, err := readTargetNativeObjects(context.Background(), kube, dyn, "fixture-services", request, &s)
			if tc.want == "" {
				require.NoError(t, err)
				require.Equal(t, sha, revision)
				require.NotNil(t, s.ArgoConfig)
				return
			}
			require.ErrorIs(t, err, workloadtargets.ErrOwnership)
			got, ok := workloadtargets.RefusalReason(err)
			require.True(t, ok)
			require.Equal(t, tc.want, got)
			if tc.forbid {
				require.True(t, apierrors.IsForbidden(workloadtargets.RefusalCause(err)))
			}
			require.NotContains(t, err.Error(), "no role", "the API server error must not reach Error()")
		})
	}
}

func bindTargetDryRun(t *testing.T, reader func(context.Context, workloadtargets.Request) (workloadtargets.Snapshot, error)) (int, map[string]interface{}) {
	t.Helper()
	h, mock, cleanup := setupTenantScopeHandler(t)
	defer cleanup()
	h.auth = &auth.JWTManager{}
	user := uuid.New()
	mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(user).WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
	h.workloadTargetSnapshotReader = reader
	router := gin.New()
	router.Use(withUserContext(user, "admin"))
	registerOperatorRoutes(router.Group("/v1"), h)
	body := `{"dry_run":true,"scope":{"project":"fixture","service":"11000000-0000-4000-8000-000000000002","environment":"production"},"args":{"namespace":"fixture","deployment":"fixture-web"}}`
	request := httptest.NewRequest(http.MethodPost, "/v1/ops/apps/bind-target", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.NoError(t, mock.ExpectationsWereMet())
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	return response.Code, result
}

// The refusal keeps its status, error text and refusal_code, and adds the
// value-free refusal_detail when the refusing check is known.
func TestBindTargetRefusalCarriesDetail(t *testing.T) {
	const message = "actual workload ownership/source could not be verified"
	t.Run("reader refusal", func(t *testing.T) {
		status, body := bindTargetDryRun(t, func(context.Context, workloadtargets.Request) (workloadtargets.Snapshot, error) {
			return workloadtargets.Snapshot{}, workloadtargets.RefuseOwnershipCause(workloadtargets.ReasonArgoConfigRead, errors.New("forbidden"))
		})
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "ownership_unverified", body["refusal_code"])
		require.Equal(t, message, body["error"])
		require.Equal(t, "argo_config_read", body["refusal_detail"])
	})
	t.Run("snapshot verification refusal", func(t *testing.T) {
		status, body := bindTargetDryRun(t, func(_ context.Context, r workloadtargets.Request) (workloadtargets.Snapshot, error) {
			s := planSnapshot(r)
			s.Deployment.Spec.Template.Spec.Containers[0].Name = "other"
			return s, nil
		})
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "ownership_unverified", body["refusal_code"])
		require.Equal(t, message, body["error"])
		require.Equal(t, "container_names", body["refusal_detail"])
	})
	t.Run("bare ownership refusal has no detail", func(t *testing.T) {
		status, body := bindTargetDryRun(t, func(context.Context, workloadtargets.Request) (workloadtargets.Snapshot, error) {
			return workloadtargets.Snapshot{}, workloadtargets.ErrOwnership
		})
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "ownership_unverified", body["refusal_code"])
		require.NotContains(t, body, "refusal_detail")
	})
	t.Run("intent refusal unchanged", func(t *testing.T) {
		status, body := bindTargetDryRun(t, func(context.Context, workloadtargets.Request) (workloadtargets.Snapshot, error) {
			return workloadtargets.Snapshot{}, workloadtargets.ErrIntent
		})
		require.Equal(t, http.StatusConflict, status)
		require.Equal(t, "ownership_unverified", body["refusal_code"])
		require.Equal(t, "persisted registration/environment ownership intent missing or conflicting", body["error"])
		require.NotContains(t, body, "refusal_detail")
	})
}
