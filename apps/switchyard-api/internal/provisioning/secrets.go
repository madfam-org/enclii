package provisioning

import (
	"context"
	"fmt"

	k8scorev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// SecretsProvisioner creates K8s Secrets in project namespaces.
type SecretsProvisioner struct {
	clientset kubernetes.Interface
	logger    logging.Logger
}

// NewSecretsProvisioner creates a new secrets provisioner.
func NewSecretsProvisioner(clientset kubernetes.Interface, logger logging.Logger) *SecretsProvisioner {
	return &SecretsProvisioner{
		clientset: clientset,
		logger:    logger,
	}
}

// RemoveEntries deletes the named keys and annotations from an existing
// Secret, leaving every other key untouched. A missing Secret is not an error
// — the desired end state is already true.
func (p *SecretsProvisioner) RemoveEntries(ctx context.Context, namespace, secretName string, keys, annotations []string) error {
	if len(keys) == 0 && len(annotations) == 0 {
		return nil
	}
	secretClient := p.clientset.CoreV1().Secrets(namespace)

	existing, err := secretClient.Get(ctx, secretName, k8smetav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get secret %s/%s: %w", namespace, secretName, err)
	}

	changed := false
	for _, key := range keys {
		if _, ok := existing.Data[key]; ok {
			delete(existing.Data, key)
			changed = true
		}
	}
	for _, key := range annotations {
		if _, ok := existing.Annotations[key]; ok {
			delete(existing.Annotations, key)
			changed = true
		}
	}
	if !changed {
		return nil
	}

	if _, err := secretClient.Update(ctx, existing, k8smetav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update secret %s/%s: %w", namespace, secretName, err)
	}
	p.logger.Info(ctx, "Removed entries from K8s secret",
		logging.String("namespace", namespace),
		logging.String("secret", secretName))
	return nil
}

// R2Auditor returns an auditor over the same cluster client this provisioner
// writes through, so the R2 ownership guard and the credential write can never
// disagree about which cluster they are looking at.
func (p *SecretsProvisioner) R2Auditor() *R2Auditor {
	if p == nil || p.clientset == nil {
		return nil
	}
	return NewR2Auditor(p.clientset)
}

// Create creates or updates a K8s Secret in the given namespace.
// If secretName is empty, defaults to <project>-credentials.
// Rejects placeholder values.
func (p *SecretsProvisioner) Create(ctx context.Context, namespace, project, secretName string, entries []types.SecretEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Validate all entries first
	for _, e := range entries {
		if e.Key == "" {
			return fmt.Errorf("secret entry has empty key")
		}
		if err := ValidateSecretValue(e.Key, e.Value); err != nil {
			return err
		}
	}

	if secretName == "" {
		secretName = project + "-credentials"
	}
	data := make(map[string][]byte, len(entries))
	for _, e := range entries {
		data[e.Key] = []byte(e.Value)
	}

	secretClient := p.clientset.CoreV1().Secrets(namespace)

	// Try to get existing secret
	existing, err := secretClient.Get(ctx, secretName, k8smetav1.GetOptions{})
	if err == nil {
		// Update existing — merge new entries
		if existing.Data == nil {
			existing.Data = make(map[string][]byte)
		}
		for k, v := range data {
			existing.Data[k] = v
		}
		if existing.Labels == nil {
			existing.Labels = make(map[string]string)
		}
		existing.Labels["enclii.dev/updated-by"] = "provisioning-api"

		_, err = secretClient.Update(ctx, existing, k8smetav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("update secret %s/%s: %w", namespace, secretName, err)
		}
		p.logger.Info(ctx, "Updated K8s secret",
			logging.String("namespace", namespace),
			logging.String("secret", secretName))
		return nil
	}

	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("get secret %s/%s: %w", namespace, secretName, err)
	}

	// Create new secret
	secret := &k8scorev1.Secret{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"enclii.dev/managed-by": "provisioning-api",
				"enclii.dev/project":    project,
			},
		},
		Type: k8scorev1.SecretTypeOpaque,
		Data: data,
	}

	_, err = secretClient.Create(ctx, secret, k8smetav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create secret %s/%s: %w", namespace, secretName, err)
	}

	p.logger.Info(ctx, "Created K8s secret",
		logging.String("namespace", namespace),
		logging.String("secret", secretName))

	return nil
}

// AppendEntries adds additional key-value pairs to an existing secret.
// If secretName is empty, defaults to <project>-credentials.
func (p *SecretsProvisioner) AppendEntries(ctx context.Context, namespace, project, secretName string, entries []types.SecretEntry) error {
	return p.AppendEntriesWithAnnotations(ctx, namespace, project, secretName, entries, nil)
}

// AppendEntriesWithAnnotations is AppendEntries plus provenance metadata
// stamped onto the Secret in the same write.
//
// The annotations are load-bearing for the R2 drift guard: they record which
// bucket enclii provisioned for this service, so credentials that were placed
// by hand (or copied from another service) are distinguishable from
// credentials the platform minted.
func (p *SecretsProvisioner) AppendEntriesWithAnnotations(
	ctx context.Context,
	namespace, project, secretName string,
	entries []types.SecretEntry,
	annotations map[string]string,
) error {
	if len(entries) == 0 && len(annotations) == 0 {
		return nil
	}

	// Validate
	for _, e := range entries {
		if err := ValidateSecretValue(e.Key, e.Value); err != nil {
			return err
		}
	}

	if secretName == "" {
		secretName = project + "-credentials"
	}
	secretClient := p.clientset.CoreV1().Secrets(namespace)

	existing, err := secretClient.Get(ctx, secretName, k8smetav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Create from scratch
			if err := p.Create(ctx, namespace, project, secretName, entries); err != nil {
				return err
			}
			if len(annotations) == 0 {
				return nil
			}
			existing, err = secretClient.Get(ctx, secretName, k8smetav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get secret %s/%s: %w", namespace, secretName, err)
			}
		} else {
			return fmt.Errorf("get secret %s/%s: %w", namespace, secretName, err)
		}
	}

	if existing.Data == nil {
		existing.Data = make(map[string][]byte)
	}
	for _, e := range entries {
		existing.Data[e.Key] = []byte(e.Value)
	}
	if len(annotations) > 0 {
		if existing.Annotations == nil {
			existing.Annotations = make(map[string]string, len(annotations))
		}
		for k, v := range annotations {
			existing.Annotations[k] = v
		}
	}

	_, err = secretClient.Update(ctx, existing, k8smetav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update secret %s/%s: %w", namespace, secretName, err)
	}

	p.logger.Info(ctx, "Appended entries to K8s secret",
		logging.String("namespace", namespace),
		logging.String("secret", secretName))

	return nil
}
