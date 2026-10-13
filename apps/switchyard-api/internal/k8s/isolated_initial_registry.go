package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Only central GHCR pull custody can enter this project's owned addon namespace.
// This is separate from permissive legacy onboarding copies: conflicts are never
// adopted/overwritten and source credentials never appear in errors or receipts.
func (c *Client) ensureInitialRegistryCustody(ctx context.Context, p IsolatedInitialJobPlan, dryRun, allowCreate bool) error {
	const name, sourceNamespace = "ghcr-credentials", "enclii"
	ns, err := c.kubeClient().CoreV1().Namespaces().Get(ctx, p.Namespace, metav1.GetOptions{})
	if err != nil || ns.UID == "" || p.Namespace != "project-"+p.ProjectID.String()[:8] || ns.Labels[LabelProjectNamespace] != p.ProjectID.String() {
		return ErrNamespaceOwnership
	}
	source, err := c.kubeClient().CoreV1().Secrets(sourceNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || source.UID == "" || source.Type != corev1.SecretTypeDockerConfigJson {
		return fmt.Errorf("central registry pull custody unavailable")
	}
	var config struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if len(source.Data[corev1.DockerConfigJsonKey]) > 65536 || json.Unmarshal(source.Data[corev1.DockerConfigJsonKey], &config) != nil {
		return fmt.Errorf("central registry pull custody malformed")
	}
	entry, found := config.Auths["ghcr.io"]
	var auth struct{ Auth, Username, Password string }
	if !found || json.Unmarshal(entry, &auth) != nil || (auth.Auth == "" && (auth.Username == "" || auth.Password == "")) {
		return fmt.Errorf("central GHCR pull credential unavailable")
	}
	// Copy only the fixed registry, even if central custody has other hosts.
	data, err := json.Marshal(map[string]any{"auths": map[string]json.RawMessage{"ghcr.io": entry}})
	if err != nil {
		return fmt.Errorf("central registry pull custody malformed")
	}
	expected := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: p.Namespace,
		Labels:          map[string]string{"enclii.dev/managed-by": initialJobManager, LabelProjectNamespace: p.ProjectID.String(), "enclii.dev/copied-from": sourceNamespace},
		Annotations:     map[string]string{"enclii.dev/registry-source-uid": string(source.UID)},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: ns.Name, UID: ns.UID}}},
		Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: data}}
	secrets := c.kubeClient().CoreV1().Secrets(p.Namespace)
	existing, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if existing.UID == "" || existing.Type != expected.Type || !reflect.DeepEqual(existing.Data, expected.Data) || !reflect.DeepEqual(existing.OwnerReferences, expected.OwnerReferences) || existing.Annotations["enclii.dev/registry-source-uid"] != string(source.UID) {
			return fmt.Errorf("owned registry pull custody conflicts")
		}
		for key, value := range expected.Labels {
			if existing.Labels[key] != value {
				return fmt.Errorf("owned registry pull custody conflicts")
			}
		}
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("owned registry pull custody unavailable")
	}
	if !allowCreate {
		return fmt.Errorf("owned registry pull custody disappeared")
	}
	options := metav1.CreateOptions{}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	created, err := secrets.Create(ctx, expected, options)
	if err != nil || (!dryRun && created.UID == "") {
		return fmt.Errorf("owned registry pull custody creation refused")
	}
	return nil
}
