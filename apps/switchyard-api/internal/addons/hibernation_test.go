package addons

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const hibNS = "fixture-data"

func hibAddon() *types.DatabaseAddon {
	return &types.DatabaseAddon{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000a1"), Type: types.DatabaseAddonTypePostgres, K8sNamespace: hibNS, K8sResourceName: "pg-kernel09b-fixture"}
}

func hibCluster(annotations map[string]string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
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

func TestPlanPostgresHibernationIsReadOnlyAndDeterministic(t *testing.T) {
	ctx := context.Background()
	dyn, kube := hibClients(t, hibCluster(nil), one())
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	require.False(t, plan.AlreadyHibernated)
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

func TestApplyPostgresHibernationAnnotatesDeletesInstancePodsAndStopsTheExporter(t *testing.T) {
	ctx := context.Background()
	dyn, kube := hibClients(t, hibCluster(map[string]string{"keep": "yes"}), one())
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)

	res, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.NoError(t, err)
	require.True(t, res.Annotated)
	require.Equal(t, []string{"pg-kernel09b-fixture-1"}, res.PodsDeleted)
	require.True(t, res.ExporterOff)

	cluster, err := dyn.Resource(cnpgGVR).Namespace(hibNS).Get(ctx, "pg-kernel09b-fixture", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"keep": "yes", CNPGHibernationAnnotation: "on"}, cluster.GetAnnotations(), "only the annotation is added")
	pods, err := kube.CoreV1().Pods(hibNS).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	names := []string{}
	for _, p := range pods.Items {
		names = append(names, p.Name)
	}
	require.ElementsMatch(t, []string{"pg-kernel09b-fixture-backup-x", "pg-other-1"}, names)
	exporter, err := kube.AppsV1().Deployments(hibNS).Get(ctx, "pg-kernel09b-fixture-exporter", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, int32(0), *exporter.Spec.Replicas)

	// Nothing is ever deleted except instance pods: no Cluster, PVC, Secret or Deployment delete.
	for _, a := range kube.Actions() {
		if a.GetVerb() == "delete" {
			require.Equal(t, "pods", a.GetResource().Resource)
			require.Equal(t, k8stypes.UID("uid-pod-1"), *a.(k8stesting.DeleteActionImpl).DeleteOptions.Preconditions.UID)
		}
	}
	for _, a := range dyn.Actions() {
		require.NotEqual(t, "delete", a.GetVerb())
	}

	// The exact retry finds nothing left to stop and writes nothing.
	kubeBefore, dynBefore := len(kube.Actions()), len(dyn.Actions())
	again, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.NoError(t, err)
	require.True(t, again.Plan.Done())
	require.False(t, again.Annotated)
	writes := 0
	for _, a := range append(kube.Actions()[kubeBefore:], dyn.Actions()[dynBefore:]...) {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			writes++
		}
	}
	require.Zero(t, writes)
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

func TestApplyPostgresHibernationFinishesAPartlyHibernatedClusterAndToleratesNoExporter(t *testing.T) {
	ctx := context.Background()
	// The 23-oct trap: annotated, but CNPG could not stop the instance.
	dyn, kube := hibClients(t, hibCluster(map[string]string{CNPGHibernationAnnotation: "on"}), nil)
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	require.True(t, plan.AlreadyHibernated)
	require.False(t, plan.Done())
	res, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.NoError(t, err)
	require.False(t, res.Annotated)
	require.Equal(t, []string{"pg-kernel09b-fixture-1"}, res.PodsDeleted)
	require.False(t, res.ExporterOff)
}

func TestApplyPostgresHibernationReportsAFailedWriteWithWhatAlreadyChanged(t *testing.T) {
	ctx := context.Background()
	dyn, kube := hibClients(t, hibCluster(nil), one())
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, hibAddon())
	require.NoError(t, err)
	kube.PrependReactor("delete", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver unavailable")
	})
	res, err := ApplyPostgresHibernation(ctx, dyn, kube, hibAddon(), plan.PlanHash)
	require.ErrorContains(t, err, "cluster already annotated")
	require.True(t, res.Annotated)
	require.Empty(t, res.PodsDeleted)
}
