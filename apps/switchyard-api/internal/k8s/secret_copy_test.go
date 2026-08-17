package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// The 2026-08-17 audit found the copied backup credential shipped into every
// tenant namespace with no owner (never GC'd) and mutable (tenant could swap
// it). These invariants are the fix and must not regress.

func testOwner() SecretCopyOwner {
	return SecretCopyOwner{
		APIVersion: "postgresql.cnpg.io/v1",
		Kind:       "Cluster",
		Name:       "pg-map-abc12345",
		UID:        k8stypes.UID("11111111-1111-4111-8111-111111111111"),
	}
}

func srcSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "enclii-db-backup-credentials", Namespace: "enclii"},
		Data:       map[string][]byte{"ACCESS_KEY_ID": []byte("ak"), "SECRET_ACCESS_KEY": []byte("sk")},
	}
}

func TestCopiedSecretIsOwnedAndImmutable(t *testing.T) {
	c := &Client{KubeClient: fake.NewSimpleClientset(srcSecret())}
	if err := c.EnsureSecretCopiedFrom(context.Background(), "enclii", "enclii-db-backup-credentials", "project-crea", testOwner()); err != nil {
		t.Fatalf("copy: %v", err)
	}
	got, err := c.KubeClient.CoreV1().Secrets("project-crea").Get(context.Background(), "enclii-db-backup-credentials", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if got.Immutable == nil || !*got.Immutable {
		t.Fatal("copied secret must be Immutable — a tenant must not be able to swap the credential")
	}
	if len(got.OwnerReferences) != 1 || got.OwnerReferences[0].UID != testOwner().UID {
		t.Fatalf("copied secret must carry the Cluster OwnerReference for GC; got %v", got.OwnerReferences)
	}
	if string(got.Data["ACCESS_KEY_ID"]) != "ak" {
		t.Fatal("data must copy through")
	}
}

func TestExistingUnownedCopyIsReplaced(t *testing.T) {
	// A pre-existing copy from the old (unowned, mutable) code path must be
	// upgraded, not left in place.
	stale := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "enclii-db-backup-credentials", Namespace: "project-crea"},
		Data:       map[string][]byte{"ACCESS_KEY_ID": []byte("ak"), "SECRET_ACCESS_KEY": []byte("sk")},
	}
	c := &Client{KubeClient: fake.NewSimpleClientset(srcSecret(), stale)}
	if err := c.EnsureSecretCopiedFrom(context.Background(), "enclii", "enclii-db-backup-credentials", "project-crea", testOwner()); err != nil {
		t.Fatalf("copy: %v", err)
	}
	got, _ := c.KubeClient.CoreV1().Secrets("project-crea").Get(context.Background(), "enclii-db-backup-credentials", metav1.GetOptions{})
	if got.Immutable == nil || len(got.OwnerReferences) == 0 {
		t.Fatal("a stale unowned/mutable copy must be replaced with an owned+immutable one")
	}
}
