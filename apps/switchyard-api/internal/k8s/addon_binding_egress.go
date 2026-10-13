package k8s

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"

	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const bindingPolicyManager = "addon-binding-api"
const bindingPolicyLabel = "enclii.dev/binding-id"

// EnsureBoundAddonEgressPolicy opens only a persisted service's connection to
// its own dedicated addon. It takes no caller-supplied peer or namespace.
func (c *Client) EnsureBoundAddonEgressPolicy(ctx context.Context, bindingID uuid.UUID, service *sdktypes.Service, addon *sdktypes.DatabaseAddon) error {
	desired, err := buildBoundAddonEgressPolicy(bindingID, service, addon)
	if err != nil {
		return err
	}
	if c == nil || c.kubeClient() == nil {
		return fmt.Errorf("addon binding network client is unavailable")
	}
	for _, namespace := range []string{desired.Namespace, addon.K8sNamespace} {
		ns, err := c.kubeClient().CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("addon binding namespace ownership cannot be verified")
		}
		if ns.Labels[LabelProjectNamespace] != addon.ProjectID.String() {
			return fmt.Errorf("addon binding namespace does not belong to the owning project")
		}
	}
	policies := c.kubeClient().NetworkingV1().NetworkPolicies(desired.Namespace)
	existing, err := policies.Get(ctx, desired.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = policies.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	for key, value := range desired.Labels {
		if existing.Labels[key] != value {
			return fmt.Errorf("refused to adopt a network policy belonging to another binding")
		}
	}
	existing.Spec = desired.Spec
	_, err = policies.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// RemoveBoundAddonEgressPolicy withdraws only this binding's owned rule. A
// missing rule is already closed; another owner's object is never deleted.
func (c *Client) RemoveBoundAddonEgressPolicy(ctx context.Context, namespace string, projectID, bindingID uuid.UUID) error {
	if projectID == uuid.Nil || bindingID == uuid.Nil || len(validation.IsDNS1123Label(namespace)) != 0 {
		return fmt.Errorf("invalid addon binding network identity")
	}
	if c == nil || c.kubeClient() == nil {
		return fmt.Errorf("addon binding network client is unavailable")
	}
	policies := c.kubeClient().NetworkingV1().NetworkPolicies(namespace)
	existing, err := policies.Get(ctx, bindingPolicyName(bindingID), metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing.Labels["enclii.dev/managed-by"] != bindingPolicyManager || existing.Labels[LabelProjectNamespace] != projectID.String() || existing.Labels[bindingPolicyLabel] != bindingID.String() {
		return fmt.Errorf("refused to remove a network policy belonging to another binding")
	}
	uid := existing.UID
	err = policies.Delete(ctx, existing.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func bindingPolicyName(id uuid.UUID) string { return "addon-binding-" + id.String() }

func buildBoundAddonEgressPolicy(bindingID uuid.UUID, service *sdktypes.Service, addon *sdktypes.DatabaseAddon) (*networkingv1.NetworkPolicy, error) {
	if bindingID == uuid.Nil || service == nil || addon == nil || service.ID == uuid.Nil || addon.ID == uuid.Nil || addon.ProjectID == uuid.Nil || service.ProjectID != addon.ProjectID {
		return nil, fmt.Errorf("addon and service must belong to the same project")
	}
	if service.K8sNamespace == nil || len(validation.IsDNS1123Label(*service.K8sNamespace)) != 0 || len(validation.IsDNS1123Label(service.Name)) != 0 {
		return nil, fmt.Errorf("addon binding requires a recorded valid service namespace and name")
	}
	expectedNamespace := "project-" + addon.ProjectID.String()[:8]
	expectedResource := fmt.Sprintf("pg-%s-%s", addon.Name, addon.ID.String()[:8])
	if addon.Type != sdktypes.DatabaseAddonTypePostgres || addon.Plan == "shared-discovered" || addon.Status != sdktypes.DatabaseAddonStatusReady || addon.K8sNamespace != expectedNamespace || addon.K8sResourceName != expectedResource || len(validation.IsDNS1123Label(expectedResource)) != 0 {
		return nil, fmt.Errorf("addon binding requires its owning project's ready dedicated PostgreSQL resource")
	}
	port := intstr.FromInt32(5432)
	protocol := corev1.ProtocolTCP
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingPolicyName(bindingID), Namespace: *service.K8sNamespace,
			Labels: map[string]string{
				"enclii.dev/managed-by": bindingPolicyManager,
				LabelProjectNamespace:   addon.ProjectID.String(),
				bindingPolicyLabel:      bindingID.String(),
				"enclii.dev/service-id": service.ID.String(),
				"enclii.dev/addon-id":   addon.ID.String(),
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"enclii.dev/service": service.Name}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": expectedNamespace, LabelProjectNamespace: addon.ProjectID.String(),
					}},
					PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": expectedResource}},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}},
			}},
		},
	}, nil
}
