package api

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const restartFixtureNS = "fixture-apps"

func restartFixtureObjects() []runtime.Object {
	two := int32(2)
	controller := true
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "fixture-logs"}}
	template := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{
		Labels:      map[string]string{"app": "fixture"},
		Annotations: map[string]string{"fixture/keep": "yes"},
	}}
	stsOwner := []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "fixture-logs", UID: "uid-sts", Controller: &controller}}
	return []runtime.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "fixture-web", Namespace: restartFixtureNS, UID: "uid-deploy", ResourceVersion: "7"},
			Spec: appsv1.DeploymentSpec{Replicas: &two, Template: *template.DeepCopy()}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "fixture-paused", Namespace: restartFixtureNS, UID: "uid-paused"},
			Spec: appsv1.DeploymentSpec{Paused: true, Template: *template.DeepCopy()}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "fixture-logs", Namespace: restartFixtureNS, UID: "uid-sts"},
			Spec:   appsv1.StatefulSetSpec{Replicas: &two, Selector: selector, Template: *template.DeepCopy()},
			Status: appsv1.StatefulSetStatus{CurrentRevision: "fixture-logs-old", UpdateRevision: "fixture-logs-new", ReadyReplicas: 1}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "fixture-agent", Namespace: restartFixtureNS, UID: "uid-ds"},
			Spec: appsv1.DaemonSetSpec{Template: *template.DeepCopy()}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fixture-logs-0", Namespace: restartFixtureNS, UID: "uid-logs-0", OwnerReferences: stsOwner,
			Labels: map[string]string{"app": "fixture-logs", "controller-revision-hash": "fixture-logs-old"}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "app", RestartCount: 4693}},
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fixture-logs-1", Namespace: restartFixtureNS, UID: "uid-logs-1", OwnerReferences: stsOwner,
			Labels: map[string]string{"app": "fixture-logs", "controller-revision-hash": "fixture-logs-new"}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fixture-bare", Namespace: restartFixtureNS, UID: "uid-bare",
			// Matches the StatefulSet's selector but is not its pod: the plan must not list it.
			Labels: map[string]string{"app": "fixture-logs"}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fixture-job-abc", Namespace: restartFixtureNS, UID: "uid-job",
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "fixture-job", UID: "uid-j", Controller: &controller}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fixture-leaving", Namespace: restartFixtureNS, UID: "uid-leaving", OwnerReferences: stsOwner,
			DeletionTimestamp: &metav1.Time{Time: time.Now()}, Finalizers: []string{"fixture/hold"}}},
	}
}

type restartFixture struct {
	handler   *Handler
	clientset *k8sfake.Clientset
	mock      sqlmock.Sqlmock
	actor     uuid.UUID
	router    *gin.Engine
}

// newRestartFixture wires the adapter behind HandleOpsOperation without the
// route guard; TestOperatorRoutesRequireDatabasePlatformRank covers the guard.
func newRestartFixture(t *testing.T, allowlist string) *restartFixture {
	t.Helper()
	t.Setenv(opsRestartNamespacesEnv, allowlist)
	gin.SetMode(gin.TestMode)
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	clientset := k8sfake.NewSimpleClientset(restartFixtureObjects()...)
	f := &restartFixture{clientset: clientset, mock: mock, actor: uuid.New()}
	f.handler = &Handler{k8sClient: &k8sclient.Client{KubeClient: clientset}, repos: &db.Repositories{AuditLogs: db.NewAuditLogRepository(database)}}
	f.router = gin.New()
	f.router.Use(func(c *gin.Context) {
		c.Set("user_id", f.actor.String())
		c.Set("user_email", "ops@example.test")
		c.Next()
	})
	f.router.POST("/v1/ops/:domain/:action", f.handler.HandleOpsOperation)
	return f
}

func (f *restartFixture) post(t *testing.T, req operatorOperationRequest) (int, operatorOperationResponse) {
	t.Helper()
	req.Operation = opsRestartOperation
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/ops/pods/restart", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httpReq)
	var resp operatorOperationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), rec.Body.String())
	return rec.Code, resp
}

func restartRequest(namespace, target string, apply bool, args ...string) operatorOperationRequest {
	req := operatorOperationRequest{DryRun: !apply, Args: map[string]string{"target": target}}
	if namespace != "" {
		req.Scope = map[string]string{"namespace": namespace}
	}
	if apply {
		req.Reason = "crash-looping pod on the old revision"
	}
	for i := 0; i+1 < len(args); i += 2 {
		req.Args[args[i]] = args[i+1]
	}
	return req
}

// mutations returns the write verbs the fake API server received.
func (f *restartFixture) mutations() []k8stesting.Action {
	out := []k8stesting.Action{}
	for _, action := range f.clientset.Actions() {
		if verb := action.GetVerb(); verb != "get" && verb != "list" && verb != "watch" {
			out = append(out, action)
		}
	}
	return out
}

// restartAuditJSON matches the audit row's context column.
type restartAuditJSON struct{ mode, name, reason, requestAudit string }

func (m restartAuditJSON) Match(value driver.Value) bool {
	raw, ok := value.([]byte)
	if !ok {
		return false
	}
	var ctx map[string]any
	if json.Unmarshal(raw, &ctx) != nil {
		return false
	}
	if m.requestAudit == "present" {
		if id, _ := ctx["request_audit_id"].(string); id == "" {
			return false
		}
	}
	return ctx["operation"] == opsRestartOperation && ctx["mode"] == m.mode && ctx["name"] == m.name &&
		ctx["namespace"] == restartFixtureNS && ctx["reason"] == m.reason && ctx["operation_id"] != nil
}

func (f *restartFixture) expectAudit(outcome, resourceType, resourceID, name, mode string) *sqlmock.ExpectedExec {
	match := restartAuditJSON{mode: mode, name: name, reason: "crash-looping pod on the old revision"}
	if outcome != "requested" {
		match.requestAudit = "present"
	}
	return f.mock.ExpectExec(`INSERT INTO audit_logs`).WithArgs(
		sqlmock.AnyArg(), sqlmock.AnyArg(), f.actor, "ops@example.test", types.Role("platform_admin"), opsRestartOperation,
		resourceType, resourceID, name, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg(), outcome, match, sqlmock.AnyArg())
}

func TestOpsPodsRestartRefusesNamespacesOutsideTheAllowlist(t *testing.T) {
	cases := []struct {
		name, allowlist, namespace, want string
	}{
		{"unset allowlist refuses everything", "", restartFixtureNS, "is empty"},
		{"unlisted namespace", "other-apps", restartFixtureNS, "not in the restart allowlist"},
		{"kube-system even when listed", "kube-system," + restartFixtureNS, "kube-system", "Kubernetes system namespace"},
		{"any kube- namespace", restartFixtureNS, "kube-node-lease", "Kubernetes system namespace"},
	}
	for _, tc := range cases {
		for _, apply := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/dry-run", true: "/apply"}[apply], func(t *testing.T) {
				f := newRestartFixture(t, tc.allowlist)
				status, resp := f.post(t, restartRequest(tc.namespace, "statefulset/fixture-logs", apply))
				require.Equal(t, http.StatusConflict, status)
				assert.Equal(t, "refused", resp.Status)
				assert.Contains(t, resp.Summary, tc.want)
				assert.Contains(t, resp.Summary, "nothing was read or changed")
				assert.Empty(t, f.clientset.Actions(), "a refused namespace must not reach the Kubernetes API")
				require.NoError(t, f.mock.ExpectationsWereMet())
			})
		}
	}
}

func TestOpsPodsRestartValidatesRequest(t *testing.T) {
	cases := []struct {
		name string
		req  operatorOperationRequest
		want string
	}{
		{"namespace required", restartRequest("", "deployment/fixture-web", false), "explicit namespace"},
		{"kind required", restartRequest(restartFixtureNS, "fixture-web", false), "<kind>/<name>"},
		{"unsupported kind", restartRequest(restartFixtureNS, "job/fixture-job", false), "unsupported kind"},
		{"invalid name", restartRequest(restartFixtureNS, "deployment/Fixture_Web", false), "invalid deployment name"},
		{"reason required on apply", operatorOperationRequest{Scope: map[string]string{"namespace": restartFixtureNS}, Args: map[string]string{"target": "deployment/fixture-web"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRestartFixture(t, restartFixtureNS)
			status, resp := f.post(t, tc.req)
			require.Equal(t, http.StatusBadRequest, status)
			assert.Contains(t, resp.Summary, tc.want)
			assert.Empty(t, f.clientset.Actions())
			require.NoError(t, f.mock.ExpectationsWereMet())
		})
	}
}

func TestOpsPodsRestartDryRunShowsPatchAndStuckOrdinal(t *testing.T) {
	f := newRestartFixture(t, restartFixtureNS)
	status, resp := f.post(t, restartRequest(restartFixtureNS, "sts/fixture-logs", false))
	require.Equal(t, http.StatusOK, status)
	assert.True(t, resp.DryRun)
	assert.Equal(t, "ready_to_apply", resp.Status)
	data := resp.Data.(map[string]any)
	assert.Equal(t, "statefulset", data["kind"])
	assert.Equal(t, "uid-sts", data["uid"])
	assert.Equal(t, "OrderedReady", data["podManagementPolicy"])
	assert.Equal(t, "fixture-logs-new", data["updateRevision"])
	annotations := data["patch"].(map[string]any)["spec"].(map[string]any)["template"].(map[string]any)["metadata"].(map[string]any)["annotations"].(map[string]any)
	assert.Contains(t, annotations, restartedAtAnnotation)

	pods := data["pods"].([]any)
	require.Len(t, pods, 2, "only pods the StatefulSet controls, not every pod its selector matches")
	stuck := pods[0].(map[string]any)
	assert.Equal(t, "fixture-logs-0", stuck["name"])
	assert.Equal(t, true, stuck["stuck"])
	assert.Equal(t, float64(4693), stuck["restarts"])
	assert.NotContains(t, pods[1].(map[string]any), "stuck")
	require.Len(t, resp.Warnings, 1)
	assert.Contains(t, resp.Warnings[0], "fixture-logs-0 is not Ready")
	assert.Contains(t, strings.Join(resp.Next, "\n"), "enclii ops pods restart pod/fixture-logs-0 -n fixture-apps --expect-uid uid-logs-0 --apply")

	assert.Empty(t, f.mutations(), "a dry run never writes to Kubernetes")
	require.NoError(t, f.mock.ExpectationsWereMet(), "a dry run writes no audit row")
}

func TestOpsPodsRestartRolloutSendsTheKubectlPatch(t *testing.T) {
	cases := []struct{ target, resource, name, uid string }{
		{"deployment/fixture-web", "deployments", "fixture-web", "uid-deploy"},
		{"statefulset/fixture-logs", "statefulsets", "fixture-logs", "uid-sts"},
		{"ds/fixture-agent", "daemonsets", "fixture-agent", "uid-ds"},
	}
	for _, tc := range cases {
		t.Run(tc.resource, func(t *testing.T) {
			f := newRestartFixture(t, " other-apps, "+restartFixtureNS+" ")
			f.expectAudit("requested", "workload", tc.uid, tc.name, "rollout-restart").WillReturnResult(sqlmock.NewResult(1, 1))
			f.expectAudit("success", "workload", tc.uid, tc.name, "rollout-restart").WillReturnResult(sqlmock.NewResult(1, 1))
			before := time.Now().UTC().Add(-time.Second)

			status, resp := f.post(t, restartRequest(restartFixtureNS, tc.target, true, "expect_uid", tc.uid))
			require.Equal(t, http.StatusAccepted, status, resp.Summary)
			assert.Equal(t, "submitted", resp.Status)
			assert.False(t, resp.DryRun)
			_, err := uuid.Parse(resp.AuditID)
			require.NoError(t, err, "the response names the audit row written before the patch")
			require.NoError(t, f.mock.ExpectationsWereMet())

			mutations := f.mutations()
			require.Len(t, mutations, 1)
			patch, ok := mutations[0].(k8stesting.PatchActionImpl)
			require.True(t, ok)
			assert.Equal(t, tc.resource, patch.GetResource().Resource)
			assert.Equal(t, tc.name, patch.GetName())
			assert.Equal(t, k8stypes.StrategicMergePatchType, patch.PatchType)
			assert.Equal(t, opsRestartFieldManager, patch.PatchOptions.FieldManager)

			// Exactly kubectl rollout restart's body: one template annotation, nothing else.
			var body map[string]any
			require.NoError(t, json.Unmarshal(patch.Patch, &body))
			stamp := body["spec"].(map[string]any)["template"].(map[string]any)["metadata"].(map[string]any)["annotations"].(map[string]any)[restartedAtAnnotation].(string)
			assert.Equal(t, map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
				"annotations": map[string]any{restartedAtAnnotation: stamp}}}}}, body)
			at, err := time.Parse(time.RFC3339, stamp)
			require.NoError(t, err)
			assert.False(t, at.Before(before.Truncate(time.Second)))
			assert.Equal(t, stamp, resp.Data.(map[string]any)["restartedAt"])

			// The live template keeps its other annotations and labels.
			obj, err := f.clientset.Tracker().Get(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: tc.resource}, restartFixtureNS, tc.name)
			require.NoError(t, err)
			var tmpl corev1.PodTemplateSpec
			switch typed := obj.(type) {
			case *appsv1.Deployment:
				tmpl = typed.Spec.Template
			case *appsv1.StatefulSet:
				tmpl = typed.Spec.Template
			case *appsv1.DaemonSet:
				tmpl = typed.Spec.Template
			}
			assert.Equal(t, map[string]string{"fixture/keep": "yes", restartedAtAnnotation: stamp}, tmpl.Annotations)
			assert.Equal(t, map[string]string{"app": "fixture"}, tmpl.Labels)
		})
	}
}

func TestOpsPodsRestartRefusesToMutateWithoutAnAuditRow(t *testing.T) {
	f := newRestartFixture(t, restartFixtureNS)
	f.expectAudit("requested", "workload", "uid-deploy", "fixture-web", "rollout-restart").WillReturnError(errors.New("database unavailable"))
	status, resp := f.post(t, restartRequest(restartFixtureNS, "deployment/fixture-web", true))
	require.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, "failed", resp.Status)
	assert.Contains(t, resp.Summary, "nothing was changed")
	assert.Empty(t, f.mutations())
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsPodsRestartWorkloadRefusals(t *testing.T) {
	cases := []struct{ name, target, want string }{
		{"paused deployment", "deployment/fixture-paused", "is paused"},
		{"recreated since review", "deployment/fixture-web", "recreated since review"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRestartFixture(t, restartFixtureNS)
			status, resp := f.post(t, restartRequest(restartFixtureNS, tc.target, true, "expect_uid", "uid-paused"))
			require.Equal(t, http.StatusConflict, status)
			assert.Equal(t, "refused", resp.Status)
			assert.Contains(t, resp.Summary, tc.want)
			assert.Empty(t, f.mutations())
			require.NoError(t, f.mock.ExpectationsWereMet())
		})
	}
	f := newRestartFixture(t, restartFixtureNS)
	status, resp := f.post(t, restartRequest(restartFixtureNS, "deployment/fixture-missing", true))
	require.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "not_found", resp.Status)
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsPodsRestartDeletesOneControllerOwnedPod(t *testing.T) {
	f := newRestartFixture(t, restartFixtureNS)
	status, plan := f.post(t, restartRequest(restartFixtureNS, "pod/fixture-logs-0", false))
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "ready_to_apply", plan.Status)
	data := plan.Data.(map[string]any)
	assert.Equal(t, "StatefulSet", data["ownerKind"])
	assert.Equal(t, true, data["stuck"])
	assert.Contains(t, plan.Next[0], "--expect-uid uid-logs-0")
	assert.Empty(t, f.mutations())

	f.expectAudit("requested", "pod", "uid-logs-0", "fixture-logs-0", "pod-delete").WillReturnResult(sqlmock.NewResult(1, 1))
	f.expectAudit("success", "pod", "uid-logs-0", "fixture-logs-0", "pod-delete").WillReturnResult(sqlmock.NewResult(1, 1))
	status, resp := f.post(t, restartRequest(restartFixtureNS, "pod/fixture-logs-0", true, "expect_uid", "uid-logs-0"))
	require.Equal(t, http.StatusAccepted, status, resp.Summary)
	assert.Equal(t, "submitted", resp.Status)
	assert.NotEmpty(t, resp.AuditID)
	require.NoError(t, f.mock.ExpectationsWereMet())

	mutations := f.mutations()
	require.Len(t, mutations, 1)
	del, ok := mutations[0].(k8stesting.DeleteActionImpl)
	require.True(t, ok)
	assert.Equal(t, "pods", del.GetResource().Resource)
	assert.Equal(t, "fixture-logs-0", del.GetName())
	require.NotNil(t, del.DeleteOptions.Preconditions)
	assert.Equal(t, k8stypes.UID("uid-logs-0"), *del.DeleteOptions.Preconditions.UID, "only the reviewed pod may be deleted")
	assert.Nil(t, del.DeleteOptions.GracePeriodSeconds, "the pod's own grace period applies; never a force delete")
	_, err := f.clientset.CoreV1().Pods(restartFixtureNS).Get(t.Context(), "fixture-logs-0", metav1.GetOptions{})
	assert.True(t, k8serrors.IsNotFound(err))
	_, err = f.clientset.CoreV1().Pods(restartFixtureNS).Get(t.Context(), "fixture-logs-1", metav1.GetOptions{})
	assert.NoError(t, err, "no other pod is touched")
}

func TestOpsPodsRestartPodRefusals(t *testing.T) {
	cases := []struct{ name, target, expectUID, want string }{
		{"bare pod", "pod/fixture-bare", "", "not owned by a ReplicaSet, StatefulSet or DaemonSet"},
		{"job pod", "pod/fixture-job-abc", "", "not owned by a ReplicaSet, StatefulSet or DaemonSet"},
		{"already terminating", "pod/fixture-leaving", "", "already terminating"},
		{"replaced since review", "pod/fixture-logs-0", "uid-older", "already replaced since review"},
	}
	for _, tc := range cases {
		for _, apply := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/dry-run", true: "/apply"}[apply], func(t *testing.T) {
				f := newRestartFixture(t, restartFixtureNS)
				args := []string{}
				if tc.expectUID != "" {
					args = []string{"expect_uid", tc.expectUID}
				}
				status, resp := f.post(t, restartRequest(restartFixtureNS, tc.target, apply, args...))
				require.Equal(t, http.StatusConflict, status)
				assert.Equal(t, "refused", resp.Status)
				assert.Contains(t, resp.Summary, tc.want)
				assert.Empty(t, f.mutations())
				require.NoError(t, f.mock.ExpectationsWereMet(), "a refusal writes no audit row")
			})
		}
	}
}

func TestOpsPodsRestartRecordsAFailedDelete(t *testing.T) {
	f := newRestartFixture(t, restartFixtureNS)
	f.clientset.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewConflict(schema.GroupResource{Resource: "pods"}, "fixture-logs-0", errors.New("Precondition failed: UID in precondition"))
	})
	f.expectAudit("requested", "pod", "uid-logs-0", "fixture-logs-0", "pod-delete").WillReturnResult(sqlmock.NewResult(1, 1))
	f.expectAudit("failure", "pod", "uid-logs-0", "fixture-logs-0", "pod-delete").WillReturnResult(sqlmock.NewResult(1, 1))
	status, resp := f.post(t, restartRequest(restartFixtureNS, "pod/fixture-logs-0", true))
	require.Equal(t, http.StatusConflict, status)
	assert.Equal(t, "refused", resp.Status)
	assert.Contains(t, resp.Summary, "nothing was changed")
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsPodsRestartReportsForbiddenRBAC(t *testing.T) {
	f := newRestartFixture(t, restartFixtureNS)
	f.clientset.PrependReactor("patch", "statefulsets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "statefulsets"}, "fixture-logs", errors.New("cannot patch"))
	})
	f.expectAudit("requested", "workload", "uid-sts", "fixture-logs", "rollout-restart").WillReturnResult(sqlmock.NewResult(1, 1))
	f.expectAudit("failure", "workload", "uid-sts", "fixture-logs", "rollout-restart").WillReturnResult(sqlmock.NewResult(1, 1))
	status, resp := f.post(t, restartRequest(restartFixtureNS, "statefulset/fixture-logs", true))
	require.Equal(t, http.StatusBadGateway, status)
	assert.Equal(t, "failed", resp.Status)
	assert.Contains(t, resp.Summary, "RBAC")
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsPodsRestartAllowlistParsing(t *testing.T) {
	t.Setenv(opsRestartNamespacesEnv, " b-apps,a-apps\n b-apps  c-apps ,")
	assert.Equal(t, []string{"a-apps", "b-apps", "c-apps"}, opsRestartAllowlist())
	assert.Empty(t, restartNamespaceRefusal("a-apps", opsRestartAllowlist()))
	assert.NotEmpty(t, restartNamespaceRefusal("a-app", opsRestartAllowlist()), "exact names only, no prefix match")
}
