package k8s

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func IsNamespaceOwnershipError(err error) bool { return errors.Is(err, ErrNamespaceOwnership) }

// PrepareOwnedStagingNamespace requires the API's durable environment/repo
// proof. It refuses untagged or conflicting existing namespaces even if a
// caller has persisted a similarly named environment. Dry runs never mutate.
func (c *Client) PrepareOwnedStagingNamespace(ctx context.Context, namespace string, project uuid.UUID, dryRun bool) error {
	if project == uuid.Nil || !strings.HasSuffix(namespace, "-staging") || len(validation.IsDNS1123Label(namespace)) != 0 || c == nil || c.kubeClient() == nil {
		return ErrNamespaceOwnership
	}
	namespaces := c.kubeClient().CoreV1().Namespaces()
	current, err := namespaces.Get(ctx, namespace, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		if dryRun {
			return nil
		}
		// A concurrent existing namespace is still refused by this helper.
		return c.EnsureOnboardingNamespace(ctx, namespace, project, false)
	}
	if err != nil {
		return err
	}
	if current.UID == "" || current.Labels[LabelProjectNamespace] != project.String() {
		return ErrNamespaceOwnership
	}
	// Refuse a foreign rule before changing namespace labels, including during
	// a dry run. A missing rule can be created; an existing rule needs the
	// centrally managed identity and exact project, not a blank owner label.
	policy, policyErr := c.kubeClient().NetworkingV1().NetworkPolicies(namespace).Get(ctx, "default-deny", metav1.GetOptions{})
	if policyErr != nil && !k8serrors.IsNotFound(policyErr) {
		return policyErr
	}
	if policyErr == nil && (policy.Labels["enclii.dev/managed-by"] != "onboarding-api" || policy.Labels[LabelProjectNamespace] != project.String()) {
		return ErrNamespaceOwnership
	}
	if dryRun {
		return nil
	}
	// Work from the verified object itself and its resourceVersion. A replaced
	// or concurrently changed namespace cannot be adopted through a second read.
	changed := false
	for key, value := range onboardingNamespaceLabels(project) {
		if current.Labels[key] != value {
			current.Labels[key] = value
			changed = true
		}
	}
	if changed {
		if _, err = namespaces.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	if err = c.ensureOnboardingDefaultDeny(ctx, namespace, project); err != nil {
		return err
	}
	final, err := namespaces.Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if final.UID != current.UID || final.Labels[LabelProjectNamespace] != project.String() {
		return ErrNamespaceOwnership
	}
	return nil
}
