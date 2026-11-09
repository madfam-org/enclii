package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"sort"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var errJanuaStageAdmission = fmt.Errorf("closed staging admission prerequisites refused; no resources were persisted")

type januaStageAdmissionSource struct {
	ProjectID        uuid.UUID
	EnvironmentID    uuid.UUID
	RegistrationID   uuid.UUID
	Service          *types.Service
	Plan             *db.ManagedDBPlan
	Namespaces       []januaStageNamespaceRead
	StorageUID       string
	StorageVersion   string
	Peers            []januaStageAPIPeer
	EndpointVersions []string
	OperatorSelector metav1.LabelSelector
	OperatorVersions []string
}

// Endpoint identities are private runtime metadata. They are never included
// in the HTTP response, error, log or source descriptor.
type januaStageAPIPeer struct {
	HostRoute string
	Port      int32
}

func (h *Handler) readJanuaStageAdmissionSource(ctx context.Context, serviceID uuid.UUID, code string) (*januaStageAdmissionSource, error) {
	p, err := h.repos.Projects.GetBySlug("janua")
	if err != nil || p == nil {
		return nil, errJanuaStageAdmission
	}
	e, envErr := h.repos.Environments.GetByProjectAndName(p.ID, "staging")
	r, repoErr := h.repos.Onboardings.GetByRepo(ctx, januaStageRepository)
	if envErr != nil || repoErr != nil || !stagingNamespaceProof(p, e, r, januaStageRepository) {
		return nil, errJanuaStageAdmission
	}
	s, err := h.repos.Services.GetByID(serviceID)
	if err != nil || !januaStageServiceOwned(p, e, s) {
		return nil, errJanuaStageAdmission
	}
	plan, err := h.repos.ManagedDBPlans.GetByCode(ctx, code)
	if err != nil || !januaStageCatalogPlanValid(plan) || plan.MaxConnections <= 0 {
		return nil, errJanuaStageAdmission
	}
	rows, err := h.repos.DatabaseAddons.ListByProject(ctx, p.ID)
	if err != nil {
		return nil, errJanuaStageAdmission
	}
	addonNamespace := "project-" + p.ID.String()[:8]
	for _, a := range rows {
		if a == nil || a.ProjectID != p.ID || a.Name == fixedJanuaStageDatabaseContract().AddonName || a.K8sNamespace == addonNamespace || a.K8sResourceName == januaStageClusterName || (a.EnvironmentID != nil && *a.EnvironmentID == e.ID) {
			return nil, errJanuaStageAdmission
		}
	}
	result := &januaStageAdmissionSource{ProjectID: p.ID, EnvironmentID: e.ID, RegistrationID: r.ID, Service: s, Plan: plan}
	for _, name := range []string{e.KubeNamespace, addonNamespace} {
		n, err := h.readJanuaStageNamespace(ctx, name, p.ID)
		if err != nil || n.State != "OWNED" || n.ResourceVersion == "" || !n.SignatureAdmissionLabel || n.DNSOnlyBaseline != "EXACT_OWNED" || n.BaselineResourceVersion == "" || n.OtherPoliciesRead != 0 || n.PoliciesRead != 1 || n.LegacySharedDataAccessLabel || n.LegacySharedPostgresConsumerLabel {
			return nil, errJanuaStageAdmission
		}
		result.Namespaces = append(result.Namespaces, n)
	}
	preview := januaStageAddonPreview{Contract: fixedJanuaStageDatabaseContract()}
	if h.readJanuaStageRetainedStorage(ctx, &preview) != nil || !preview.StorageRetainsData || preview.StorageClassResourceVersion == "" {
		return nil, errJanuaStageAdmission
	}
	result.StorageUID, result.StorageVersion = preview.StorageClassUID, preview.StorageClassResourceVersion
	clusters, err := h.k8sClient.DynamicClient.Resource(januaStageClusterGVR).Namespace(addonNamespace).List(ctx, metav1.ListOptions{})
	if err != nil || clusters.GetContinue() != "" || (clusters.GetRemainingItemCount() != nil && *clusters.GetRemainingItemCount() > 0) || len(clusters.Items) != 0 {
		return nil, errJanuaStageAdmission
	}
	result.Peers, result.EndpointVersions, err = h.readJanuaStageAPIPeers(ctx)
	if err != nil {
		return nil, errJanuaStageAdmission
	}
	result.OperatorSelector, result.OperatorVersions, err = h.readJanuaStageOperator(ctx)
	if err != nil {
		return nil, errJanuaStageAdmission
	}
	return result, nil
}

// Read the canonical selectorless Kubernetes API Service and Endpoints, not
// arbitrary caller IPs or a broad private range. Endpoints is the apiserver's
// own fixed native profile; an unknown, partial or not-ready set fails closed.
// Both destination translations are exact /32 or /128 routes. Re-reads bind
// resource versions around the admission, but do not prove live CNI behavior.
func (h *Handler) readJanuaStageAPIPeers(ctx context.Context) ([]januaStageAPIPeer, []string, error) {
	service, err := h.k8sClient.Kube().CoreV1().Services("default").Get(ctx, "kubernetes", metav1.GetOptions{})
	if err != nil || service.UID == "" || service.ResourceVersion == "" || service.DeletionTimestamp != nil || service.Spec.Type != corev1.ServiceTypeClusterIP || len(service.Spec.Selector) != 0 || service.Spec.ExternalName != "" || len(service.Spec.ExternalIPs) != 0 || service.Labels["component"] != "apiserver" || service.Labels["provider"] != "kubernetes" || len(service.Spec.Ports) != 1 {
		return nil, nil, errJanuaStageAdmission
	}
	port := service.Spec.Ports[0]
	if port.Protocol != corev1.ProtocolTCP || port.Port <= 0 || port.TargetPort.Type != intstr.Int || port.TargetPort.IntVal <= 0 {
		return nil, nil, errJanuaStageAdmission
	}
	endpoints, err := h.k8sClient.Kube().CoreV1().Endpoints("default").Get(ctx, "kubernetes", metav1.GetOptions{})
	if err != nil || endpoints.UID == "" || endpoints.ResourceVersion == "" || endpoints.DeletionTimestamp != nil || endpoints.Annotations["endpoints.kubernetes.io/over-capacity"] != "" || len(endpoints.Subsets) == 0 || len(endpoints.Subsets) > 16 {
		return nil, nil, errJanuaStageAdmission
	}
	peers := map[januaStageAPIPeer]bool{}
	add := func(raw string, p int32) error {
		ip, err := netip.ParseAddr(raw)
		if err != nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" || p <= 0 || p > 65535 {
			return errJanuaStageAdmission
		}
		ip = ip.Unmap()
		peers[januaStageAPIPeer{HostRoute: netip.PrefixFrom(ip, ip.BitLen()).String(), Port: p}] = true
		return nil
	}
	if len(service.Spec.ClusterIPs) == 0 || len(service.Spec.ClusterIPs) > 2 || service.Spec.ClusterIPs[0] != service.Spec.ClusterIP {
		return nil, nil, errJanuaStageAdmission
	}
	for _, ip := range service.Spec.ClusterIPs {
		if add(ip, port.Port) != nil {
			return nil, nil, errJanuaStageAdmission
		}
	}
	for _, subset := range endpoints.Subsets {
		if len(subset.NotReadyAddresses) != 0 || len(subset.Addresses) == 0 || len(subset.Addresses) > 16 || len(subset.Ports) != 1 || subset.Ports[0].Protocol != corev1.ProtocolTCP || subset.Ports[0].Port != port.TargetPort.IntVal || subset.Ports[0].Name != port.Name {
			return nil, nil, errJanuaStageAdmission
		}
		for _, address := range subset.Addresses {
			if add(address.IP, subset.Ports[0].Port) != nil {
				return nil, nil, errJanuaStageAdmission
			}
		}
	}
	result := make([]januaStageAPIPeer, 0, len(peers))
	for peer := range peers {
		result = append(result, peer)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].HostRoute == result[j].HostRoute {
			return result[i].Port < result[j].Port
		}
		return result[i].HostRoute < result[j].HostRoute
	})
	return result, []string{string(service.UID), service.ResourceVersion, string(endpoints.UID), endpoints.ResourceVersion}, nil
}

func (h *Handler) readJanuaStageOperator(ctx context.Context) (metav1.LabelSelector, []string, error) {
	n, err := h.k8sClient.Kube().CoreV1().Namespaces().Get(ctx, "cnpg-system", metav1.GetOptions{})
	if err != nil || n.UID == "" || n.ResourceVersion == "" || n.DeletionTimestamp != nil || n.Labels["kubernetes.io/metadata.name"] != "cnpg-system" {
		return metav1.LabelSelector{}, nil, errJanuaStageAdmission
	}
	d, err := h.k8sClient.Kube().AppsV1().Deployments("cnpg-system").List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/name=cloudnative-pg"})
	if err != nil || d.Continue != "" || (d.RemainingItemCount != nil && *d.RemainingItemCount > 0) || len(d.Items) != 1 {
		return metav1.LabelSelector{}, nil, errJanuaStageAdmission
	}
	operator := d.Items[0]
	if operator.UID == "" || operator.ResourceVersion == "" || operator.DeletionTimestamp != nil || operator.Spec.Selector == nil || len(operator.Spec.Selector.MatchExpressions) != 0 || len(operator.Spec.Selector.MatchLabels) == 0 || operator.Spec.Selector.MatchLabels["app.kubernetes.io/name"] != "cloudnative-pg" || operator.Spec.Template.Spec.HostNetwork || operator.Status.ObservedGeneration != operator.Generation || operator.Status.AvailableReplicas < 1 {
		return metav1.LabelSelector{}, nil, errJanuaStageAdmission
	}
	selector, err := metav1.LabelSelectorAsSelector(operator.Spec.Selector)
	if err != nil || !selector.Matches(labels.Set(operator.Spec.Template.Labels)) {
		return metav1.LabelSelector{}, nil, errJanuaStageAdmission
	}
	return *operator.Spec.Selector.DeepCopy(), []string{string(n.UID), n.ResourceVersion, string(operator.UID), operator.ResourceVersion}, nil
}

func januaStageSourceFingerprint(source *januaStageAdmissionSource) string {
	// Only authority-relevant persisted service fields enter the observation;
	// build configs, unrelated production addon data and credentials never do.
	projection := struct {
		Project, Environment, Registration uuid.UUID
		ServiceID                          uuid.UUID
		Name, Repository, Path, Namespace  string
		Plan                               *db.ManagedDBPlan
		Namespaces                         []januaStageNamespaceRead
		Storage                            []string
		Peers                              []januaStageAPIPeer
		Endpoints                          []string
		Operator                           metav1.LabelSelector
		OperatorVersions                   []string
	}{
		Project: source.ProjectID, Environment: source.EnvironmentID, Registration: source.RegistrationID, ServiceID: source.Service.ID, Name: source.Service.Name, Repository: source.Service.GitRepo, Path: source.Service.AppPath, Namespace: *source.Service.K8sNamespace, Plan: source.Plan, Namespaces: source.Namespaces, Storage: []string{source.StorageUID, source.StorageVersion}, Peers: source.Peers, Endpoints: source.EndpointVersions, Operator: source.OperatorSelector, OperatorVersions: source.OperatorVersions,
	}
	encoded, _ := json.Marshal(projection)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func januaStageAdmissionObjectUnchanged(expected, actual any) bool {
	return reflect.DeepEqual(expected, actual)
}
