package addons

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const hibNS = "fixture-data"

func hibAddon() *types.DatabaseAddon {
	return &types.DatabaseAddon{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000a1"), Type: types.DatabaseAddonTypePostgres, K8sNamespace: hibNS, K8sResourceName: "pg-kernel09b-fixture"}
}

const extractionError = "Instance Status Extraction Error: HTTP communication issue"

// hibCluster is a fixture Cluster in the given CNPG phase; hibernated sets the
// cnpg.io/hibernation condition to True.
func hibCluster(annotations map[string]string) *unstructured.Unstructured {
	return hibClusterIn(annotations, cnpgHealthyPhase, false)
}

func hibClusterIn(annotations map[string]string, phase string, hibernated bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{"status": map[string]interface{}{"phase": phase, "conditions": []interface{}{
		map[string]interface{}{"type": "Ready", "status": "True"},
		map[string]interface{}{"type": CNPGHibernationAnnotation, "status": map[bool]string{true: "True", false: "False"}[hibernated]},
	}}}}
	u.SetAPIVersion("postgresql.cnpg.io/v1")
	u.SetKind("Cluster")
	u.SetNamespace(hibNS)
	u.SetName("pg-kernel09b-fixture")
	u.SetUID("uid-cluster")
	u.SetResourceVersion("41")
	u.SetAnnotations(annotations)
	return u
}

func hibClients(t *testing.T, cluster *unstructured.Unstructured, exporterReplicas *int32) (*dynamicfake.FakeDynamicClient, *k8sfake.Clientset) {
	t.Helper()
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{cnpgGVR: "ClusterList"}, cluster)
	objects := []runtime.Object{
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pg-kernel09b-fixture-1", Namespace: hibNS, UID: "uid-pod-1", Labels: map[string]string{cnpgClusterLabel: "pg-kernel09b-fixture", cnpgInstanceRoleLabel: "primary"}}},
		// A backup Job pod of the same cluster is not an instance.
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pg-kernel09b-fixture-backup-x", Namespace: hibNS, UID: "uid-pod-b", Labels: map[string]string{cnpgClusterLabel: "pg-kernel09b-fixture"}}},
		// Another cluster's instance in the same namespace is never touched.
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pg-other-1", Namespace: hibNS, UID: "uid-other", Labels: map[string]string{cnpgClusterLabel: "pg-other", cnpgInstanceRoleLabel: "primary"}}},
	}
	if exporterReplicas != nil {
		objects = append(objects, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pg-kernel09b-fixture-exporter", Namespace: hibNS}, Spec: appsv1.DeploymentSpec{Replicas: exporterReplicas}})
	}
	return dyn, k8sfake.NewSimpleClientset(objects...)
}

func one() *int32 { v := int32(1); return &v }

// fastHibernationWait shortens the apply's bounded wait for the test.
func fastHibernationWait(t *testing.T) {
	t.Helper()
	timeout, interval := hibernationConfirmTimeout, hibernationPollInterval
	hibernationConfirmTimeout, hibernationPollInterval = 300*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { hibernationConfirmTimeout, hibernationPollInterval = timeout, interval })
}

// cnpgActsOnAnnotation simulates the operator: when the annotation lands, the
// hibernation condition turns True and the instance pod goes away.
func cnpgActsOnAnnotation(dyn *dynamicfake.FakeDynamicClient, kube *k8sfake.Clientset) {
	dyn.PrependReactor("update", "clusters", func(action k8stesting.Action) (bool, runtime.Object, error) {
		u := action.(k8stesting.UpdateAction).GetObject().(*unstructured.Unstructured)
		if u.GetAnnotations()[CNPGHibernationAnnotation] == "on" {
			_ = unstructured.SetNestedSlice(u.Object, []interface{}{map[string]interface{}{"type": CNPGHibernationAnnotation, "status": "True"}}, "status", "conditions")
			_ = kube.Tracker().Delete(schema.GroupVersionResource{Version: "v1", Resource: "pods"}, hibNS, "pg-kernel09b-fixture-1")
		}
		return false, nil, nil
	})
}

func writesIn(actions []k8stesting.Action) []string {
	out := []string{}
	for _, a := range actions {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			out = append(out, a.GetVerb()+" "+a.GetResource().Resource)
		}
	}
	return out
}

func TestPlanPostgresHibernationIsReadOnlyAndDeterministic(t *testing.T) {
	ctx := context.Background()
	dyn, kube := hibClients(t, hibCluster(nil), one())
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	require.False(t, plan.AlreadyHibernated)
	require.False(t, plan.Hibernated)
	require.Equal(t, cnpgHealthyPhase, plan.Phase)
	require.Empty(t, plan.Blocked)
	require.Equal(t, []HibernationPod{{Name: "pg-kernel09b-fixture-1", UID: "uid-pod-1"}}, plan.InstancePods)
	require.Equal(t, "pg-kernel09b-fixture-exporter", plan.Exporter)
	require.Equal(t, int32(1), plan.ExporterReplicas)
	require.Len(t, plan.PlanHash, 64)
	again, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	require.Equal(t, plan.PlanHash, again.PlanHash)
	for _, a := range kube.Actions() {
		require.Contains(t, []string{"list", "get"}, a.GetVerb(), "the plan only reads")
	}
	for _, a := range dyn.Actions() {
		require.Equal(t, "get", a.GetVerb())
	}
}

func TestPlanPostgresHibernationRefusesWhatIsNotAProvisionedPostgres(t *testing.T) {
	dyn, kube := hibClients(t, hibCluster(nil), one())
	for _, addon := range []*types.DatabaseAddon{
		nil,
		{Type: types.DatabaseAddonTypeRedis, K8sNamespace: hibNS, K8sResourceName: "x"},
		{Type: types.DatabaseAddonTypePostgres, K8sNamespace: hibNS},
		{Type: types.DatabaseAddonTypePostgres, K8sResourceName: "x"},
	} {
		_, err := PlanPostgresHibernation(context.Background(), dyn, kube, addon)
		require.ErrorIs(t, err, ErrHibernationUnsupported)
	}
	_, err := PlanPostgresHibernation(context.Background(), nil, kube, hibAddon())
	require.Error(t, err)
}

func TestApplyPostgresHibernationAnnotatesWaitsForCNPGThenStopsTheExporter(t *testing.T) {
	fastHibernationWait(t)
	ctx := context.Background()
	dyn, kube := hibClients(t, hibCluster(map[string]string{"keep": "yes"}), one())
	cnpgActsOnAnnotation(dyn, kube)
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)

	res, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.NoError(t, err)
	require.True(t, res.Annotated)
	require.True(t, res.Confirmed)
	require.True(t, res.ExporterOff)

	cluster, err := dyn.Resource(cnpgGVR).Namespace(hibNS).Get(ctx, "pg-kernel09b-fixture", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"keep": "yes", CNPGHibernationAnnotation: "on"}, cluster.GetAnnotations(), "only the annotation is added")
	exporter, err := kube.AppsV1().Deployments(hibNS).Get(ctx, "pg-kernel09b-fixture-exporter", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(0), *exporter.Spec.Replicas)

	// CNPG stops the instance; the operation itself deletes nothing.
	require.Equal(t, []string{"patch deployments"}, writesIn(kube.Actions()))
	require.Equal(t, []string{"update clusters"}, writesIn(dyn.Actions()))

	// The exact retry finds nothing left to stop and writes nothing.
	kubeBefore, dynBefore := len(kube.Actions()), len(dyn.Actions())
	again, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.NoError(t, err)
	require.True(t, again.Plan.Done())
	require.True(t, again.Confirmed)
	require.False(t, again.Annotated)
	require.Empty(t, writesIn(append(kube.Actions()[kubeBefore:], dyn.Actions()[dynBefore:]...)))
}

func TestPostgresHibernationIsBlockedWhileTheOperatorCannotReachTheInstance(t *testing.T) {
	// The 24-oct trap (crea-sim kernel01g): no operator ingress, so CNPG never
	// acts on the annotation and recreates a deleted pod.
	for name, annotations := range map[string]map[string]string{"not annotated": nil, "annotated earlier": {CNPGHibernationAnnotation: "on"}} {
		ctx := context.Background()
		dyn, kube := hibClients(t, hibClusterIn(annotations, extractionError, false), one())
		plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
		require.NoError(t, err, name)
		require.Equal(t, extractionError, plan.Phase, name)
		require.Contains(t, plan.Blocked, "operator-ingress", name)
		require.False(t, plan.Done(), name)

		res, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
		require.ErrorIs(t, err, ErrHibernationBlocked, name)
		require.Nil(t, res, name)
		require.Empty(t, writesIn(append(kube.Actions(), dyn.Actions()...)), name+": a blocked apply writes nothing")
	}
}

func TestPostgresHibernationDoneOnlyWhenCNPGConfirms(t *testing.T) {
	ctx := context.Background()
	// Annotated, healthy, condition still False: not done, and not blocked.
	dyn, kube := hibClients(t, hibClusterIn(map[string]string{CNPGHibernationAnnotation: "on"}, cnpgHealthyPhase, false), nil)
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	require.True(t, plan.AlreadyHibernated)
	require.False(t, plan.Done())
	require.Empty(t, plan.Blocked)

	// Hibernated by CNPG: the phase does not matter once the condition is True.
	dyn, kube = hibClients(t, hibClusterIn(map[string]string{CNPGHibernationAnnotation: "on"}, "Cluster is hibernated", true), nil)
	require.NoError(t, kube.CoreV1().Pods(hibNS).Delete(ctx, "pg-kernel09b-fixture-1", metav1.DeleteOptions{}))
	plan, err = PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	require.Empty(t, plan.Blocked)
	require.True(t, plan.Done())
}

func TestApplyPostgresHibernationStopsTheExporterOfAClusterCNPGAlreadyHibernated(t *testing.T) {
	ctx := context.Background()
	dyn, kube := hibClients(t, hibClusterIn(map[string]string{CNPGHibernationAnnotation: "on"}, cnpgHealthyPhase, true), one())
	require.NoError(t, kube.CoreV1().Pods(hibNS).Delete(ctx, "pg-kernel09b-fixture-1", metav1.DeleteOptions{}))
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	require.False(t, plan.Done(), "the exporter still runs")
	before := len(kube.Actions())
	res, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.NoError(t, err)
	require.False(t, res.Annotated)
	require.True(t, res.Confirmed)
	require.True(t, res.ExporterOff)
	require.Equal(t, []string{"patch deployments"}, writesIn(kube.Actions()[before:]))
	require.Empty(t, writesIn(dyn.Actions()))
}

func TestApplyPostgresHibernationReportsUnconfirmedAndLeavesTheExporter(t *testing.T) {
	fastHibernationWait(t)
	ctx := context.Background()
	dyn, kube := hibClients(t, hibCluster(nil), one()) // CNPG never acts
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	res, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.ErrorIs(t, err, ErrHibernationUnconfirmed)
	require.True(t, res.Annotated)
	require.False(t, res.Confirmed)
	require.False(t, res.ExporterOff)
	require.Empty(t, writesIn(kube.Actions()), "no pod delete, no exporter change")
}

func TestApplyPostgresHibernationRefusesAStaleOrMissingPlan(t *testing.T) {
	ctx := context.Background()
	dyn, kube := hibClients(t, hibCluster(nil), one())
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	_, err = ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), "")
	require.ErrorIs(t, err, ErrHibernationPlanChanged)

	// A new instance pod appears after the review: the old plan no longer applies.
	_, err = kube.CoreV1().Pods(hibNS).Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pg-kernel09b-fixture-2", Namespace: hibNS, UID: "uid-pod-2", Labels: map[string]string{cnpgClusterLabel: "pg-kernel09b-fixture", "cnpg.io/podRole": "instance"}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.ErrorIs(t, err, ErrHibernationPlanChanged)
	c, err := dyn.Resource(cnpgGVR).Namespace(hibNS).Get(ctx, "pg-kernel09b-fixture", metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, c.GetAnnotations()[CNPGHibernationAnnotation], "a refused apply writes nothing")
}

func TestApplyPostgresHibernationReportsAFailedWriteWithWhatAlreadyChanged(t *testing.T) {
	fastHibernationWait(t)
	ctx := context.Background()
	dyn, kube := hibClients(t, hibCluster(nil), one())
	cnpgActsOnAnnotation(dyn, kube)
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	kube.PrependReactor("patch", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver unavailable")
	})
	res, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.ErrorContains(t, err, "database already stopped")
	require.True(t, res.Annotated)
	require.True(t, res.Confirmed)
	require.False(t, res.ExporterOff)
}
