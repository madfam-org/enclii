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
	require.Len(t, dyn.Actions(), 1, "the Cluster itself is still only read")
	require.NoError(t, mock.ExpectationsWereMet())
}
