package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestJanuaInertRequestIsClosedAndDoesNotAcceptAuthoritySelectors(t *testing.T) {
	id, intent := uuid.NewString(), uuid.NewString()
	body := `{"operation_id":"` + id + `","held_intent_id":"` + intent + `","revision":"` + strings.Repeat("a", 40) + `","dry_run":true}`
	r, ok := parseJanuaInertRequest([]byte(body))
	require.True(t, ok)
	require.True(t, *r.DryRun)
	for _, change := range []string{
		strings.Replace(body, `"dry_run":true`, `"dry_run":false`, 1),
		strings.Replace(body, `"dry_run":true`, `"dry_run":true,"namespace":"janua"`, 1),
		strings.Replace(body, `"dry_run":true`, `"dry_run":true,"image":"caller/image:latest"`, 1),
		strings.Replace(body, `"dry_run":true`, `"dry_run":true,"sql":"SELECT 1"`, 1),
		strings.Replace(body, `"dry_run":true`, `"dry_run":true,"uid":"caller-supplied"`, 1),
		strings.Replace(body, strings.Repeat("a", 40), "main", 1),
		strings.Replace(body, `"dry_run":true`, `"dry_run":true,"dry_run":false`, 1),
	} {
		_, ok := parseJanuaInertRequest([]byte(change))
		require.False(t, ok, change)
	}
	apply := strings.Replace(body, `"dry_run":true`, `"dry_run":false,"expect_plan_sha256":"`+strings.Repeat("b", 64)+`","reason":"reviewed inert source"`, 1)
	_, ok = parseJanuaInertRequest([]byte(apply))
	require.True(t, ok)
}

func TestJanuaInertNativeGuardReadsOnlyAndRejectsForeignOrActivatingState(t *testing.T) {
	for _, state := range []string{"owned", "namespace-replaced", "redis-consumer", "pod", "foreign-deployment", "global-project", "hpa", "additive-policy"} {
		t.Run(state, func(t *testing.T) {
			project := uuid.New()
			plan := januastage.Plan{ID: uuid.New(), ProjectID: project, NamespaceUID: "namespace-fixture", BaselineUID: "baseline-fixture"}
			kube := kubefake.NewSimpleClientset()
			labels := map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/verify-signatures": "true"}
			if state == "redis-consumer" {
				labels["enclii.dev/redis-consumer"] = "true"
			}
			_, err := kube.CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: januastage.Namespace, UID: "namespace-fixture", ResourceVersion: "1", Labels: labels}}, metav1.CreateOptions{})
			require.NoError(t, err)
			_, err = kube.NetworkingV1().NetworkPolicies(januastage.Namespace).Create(t.Context(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: januastage.Namespace, UID: "baseline-fixture", ResourceVersion: "1", Labels: map[string]string{k8s.LabelProjectNamespace: project.String(), "enclii.dev/managed-by": "onboarding-api"}}, Spec: januaStageDNSOnlyBaseline()}, metav1.CreateOptions{})
			require.NoError(t, err)
			globalProjects := "[]"
			if state == "global-project" {
				globalProjects = "unreviewed-global-project"
			}
			_, err = kube.CoreV1().ConfigMaps("argocd").Create(t.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argocd-cm", Namespace: "argocd", UID: "config-fixture", ResourceVersion: "1"}, Data: map[string]string{"globalProjects": globalProjects}}, metav1.CreateOptions{})
			require.NoError(t, err)
			lists := map[schema.GroupVersionResource]string{}
			for _, item := range []struct{ group, version, resource, kind string }{
				{"apps", "v1", "statefulsets", "StatefulSetList"}, {"apps", "v1", "daemonsets", "DaemonSetList"},
				{"batch", "v1", "jobs", "JobList"}, {"batch", "v1", "cronjobs", "CronJobList"},
				{"autoscaling", "v2", "horizontalpodautoscalers", "HorizontalPodAutoscalerList"}, {"", "v1", "replicationcontrollers", "ReplicationControllerList"},
			} {
				lists[schema.GroupVersionResource{Group: item.group, Version: item.version, Resource: item.resource}] = item.kind
			}
			dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), lists)
			switch state {
			case "namespace-replaced":
				plan.NamespaceUID = "different-fixture"
			case "pod":
				_, err = kube.CoreV1().Pods(januastage.Namespace).Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unreviewed", Namespace: januastage.Namespace}}, metav1.CreateOptions{})
			case "foreign-deployment":
				zero := int32(0)
				_, err = kube.AppsV1().Deployments(januastage.Namespace).Create(t.Context(), &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: januastage.Deployment, Namespace: januastage.Namespace, UID: "foreign"}, Spec: appsv1.DeploymentSpec{Replicas: &zero}}, metav1.CreateOptions{})
			case "hpa":
				_, err = dynamic.Resource(schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}).Namespace(januastage.Namespace).Create(t.Context(), &unstructured.Unstructured{Object: map[string]any{"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscaler", "metadata": map[string]any{"name": "unexpected", "namespace": januastage.Namespace}}}, metav1.CreateOptions{})
			case "additive-policy":
				_, err = kube.NetworkingV1().NetworkPolicies(januastage.Namespace).Create(t.Context(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "unexpected", Namespace: januastage.Namespace}}, metav1.CreateOptions{})
			}
			require.NoError(t, err)
			kube.ClearActions()
			dynamic.ClearActions()
			native := januaInertNative{h: &Handler{k8sClient: &k8s.Client{KubeClient: kube, DynamicClient: dynamic}}, initial: true}
			err = native.Guard(t.Context(), plan)
			if state == "owned" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
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
