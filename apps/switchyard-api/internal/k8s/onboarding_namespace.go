package k8s

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
)

var ErrNamespaceOwnership = errors.New("namespace ownership requires a matching registered project, repository and namespace")

// EnsureOnboardingNamespace creates a new project namespace, or updates only
// one whose ownership the caller has proved from a persisted registration.
// registeredOwner must never be taken from request JSON or namespace labels.
func (c *Client) EnsureOnboardingNamespace(ctx context.Context, namespace string, projectID uuid.UUID, registeredOwner bool) error {
	if projectID == uuid.Nil || len(validation.IsDNS1123Label(namespace)) != 0 {
		return ErrNamespaceOwnership
	}
	if c == nil || c.kubeClient() == nil {
		return fmt.Errorf("namespace network client is unavailable")
	}
	namespaces := c.kubeClient().CoreV1().Namespaces()
	ns, err := namespaces.Get(ctx, namespace, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: onboardingNamespaceLabels(projectID)}}
		created, createErr := namespaces.Create(ctx, ns, metav1.CreateOptions{})
		if createErr != nil {
			// A race must not convert another caller's namespace into ours.
			if k8serrors.IsAlreadyExists(createErr) {
				return ErrNamespaceOwnership
			}
			return fmt.Errorf("create owned onboarding namespace: %w", createErr)
		}
		if denyErr := c.ensureOnboardingDefaultDeny(ctx, namespace, projectID); denyErr != nil {
			// No Argo/workload effect is allowed until this prerequisite succeeds.
			// Retire only the namespace this call actually created, by server UID,
			// so a transient policy failure does not strand a non-retryable name.
			if cleanupErr := c.rollbackNewOnboardingNamespace(ctx, created, projectID); cleanupErr != nil {
				return fmt.Errorf("default-deny failed; newly owned namespace requires explicit cleanup: %w", cleanupErr)
			}
			return fmt.Errorf("default-deny failed; newly created namespace rolled back: %w", denyErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read onboarding namespace: %w", err)
	}
	if !registeredOwner || (ns.Labels[LabelProjectNamespace] != "" && ns.Labels[LabelProjectNamespace] != projectID.String()) {
		return ErrNamespaceOwnership
	}
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	changed := false
	for key, value := range onboardingNamespaceLabels(projectID) {
		if ns.Labels[key] != value {
			ns.Labels[key] = value
			changed = true
		}
	}
	if changed {
		if _, err = namespaces.Update(ctx, ns, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update owned onboarding namespace: %w", err)
		}
	}
	return c.ensureOnboardingDefaultDeny(ctx, namespace, projectID)
}

func (c *Client) rollbackNewOnboardingNamespace(ctx context.Context, created *corev1.Namespace, projectID uuid.UUID) error {
	if created == nil || created.UID == "" {
		return ErrNamespaceOwnership
	}
	namespaces := c.kubeClient().CoreV1().Namespaces()
	current, err := namespaces.Get(ctx, created.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.UID != created.UID || current.Labels[LabelProjectNamespace] != projectID.String() {
		return ErrNamespaceOwnership
	}
	uid := created.UID
	err = namespaces.Delete(ctx, created.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func onboardingNamespaceLabels(projectID uuid.UUID) map[string]string {
	return map[string]string{
		"managed-by": "enclii", "enclii.dev/type": "application",
		"enclii.dev/verify-signatures": "true", "enclii.dev/data-access": "true",
		LabelProjectNamespace: projectID.String(),
	}
}

// Bounded onboarding uses its injectable client while the legacy namespace
// helper stays unchanged. Reconcile only the centrally owned default rule.
func (c *Client) ensureOnboardingDefaultDeny(ctx context.Context, namespace string, projectID uuid.UUID) error {
	port := intstr.FromInt32(53)
	tcp, udp := corev1.ProtocolTCP, corev1.ProtocolUDP
	desired := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: namespace, Labels: map[string]string{"enclii.dev/managed-by": "onboarding-api", LabelProjectNamespace: projectID.String()}},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Egress:      []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &port}, {Protocol: &tcp, Port: &port}}}},
		},
	}
	policies := c.kubeClient().NetworkingV1().NetworkPolicies(namespace)
	existing, err := policies.Get(ctx, desired.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = policies.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Labels["enclii.dev/managed-by"] != "onboarding-api" || (existing.Labels[LabelProjectNamespace] != "" && existing.Labels[LabelProjectNamespace] != projectID.String()) {
		return ErrNamespaceOwnership
	}
	if reflect.DeepEqual(existing.Spec, desired.Spec) && existing.Labels[LabelProjectNamespace] == projectID.String() {
		return nil
	}
	existing.Spec = desired.Spec
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[LabelProjectNamespace] = projectID.String()
	_, err = policies.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}
