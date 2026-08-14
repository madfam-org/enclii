package provisioning

import (
	"context"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSecretsProvisionerUpdatesNamedSecretWithoutLabels(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tulana-secrets",
			Namespace: "tulana",
		},
		Data: map[string][]byte{
			"EXISTING": []byte("value"),
		},
	})
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{
		Level:  "panic",
		Format: "text",
		Output: "stderr",
	})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	provisioner := NewSecretsProvisioner(clientset, logger)
	err = provisioner.Create(context.Background(), "tulana", "tulana", "tulana-secrets", []types.SecretEntry{
		{Key: "NEXT_PUBLIC_OIDC_CLIENT_ID", Value: "client-id"},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	secret, err := clientset.CoreV1().Secrets("tulana").Get(context.Background(), "tulana-secrets", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if string(secret.Data["EXISTING"]) != "value" {
		t.Fatalf("existing key was not preserved")
	}
	if string(secret.Data["NEXT_PUBLIC_OIDC_CLIENT_ID"]) != "client-id" {
		t.Fatalf("new key was not written")
	}
	if secret.Labels["enclii.dev/updated-by"] != "provisioning-api" {
		t.Fatalf("updated-by label = %q, want provisioning-api", secret.Labels["enclii.dev/updated-by"])
	}
}

// TestAppendEntriesWithAnnotations covers the provenance write. The
// annotations are what let the R2 drift guard distinguish credentials the
// platform minted from credentials someone pasted in by hand.
func TestAppendEntriesWithAnnotations(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{
		Level: "panic", Format: "text", Output: "stderr",
	})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	provisioner := NewSecretsProvisioner(clientset, logger)
	ctx := context.Background()

	// Creates the Secret from scratch, then stamps annotations onto it.
	if err := provisioner.AppendEntriesWithAnnotations(ctx, "karafiel", "karafiel", "",
		[]types.SecretEntry{{Key: SecretKeyR2Bucket, Value: "karafiel-documents"}},
		map[string]string{AnnotationR2Bucket: "karafiel-documents", AnnotationR2Project: "karafiel"},
	); err != nil {
		t.Fatalf("AppendEntriesWithAnnotations (create path): %v", err)
	}

	secret, err := clientset.CoreV1().Secrets("karafiel").Get(ctx, "karafiel-credentials", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if string(secret.Data[SecretKeyR2Bucket]) != "karafiel-documents" {
		t.Errorf("bucket = %q", secret.Data[SecretKeyR2Bucket])
	}
	if secret.Annotations[AnnotationR2Bucket] != "karafiel-documents" {
		t.Errorf("annotations = %v, want the provenance bucket recorded", secret.Annotations)
	}

	// Update path: existing keys are preserved, annotations merged.
	if err := provisioner.AppendEntriesWithAnnotations(ctx, "karafiel", "karafiel", "",
		[]types.SecretEntry{{Key: SecretKeyR2AccessKeyID, Value: "key-k"}},
		map[string]string{AnnotationR2TokenName: "enclii-r2-karafiel-documents-1"},
	); err != nil {
		t.Fatalf("AppendEntriesWithAnnotations (update path): %v", err)
	}
	secret, _ = clientset.CoreV1().Secrets("karafiel").Get(ctx, "karafiel-credentials", metav1.GetOptions{})
	if string(secret.Data[SecretKeyR2Bucket]) == "" {
		t.Error("append must not drop existing keys")
	}
	if secret.Annotations[AnnotationR2Bucket] == "" || secret.Annotations[AnnotationR2TokenName] == "" {
		t.Errorf("annotations must merge, got %v", secret.Annotations)
	}
}

// TestRemoveEntries covers the unbind path used by `enclii buckets destroy`:
// R2 keys and provenance go away, everything else survives.
func TestRemoveEntries(t *testing.T) {
	clientset := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "karafiel-credentials",
			Namespace:   "karafiel",
			Annotations: map[string]string{AnnotationR2Bucket: "karafiel-documents", "keep": "me"},
		},
		Data: map[string][]byte{
			SecretKeyR2Bucket:          []byte("karafiel-documents"),
			SecretKeyR2AccessKeyID:     []byte("key-k"),
			SecretKeyR2SecretAccessKey: []byte("secret-k"),
			"DATABASE_URL":             []byte("postgres://localhost/db"),
		},
	})
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{
		Level: "panic", Format: "text", Output: "stderr",
	})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	provisioner := NewSecretsProvisioner(clientset, logger)
	ctx := context.Background()

	if err := provisioner.RemoveEntries(ctx, "karafiel", "karafiel-credentials",
		[]string{SecretKeyR2Bucket, SecretKeyR2AccessKeyID, SecretKeyR2SecretAccessKey},
		[]string{AnnotationR2Bucket},
	); err != nil {
		t.Fatalf("RemoveEntries: %v", err)
	}

	secret, err := clientset.CoreV1().Secrets("karafiel").Get(ctx, "karafiel-credentials", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	for _, key := range []string{SecretKeyR2Bucket, SecretKeyR2AccessKeyID, SecretKeyR2SecretAccessKey} {
		if _, ok := secret.Data[key]; ok {
			t.Errorf("%s should have been removed", key)
		}
	}
	if string(secret.Data["DATABASE_URL"]) != "postgres://localhost/db" {
		t.Error("unrelated keys must survive an unbind")
	}
	if _, ok := secret.Annotations[AnnotationR2Bucket]; ok {
		t.Error("provenance annotation should have been removed")
	}
	if secret.Annotations["keep"] != "me" {
		t.Error("unrelated annotations must survive")
	}

	// Missing Secret is not an error — the end state is already true.
	if err := provisioner.RemoveEntries(ctx, "karafiel", "does-not-exist",
		[]string{SecretKeyR2Bucket}, nil); err != nil {
		t.Errorf("removing from a missing secret should be a no-op, got: %v", err)
	}
}
