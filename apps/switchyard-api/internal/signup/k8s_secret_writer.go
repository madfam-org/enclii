package signup

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

// K8sSecretWriter writes GitHub access tokens into a dedicated K8s Secret
// inside the control-plane's namespace. It implements SecretWriter.
//
// The secret-ref we return follows the RFC 0005 convention:
//
//	"<namespace>/<secret-name>#<key>"
//
// which is the form the rest of enclii already uses for addon creds,
// webhook-signing keys, etc. The raw token never leaves this function's
// stack frame.
type K8sSecretWriter struct {
	client     *k8s.Client
	namespace  string // default: "enclii"
	secretName string // default: "signup-github-tokens"
}

// NewK8sSecretWriter constructs a writer. Pass an empty namespace/
// secretName to use the defaults.
func NewK8sSecretWriter(client *k8s.Client, namespace, secretName string) *K8sSecretWriter {
	if namespace == "" {
		namespace = "enclii"
	}
	if secretName == "" {
		secretName = "signup-github-tokens" // #nosec G101 -- K8s Secret object name, not a credential
	}
	return &K8sSecretWriter{
		client:     client,
		namespace:  namespace,
		secretName: secretName,
	}
}

// WriteGithubToken stores the token under the key "ghat-<signup-id>" in
// the shared signup-github-tokens Secret. Creates the Secret if absent;
// patches otherwise. Returns the secret-ref string.
func (w *K8sSecretWriter) WriteGithubToken(ctx context.Context, signupID uuid.UUID, rawToken string) (string, error) {
	if w.client == nil || !w.client.IsValid() {
		return "", fmt.Errorf("k8s client not available")
	}
	if err := w.client.EnsureNamespace(ctx, w.namespace); err != nil {
		return "", fmt.Errorf("ensure namespace %s: %w", w.namespace, err)
	}

	key := fmt.Sprintf("ghat-%s", signupID)
	secretsAPI := w.client.Clientset.CoreV1().Secrets(w.namespace)

	existing, getErr := secretsAPI.Get(ctx, w.secretName, metav1.GetOptions{})
	if getErr != nil {
		// Assume not found — create.
		newSec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      w.secretName,
				Namespace: w.namespace,
				Labels: map[string]string{
					"enclii.dev/managed-by": "signup-service",
					"enclii.dev/purpose":    "github-oauth-tokens",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				key: []byte(rawToken),
			},
		}
		if _, err := secretsAPI.Create(ctx, newSec, metav1.CreateOptions{}); err != nil {
			return "", fmt.Errorf("create secret: %w", err)
		}
		return w.secretRef(key), nil
	}

	// Update in place.
	if existing.Data == nil {
		existing.Data = map[string][]byte{}
	}
	existing.Data[key] = []byte(rawToken)
	if _, err := secretsAPI.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
		return "", fmt.Errorf("update secret: %w", err)
	}
	return w.secretRef(key), nil
}

func (w *K8sSecretWriter) secretRef(key string) string {
	return fmt.Sprintf("%s/%s#%s", w.namespace, w.secretName, key)
}
