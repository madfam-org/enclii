package addons

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"

	"encoding/json"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Fixed API endpoint peers are resolved from Kubernetes Service/Endpoints by
// the server. A caller cannot supply IP ranges or ports to the HTTP adapter.
type KarafielControlPeer struct {
	HostRoute string
	Port      int32
}

func KarafielStagingPolicies(a *types.DatabaseAddon, peers []KarafielControlPeer, operator metav1.LabelSelector) ([]*networkingv1.NetworkPolicy, error) {
	if a == nil || a.EnvironmentID == nil || !KarafielStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID) || len(peers) == 0 || len(operator.MatchLabels) == 0 || len(operator.MatchExpressions) != 0 {
		return nil, fmt.Errorf("closed staging network authority refused")
	}
	namespace := "project-" + a.ProjectID.String()[:8]
	cluster := fmt.Sprintf("pg-%s-%s", a.Name, a.ID.String()[:8])
	selector := metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": cluster}}
	labels := map[string]string{LabelProjectID: a.ProjectID.String(), LabelAddonID: a.ID.String(), "enclii.dev/managed-by": KarafielStagingProfile}
	metadata := func(name, ns string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}
	}
	port := func(p int32) []networkingv1.NetworkPolicyPort {
		tcp := corev1.ProtocolTCP
		number := intstr.FromInt32(p)
		return []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &number}}
	}
	ns := func(name string, owned bool) *metav1.LabelSelector {
		values := map[string]string{"kubernetes.io/metadata.name": name}
		if owned {
			values[k8s.LabelProjectNamespace] = a.ProjectID.String()
		}
		return &metav1.LabelSelector{MatchLabels: values}
	}
	clusterPeer := networkingv1.NetworkPolicyPeer{PodSelector: &selector}
	egress := []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{clusterPeer}, Ports: port(5432)}}
	for _, peer := range peers {
		prefix, err := netip.ParsePrefix(peer.HostRoute)
		if err != nil || prefix.Bits() != prefix.Addr().BitLen() || !prefix.Addr().IsGlobalUnicast() || peer.Port < 1 || peer.Port > 65535 {
			return nil, fmt.Errorf("canonical API endpoint peer refused")
		}
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: peer.HostRoute}}}, Ports: port(peer.Port)})
	}
	dns := intstr.FromInt32(53)
	tcp, udp := corev1.ProtocolTCP, corev1.ProtocolUDP
	egress = append(egress, networkingv1.NetworkPolicyEgressRule{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns("kube-system", false)}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: &dns}, {Protocol: &tcp, Port: &dns}}})
	database := &networkingv1.NetworkPolicy{ObjectMeta: metadata(cluster+"-staging-isolation", namespace), Spec: networkingv1.NetworkPolicySpec{PodSelector: selector, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Egress: egress, Ingress: []networkingv1.NetworkPolicyIngressRule{
		{From: []networkingv1.NetworkPolicyPeer{clusterPeer}, Ports: port(5432)},
		{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns(KarafielStagingNamespace, true), PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"enclii.dev/service": KarafielStagingService}}}}, Ports: port(5432)},
		{From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"enclii.dev/bootstrap-profile": "karafiel-staging-migrate", "enclii.dev/addon-id": a.ID.String(), k8s.LabelProjectNamespace: a.ProjectID.String()}}}}, Ports: port(5432)},
		{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns("enclii", false), PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "switchyard-api"}}}}, Ports: port(5432)},
		{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns(CNPGOperatorNamespace, false), PodSelector: &operator}}, Ports: port(8000)},
	}}}
	platform := &networkingv1.NetworkPolicy{ObjectMeta: metadata(cluster+"-metadata-probe", "enclii"), Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "switchyard-api"}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns(namespace, true), PodSelector: &selector}}, Ports: port(5432)}}}}
	return []*networkingv1.NetworkPolicy{database, platform}, nil
}

// ProvisionKarafielStaging reuses native runtime Secret/role/CA custody but
// deliberately excludes legacy best-effort network/backup/namespace writes.
// All native preconditions and dry admission must pass before the one create.
// Unknown outcomes remain held: no deletion, adoption, reset or blind retry.
func (s *AddonService) ProvisionKarafielStaging(ctx context.Context, a *types.DatabaseAddon, policies []*networkingv1.NetworkPolicy, planHash string, revalidate func(context.Context) error, dryRun bool) (*ProvisionResult, error) {
	p, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok || p.k8sClient == nil || p.k8sClient.Kube() == nil || p.k8sClient.DynamicClient == nil || revalidate == nil || len(planHash) != 64 || len(policies) != 2 {
		return nil, fmt.Errorf("typed staging provision adapter unavailable")
	}
	cluster, err := p.KarafielStagingCluster(a)
	if err != nil {
		return nil, err
	}
	cluster.SetAnnotations(map[string]string{"enclii.dev/staging-profile": KarafielStagingProfile, "enclii.dev/staging-plan-sha256": planHash})
	ns, err := p.k8sClient.Kube().CoreV1().Namespaces().Get(ctx, cluster.GetNamespace(), metav1.GetOptions{})
	if err != nil || ns.UID == "" || ns.Labels[k8s.LabelProjectNamespace] != a.ProjectID.String() || ns.Labels["enclii.dev/verify-signatures"] != "true" {
		return nil, fmt.Errorf("owned signature-enforcing addon namespace required")
	}
	if err = revalidate(ctx); err != nil {
		return nil, fmt.Errorf("staging authority changed")
	}
	if err = s.VerifyKarafielStagingPolicies(ctx, a, policies, false); err != nil {
		return nil, err
	}
	options := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldValidation: metav1.FieldValidationStrict, FieldManager: KarafielStagingProfile}
	admitted, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(cluster.GetNamespace()).Create(ctx, cluster, options)
	if err != nil || admitted == nil || !closedKarafielCluster(cluster, admitted) {
		return nil, fmt.Errorf("fixed staging Cluster dry admission refused")
	}
	for _, policy := range policies {
		if policy == nil || policy.Labels[LabelProjectID] != a.ProjectID.String() || policy.Labels[LabelAddonID] != a.ID.String() || policy.Labels["enclii.dev/managed-by"] != KarafielStagingProfile {
			return nil, fmt.Errorf("typed staging policy ownership refused")
		}
		admitted, err := p.k8sClient.Kube().NetworkingV1().NetworkPolicies(policy.Namespace).Create(ctx, policy, options)
		if err != nil || admitted == nil || !reflect.DeepEqual(admitted.Spec, policy.Spec) {
			return nil, fmt.Errorf("fixed staging policy dry admission refused")
		}
	}
	result := &ProvisionResult{K8sResourceName: cluster.GetName(), ConnectionSecret: runtimeSecretName(cluster.GetName()), Message: "typed staging create submitted; readiness not established"}
	if dryRun {
		return result, nil
	}
	if err = revalidate(ctx); err != nil {
		return nil, fmt.Errorf("staging authority changed before persistence")
	}
	for _, policy := range policies {
		if err = revalidate(ctx); err != nil {
			return nil, fmt.Errorf("staging authority changed before policy creation")
		}
		api := p.k8sClient.Kube().NetworkingV1().NetworkPolicies(policy.Namespace)
		existing, e := api.Get(ctx, policy.Name, metav1.GetOptions{})
		if e == nil {
			if existing.UID == "" || !reflect.DeepEqual(existing.Spec, policy.Spec) || !reflect.DeepEqual(existing.Labels, policy.Labels) {
				return nil, fmt.Errorf("staging policy conflict; no adoption")
			}
			continue
		}
		if !k8serrors.IsNotFound(e) {
			return nil, fmt.Errorf("staging policy read refused")
		}
		created, e := api.Create(ctx, policy, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: KarafielStagingProfile})
		if e != nil || created == nil || created.UID == "" || !reflect.DeepEqual(created.Spec, policy.Spec) {
			return nil, fmt.Errorf("staging policy create uncertain; inspect same operation")
		}
	}
	if err = revalidate(ctx); err != nil {
		return nil, fmt.Errorf("staging authority changed before runtime custody")
	}
	if err = p.ensureRuntimeSecret(ctx, &ProvisionRequest{Addon: a, ProjectID: a.ProjectID, Namespace: cluster.GetNamespace()}, cluster.GetName()); err != nil {
		return nil, fmt.Errorf("staging runtime credential creation refused")
	}
	if err = revalidate(ctx); err != nil {
		return nil, fmt.Errorf("staging authority changed before Cluster creation")
	}
	created, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(cluster.GetNamespace()).Create(ctx, cluster, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: KarafielStagingProfile})
	if err != nil || created == nil || created.GetUID() == "" || !closedKarafielCluster(cluster, created) {
		return nil, fmt.Errorf("staging Cluster create uncertain; inspect same operation")
	}
	if err = p.ownRuntimeSecret(ctx, &ProvisionRequest{Addon: a, ProjectID: a.ProjectID, Namespace: cluster.GetNamespace()}, cluster.GetName(), created.GetUID()); err != nil {
		return nil, fmt.Errorf("staging runtime custody ownership incomplete")
	}
	return result, nil
}

func closedKarafielCluster(expected, actual *unstructured.Unstructured) bool {
	if expected == nil || actual == nil || expected.GetName() != actual.GetName() || expected.GetNamespace() != actual.GetNamespace() || !stageContains(expected.Object["spec"], actual.Object["spec"]) {
		return false
	}
	bootstrap, ok, _ := unstructured.NestedMap(actual.Object, "spec", "bootstrap")
	if !ok || len(bootstrap) != 1 {
		return false
	}
	for _, field := range []string{"externalClusters", "replica", "superuserSecret", "backup", "plugins", "serviceAccountTemplate", "inheritedMetadata", "podTemplate", "env", "envFrom", "projectedVolumeTemplate", "walStorage", "tablespaces"} {
		value, found, _ := unstructured.NestedFieldNoCopy(actual.Object, "spec", field)
		if found && value != nil {
			return false
		}
	}
	// No alternate authentication or imported volume can be hidden among
	// otherwise harmless CNPG defaults.
	for _, path := range [][]string{{"spec", "postgresql", "pg_hba"}, {"spec", "postgresql", "ldap"}, {"spec", "storage", "pvcTemplate"}} {
		value, found, _ := unstructured.NestedFieldNoCopy(actual.Object, path...)
		if found && !karafielAdmissionEmpty(value) {
			return false
		}
	}
	initdb, ok := bootstrap["initdb"].(map[string]interface{})
	if !ok {
		return false
	}
	for key := range initdb {
		if key != "database" && key != "owner" && key != "postInitApplicationSQL" && key != "encoding" && key != "localeCollate" && key != "localeCType" && key != "locale" && key != "dataChecksums" && key != "walSegmentSize" {
			return false
		}
	}
	roles, _, _ := unstructured.NestedSlice(actual.Object, "spec", "managed", "roles")
	if len(roles) != 1 {
		return false
	}
	role, ok := roles[0].(map[string]interface{})
	if !ok {
		return false
	}
	if memberships, found := role["inRoles"]; found && memberships != nil && !reflect.DeepEqual(memberships, []interface{}{}) {
		return false
	}
	return true
}

func stageContains(expected, actual interface{}) bool {
	switch e := expected.(type) {
	case map[string]interface{}:
		a, ok := actual.(map[string]interface{})
		if !ok {
			return false
		}
		for key, value := range e {
			if !stageContains(value, a[key]) {
				return false
			}
		}
		return true
	case []interface{}:
		a, ok := actual.([]interface{})
		if !ok || len(a) != len(e) {
			return false
		}
		for i := range e {
			if !stageContains(e[i], a[i]) {
				return false
			}
		}
		return true
	default:
		wanted, _ := json.Marshal(expected)
		got, _ := json.Marshal(actual)
		return string(wanted) == string(got)
	}
}

// This typed profile has its own exact policies; a legacy background refresh
// must never add broad shared-data/control-plane egress to it.
func isKarafielStagingAddon(a *types.DatabaseAddon) bool {
	return a != nil && a.EnvironmentID != nil && KarafielStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID)
}

// Read-only effective additive-policy check. It never rewrites a shared addon
// namespace. An existing namespace with an unrelated broad allow remains held.
func (s *AddonService) VerifyKarafielStagingPolicies(ctx context.Context, a *types.DatabaseAddon, expected []*networkingv1.NetworkPolicy, requirePresent bool) error {
	if s == nil || s.k8sClient == nil || s.k8sClient.Kube() == nil || a == nil || len(expected) != 2 {
		return fmt.Errorf("staging policy authority unavailable")
	}
	cluster := fmt.Sprintf("pg-%s-%s", a.Name, a.ID.String()[:8])
	namespace := "project-" + a.ProjectID.String()[:8]
	policies, err := s.k8sClient.Kube().NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil || policies.Continue != "" {
		return fmt.Errorf("staging effective policies unavailable")
	}
	found := false
	for _, actual := range policies.Items {
		if actual.UID == "" || actual.ResourceVersion == "" || actual.DeletionTimestamp != nil {
			return fmt.Errorf("staging policy identity unavailable")
		}
		if actual.Name == expected[0].Name {
			if !karafielPolicyExact(&actual, expected[0]) {
				return fmt.Errorf("staging Cluster policy changed")
			}
			found = true
			continue
		}
		// A selector tied to another concrete CNPG cluster cannot select this one.
		if other := actual.Spec.PodSelector.MatchLabels["cnpg.io/cluster"]; other != "" && other != cluster {
			continue
		}
		// Native bootstrap policies select only a Job label absent from the closed
		// CNPG pod template. The native job kernel independently verifies their UID.
		if job := actual.Spec.PodSelector.MatchLabels["enclii.dev/bootstrap-job"]; job != "" && len(actual.Spec.PodSelector.MatchLabels) == 1 && len(actual.Spec.PodSelector.MatchExpressions) == 0 {
			continue
		}
		if !karafielDenyDNSOnly(actual.Spec) {
			return fmt.Errorf("foreign additive policy can broaden staging Cluster access")
		}
	}
	if requirePresent && !found {
		return fmt.Errorf("staging Cluster isolation not observed")
	}
	actual, err := s.k8sClient.Kube().NetworkingV1().NetworkPolicies(expected[1].Namespace).Get(ctx, expected[1].Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) && !requirePresent {
		return nil
	}
	if err != nil || !karafielPolicyExact(actual, expected[1]) {
		return fmt.Errorf("staging metadata probe policy changed")
	}
	return nil
}

func karafielPolicyExact(actual, expected *networkingv1.NetworkPolicy) bool {
	if actual == nil || expected == nil || actual.Name != expected.Name || actual.Namespace != expected.Namespace || actual.UID == "" || actual.ResourceVersion == "" || actual.DeletionTimestamp != nil || !reflect.DeepEqual(actual.Spec, expected.Spec) {
		return false
	}
	for key, value := range expected.Labels {
		if actual.Labels[key] != value {
			return false
		}
	}
	return true
}

func karafielDenyDNSOnly(spec networkingv1.NetworkPolicySpec) bool {
	if len(spec.Ingress) != 0 || !reflect.DeepEqual(spec.PolicyTypes, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}) {
		return false
	}
	for _, rule := range spec.Egress {
		if len(rule.To) != 1 || rule.To[0].PodSelector != nil || rule.To[0].IPBlock != nil || !reflect.DeepEqual(rule.To[0].NamespaceSelector, &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}) || len(rule.Ports) != 2 {
			return false
		}
		seen := map[corev1.Protocol]bool{}
		for _, p := range rule.Ports {
			if p.Port == nil || p.Port.Type != intstr.Int || p.Port.IntVal != 53 || p.EndPort != nil || p.Protocol == nil || (*p.Protocol != corev1.ProtocolTCP && *p.Protocol != corev1.ProtocolUDP) || seen[*p.Protocol] {
				return false
			}
			seen[*p.Protocol] = true
		}
	}
	return true
}

func karafielAdmissionEmpty(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return true
	case []interface{}:
		return len(v) == 0
	case map[string]interface{}:
		return len(v) == 0
	case string:
		return v == ""
	}
	return false
}
