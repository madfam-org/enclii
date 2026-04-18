//go:build integration

package reconciler

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// These tests run against a live kind/k3d cluster when
// INTEGRATION_USE_REAL_CLUSTER=true and KUBECONFIG is set. Skipped otherwise.
// Matches the pattern in service_integration_test.go.

func requireCanaryCluster(t *testing.T) *k8s.Client {
	t.Helper()
	if os.Getenv("INTEGRATION_USE_REAL_CLUSTER") != "true" {
		t.Skip("set INTEGRATION_USE_REAL_CLUSTER=true to run canary integration tests")
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("KUBECONFIG not set")
	}
	client, err := k8s.NewClient(kubeconfig, "")
	if err != nil {
		t.Fatalf("k8s client: %v", err)
	}
	return client
}

// TestIntegration_EnsureCanaryReplicas_ScalesAndCreates verifies that after
// ensureCanaryReplicas runs:
//   - The stable Deployment's replicas == StableReplicas
//   - A canary Deployment exists at CanaryReplicas with the canary image
func TestIntegration_EnsureCanaryReplicas_ScalesAndCreates(t *testing.T) {
	kc := requireCanaryCluster(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ns := "canary-test-" + uuid.New().String()[:8]
	svcName := "nginx-demo"
	if _, err := kc.Clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ns: %v", err)
	}
	t.Cleanup(func() {
		_ = kc.Clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	})

	// 1. Create the stable Deployment
	stableReplicas := int32(5)
	stable := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"app": svcName, "enclii.dev/service": svcName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &stableReplicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": svcName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": svcName}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "nginx", Image: "nginx:1.25"}},
				},
			},
		},
	}
	if _, err := kc.Clientset.AppsV1().Deployments(ns).Create(ctx, stable, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create stable: %v", err)
	}

	// 2. Run ensureCanaryReplicas
	rec := NewCanaryReconciler(kc, nil, logrus.New())
	rolloutID := uuid.New()
	in := TickInput{
		Rollout: &types.CanaryRollout{
			ID:             rolloutID,
			TotalReplicas:  5,
			CanaryReplicas: 1,
			StableReplicas: 4,
		},
		Service:     &types.Service{Name: svcName},
		Environment: &types.Environment{KubeNamespace: ns},
		CanaryRel:   &types.Release{ImageURI: "nginx:1.26"},
	}
	if err := rec.ensureCanaryReplicas(ctx, in); err != nil {
		t.Fatalf("ensureCanaryReplicas: %v", err)
	}

	// 3. Assert stable replicas == 4
	got, err := kc.Clientset.AppsV1().Deployments(ns).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get stable: %v", err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 4 {
		t.Errorf("stable replicas = %v, want 4", got.Spec.Replicas)
	}

	// 4. Assert canary exists at 1 replica with new image
	canary, err := kc.Clientset.AppsV1().Deployments(ns).Get(ctx, svcName+"-canary", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get canary: %v", err)
	}
	if canary.Spec.Replicas == nil || *canary.Spec.Replicas != 1 {
		t.Errorf("canary replicas = %v, want 1", canary.Spec.Replicas)
	}
	if canary.Spec.Template.Spec.Containers[0].Image != "nginx:1.26" {
		t.Errorf("canary image = %q", canary.Spec.Template.Spec.Containers[0].Image)
	}
}

// TestIntegration_ScaleDownCanary_RollbackRestoresStable verifies rollback
// scales canary to 0 and bumps stable back to total replicas.
func TestIntegration_ScaleDownCanary_RollbackRestoresStable(t *testing.T) {
	kc := requireCanaryCluster(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ns := "canary-rb-" + uuid.New().String()[:8]
	svcName := "nginx-rb"
	if _, err := kc.Clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ns: %v", err)
	}
	t.Cleanup(func() {
		_ = kc.Clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	})

	// Create stable at 4 replicas (post-scale-down state that a rollback starts from)
	stableR := int32(4)
	stable := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"app": svcName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &stableR,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": svcName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": svcName}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "nginx", Image: "nginx:1.25"}},
				},
			},
		},
	}
	if _, err := kc.Clientset.AppsV1().Deployments(ns).Create(ctx, stable, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create stable: %v", err)
	}
	// Create canary at 1 replica (mid-rollout state)
	canaryR := int32(1)
	canary := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: svcName + "-canary", Namespace: ns, Labels: map[string]string{"app": svcName}},
		Spec: appsv1.DeploymentSpec{
			Replicas: &canaryR,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": svcName, "enclii.dev/canary-rollout": uuid.New().String()}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": svcName}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "nginx", Image: "nginx:1.26"}},
				},
			},
		},
	}
	if _, err := kc.Clientset.AppsV1().Deployments(ns).Create(ctx, canary, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create canary: %v", err)
	}

	rec := NewCanaryReconciler(kc, nil, logrus.New())
	in := TickInput{
		Rollout:     &types.CanaryRollout{ID: uuid.New(), TotalReplicas: 5, CanaryReplicas: 1, StableReplicas: 4},
		Service:     &types.Service{Name: svcName},
		Environment: &types.Environment{KubeNamespace: ns},
	}
	if err := rec.scaleDownCanary(ctx, in); err != nil {
		t.Fatalf("scaleDownCanary: %v", err)
	}

	// Canary scaled to 0
	c2, err := kc.Clientset.AppsV1().Deployments(ns).Get(ctx, svcName+"-canary", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get canary: %v", err)
	}
	if c2.Spec.Replicas == nil || *c2.Spec.Replicas != 0 {
		t.Errorf("canary replicas after rollback = %v, want 0", c2.Spec.Replicas)
	}
	// Stable restored to 5
	s2, err := kc.Clientset.AppsV1().Deployments(ns).Get(ctx, svcName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get stable: %v", err)
	}
	if s2.Spec.Replicas == nil || *s2.Spec.Replicas != 5 {
		t.Errorf("stable replicas after rollback = %v, want 5", s2.Spec.Replicas)
	}
}
