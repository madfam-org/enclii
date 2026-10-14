package k8s

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestNamespaceExistsSeparatesNotFoundFromOtherErrors(t *testing.T) {
	ctx := context.Background()

	present := &Client{KubeClient: fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "synthetic-training"}})}
	if ok, err := present.NamespaceExists(ctx, "synthetic-training"); !ok || err != nil {
		t.Fatalf("existing namespace: got %v, %v", ok, err)
	}

	missing := &Client{KubeClient: fake.NewSimpleClientset()}
	if ok, err := missing.NamespaceExists(ctx, "synthetic-training"); ok || err != nil {
		t.Fatalf("missing namespace: got %v, %v", ok, err)
	}

	kube := fake.NewSimpleClientset()
	kube.PrependReactor("get", "namespaces", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewForbidden(schema.GroupResource{Resource: "namespaces"}, "synthetic-training", errors.New("RBAC: access denied"))
	})
	failing := &Client{KubeClient: kube}
	if ok, err := failing.NamespaceExists(ctx, "synthetic-training"); ok || !k8serrors.IsForbidden(err) {
		t.Fatalf("lookup failure must be returned, got %v, %v", ok, err)
	}

	if _, err := (*Client)(nil).NamespaceExists(ctx, "synthetic-training"); err == nil {
		t.Fatal("nil client must fail")
	}
}

func TestIsNamespacedKindRequiresIdentity(t *testing.T) {
	c := &Client{}
	if _, err := c.IsNamespacedKind("", "Deployment"); err == nil || err.Error() != "manifest missing apiVersion" {
		t.Fatalf("missing apiVersion: %v", err)
	}
	if _, err := c.IsNamespacedKind("apps/v1", ""); err == nil || err.Error() != "manifest missing kind" {
		t.Fatalf("missing kind: %v", err)
	}
}
