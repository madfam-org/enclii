package k8s

import (
	"context"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// PrepareJanuaStagingNamespace is a closed, create-only staging prerequisite.
// A replay must present the namespace UID retained by its native birth receipt.
// Matching labels alone never authorize adoption, label repair or deletion.
// The HTTP staging adapters do not enable this writer until BatchA review.
func (c *Client) PrepareJanuaStagingNamespace(ctx context.Context, name string, project uuid.UUID, expectedUID k8stypes.UID, revalidate func(context.Context) error, dryRun bool) (*corev1.Namespace, error) {
	if c == nil || c.kubeClient() == nil || project == uuid.Nil || revalidate == nil || (name != "janua-staging" && name != "project-"+project.String()[:8]) {
		return nil, ErrNamespaceOwnership
	}
	labels := onboardingNamespaceLabels(project)
	for _, key := range []string{"enclii.dev/data-access", "enclii.dev/postgres-consumer", "enclii.dev/redis-consumer"} {
		labels[key] = "false"
	}
	labels["enclii.dev/staging-profile"] = "janua-api-staging-v1"
	desired := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: name, Labels: map[string]string{"enclii.dev/managed-by": "onboarding-api", LabelProjectNamespace: project.String()}}, Spec: JanuaStagingDNSOnlyBaseline()}

	namespaces := c.kubeClient().CoreV1().Namespaces()
	current, err := namespaces.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if expectedUID == "" || current.UID != expectedUID || current.ResourceVersion == "" || current.DeletionTimestamp != nil {
			return nil, ErrNamespaceOwnership
		}
		if !januaNamespaceLabelsExact(current, desired) {
			return nil, ErrNamespaceOwnership
		}
		actual, readErr := c.kubeClient().NetworkingV1().NetworkPolicies(name).Get(ctx, policy.Name, metav1.GetOptions{})
		if readErr != nil || actual.UID == "" || actual.ResourceVersion == "" || actual.DeletionTimestamp != nil || !reflect.DeepEqual(actual.Spec, policy.Spec) || !reflect.DeepEqual(actual.Labels, policy.Labels) {
			return nil, ErrNamespaceOwnership
		}
		return current, nil
	}
	if !k8serrors.IsNotFound(err) || expectedUID != "" {
		return nil, ErrNamespaceOwnership
	}
	options := metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: "janua-api-staging-v1"}
	if dryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	if err := revalidate(ctx); err != nil {
		return nil, ErrNamespaceOwnership
	}
	created, err := namespaces.Create(ctx, desired, options)
	if err != nil || !januaNamespaceLabelsExact(created, desired) || (!dryRun && (created.UID == "" || created.ResourceVersion == "")) {
		return nil, fmt.Errorf("Janua staging namespace create uncertain; no adoption or retry")
	}
	if dryRun {
		return created, nil // the child namespace does not exist for policy admission
	}
	// No rollback or implicit repair. An uncertain child write retains evidence
	// and holds the operation until its exact server UID is reviewed.
	if err := revalidate(ctx); err != nil {
		return nil, ErrNamespaceOwnership
	}
	current, err = namespaces.Get(ctx, name, metav1.GetOptions{})
	if err != nil || current.UID != created.UID || current.ResourceVersion != created.ResourceVersion || !januaNamespaceLabelsExact(current, desired) {
		return nil, ErrNamespaceOwnership
	}
	policy.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Namespace", Name: name, UID: created.UID}}
	createdPolicy, err := c.kubeClient().NetworkingV1().NetworkPolicies(name).Create(ctx, policy, options)
	if err != nil || createdPolicy == nil || createdPolicy.UID == "" || createdPolicy.ResourceVersion == "" || !reflect.DeepEqual(createdPolicy.Spec, policy.Spec) || !reflect.DeepEqual(createdPolicy.Labels, policy.Labels) || !reflect.DeepEqual(createdPolicy.OwnerReferences, policy.OwnerReferences) {
		return nil, fmt.Errorf("Janua staging namespace baseline incomplete; operation held")
	}
	final, err := namespaces.Get(ctx, name, metav1.GetOptions{})
	if err != nil || final.UID != created.UID || final.ResourceVersion != created.ResourceVersion || !januaNamespaceLabelsExact(final, desired) {
		return nil, ErrNamespaceOwnership
	}
	return final, nil
}

func januaNamespaceLabelsExact(actual, desired *corev1.Namespace) bool {
	if actual == nil || desired == nil || actual.Name != desired.Name || actual.DeletionTimestamp != nil || len(actual.OwnerReferences) != 0 {
		return false
	}
	for key, value := range desired.Labels {
		if actual.Labels[key] != value {
			return false
		}
	}
	return true
}

// JanuaStagingDNSOnlyBaseline is shared by the closed writer and readonly
// observers. Legacy generic namespace/DNS profiles remain unchanged.
func JanuaStagingDNSOnlyBaseline() networkingv1.NetworkPolicySpec {
	port, tcp, udp := intstr.FromInt32(53), corev1.ProtocolTCP, corev1.ProtocolUDP
	return networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &port}, {Protocol: &tcp, Port: &port}}}}}
}
