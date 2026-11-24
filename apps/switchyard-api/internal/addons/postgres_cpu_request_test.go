package addons

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestPostgresCPURequestDefaultsBelowThePlanLimit(t *testing.T) {
	for plan, want := range map[string]string{
		"500m":  DefaultPostgresCPURequest,
		"1000m": DefaultPostgresCPURequest,
		"1":     DefaultPostgresCPURequest,
		"0.5":   DefaultPostgresCPURequest,
		"100m":  "100m", // equal to the default: unchanged
		"50m":   "50m",  // a request may not exceed its limit
		"0.05":  "0.05",
		"bogus": "bogus", // the API server rejects it, as before
	} {
		require.Equal(t, want, postgresCPURequest(plan), plan)
	}
	require.Equal(t, "100m", DefaultPostgresCPURequest)
}

func TestManifestReservesTheDefaultRequestAndKeepsThePlanAsTheLimit(t *testing.T) {
	p := &PostgresProvisioner{}
	resourcesFor := func(cpu, memory string) map[string]interface{} {
		req := provisionReq("")
		req.Addon.Config.CPU, req.Addon.Config.Memory = cpu, memory
		spec := p.buildClusterManifest(req, "pg-map-abc12345")["spec"].(map[string]interface{})
		resources, _ := spec["resources"].(map[string]interface{})
		return resources
	}

	// standard-1: 500m / 1Gi. CPU: 100m reserved, 500m ceiling. Memory untouched.
	require.Equal(t, map[string]interface{}{
		"requests": map[string]interface{}{"cpu": "100m", "memory": "1Gi"},
		"limits":   map[string]interface{}{"cpu": "500m", "memory": "1Gi"},
	}, resourcesFor("500m", "1Gi"))
	// standard-0 is already at the default: request == limit, as before.
	require.Equal(t, map[string]interface{}{
		"requests": map[string]interface{}{"cpu": "100m", "memory": "256Mi"},
		"limits":   map[string]interface{}{"cpu": "100m", "memory": "256Mi"},
	}, resourcesFor("100m", "256Mi"))
	// No CPU in the plan: no CPU key at all (the namespace default applies).
	require.Equal(t, map[string]interface{}{
		"requests": map[string]interface{}{"memory": "512Mi"},
		"limits":   map[string]interface{}{"memory": "512Mi"},
	}, resourcesFor("", "512Mi"))
	require.Nil(t, resourcesFor("", ""))
}

const cpuNS = "project-fixture"

var cpuAddonID = uuid.MustParse("00000000-0000-0000-0000-0000000000c1")

func cpuAddon() *types.DatabaseAddon {
	return &types.DatabaseAddon{ID: cpuAddonID, Type: types.DatabaseAddonTypePostgres, K8sNamespace: cpuNS, K8sResourceName: "pg-fixture"}
}

// cpuCluster is a fixture Cluster provisioned before 2026-11-04: CPU request
// == limit. An empty limit omits it; phase is the CNPG status phase.
func cpuCluster(request, limit, phase string, mutate ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	resources := map[string]interface{}{
		"requests": map[string]interface{}{"cpu": request, "memory": "1Gi"},
		"limits":   map[string]interface{}{"memory": "1Gi"},
	}
	if limit != "" {
		resources["limits"].(map[string]interface{})["cpu"] = limit
	}
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"instances": int64(1), "primaryUpdateStrategy": "unsupervised", "resources": resources,
			"storage": map[string]interface{}{"size": "10Gi"},
		},
		"status": map[string]interface{}{"phase": phase},
	}}
	u.SetAPIVersion("postgresql.cnpg.io/v1")
	u.SetKind("Cluster")
	u.SetNamespace(cpuNS)
	u.SetName("pg-fixture")
	u.SetUID("uid-pg-fixture")
	u.SetResourceVersion("77")
	u.SetLabels(map[string]string{LabelManagedBy: LabelManagedValue, LabelAddonID: cpuAddonID.String()})
	for _, m := range mutate {
		m(u)
	}
	return u
}

func cpuClient(cluster *unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{cnpgGVR: "ClusterList"}, cluster)
}

func TestPlanPostgresCPURequestIsReadOnlyAndProposesOnlyTheRequest(t *testing.T) {
	ctx := context.Background()
	dyn := cpuClient(cpuCluster("500m", "500m", cnpgHealthyPhase))
	plan, err := PlanPostgresCPURequest(ctx, dyn, cpuAddon())
	require.NoError(t, err)
	require.Empty(t, plan.Blocked)
	require.False(t, plan.Done())
	require.Equal(t, "500m", plan.CurrentRequest)
	require.Equal(t, "500m", plan.CurrentLimit)
	require.Equal(t, "100m", plan.DesiredRequest)
	require.Equal(t, "500m", plan.DesiredLimit, "the plan's ceiling stays")
	require.Contains(t, plan.Restart, "single instance")
	require.Len(t, plan.PlanHash, 64)
	again, err := PlanPostgresCPURequest(ctx, dyn, cpuAddon())
	require.NoError(t, err)
	require.Equal(t, plan.PlanHash, again.PlanHash)
	require.Empty(t, writesIn(dyn.Actions()), "the plan only reads")
}

func TestApplyPostgresCPURequestChangesTheCPURequestAndNothingElse(t *testing.T) {
	ctx := context.Background()
	before := cpuCluster("500m", "500m", cnpgHealthyPhase)
	dyn := cpuClient(before.DeepCopy())
	plan, err := PlanPostgresCPURequest(ctx, dyn, cpuAddon())
	require.NoError(t, err)

	res, err := ApplyPostgresCPURequest(ctx, dyn, cpuAddon(), plan.PlanHash)
	require.NoError(t, err)
	require.True(t, res.Patched)
	require.Equal(t, []string{"update clusters"}, writesIn(dyn.Actions()))

	after, err := dyn.Resource(cnpgGVR).Namespace(cpuNS).Get(ctx, "pg-fixture", metav1.GetOptions{})
	require.NoError(t, err)
	want := before.DeepCopy()
	require.NoError(t, unstructured.SetNestedField(want.Object, "100m", "spec", "resources", "requests", "cpu"))
	require.Equal(t, want.Object["spec"], after.Object["spec"], "only requests.cpu moved; memory, limits and the rest are as they were")
	require.Equal(t, want.GetLabels(), after.GetLabels())

	// The exact retry finds the Cluster done and writes nothing.
	writes := len(writesIn(dyn.Actions()))
	again, err := ApplyPostgresCPURequest(ctx, dyn, cpuAddon(), plan.PlanHash)
	require.NoError(t, err)
	require.False(t, again.Patched)
	require.True(t, again.Plan.Done())
	require.Equal(t, "none: the Cluster already carries the desired CPU", again.Plan.Restart)
	require.Len(t, writesIn(dyn.Actions()), writes)
}

func TestPostgresCPURequestKeepsTheOldCeilingWhenNoLimitWasSet(t *testing.T) {
	ctx := context.Background()
	dyn := cpuClient(cpuCluster("500m", "", cnpgHealthyPhase))
	plan, err := PlanPostgresCPURequest(ctx, dyn, cpuAddon())
	require.NoError(t, err)
	require.Equal(t, "100m", plan.DesiredRequest)
	require.Equal(t, "500m", plan.DesiredLimit, "the old request was the effective ceiling")
	_, err = ApplyPostgresCPURequest(ctx, dyn, cpuAddon(), plan.PlanHash)
	require.NoError(t, err)
	after, err := dyn.Resource(cnpgGVR).Namespace(cpuNS).Get(ctx, "pg-fixture", metav1.GetOptions{})
	require.NoError(t, err)
	limit, _, _ := unstructured.NestedString(after.Object, "spec", "resources", "limits", "cpu")
	require.Equal(t, "500m", limit)
}

func TestPostgresCPURequestAtOrBelowTheDefaultIsAlreadyDone(t *testing.T) {
	for _, cpu := range []string{"100m", "50m", "0.1"} {
		dyn := cpuClient(cpuCluster(cpu, cpu, cnpgHealthyPhase))
		plan, err := PlanPostgresCPURequest(context.Background(), dyn, cpuAddon())
		require.NoError(t, err, cpu)
		require.True(t, plan.Done(), cpu)
		res, err := ApplyPostgresCPURequest(context.Background(), dyn, cpuAddon(), "")
		require.NoError(t, err, cpu)
		require.False(t, res.Patched, cpu)
		require.Empty(t, writesIn(dyn.Actions()), cpu)
	}
	// Converged but still rolling: done, not blocked.
	dyn := cpuClient(cpuCluster("100m", "500m", "Waiting for the instances to become active"))
	plan, err := PlanPostgresCPURequest(context.Background(), dyn, cpuAddon())
	require.NoError(t, err)
	require.Empty(t, plan.Blocked)
	require.True(t, plan.Done())
}

func TestPostgresCPURequestRefusesWhatItMustNotChange(t *testing.T) {
	foreign := func(u *unstructured.Unstructured) {
		u.SetLabels(map[string]string{LabelManagedBy: LabelManagedValue, LabelAddonID: uuid.NewString()})
	}
	noRequest := func(u *unstructured.Unstructured) {
		unstructured.RemoveNestedField(u.Object, "spec", "resources", "requests", "cpu")
	}
	for name, tc := range map[string]struct {
		cluster *unstructured.Unstructured
		reason  string
	}{
		"another addon's cluster": {cpuCluster("500m", "500m", cnpgHealthyPhase, foreign), "not ours"},
		"no CPU request":          {cpuCluster("500m", "500m", cnpgHealthyPhase, noRequest), "nothing to lower"},
		"invalid quantity":        {cpuCluster("lots", "lots", cnpgHealthyPhase), "not a valid quantity"},
		"operator not managing":   {cpuCluster("500m", "500m", "Instance Status Extraction Error: HTTP communication issue"), "not managing cleanly"},
	} {
		dyn := cpuClient(tc.cluster)
		plan, err := PlanPostgresCPURequest(context.Background(), dyn, cpuAddon())
		require.NoError(t, err, name)
		require.Contains(t, plan.Blocked, tc.reason, name)
		require.False(t, plan.Done(), name)
		res, err := ApplyPostgresCPURequest(context.Background(), dyn, cpuAddon(), plan.PlanHash)
		require.ErrorIs(t, err, ErrCPURequestBlocked, name)
		require.Nil(t, res, name)
		require.Empty(t, writesIn(dyn.Actions()), name+": a blocked apply writes nothing")
	}
}

func TestApplyPostgresCPURequestRefusesAStaleOrMissingPlan(t *testing.T) {
	ctx := context.Background()
	dyn := cpuClient(cpuCluster("500m", "500m", cnpgHealthyPhase))
	plan, err := PlanPostgresCPURequest(ctx, dyn, cpuAddon())
	require.NoError(t, err)
	for _, expected := range []string{"", "deadbeef"} {
		_, err := ApplyPostgresCPURequest(ctx, dyn, cpuAddon(), expected)
		require.ErrorIs(t, err, ErrCPURequestPlanChanged, expected)
	}
	// Someone edits the Cluster after the review: the hash moves.
	live, err := dyn.Resource(cnpgGVR).Namespace(cpuNS).Get(ctx, "pg-fixture", metav1.GetOptions{})
	require.NoError(t, err)
	live.SetResourceVersion("78")
	_, err = dyn.Resource(cnpgGVR).Namespace(cpuNS).Update(ctx, live, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = ApplyPostgresCPURequest(ctx, dyn, cpuAddon(), plan.PlanHash)
	require.ErrorIs(t, err, ErrCPURequestPlanChanged)
}

func TestPostgresCPURequestRestartWording(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		mutate func(*unstructured.Unstructured)
		want   string
	}{
		"hibernated": {func(u *unstructured.Unstructured) {
			u.SetAnnotations(map[string]string{CNPGHibernationAnnotation: "on"})
		}, "hibernated"},
		"supervised": {func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, "supervised", "spec", "primaryUpdateStrategy")
		}, "waits"},
		"three instances": {func(u *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(u.Object, int64(3), "spec", "instances")
		}, "switches over"},
	} {
		plan, err := PlanPostgresCPURequest(ctx, cpuClient(cpuCluster("500m", "500m", cnpgHealthyPhase, tc.mutate)), cpuAddon())
		require.NoError(t, err, name)
		require.Contains(t, plan.Restart, tc.want, name)
	}
}

func TestPlanPostgresCPURequestRefusesWhatIsNotAProvisionedPostgres(t *testing.T) {
	dyn := cpuClient(cpuCluster("500m", "500m", cnpgHealthyPhase))
	for _, addon := range []*types.DatabaseAddon{
		nil,
		{Type: types.DatabaseAddonTypeRedis, K8sNamespace: cpuNS, K8sResourceName: "x"},
		{Type: types.DatabaseAddonTypePostgres, K8sNamespace: cpuNS},
		{Type: types.DatabaseAddonTypePostgres, K8sResourceName: "x"},
	} {
		_, err := PlanPostgresCPURequest(context.Background(), dyn, addon)
		require.ErrorIs(t, err, ErrCPURequestUnsupported)
	}
	_, err := PlanPostgresCPURequest(context.Background(), nil, cpuAddon())
	require.Error(t, err)
}
