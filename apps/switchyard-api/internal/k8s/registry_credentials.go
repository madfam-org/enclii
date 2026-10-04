package k8s

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	registryCredentialName   = "ghcr-credentials" // #nosec G101 -- Kubernetes object name, not a credential
	registryCredentialSource = "enclii"
	registryManagerLabel     = "enclii.dev/managed-by"
	registrySourceLabel      = "enclii.dev/copied-from"
)

// EnsureRegistryCredentials creates missing pull secrets and converges only
// copies previously managed by Enclii. It never adopts another controller's
// secret or changes registry access. Callers must already own the namespace.
// Errors intentionally omit API-server bodies and credential/parser contents.
func (c *Client) EnsureRegistryCredentials(ctx context.Context, targetNamespace, manager string) error {
	if targetNamespace == "" || targetNamespace == registryCredentialSource {
		return fmt.Errorf("registry credential target must be a non-source namespace")
	}
	if !registryCredentialManager(manager) {
		return fmt.Errorf("unrecognized registry credential manager")
	}
	if c == nil || (c.KubeClient == nil && c.Clientset == nil) {
		return fmt.Errorf("registry credential client unavailable")
	}
	targets := c.Kube().CoreV1().Secrets(targetNamespace)
	target, err := targets.Get(ctx, registryCredentialName, metav1.GetOptions{})
	missing := k8serrors.IsNotFound(err)
	if err != nil && !missing {
		return fmt.Errorf("unable to inspect target registry credentials")
	}
	if !missing {
		if err := validateRegistryCredentialTarget(target); err != nil {
			return err
		}
	}
	source, err := c.Kube().CoreV1().Secrets(registryCredentialSource).Get(ctx, registryCredentialName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("canonical registry credentials unavailable")
	}
	if !validRegistryCredentials(source) {
		return fmt.Errorf("canonical registry credentials are malformed")
	}
	if missing {
		target = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: registryCredentialName, Namespace: targetNamespace, Labels: map[string]string{registryManagerLabel: manager, registrySourceLabel: registryCredentialSource}},
			Type:       source.Type, Data: source.DeepCopy().Data,
		}
		if _, err := targets.Create(ctx, target, metav1.CreateOptions{}); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				return fmt.Errorf("registry credential target appeared during reconciliation; retry required")
			}
			return fmt.Errorf("unable to create registry credentials")
		}
		return nil
	}
	if target.Type == source.Type && bytes.Equal(target.Data[corev1.DockerConfigJsonKey], source.Data[corev1.DockerConfigJsonKey]) {
		return nil
	}
	if target.ResourceVersion == "" {
		return fmt.Errorf("registry credential target has no resource version")
	}
	// Preserve namespace-specific labels, annotations and unrelated data. Update
	// carries the inspected resourceVersion: a concurrent change must conflict,
	// never be overwritten by an unconditional patch or an automatic retry.
	updated := target.DeepCopy()
	updated.Type = source.Type
	if updated.Data == nil {
		updated.Data = map[string][]byte{}
	}
	updated.Data[corev1.DockerConfigJsonKey] = bytes.Clone(source.Data[corev1.DockerConfigJsonKey])
	if _, err := targets.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		if k8serrors.IsConflict(err) {
			return fmt.Errorf("registry credentials changed during reconciliation; retry required")
		}
		return fmt.Errorf("unable to update registry credentials")
	}
	return nil
}

func registryCredentialManager(manager string) bool {
	return manager == "switchyard-reconciler" || manager == "onboarding-api"
}

func validateRegistryCredentialTarget(target *corev1.Secret) error {
	if !registryCredentialManager(target.Labels[registryManagerLabel]) || target.Labels[registrySourceLabel] != registryCredentialSource {
		return fmt.Errorf("registry credential target is not an Enclii-managed copy")
	}
	if len(target.OwnerReferences) > 0 {
		return fmt.Errorf("registry credential target has another owner")
	}
	for _, metadata := range []map[string]string{target.Labels, target.Annotations} {
		for key, value := range metadata {
			if strings.HasPrefix(key, "external-secrets.io/") || strings.HasPrefix(key, "reconcile.external-secrets.io/") || (key == "app.kubernetes.io/managed-by" && (value == "external-secrets" || value == "external-secrets-operator")) {
				return fmt.Errorf("registry credential target is managed by External Secrets")
			}
		}
	}
	if target.Immutable != nil && *target.Immutable {
		return fmt.Errorf("registry credential target is immutable")
	}
	return nil
}

func validRegistryCredentials(secret *corev1.Secret) bool {
	if secret.Type != corev1.SecretTypeDockerConfigJson || len(secret.Data) != 1 {
		return false
	}
	var config struct {
		Auths map[string]struct {
			Auth          string `json:"auth"`
			Username      string `json:"username"`
			Password      string `json:"password"`
			IdentityToken string `json:"identitytoken"`
		} `json:"auths"`
	}
	if json.Unmarshal(secret.Data[corev1.DockerConfigJsonKey], &config) != nil {
		return false
	}
	entry, ok := config.Auths["ghcr.io"]
	if !ok {
		return false
	}
	if entry.Auth != "" {
		decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
		if err != nil {
			return false
		}
		user, password, ok := strings.Cut(string(decoded), ":")
		return ok && user != "" && password != ""
	}
	return (entry.Username != "" && entry.Password != "") || entry.IdentityToken != ""
}
