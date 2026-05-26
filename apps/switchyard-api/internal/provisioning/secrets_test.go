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
