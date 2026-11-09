package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubetypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func adoptionNamespace() (*corev1.Namespace, *corev1.ConfigMap) {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: addons.KarafielStagingNamespace, UID: "stage-namespace", ResourceVersion: "1", Labels: map[string]string{"app.kubernetes.io/instance": "karafiel-staging", "enclii.dev/project": "karafiel", "example.org/preserve": "unchanged"}, Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "karafiel-staging:/Namespace:/enclii-karafiel-staging", "argocd.argoproj.io/installation-id": "installation-one", "argocd.argoproj.io/sync-options": "Prune=false", "example.org/preserve": "unchanged"}}, Spec: corev1.NamespaceSpec{Finalizers: []corev1.FinalizerName{"kubernetes"}}}, &corev1.ConfigMap{Data: map[string]string{"application.resourceTrackingMethod": "annotation+label", "installationID": "installation-one"}}
}

func TestKarafielAdoptionDetachesOnlyExactArgoMarkersAndPreservesNamespace(t *testing.T) {
	namespace, config := adoptionNamespace()
	before := namespace.DeepCopy()
	orphaned, detached, err := karafielOrphanStageNamespace(namespace, config)
	require.NoError(t, err)
	require.False(t, detached)
	require.Equal(t, before, namespace, "planning is read-only")
	require.Equal(t, "unchanged", orphaned.Labels["example.org/preserve"])
	require.Equal(t, "unchanged", orphaned.Annotations["example.org/preserve"])
	require.Equal(t, "karafiel", orphaned.Labels["enclii.dev/project"])
	require.Equal(t, "Prune=false", orphaned.Annotations["argocd.argoproj.io/sync-options"])
	require.Equal(t, before.Spec, orphaned.Spec)
	require.Empty(t, orphaned.Labels["app.kubernetes.io/instance"])
	require.Empty(t, orphaned.Annotations["argocd.argoproj.io/tracking-id"])
	_, detached, err = karafielOrphanStageNamespace(orphaned, config)
	require.NoError(t, err)
	require.True(t, detached)
	for _, mode := range []string{"foreign-tracker", "foreign-installation", "foreign-label", "unknown-method"} {
		t.Run(mode, func(t *testing.T) {
			n, c := adoptionNamespace()
			switch mode {
			case "foreign-tracker":
				n.Annotations["argocd.argoproj.io/tracking-id"] = "foreign:/Namespace:/enclii-karafiel-staging"
			case "foreign-installation":
				n.Annotations["argocd.argoproj.io/installation-id"] = "foreign"
			case "foreign-label":
				n.Labels["app.kubernetes.io/instance"] = "foreign"
			case "unknown-method":
				delete(c.Data, "application.resourceTrackingMethod")
			}
			_, _, err := karafielOrphanStageNamespace(n, c)
			require.Error(t, err)
		})
	}
	for _, value := range []string{"", "Prune=true", "Prune=false,Prune=true", "Prune=false,Prune=false"} {
		require.False(t, karafielPruneProtected(value))
	}
	require.True(t, karafielPruneProtected("ApplyOutOfSyncOnly=true, Prune=false"))
	changed := orphaned.DeepCopy()
	changed.ResourceVersion = "2"
	changed.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "controller"}}
	require.Equal(t, karafielNamespaceMetadataHash(orphaned), karafielNamespaceMetadataHash(changed), "no volatile/GC metadata in the poststate hash")
	changed.Labels["example.org/preserve"] = "drift"
	require.NotEqual(t, karafielNamespaceMetadataHash(orphaned), karafielNamespaceMetadataHash(changed))
}

func TestKarafielAdoptionRejectsUnexpectedTrackedResourceAndNeverNeedsServiceOrDB(t *testing.T) {
	base := []interface{}{map[string]interface{}{"kind": "Namespace", "version": "v1", "name": addons.KarafielStagingNamespace}, map[string]interface{}{"group": "kyverno.io", "kind": "PolicyException", "namespace": addons.KarafielStagingNamespace, "name": karafielLegacyException}, map[string]interface{}{"group": "batch", "kind": "Job", "namespace": addons.KarafielStagingNamespace, "name": "karafiel-migrate"}}
	app := &unstructured.Unstructured{Object: map[string]interface{}{"status": map[string]interface{}{"resources": base}}}
	require.True(t, karafielLegacyResourceInventoryClosed(app, false, true))
	for _, row := range []map[string]interface{}{{"kind": "Namespace", "name": "karafiel"}, {"group": "kyverno.io", "kind": "PolicyException", "namespace": addons.KarafielStagingNamespace, "name": "foreign"}, {"group": "apps", "kind": "Deployment", "namespace": addons.KarafielStagingNamespace, "name": "foreign"}, {"group": "rbac.authorization.k8s.io", "kind": "RoleBinding", "namespace": addons.KarafielStagingNamespace, "name": "foreign"}, {"group": "batch", "kind": "Job", "namespace": addons.KarafielStagingNamespace, "name": "foreign"}} {
		other := app.DeepCopy()
		require.NoError(t, unstructured.SetNestedSlice(other.Object, append(append([]interface{}{}, base...), row), "status", "resources"))
		require.False(t, karafielLegacyResourceInventoryClosed(other, false, true))
	}
	h := &Handler{}
	_, err := h.readKarafielStageAdoption(context.Background())
	require.Error(t, err)
}

type adoptionFixture struct {
	h                                  *Handler
	kube                               *fake.Clientset
	dynamic                            *dynamicfake.FakeDynamicClient
	read                               func(context.Context) (*karafielStageAdoptionSnapshot, error)
	namespaceUpdates, exceptionDeletes int
}

func newAdoptionFixture(t *testing.T, mode string) *adoptionFixture {
	t.Helper()
	n, config := adoptionNamespace()
	kube := fake.NewSimpleClientset(n)
	exception := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "kyverno.io/v2beta1", "kind": "PolicyException", "metadata": map[string]interface{}{"name": karafielLegacyException, "namespace": n.Name, "uid": "legacy-exception", "resourceVersion": "1"}, "spec": map[string]interface{}{"exceptions": []interface{}{map[string]interface{}{"policyName": "legacy-review-fixture"}}}}}
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{karafielExceptionGVR: "PolicyExceptionList"}, exception)
	f := &adoptionFixture{kube: kube, dynamic: dynamic}
	f.h = &Handler{k8sClient: &k8s.Client{KubeClient: kube, DynamicClient: dynamic}}
	// No repository/service/addon exists in this mutation fixture. The private
	// reader seam models the independently tested mandatory authority snapshot;
	// production always passes the actual root/ACL/UID reader.
	f.read = func(ctx context.Context) (*karafielStageAdoptionSnapshot, error) {
		actual, e := kube.CoreV1().Namespaces().Get(ctx, n.Name, metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		orphaned, detached, e := karafielOrphanStageNamespace(actual, config)
		if e != nil {
			return nil, e
		}
		ex, e := dynamic.Resource(karafielExceptionGVR).Namespace(n.Name).Get(ctx, karafielLegacyException, metav1.GetOptions{})
		if apierrors.IsNotFound(e) {
			ex = nil
		} else if e != nil {
			return nil, e
		}
		authority := strings.Repeat("a", 64)
		if mode == "source-drift" && f.namespaceUpdates > 0 {
			authority = strings.Repeat("b", 64)
		}
		return &karafielStageAdoptionSnapshot{Observation: &karafielStageAdoptionObservation{Schema: karafielAdoptionProfile, AuthoritySHA256: authority, NamespaceUID: string(actual.UID), NamespaceRV: actual.ResourceVersion, PruneProtected: true, TrackingDetached: detached, ExceptionAbsent: ex == nil}, Namespace: actual, Orphaned: orphaned, Exception: ex}, nil
	}
	kube.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		object := action.(ktesting.CreateAction).GetObject().(*corev1.ConfigMap)
		object.UID = kubetypes.UID("fixture-" + object.Name)
		object.ResourceVersion = "1"
		return false, nil, nil
	})
	kube.PrependReactor("update", "namespaces", func(action ktesting.Action) (bool, runtime.Object, error) {
		f.namespaceUpdates++
		actual := action.(ktesting.UpdateAction).GetObject().(*corev1.Namespace)
		require.Equal(t, "stage-namespace", string(actual.UID))
		require.Equal(t, "1", actual.ResourceVersion)
		require.Equal(t, "unchanged", actual.Labels["example.org/preserve"])
		require.Equal(t, "Prune=false", actual.Annotations["argocd.argoproj.io/sync-options"])
		if mode == "namespace-timeout-uncommitted" {
			return true, nil, errors.New("synthetic unknown update")
		}
		actual.ResourceVersion = "2"
		if mode == "namespace-timeout-committed" {
			require.NoError(t, kube.Tracker().Update(corev1.SchemeGroupVersion.WithResource("namespaces"), actual, ""))
			return true, nil, errors.New("synthetic committed timeout")
		}
		return false, nil, nil
	})
	dynamic.PrependReactor("delete", "policyexceptions", func(action ktesting.Action) (bool, runtime.Object, error) {
		f.exceptionDeletes++
		deleteAction := action.(ktesting.DeleteAction)
		require.Equal(t, n.Name, deleteAction.GetNamespace())
		require.Equal(t, karafielLegacyException, deleteAction.GetName())
		options := action.(ktesting.DeleteActionImpl).GetDeleteOptions()
		require.NotNil(t, options.Preconditions)
		require.Equal(t, kubetypes.UID("legacy-exception"), *options.Preconditions.UID)
		require.Equal(t, "1", *options.Preconditions.ResourceVersion)
		if mode == "delete-timeout-uncommitted" {
			return true, nil, errors.New("synthetic unknown delete")
		}
		if mode == "delete-timeout-committed" {
			require.NoError(t, dynamic.Tracker().Delete(karafielExceptionGVR, n.Name, karafielLegacyException))
			return true, nil, errors.New("synthetic committed timeout")
		}
		return false, nil, nil
	})
	return f
}

func TestKarafielAdoptionDryPlanCASDeleteRetentionAndUnknownOutcomesAreBounded(t *testing.T) {
	for _, mode := range []string{"success", "namespace-timeout-committed", "delete-timeout-committed", "namespace-timeout-uncommitted", "delete-timeout-uncommitted", "source-drift"} {
		t.Run(mode, func(t *testing.T) {
			f := newAdoptionFixture(t, mode)
			actor, operation := uuid.New(), uuid.New()
			reason := "reviewed exact legacy boundary"
			result, err := f.h.applyKarafielStageAdoptionWithReader(t.Context(), actor, operation, reason, "", true, f.read)
			require.NoError(t, err)
			plan := result.(gin.H)["plan"].(*karafielStageAdoptionPlan)
			require.Zero(t, f.namespaceUpdates)
			require.Zero(t, f.exceptionDeletes)
			for _, action := range f.kube.Actions() {
				require.Contains(t, []string{"get", "list"}, action.GetVerb())
			}
			result, err = f.h.applyKarafielStageAdoptionWithReader(t.Context(), actor, operation, reason, plan.PlanSHA256, false, f.read)
			accepted := mode == "success" || mode == "namespace-timeout-committed" || mode == "delete-timeout-committed"
			if accepted {
				require.NoError(t, err)
				receipt := result.(*karafielStageAdoptionReceipt)
				require.True(t, receipt.TrackingDetached)
				require.True(t, receipt.ExceptionAbsent)
			} else {
				require.Error(t, err)
			}
			updates, deletes := f.namespaceUpdates, f.exceptionDeletes
			_, again := f.h.applyKarafielStageAdoptionWithReader(t.Context(), actor, operation, reason, plan.PlanSHA256, false, f.read)
			if accepted {
				require.NoError(t, again)
			} else {
				require.Error(t, again)
			}
			require.Equal(t, updates, f.namespaceUpdates, "unknown namespace update is never blindly retried")
			require.Equal(t, deletes, f.exceptionDeletes, "unknown exception delete is never blindly retried")
			if !accepted {
				other := uuid.New()
				fresh, e := f.read(t.Context())
				require.NoError(t, e)
				otherPlan := karafielAdoptionPlan(fresh, other, actor, reason)
				_, e = f.h.applyKarafielStageAdoptionWithReader(t.Context(), actor, other, reason, otherPlan.PlanSHA256, false, f.read)
				require.Error(t, e, "new key cannot bypass the retained single-operation claim")
				require.Equal(t, updates, f.namespaceUpdates)
				require.Equal(t, deletes, f.exceptionDeletes)
			}
			for _, action := range f.kube.Actions() {
				if action.GetVerb() == "update" {
					require.Equal(t, "namespaces", action.GetResource().Resource)
				}
				if action.GetVerb() == "create" {
					require.Equal(t, "configmaps", action.GetResource().Resource)
				}
			}
			for _, action := range f.dynamic.Actions() {
				require.Contains(t, []string{"get", "delete"}, action.GetVerb())
				if action.GetVerb() == "delete" {
					require.Equal(t, "policyexceptions", action.GetResource().Resource)
				}
			}
		})
	}
}

func TestKarafielAdoptionRecordRejectsTamperAndKeepsOnlyBoundedHashes(t *testing.T) {
	n, _ := adoptionNamespace()
	operation := uuid.New()
	row, err := karafielAdoptionRecord(n, operation, "intent", map[string]string{"plan_sha256": strings.Repeat("a", 64)})
	require.NoError(t, err)
	row.UID = "receipt"
	row.ResourceVersion = "1"
	require.True(t, karafielAdoptionRecordExact(row, row.DeepCopy()))
	for i, mutation := range []func(*corev1.ConfigMap){func(c *corev1.ConfigMap) { c.Data["payload"] += " " }, func(c *corev1.ConfigMap) { c.OwnerReferences[0].UID = "foreign" }, func(c *corev1.ConfigMap) { c.Immutable = nil }, func(c *corev1.ConfigMap) { c.Labels["enclii.dev/managed-by"] = "foreign" }} {
		changed := row.DeepCopy()
		mutation(changed)
		require.False(t, karafielAdoptionRecordExact(changed, row), strconv.Itoa(i))
	}
	body, err := json.Marshal(row)
	require.NoError(t, err)
	require.NotContains(t, string(body), "example.org/preserve")
	require.NotContains(t, string(body), "DB_PASSWORD")
}

func TestKarafielAdoptionActualReaderRequiresFreshRootACLTrackingAndPruneProof(t *testing.T) {
	for _, mode := range []string{"accepted", "no-prune", "foreign-owner", "root-tracking", "unknown-tracking", "held-automation", "foreign-exception"} {
		t.Run(mode, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { require.NoError(t, database.Close()); require.NoError(t, mock.ExpectationsWereMet()) }()
			project, environment := uuid.New(), uuid.New()
			now := time.Now()
			// These two reads are the whole database dependency: no Service registration,
			// addon registry, runtime credentials, provider or original financial records.
			mock.ExpectQuery(`FROM projects WHERE slug`).WithArgs("karafiel").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}).AddRow(project, "karafiel", "karafiel", "github", now, now))
			mock.ExpectQuery(`FROM environments WHERE project_id`).WithArgs(project, "staging").WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "kube_namespace", "created_at", "updated_at"}).AddRow(environment, project, "staging", addons.KarafielStagingNamespace, now, now))
			mock.ExpectClose()
			namespace, config := adoptionNamespace()
			config.Name = "argocd-cm"
			config.Namespace = "argocd"
			config.UID = "argo-config"
			config.ResourceVersion = "1"
			app := releaseRouteApp()
			app.SetAnnotations(map[string]string{"argocd.argoproj.io/tracking-id": "enclii-infrastructure:argoproj.io/Application:argocd/karafiel-staging", "argocd.argoproj.io/installation-id": "installation-one"})
			source, _, _ := unstructured.NestedMap(app.Object, "spec", "source")
			require.NoError(t, unstructured.SetNestedMap(app.Object, source, "status", "sync", "comparedTo", "source"))
			resources := []interface{}{map[string]interface{}{"kind": "Namespace", "name": addons.KarafielStagingNamespace}, map[string]interface{}{"group": "kyverno.io", "kind": "PolicyException", "name": karafielLegacyException, "namespace": addons.KarafielStagingNamespace}}
			require.NoError(t, unstructured.SetNestedSlice(app.Object, resources, "status", "resources"))
			rootSource := map[string]interface{}{"repoURL": "https://github.com/madfam-org/enclii.git", "path": "infra/argocd/apps", "targetRevision": "main"}
			root := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]interface{}{"name": "enclii-infrastructure", "namespace": "argocd", "uid": "root-uid", "resourceVersion": "1"}, "spec": map[string]interface{}{"source": rootSource}, "status": map[string]interface{}{"sync": map[string]interface{}{"status": "Synced", "comparedTo": map[string]interface{}{"source": rootSource}}, "resources": []interface{}{map[string]interface{}{"group": "argoproj.io", "kind": "Application", "namespace": "argocd", "name": "karafiel-staging", "status": "Synced"}}}}}
			restricted := releaseRouteProject()
			a := restricted.GetAnnotations()
			a["argocd.argoproj.io/installation-id"] = "installation-one"
			restricted.SetAnnotations(a)
			exception := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "kyverno.io/v2beta1", "kind": "PolicyException", "metadata": map[string]interface{}{"name": karafielLegacyException, "namespace": namespace.Name, "uid": "legacy-exception", "resourceVersion": "1", "annotations": map[string]interface{}{"argocd.argoproj.io/tracking-id": "karafiel-staging:kyverno.io/PolicyException:" + namespace.Name + "/" + karafielLegacyException, "argocd.argoproj.io/installation-id": "installation-one"}}, "spec": map[string]interface{}{"private-fixture": "not-exported"}}}
			switch mode {
			case "no-prune":
				delete(namespace.Annotations, "argocd.argoproj.io/sync-options")
			case "foreign-owner":
				namespace.Labels["enclii.dev/project"] = uuid.NewString()
			case "root-tracking":
				app.SetAnnotations(nil)
			case "unknown-tracking":
				delete(config.Data, "application.resourceTrackingMethod")
			case "held-automation":
				require.NoError(t, unstructured.SetNestedField(app.Object, true, "spec", "syncPolicy", "automated", "enabled"))
			case "foreign-exception":
				exception.SetName("foreign-exception")
			}
			kube := fake.NewSimpleClientset(namespace, config)
			dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{karafielExceptionGVR: "PolicyExceptionList"}, app, root, restricted, exception)
			h := &Handler{repos: db.NewRepositories(database), k8sClient: &k8s.Client{KubeClient: kube, DynamicClient: dynamic}}
			snapshot, err := h.readKarafielStageAdoptionSnapshot(t.Context())
			if mode == "accepted" {
				require.NoError(t, err)
				require.False(t, snapshot.Observation.TrackingDetached)
				require.False(t, snapshot.Observation.ExceptionAbsent)
				plan := karafielAdoptionPlan(snapshot, uuid.New(), uuid.New(), "reviewed scoped deletion")
				body, _ := json.Marshal(plan)
				require.NotContains(t, string(body), "not-exported")
			} else {
				require.Error(t, err)
				require.Nil(t, snapshot)
			}
			for _, action := range kube.Actions() {
				require.Contains(t, []string{"get", "list"}, action.GetVerb())
			}
			for _, action := range dynamic.Actions() {
				require.Contains(t, []string{"get", "list"}, action.GetVerb())
			}
		})
	}
}
