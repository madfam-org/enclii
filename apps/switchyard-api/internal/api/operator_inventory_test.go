package api

import (
	"context"
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
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/auth"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func inventoryFixture() (*Handler, *fake.Clientset, *dynamicfake.FakeDynamicClient) {
	now := metav1.NewTime(time.Now().Add(-time.Hour))
	kube := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""}}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}, Capacity: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("8Gi"), corev1.ResourcePods: resource.MustParse("110")}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "tenant-a"}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "api", Image: "registry.example.org/api:fixture", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("128Mi")}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, StartTime: &now, ContainerStatuses: []corev1.ContainerStatus{{Name: "api", Ready: true, RestartCount: 2}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "completed", Namespace: "tenant-a"}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "job", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("8")}}}}}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "tenant-a"}, Spec: corev1.ServiceSpec{ClusterIP: "10.0.0.10", Selector: map[string]string{"app": "api"}, Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromString("http")}}}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "tenant-a"}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "deny", Namespace: "tenant-a"}, Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}}},
	)
	app := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "argoproj.io/v1alpha1", "kind": "Application", "metadata": map[string]any{"name": "app-a", "namespace": "argocd"}, "spec": map[string]any{"source": map[string]any{"repoURL": "https://example.org/repo", "path": "infra/k8s", "targetRevision": "main"}, "destination": map[string]any{"namespace": "tenant-a", "server": "https://kubernetes.default.svc"}}, "status": map[string]any{"sync": map[string]any{"status": "Synced", "revision": "fixture-revision"}, "health": map[string]any{"status": "Healthy", "message": "observed"}, "operationState": map[string]any{"startedAt": "2026-01-01T00:00:00Z"}, "conditions": []any{map[string]any{"type": "SharedResourceWarning", "message": "fixture"}}}}}
	objects := []runtime.Object{app}
	for i, robustness := range []string{"healthy", "faulted", "degraded"} {
		name := []string{"vol-a", "vol-b", "vol-c"}[i]
		objects = append(objects, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "longhorn.io/v1beta2", "kind": "Volume", "metadata": map[string]any{"name": name, "namespace": "longhorn-system", "creationTimestamp": "2026-01-01T00:00:00Z"}, "spec": map[string]any{"size": "1073741824", "numberOfReplicas": int64(2), "dataEngine": "v1"}, "status": map[string]any{"state": "attached", "robustness": robustness, "currentNodeID": "node-a", "kubernetesStatus": map[string]any{"namespace": "tenant-a", "pvcName": "data", "workloadsStatus": []any{map[string]any{"podName": "pod-a"}}}}}})
	}
	objects = append(objects, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "longhorn.io/v1beta2", "kind": "Replica", "metadata": map[string]any{"name": "rep-a", "namespace": "longhorn-system"}, "spec": map[string]any{"volumeName": "vol-a", "nodeID": "node-a"}, "status": map[string]any{"currentState": "running", "mode": "RW"}}})
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		argoApplicationGVR: "ApplicationList",
		{Group: "longhorn.io", Version: "v1beta2", Resource: "volumes"}:  "VolumeList",
		{Group: "longhorn.io", Version: "v1beta2", Resource: "replicas"}: "ReplicaList",
	}, objects...)
	return &Handler{k8sClient: &k8s.Client{KubeClient: kube, DynamicClient: dyn}}, kube, dyn
}

func TestInventoryTopologyRetainsLiveSchema(t *testing.T) {
	h, _, _ := inventoryFixture()
	got, err := h.readInventoryTopology(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(4000), got.Totals.CPUCapacity)
	require.Equal(t, int64(250), got.Totals.CPUUsed, "completed jobs must not count toward running requests")
	require.Equal(t, int64(128*1024*1024), got.Totals.MemoryUsed)
	require.Equal(t, "4.0 cores", got.Display.CPUCapacity)
	require.Len(t, got.Nodes, 1)
	require.Equal(t, "control-plane", got.Nodes[0].Role)
	require.Equal(t, "Ready", got.Nodes[0].Status)
	require.Equal(t, 1, got.Nodes[0].Used.Pods)
	require.NotNil(t, got.Nodes[0].Taints)
	require.Len(t, got.Namespaces, 1)
	require.Equal(t, 2, got.Namespaces[0].Pods)
	require.Equal(t, 1, got.Namespaces[0].Deployments)
	require.Equal(t, 1, got.Namespaces[0].Services)
	require.Equal(t, "http", got.Services[0].Ports[0].Target)
	var pod inventoryPod
	for _, p := range got.Pods {
		if p.Name == "pod-a" {
			pod = p
		}
	}
	require.Equal(t, "1/1", pod.Ready)
	require.Equal(t, int32(2), pod.Restarts)
	require.GreaterOrEqual(t, pod.Age, int64(3599))
	require.True(t, pod.Containers[0].Ready)
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))
	require.ElementsMatch(t, []string{"nodes", "namespaces", "pods", "services", "totals", "display", "synced_at"}, mapKeys(body))
}
func mapKeys(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestInventoryApplicationsRetainArgoMetadata(t *testing.T) {
	h, _, _ := inventoryFixture()
	got, err := h.readInventoryApplications(context.Background(), "argocd")
	require.NoError(t, err)
	require.Len(t, got.Applications, 1)
	app := got.Applications[0]
	require.Equal(t, "Synced", app.Sync)
	require.Equal(t, "Healthy", app.Health)
	require.Equal(t, "https://example.org/repo", *app.SourceRepo)
	require.Equal(t, "infra/k8s", *app.SourcePath)
	require.Equal(t, "main", *app.TargetRevision)
	require.Equal(t, "fixture-revision", *app.CurrentRevision)
	require.Equal(t, "tenant-a", *app.DestinationNamespace)
	require.Equal(t, "2026-01-01T00:00:00Z", *app.LastSync)
	require.Len(t, app.Conditions, 1)
	empty, err := h.readInventoryApplications(context.Background(), "empty")
	require.NoError(t, err)
	require.NotNil(t, empty.Applications)
	require.Empty(t, empty.Applications)
}
func TestInventoryVolumesRetainReplicasAndTotals(t *testing.T) {
	h, _, _ := inventoryFixture()
	got, err := h.readInventoryVolumes(context.Background(), "longhorn-system")
	require.NoError(t, err)
	require.Equal(t, []string{"faulted", "degraded", "healthy"}, []string{got.Volumes[0].Robustness, got.Volumes[1].Robustness, got.Volumes[2].Robustness})
	require.Equal(t, 3, got.Summary.Total)
	require.Equal(t, int64(3*1024*1024*1024), got.Summary.Bytes)
	require.Equal(t, "3.0 GiB", got.Summary.Display)
	vol := got.Volumes[2]
	require.Equal(t, "tenant-a", *vol.Namespace)
	require.Equal(t, "data", *vol.PVC)
	require.Equal(t, "pod-a", *vol.Pod)
	require.Equal(t, int64(2), vol.ReplicaTarget)
	require.Equal(t, []inventoryReplica{{"rep-a", "node-a", true, "RW"}}, vol.Replicas)
	require.NotNil(t, got.Volumes[0].Replicas)
}
func TestInventoryNetworkPoliciesPreserveDenyAndSelectors(t *testing.T) {
	h, kube, _ := inventoryFixture()
	port := intstr.FromInt32(443)
	_, err := kube.NetworkingV1().NetworkPolicies("other").Create(context.Background(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow"}, Spec: networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"api"}}}},
		Egress:      []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "0.0.0.0/0", Except: []string{"169.254.0.0/16"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Port: &port}}}},
	}}, metav1.CreateOptions{})
	require.NoError(t, err)
	got, err := h.readInventoryNetworkPolicies(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, got.Policies)
	require.Equal(t, 2, got.Namespaces)
	require.Equal(t, "other", got.Groups[0].Namespace)
	allow := got.Groups[0].Policies[0]
	require.Contains(t, allow.SelectorSummary, "app In (api)")
	require.Contains(t, allow.Egress[0], "TCP/443 -> 0.0.0.0/0 (except 169.254.0.0/16)")
	deny := got.Groups[1].Policies[0]
	require.Empty(t, deny.Ingress)
	require.NotNil(t, deny.Ingress)
	require.Empty(t, deny.Egress)
	require.Equal(t, []string{"Ingress", "Egress"}, deny.Types)
	require.Nil(t, deny.Selector)
}
func TestInventoryFailuresNeverReturnHealthyEmptyData(t *testing.T) {
	for _, action := range []string{"topology", "applications", "volumes", "network-policies"} {
		t.Run(action, func(t *testing.T) {
			var empty Handler
			unavailable := empty.readPlatformInventory(context.Background(), action, "ops.inventory."+action, operatorOperationRequest{})
			require.Equal(t, "adapter_unconfigured", unavailable.Status)
			require.Nil(t, unavailable.Data)
			h, kube, dyn := inventoryFixture()
			fail := func(ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, errors.New("private upstream connection details")
			}
			kube.PrependReactor("list", "*", fail)
			dyn.PrependReactor("list", "*", fail)
			failed := h.readPlatformInventory(context.Background(), action, "ops.inventory."+action, operatorOperationRequest{})
			require.Equal(t, "failed", failed.Status)
			require.Nil(t, failed.Data)
			encoded, err := json.Marshal(failed)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private upstream")
		})
	}
}

// Exercise route registration, rank resolution, capability lookup, and adapter
// dispatch together. A stubbed envelope would miss a missing registry entry.
func TestInventoryRoutesReturnLiveEnvelopeForPlatformCaller(t *testing.T) {
	for _, action := range []string{"topology", "applications", "volumes", "network-policies"} {
		t.Run(action, func(t *testing.T) {
			h, mock, cleanup := setupTenantScopeHandler(t)
			defer cleanup()
			h.auth = &auth.JWTManager{}
			fixture, kube, dyn := inventoryFixture()
			h.k8sClient = fixture.k8sClient
			id := uuid.New()
			mock.ExpectQuery(`SELECT is_platform_admin FROM users WHERE id`).WithArgs(id).
				WillReturnRows(sqlmock.NewRows([]string{"is_platform_admin"}).AddRow(true))
			engine := gin.New()
			engine.Use(withUserContext(id, "admin"))
			registerOperatorRoutes(engine.Group("/v1"), h)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/ops/inventory/"+action, strings.NewReader(`{"dry_run":true}`))
			req.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var got operatorOperationResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			require.Equal(t, "succeeded", got.Status, w.Body.String())
			require.NotNil(t, got.Data)
			require.True(t, got.DryRun)
			for _, request := range append(kube.Actions(), dyn.Actions()...) {
				require.Equal(t, "list", request.GetVerb(), "inventory must never mutate cluster state")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
