package api

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type januaStageNamespaceRead struct {
	Name                              string `json:"name"`
	State                             string `json:"state"`
	UID                               string `json:"uid,omitempty"`
	ResourceVersion                   string `json:"resource_version,omitempty"`
	SignatureAdmissionLabel           bool   `json:"signature_admission_label"`
	LegacySharedDataAccessLabel       bool   `json:"legacy_shared_data_access_label"`
	LegacySharedPostgresConsumerLabel bool   `json:"legacy_shared_postgres_consumer_label"`
	DNSOnlyBaseline                   string `json:"dns_only_baseline"`
	BaselineUID                       string `json:"baseline_uid,omitempty"`
	BaselineResourceVersion           string `json:"baseline_resource_version,omitempty"`
	PoliciesRead                      int    `json:"policies_read"`
	OtherPoliciesRead                 int    `json:"other_policies_read"`
}

func (h *Handler) readJanuaStageNamespace(ctx context.Context, name string, project uuid.UUID) (januaStageNamespaceRead, error) {
	result := januaStageNamespaceRead{Name: name, State: "ABSENT", DNSOnlyBaseline: "NOT_OBSERVED"}
	namespace, err := h.k8sClient.Kube().CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return result, nil
	}
	if err != nil || namespace.UID == "" || namespace.Labels[k8s.LabelProjectNamespace] != project.String() {
		return result, errJanuaStagePlanRead
	}
	result.State, result.UID, result.ResourceVersion = "OWNED", string(namespace.UID), namespace.ResourceVersion
	result.SignatureAdmissionLabel = namespace.Labels["enclii.dev/verify-signatures"] == "true"
	result.LegacySharedDataAccessLabel = namespace.Labels["enclii.dev/data-access"] == "true"
	result.LegacySharedPostgresConsumerLabel = namespace.Labels["enclii.dev/postgres-consumer"] == "true"
	policies, err := h.k8sClient.Kube().NetworkingV1().NetworkPolicies(name).List(ctx, metav1.ListOptions{})
	if err != nil {
		return result, errJanuaStagePlanRead
	}
	result.PoliciesRead = len(policies.Items)
	for _, policy := range policies.Items {
		if policy.Name != "default-deny" {
			result.OtherPoliciesRead++
			continue
		}
		if policy.UID == "" || policy.Namespace != name || policy.Labels["enclii.dev/managed-by"] != "onboarding-api" || policy.Labels[k8s.LabelProjectNamespace] != project.String() {
			return result, errJanuaStagePlanRead
		}
		result.BaselineUID, result.BaselineResourceVersion = string(policy.UID), policy.ResourceVersion
		result.DNSOnlyBaseline = "DRIFTED"
		if reflect.DeepEqual(policy.Spec, januaStageDNSOnlyBaseline()) {
			result.DNSOnlyBaseline = "EXACT_OWNED"
		}
	}
	return result, nil
}

func januaStageDNSOnlyBaseline() networkingv1.NetworkPolicySpec {
	port := intstr.FromInt32(53)
	tcp, udp := corev1.ProtocolTCP, corev1.ProtocolUDP
	return networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{},
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		Egress:      []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &port}, {Protocol: &tcp, Port: &port}}}},
	}
}

func (h *Handler) readJanuaStageRetainedStorage(ctx context.Context, preview *januaStageAddonPreview) error {
	storage, err := h.k8sClient.Kube().StorageV1().StorageClasses().Get(ctx, preview.Contract.StorageClass, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errJanuaStagePlanRead
	}
	preview.StorageClassRead = true
	preview.StorageClassUID, preview.StorageClassResourceVersion = string(storage.UID), storage.ResourceVersion
	preview.StorageRetainsData = storage.UID != "" && storage.ReclaimPolicy != nil && *storage.ReclaimPolicy == corev1.PersistentVolumeReclaimRetain
	return nil
}
