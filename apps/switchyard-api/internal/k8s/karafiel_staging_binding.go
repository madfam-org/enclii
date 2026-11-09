package k8s

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"reflect"
)

// Only a namespace freshly created by the typed Karafiel operation may use
// this bootstrap. Existing shared project namespaces are never reconfigured.
func (c *Client) EnsureKarafielStagingNamespaceBaseline(ctx context.Context, project uuid.UUID, uid string) error {
	if c == nil || c.kubeClient() == nil || project == uuid.Nil || uid == "" {
		return fmt.Errorf("new owned addon namespace required")
	}
	name := "project-" + project.String()[:8]
	n, err := c.kubeClient().CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil || string(n.UID) != uid || n.Labels[LabelProjectNamespace] != project.String() || n.Labels["enclii.dev/managed-by"] != "karafiel-staging-database-v1" || n.Labels["enclii.dev/verify-signatures"] != "true" {
		return fmt.Errorf("new owned addon namespace custody refused")
	}
	// First creation only: no existing workloads or policy set is modified.
	pods, e := c.kubeClient().CoreV1().Pods(name).List(ctx, metav1.ListOptions{})
	if e != nil || pods.Continue != "" || len(pods.Items) != 0 {
		return fmt.Errorf("new addon namespace is not empty")
	}
	policies, e := c.kubeClient().NetworkingV1().NetworkPolicies(name).List(ctx, metav1.ListOptions{})
	if e != nil || policies.Continue != "" || len(policies.Items) != 0 {
		return fmt.Errorf("existing addon namespace policy changes require separate review")
	}
	tcp, udp := corev1.ProtocolTCP, corev1.ProtocolUDP
	port := intstr.FromInt32(53)
	expected := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: name, Labels: map[string]string{LabelProjectNamespace: project.String(), "enclii.dev/managed-by": "karafiel-staging-database-v1"}}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &port}, {Protocol: &tcp, Port: &port}}}}}}
	actual, e := c.kubeClient().NetworkingV1().NetworkPolicies(name).Create(ctx, expected, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: "karafiel-staging-database-v1"})
	if e != nil || actual == nil || actual.UID == "" || !reflect.DeepEqual(expected.Spec, actual.Spec) {
		if apierrors.IsAlreadyExists(e) {
			return fmt.Errorf("namespace policy appeared concurrently; no adoption")
		}
		return fmt.Errorf("new addon baseline creation uncertain")
	}
	return nil
}

// Read-only counterpart to the existing native binding policy builder.
func (c *Client) VerifyBoundAddonEgressPolicy(ctx context.Context, id uuid.UUID, service *types.Service, addon *types.DatabaseAddon) (*networkingv1.NetworkPolicy, error) {
	expected, err := buildBoundAddonEgressPolicy(id, service, addon)
	if err != nil || c == nil || c.kubeClient() == nil {
		return nil, fmt.Errorf("owned native binding unavailable")
	}
	actual, err := c.kubeClient().NetworkingV1().NetworkPolicies(expected.Namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if err != nil || actual.UID == "" || actual.ResourceVersion == "" || actual.DeletionTimestamp != nil || !reflect.DeepEqual(actual.Spec, expected.Spec) {
		return nil, fmt.Errorf("exact native binding policy not observed")
	}
	for key, value := range expected.Labels {
		if actual.Labels[key] != value {
			return nil, fmt.Errorf("native binding policy ownership refused")
		}
	}
	return actual, nil
}
