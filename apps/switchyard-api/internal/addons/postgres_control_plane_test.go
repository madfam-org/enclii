package addons

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestControlPlaneEgressOpensOnlyTheAPIServerForOneCluster(t *testing.T) {
	pol := buildControlPlaneEgressPolicy("project-fixture", "pg-fixture")
	require.Equal(t, "pg-fixture-control-plane", pol.Name)
	require.Equal(t, "project-fixture", pol.Namespace)
	require.Equal(t, map[string]string{"cnpg.io/cluster": "pg-fixture"}, pol.Spec.PodSelector.MatchLabels)
	require.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, pol.Spec.PolicyTypes, "egress only: ingress stays default-deny")
	require.Len(t, pol.Spec.Egress, 2)
	require.Equal(t, "10.0.0.0/8", pol.Spec.Egress[0].To[0].IPBlock.CIDR)
	require.Empty(t, pol.Spec.Egress[1].To, "the external-address rule is limited by port, not peer")
	ports := map[int32]int{}
	for _, rule := range pol.Spec.Egress {
		for _, port := range rule.Ports {
			require.Equal(t, corev1.ProtocolTCP, *port.Protocol)
			ports[port.Port.IntVal]++
		}
	}
	require.Equal(t, map[int32]int{443: 1, 6443: 2}, ports, "only the API-server ports the service namespaces already allow")
}

func TestEnsureControlPlaneEgressIsIdempotent(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: kube}}
	require.NoError(t, p.ensureControlPlaneEgress(ctx, "project-fixture", "pg-fixture"))
	require.NoError(t, p.ensureControlPlaneEgress(ctx, "project-fixture", "pg-fixture"))
	got, err := kube.NetworkingV1().NetworkPolicies("project-fixture").Get(ctx, "pg-fixture-control-plane", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, buildControlPlaneEgressPolicy("project-fixture", "pg-fixture").Spec, got.Spec)
	require.Error(t, (&PostgresProvisioner{}).ensureControlPlaneEgress(ctx, "project-fixture", "pg-fixture"))
	require.Error(t, p.ensureControlPlaneEgress(ctx, "", "pg-fixture"))
}

func TestOperatorIngressAdmitsOnlyTheCNPGOperatorOnTheStatusPort(t *testing.T) {
	pol := buildOperatorIngressPolicy("project-fixture", "pg-fixture")
	require.Equal(t, "pg-fixture-operator-ingress", pol.Name)
	require.Equal(t, "project-fixture", pol.Namespace)
	require.Equal(t, map[string]string{"cnpg.io/cluster": "pg-fixture"}, pol.Spec.PodSelector.MatchLabels)
	require.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, pol.Spec.PolicyTypes, "ingress only: egress is the control-plane policy")
	require.Len(t, pol.Spec.Ingress, 1)
	rule := pol.Spec.Ingress[0]
	require.Len(t, rule.From, 1)
	require.Nil(t, rule.From[0].IPBlock)
	require.Equal(t, map[string]string{"kubernetes.io/metadata.name": "cnpg-system"}, rule.From[0].NamespaceSelector.MatchLabels)
	require.Equal(t, map[string]string{"app.kubernetes.io/name": "cloudnative-pg"}, rule.From[0].PodSelector.MatchLabels, "the operator's pods, not the whole namespace")
	require.Len(t, rule.Ports, 1)
	require.Equal(t, corev1.ProtocolTCP, *rule.Ports[0].Protocol)
	require.Equal(t, int32(8000), rule.Ports[0].Port.IntVal, "the instance status port; 5432 stays with data-access")
}

func TestEnsureCNPGNetworkPoliciesWritesBothAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: kube}}
	require.NoError(t, p.ensureCNPGNetworkPolicies(ctx, "project-fixture", "pg-fixture"))
	require.NoError(t, p.ensureCNPGNetworkPolicies(ctx, "project-fixture", "pg-fixture"))
	list, err := kube.NetworkingV1().NetworkPolicies("project-fixture").List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	names := []string{}
	for _, pol := range list.Items {
		names = append(names, pol.Name)
	}
	require.ElementsMatch(t, []string{"pg-fixture-control-plane", "pg-fixture-operator-ingress"}, names)
	require.Error(t, (&PostgresProvisioner{}).ensureCNPGNetworkPolicies(ctx, "project-fixture", "pg-fixture"))
}

func TestConvergeCNPGNetworkPoliciesSkipsWhatIsNotALiveCluster(t *testing.T) {
	kube := kubefake.NewSimpleClientset()
	s := &AddonService{logger: testLogger(), provisioners: map[types.DatabaseAddonType]AddonProvisioner{
		types.DatabaseAddonTypePostgres: &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: kube}}}}
	live := &types.DatabaseAddon{Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusReady, K8sNamespace: "project-fixture", K8sResourceName: "pg-fixture"}
	require.NoError(t, s.ConvergeCNPGNetworkPolicies(context.Background(), live))
	for name, addon := range map[string]*types.DatabaseAddon{
		"nil":       nil,
		"redis":     {Type: types.DatabaseAddonTypeRedis, Status: types.DatabaseAddonStatusReady, K8sNamespace: "n", K8sResourceName: "r"},
		"no ns":     {Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusReady, K8sResourceName: "r"},
		"deleting":  {Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusDeleting, K8sNamespace: "n", K8sResourceName: "r"},
		"retention": {Type: types.DatabaseAddonTypePostgres, Status: types.DatabaseAddonStatusPendingDeletion, K8sNamespace: "n", K8sResourceName: "r"},
	} {
		require.ErrorIs(t, s.ConvergeCNPGNetworkPolicies(context.Background(), addon), ErrNetworkConvergenceNotApplicable, name)
	}
	list, err := kube.NetworkingV1().NetworkPolicies("n").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, list.Items)
}

func TestRefreshConvergesControlPlaneEgressForExistingCluster(t *testing.T) {
	raw, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	repos := db.NewRepositories(raw)
	repos.ManagedDBAddonEvents = nil
	id, project := uuid.New(), uuid.New()
	encoded, err := json.Marshal(types.DatabaseAddonConfig{DatabaseName: "training_sim", RuntimeRole: "training_runtime"})
	require.NoError(t, err)
	prepared := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	ns, resource := "project-fixture", "pg-fixture"
	host := resource + "-rw." + ns + ".svc.cluster.local"
	mock.ExpectQuery(`FROM database_addons WHERE id = \$1`).WithArgs(id).
		WillReturnRows(sqlmock.NewRows(addonColsSvc).AddRow(id, project, nil, "postgres", "training", "standard-1", "ready", "cached ready", encoded, ns, resource, resource+"-runtime", host, 5432, "app", "app", 0, 0, nil, nil, "", prepared, prepared, prepared, nil, nil))
	status := map[string]interface{}{"phase": "Cluster in healthy state", "instances": int64(1), "readyInstances": int64(1), "writeService": resource + "-rw",
		"managedRolesStatus": map[string]interface{}{"byStatus": map[string]interface{}{"reconciled": []interface{}{"training_runtime"}}}}
	cluster := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": CloudNativePGAPIVersion, "kind": CloudNativePGKind, "metadata": map[string]interface{}{"name": resource, "namespace": ns}, "status": status}}
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), cluster)
	kube := kubefake.NewSimpleClientset()
	s := &AddonService{repos: repos, logger: testLogger(), provisioners: map[types.DatabaseAddonType]AddonProvisioner{
		types.DatabaseAddonTypePostgres: &PostgresProvisioner{dynamicClient: dyn, k8sClient: &k8s.Client{KubeClient: kube}}}}
	mock.ExpectExec(`(?s)UPDATE database_addons.*WHERE id = \$9 AND status = \$10 AND updated_at = \$11 AND deleted_at IS NULL`).WithArgs(types.DatabaseAddonStatusReady, sqlmock.AnyArg(), host, 5432, "training_sim", "training_runtime", sqlmock.AnyArg(), prepared, id, types.DatabaseAddonStatusReady, prepared).WillReturnResult(sqlmock.NewResult(0, 1))
	got, err := s.RefreshStatus(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, types.DatabaseAddonStatusReady, got.Status)
	pol, err := kube.NetworkingV1().NetworkPolicies(ns).Get(context.Background(), resource+"-control-plane", metav1.GetOptions{})
	require.NoError(t, err, "refresh converges the egress for a cluster provisioned before it existed")
	require.Equal(t, map[string]string{"cnpg.io/cluster": resource}, pol.Spec.PodSelector.MatchLabels)
	_, err = kube.NetworkingV1().NetworkPolicies(ns).Get(context.Background(), resource+"-operator-ingress", metav1.GetOptions{})
	require.NoError(t, err, "refresh also converges the operator ingress")
	require.Len(t, dyn.Actions(), 1, "the Cluster itself is still only read")
	require.NoError(t, mock.ExpectationsWereMet())
}
