package api

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const (
	cnpgFixtureProject = "0a1b2c3d-1111-4222-8333-444455556666"
	cnpgFixtureNS      = "project-0a1b2c3d"
	cnpgFixtureCluster = "pg-kernel-9508ca59"
	cnpgFixturePod     = "pg-kernel-9508ca59-1"
)

func cnpgFixturePodObject(name, cluster, podRole, ownerUID string, ready bool) *corev1.Pod {
	controller := true
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	grace := int64(1800)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cnpgFixtureNS, UID: k8stypes.UID("uid-" + name),
			Labels: map[string]string{cnpgClusterPodLabel: cluster, cnpgPodRoleLabel: podRole, cnpgInstanceNameLabel: name, cnpgInstanceRoleLabel: "primary"},
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "postgresql.cnpg.io/v1", Kind: "Cluster", Name: cluster,
				UID: k8stypes.UID(ownerUID), Controller: &controller}}},
		Spec: corev1.PodSpec{TerminationGracePeriodSeconds: &grace, Volumes: []corev1.Volume{
			{Name: "pgdata", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name}}},
			{Name: "scratch-data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}},
	}
}

func cnpgFixtureClusterObject(name, uid string, instances int64, annotations map[string]string, labels map[string]string) *unstructured.Unstructured {
	if labels == nil {
		labels = map[string]string{"managed-by": "enclii", "enclii.dev/project-id": cnpgFixtureProject, "enclii.dev/addon-type": "postgres"}
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1", "kind": "Cluster",
		"metadata": map[string]any{"name": name, "namespace": cnpgFixtureNS, "uid": uid},
		"spec":     map[string]any{"instances": instances},
		"status":   map[string]any{"phase": "Cluster in healthy state", "readyInstances": int64(0)},
	}}
	obj.SetLabels(labels)
	if annotations != nil {
		obj.SetAnnotations(annotations)
	}
	return obj
}

func cnpgFixtureObjects() []runtime.Object {
	controller := true
	return []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cnpgFixtureNS, Labels: map[string]string{
			"managed-by": "enclii", k8sclient.LabelProjectNamespace: cnpgFixtureProject, "enclii.dev/type": "application"}}},
		// Not Ready: the 2026-10-24 I/O-error instance.
		cnpgFixturePodObject(cnpgFixturePod, cnpgFixtureCluster, "instance", "uid-cluster", false),
		cnpgFixturePodObject("pg-healthy-1", "pg-healthy", "instance", "uid-healthy", true),
		cnpgFixturePodObject("pg-ha-1", "pg-ha", "instance", "uid-ha", false),
		cnpgFixturePodObject("pg-kernel-9508ca59-pooler-rw-x", cnpgFixtureCluster, "pooler", "uid-cluster", true),
		cnpgFixturePodObject("pg-hib-1", "pg-hib", "instance", "uid-hib", false),
		cnpgFixturePodObject("pg-foreign-1", "pg-foreign", "instance", "uid-foreign", false),
		cnpgFixturePodObject("pg-replaced-1", "pg-replaced", "instance", "uid-old-cluster", false),
		cnpgFixtureMislabelledPod(),
		// An application pod in the same project namespace: never this path's.
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-abc", Namespace: cnpgFixtureNS, UID: "uid-web",
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "web-rs", UID: "uid-rs", Controller: &controller}}}},
		// A project-looking namespace Enclii did not create.
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "project-deadbeef"}},
		cnpgFixturePodInNamespace("project-deadbeef"),
		// Labelled for another project than its name says.
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "project-feedface", Labels: map[string]string{
			"managed-by": "enclii", k8sclient.LabelProjectNamespace: cnpgFixtureProject}}},
		cnpgFixturePodInNamespace("project-feedface"),
	}
}

// cnpgFixtureMislabelledPod is owned by the kernel Cluster but labelled for
// another one.
func cnpgFixtureMislabelledPod() *corev1.Pod {
	pod := cnpgFixturePodObject("pg-mislabelled-1", cnpgFixtureCluster, "instance", "uid-cluster", false)
	pod.Labels[cnpgClusterPodLabel] = "some-other-cluster"
	return pod
}

func cnpgFixturePodInNamespace(namespace string) *corev1.Pod {
	pod := cnpgFixturePodObject(cnpgFixturePod, cnpgFixtureCluster, "instance", "uid-cluster", false)
	pod.Namespace = namespace
	return pod
}

func newCNPGRestartFixture(t *testing.T) (*restartFixture, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	// The allowlist holds a platform namespace, as production does; project
	// namespaces are never listed.
	f := newRestartFixture(t, "logging")
	for _, obj := range cnpgFixtureObjects() {
		require.NoError(t, f.clientset.Tracker().Add(obj))
	}
	f.clientset.ClearActions()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{restartCNPGClusterGVR: "ClusterList"},
		cnpgFixtureClusterObject(cnpgFixtureCluster, "uid-cluster", 1, nil, nil),
		cnpgFixtureClusterObject("pg-healthy", "uid-healthy", 1, nil, nil),
		cnpgFixtureClusterObject("pg-ha", "uid-ha", 2, nil, nil),
		cnpgFixtureClusterObject("pg-hib", "uid-hib", 1, map[string]string{"cnpg.io/hibernation": "on"}, nil),
		cnpgFixtureClusterObject("pg-foreign", "uid-foreign", 1, nil, map[string]string{"managed-by": "someone-else"}),
		cnpgFixtureClusterObject("pg-replaced", "uid-new-cluster", 1, nil, nil),
	)
	f.handler.k8sClient.DynamicClient = dyn
	return f, dyn
}

// cnpgAuditJSON matches the audit row's context for the CNPG path.
type cnpgAuditJSON struct{ name, requestAudit string }

func (m cnpgAuditJSON) Match(value driver.Value) bool {
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
	return ctx["operation"] == opsRestartOperation && ctx["mode"] == restartModeCNPGInstance && ctx["name"] == m.name &&
		ctx["namespace"] == cnpgFixtureNS && ctx["owner_kind"] == "Cluster" && ctx["cluster_uid"] == "uid-cluster" &&
		ctx["project"] == cnpgFixtureProject && ctx["expect_uid"] == "uid-"+m.name && ctx["reason"] != ""
}

func (f *restartFixture) expectCNPGAudit(outcome, name string) *sqlmock.ExpectedExec {
	match := cnpgAuditJSON{name: name}
	if outcome != "requested" {
		match.requestAudit = "present"
	}
	return f.mock.ExpectExec(`INSERT INTO audit_logs`).WithArgs(
		sqlmock.AnyArg(), sqlmock.AnyArg(), f.actor, "ops@example.test", types.Role("platform_admin"), opsRestartOperation,
		"pod", "uid-"+name, name, nil, nil, sqlmock.AnyArg(), sqlmock.AnyArg(), outcome, match, sqlmock.AnyArg())
}

func TestOpsPodsRestartCNPGInstanceDryRunThenApply(t *testing.T) {
	f, _ := newCNPGRestartFixture(t)
	status, plan := f.post(t, restartRequest(cnpgFixtureNS, "pod/"+cnpgFixturePod, false))
	require.Equal(t, http.StatusOK, status, plan.Summary)
	assert.Equal(t, "ready_to_apply", plan.Status)
	data := plan.Data.(map[string]any)
	assert.Equal(t, restartModeCNPGInstance, data["mode"])
	assert.Equal(t, cnpgFixtureCluster, data["cluster"])
	assert.Equal(t, "uid-cluster", data["clusterUID"])
	assert.Equal(t, "Cluster in healthy state", data["clusterPhase"])
	assert.Equal(t, cnpgFixtureProject, data["project"])
	assert.Equal(t, []any{cnpgFixturePod}, data["pvcsKept"])
	assert.Contains(t, plan.Summary, "same PVCs "+cnpgFixturePod)
	assert.Contains(t, plan.Next[0], "--expect-uid uid-"+cnpgFixturePod)
	require.Len(t, plan.Warnings, 1)
	assert.Contains(t, plan.Warnings[0], "runs one instance")
	assert.Empty(t, f.mutations(), "a dry run changes nothing")

	f.expectCNPGAudit("requested", cnpgFixturePod).WillReturnResult(sqlmock.NewResult(1, 1))
	f.expectCNPGAudit("success", cnpgFixturePod).WillReturnResult(sqlmock.NewResult(1, 1))
	status, resp := f.post(t, restartRequest(cnpgFixtureNS, "pod/"+cnpgFixturePod, true, "expect_uid", "uid-"+cnpgFixturePod))
	require.Equal(t, http.StatusAccepted, status, resp.Summary)
	assert.Equal(t, "submitted", resp.Status)
	assert.NotEmpty(t, resp.AuditID)
	require.NoError(t, f.mock.ExpectationsWereMet())

	mutations := f.mutations()
	require.Len(t, mutations, 1, "exactly one write: the pod delete")
	del, ok := mutations[0].(k8stesting.DeleteActionImpl)
	require.True(t, ok)
	assert.Equal(t, "pods", del.GetResource().Resource)
	assert.Equal(t, cnpgFixturePod, del.GetName())
	require.NotNil(t, del.DeleteOptions.Preconditions)
	assert.Equal(t, k8stypes.UID("uid-"+cnpgFixturePod), *del.DeleteOptions.Preconditions.UID)
	assert.Nil(t, del.DeleteOptions.GracePeriodSeconds, "the pod's own grace period; never a force delete")
	for _, action := range f.clientset.Actions() {
		assert.NotEqual(t, "persistentvolumeclaims", action.GetResource().Resource, "the PVC is never read or written")
	}
	_, err := f.clientset.CoreV1().Pods(cnpgFixtureNS).Get(t.Context(), "pg-healthy-1", metav1.GetOptions{})
	assert.NoError(t, err, "no other instance is touched")
}

func TestOpsPodsRestartCNPGApplyRequiresAReviewedUID(t *testing.T) {
	f, _ := newCNPGRestartFixture(t)
	status, resp := f.post(t, restartRequest(cnpgFixtureNS, "pod/"+cnpgFixturePod, true))
	require.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, resp.Summary, "requires --expect-uid")
	assert.Empty(t, f.mutations())
	require.NoError(t, f.mock.ExpectationsWereMet(), "no audit row without a reviewed UID")

	status, resp = f.post(t, restartRequest(cnpgFixtureNS, "pod/"+cnpgFixturePod, true, "expect_uid", "uid-something-older"))
	require.Equal(t, http.StatusConflict, status)
	assert.Contains(t, resp.Summary, "already replaced since review")
	assert.Empty(t, f.mutations())
	require.NoError(t, f.mock.ExpectationsWereMet())
}

func TestOpsPodsRestartCNPGWarnings(t *testing.T) {
	f, _ := newCNPGRestartFixture(t)
	_, plan := f.post(t, restartRequest(cnpgFixtureNS, "pod/pg-healthy-1", false))
	require.Equal(t, "ready_to_apply", plan.Status)
	assert.Contains(t, plan.Warnings[0], "is Ready")

	_, plan = f.post(t, restartRequest(cnpgFixtureNS, "pod/pg-ha-1", false))
	require.Equal(t, "ready_to_apply", plan.Status)
	require.Len(t, plan.Warnings, 1)
	assert.Contains(t, plan.Warnings[0], "fails over to a standby")
	assert.Empty(t, f.mutations())
}

func TestOpsPodsRestartCNPGRefusals(t *testing.T) {
	cases := []struct{ name, namespace, target, want string }{
		{"pooler pod", cnpgFixtureNS, "pod/pg-kernel-9508ca59-pooler-rw-x", `not a database instance (cnpg.io/podRole="pooler")`},
		{"application pod", cnpgFixtureNS, "pod/web-abc", "not controlled by a CloudNativePG Cluster"},
		{"labels name another cluster", cnpgFixtureNS, "pod/pg-mislabelled-1", "does not name its owning Cluster"},
		{"hibernating cluster", cnpgFixtureNS, "pod/pg-hib-1", "is hibernating"},
		{"cluster not an Enclii addon", cnpgFixtureNS, "pod/pg-foreign-1", "is not an Enclii addon of project"},
		{"cluster replaced under the pod", cnpgFixtureNS, "pod/pg-replaced-1", "was replaced"},
		{"namespace not managed by Enclii", "project-deadbeef", "pod/" + cnpgFixturePod, "is not managed by Enclii"},
		{"namespace name and project label disagree", "project-feedface", "pod/" + cnpgFixturePod, "does not match its project label"},
	}
	for _, tc := range cases {
		for _, apply := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/dry-run", true: "/apply"}[apply], func(t *testing.T) {
				f, _ := newCNPGRestartFixture(t)
				status, resp := f.post(t, restartRequest(tc.namespace, tc.target, apply, "expect_uid", "uid-x"))
				require.Equal(t, http.StatusConflict, status, resp.Summary)
				assert.Equal(t, "refused", resp.Status)
				assert.Contains(t, resp.Summary, tc.want)
				assert.Contains(t, resp.Summary, "not in the restart allowlist")
				assert.Contains(t, resp.Summary, "nothing was changed")
				assert.Empty(t, f.mutations())
				require.NoError(t, f.mock.ExpectationsWereMet(), "a refusal writes no audit row")
			})
		}
	}
}

func TestOpsPodsRestartProjectNamespaceWorkloadsStayRefused(t *testing.T) {
	// The exception covers pod/<name> only: a rollout restart in a project
	// namespace is refused before anything is read, exactly as before.
	f, _ := newCNPGRestartFixture(t)
	for _, target := range []string{"statefulset/" + cnpgFixtureCluster, "deployment/web"} {
		status, resp := f.post(t, restartRequest(cnpgFixtureNS, target, false))
		require.Equal(t, http.StatusConflict, status)
		assert.Contains(t, resp.Summary, "nothing was read or changed")
	}
	assert.Empty(t, f.clientset.Actions())
}

func TestOpsPodsRestartCNPGNeedsTheDynamicClient(t *testing.T) {
	f, _ := newCNPGRestartFixture(t)
	f.handler.k8sClient.DynamicClient = nil
	status, resp := f.post(t, restartRequest(cnpgFixtureNS, "pod/"+cnpgFixturePod, false))
	require.Equal(t, http.StatusServiceUnavailable, status)
	assert.Equal(t, "adapter_unconfigured", resp.Status)
	assert.Empty(t, f.mutations())
}

func TestOpsPodsRestartCNPGTerminatingAndMissingPods(t *testing.T) {
	f, _ := newCNPGRestartFixture(t)
	leaving := cnpgFixturePodObject("pg-leaving-1", cnpgFixtureCluster, "instance", "uid-cluster", false)
	leaving.Labels[cnpgInstanceNameLabel] = "pg-leaving-1"
	leaving.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	leaving.Finalizers = []string{"fixture/hold"}
	require.NoError(t, f.clientset.Tracker().Add(leaving))

	status, resp := f.post(t, restartRequest(cnpgFixtureNS, "pod/pg-leaving-1", false))
	require.Equal(t, http.StatusConflict, status)
	assert.Contains(t, resp.Summary, "already terminating")

	status, resp = f.post(t, restartRequest(cnpgFixtureNS, "pod/pg-gone-1", false))
	require.Equal(t, http.StatusNotFound, status)
	assert.Equal(t, "not_found", resp.Status)
	assert.Empty(t, f.mutations())
}

func TestCNPGInstancePodRefusalNeedsTheCNPGGroup(t *testing.T) {
	pod := cnpgFixturePodObject(cnpgFixturePod, cnpgFixtureCluster, "instance", "uid-cluster", false)
	pod.OwnerReferences[0].APIVersion = "example.com/v1"
	why, owner := cnpgInstancePodRefusal(pod)
	assert.Nil(t, owner)
	assert.Contains(t, why, "not controlled by a CloudNativePG Cluster")
}

func TestOpsPodsRestartCNPGRefusesToMutateWithoutAnAuditRow(t *testing.T) {
	f, _ := newCNPGRestartFixture(t)
	f.expectCNPGAudit("requested", cnpgFixturePod).WillReturnError(assert.AnError)
	status, resp := f.post(t, restartRequest(cnpgFixtureNS, "pod/"+cnpgFixturePod, true, "expect_uid", "uid-"+cnpgFixturePod))
	require.Equal(t, http.StatusServiceUnavailable, status)
	assert.Contains(t, resp.Summary, "nothing was changed")
	assert.Empty(t, f.mutations(), "no audit row, no delete")
	require.NoError(t, f.mock.ExpectationsWereMet())
}
