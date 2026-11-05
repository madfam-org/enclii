package addons

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func JanuaStagingPolicies(a *types.DatabaseAddon, peers []KarafielControlPeer, operator metav1.LabelSelector) ([]*networkingv1.NetworkPolicy, error) {
	if a == nil || a.EnvironmentID == nil || !JanuaStagingAddonOwned(a, a.ProjectID, *a.EnvironmentID) || len(peers) == 0 || len(operator.MatchLabels) == 0 || len(operator.MatchExpressions) != 0 {
		return nil, fmt.Errorf("closed staging network authority refused")
	}
	namespace := "project-" + a.ProjectID.String()[:8]
	cluster := JanuaStagingClusterName
	selector := metav1.LabelSelector{MatchLabels: map[string]string{"cnpg.io/cluster": cluster}}
	labels := map[string]string{LabelProjectID: a.ProjectID.String(), LabelAddonID: a.ID.String(), "enclii.dev/managed-by": JanuaStagingProfile}
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
		{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns(JanuaStagingNamespace, true), PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"enclii.dev/service": "janua-api-staging"}}}}, Ports: port(5432)},
		{From: []networkingv1.NetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"enclii.dev/bootstrap-profile": "janua-staging-migrate", "enclii.dev/addon-id": a.ID.String(), k8s.LabelProjectNamespace: a.ProjectID.String()}}}}, Ports: port(5432)},
		{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns("enclii", false), PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "switchyard-api"}}}}, Ports: port(5432)},
		{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns(CNPGOperatorNamespace, false), PodSelector: &operator}}, Ports: port(8000)},
	}}}
	platform := &networkingv1.NetworkPolicy{ObjectMeta: metadata(cluster+"-metadata-probe", "enclii"), Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": "switchyard-api"}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns(namespace, true), PodSelector: &selector}}, Ports: port(5432)}}}}
	consumer := &networkingv1.NetworkPolicy{ObjectMeta: metadata(cluster+"-consumer", JanuaStagingNamespace), Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"enclii.dev/service": "janua-api-staging"}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: ns(namespace, true), PodSelector: &selector}}, Ports: port(5432)}}}}
	return []*networkingv1.NetworkPolicy{database, consumer, platform}, nil
}

// This writer has no HTTP registration. BatchA review must approve the complete
// persisted-source adapter before ApplySupported can change. revalidate must
// resolve the authenticated actor and persisted project/env/repo/service/catalog
// anew before every effect; the journal or plan hash is never authorization.
func (s *AddonService) ProvisionJanuaStaging(ctx context.Context, a *types.DatabaseAddon, operation JanuaStagingOperation, journalUID k8stypes.UID, binding JanuaStagingNamespaceBinding, maxConnections int, peers []KarafielControlPeer, operator metav1.LabelSelector, revalidate func(context.Context) error, dryRun bool) (*JanuaStagingBirthReceipt, error) {
	if s == nil {
		return nil, fmt.Errorf("closed Janua staging provision adapter unavailable")
	}
	p, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok || p.k8sClient == nil || p.k8sClient.Kube() == nil || p.k8sClient.DynamicClient == nil || !operation.valid() || a == nil || a.EnvironmentID == nil || operation.ProjectID != a.ProjectID || operation.EnvironmentID != *a.EnvironmentID || operation.AddonID != a.ID || journalUID == "" || binding.ApplicationUID == "" || binding.AddonUID == "" || revalidate == nil {
		return nil, fmt.Errorf("closed Janua staging provision authority unavailable")
	}
	cluster, err := p.JanuaStagingCluster(a, maxConnections)
	if err != nil {
		return nil, err
	}
	cluster.SetAnnotations(map[string]string{"enclii.dev/staging-profile": JanuaStagingProfile, "enclii.dev/staging-plan-sha256": operation.PlanSHA256, "enclii.dev/staging-operation": operation.OperationID.String()})
	policies, err := JanuaStagingPolicies(a, peers, operator)
	if err != nil {
		return nil, err
	}
	check := func() error {
		if err := revalidate(ctx); err != nil {
			return fmt.Errorf("Janua staging persisted authority changed")
		}
		claim, e := p.k8sClient.Kube().CoreV1().ConfigMaps("enclii").Get(ctx, operation.journalName(), metav1.GetOptions{})
		if e != nil || !januaJournalExact(claim, operation.claimObject()) || claim.UID != journalUID {
			return fmt.Errorf("Janua staging operation changed")
		}
		return p.verifyJanuaNamespaces(ctx, a, binding)
	}
	if err = check(); err != nil {
		return nil, err
	}
	// Require all effect names to be absent before the first create. A partial
	// operation cannot silently progress on retry, even when labels still match.
	if _, e := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(cluster.GetNamespace()).Get(ctx, cluster.GetName(), metav1.GetOptions{}); !k8serrors.IsNotFound(e) {
		return nil, fmt.Errorf("Janua staging Cluster name already exists or is unreadable; no adoption")
	}
	if _, e := p.k8sClient.Kube().CoreV1().Secrets(cluster.GetNamespace()).Get(ctx, JanuaStagingRuntimeSecretName, metav1.GetOptions{}); !k8serrors.IsNotFound(e) {
		return nil, fmt.Errorf("Janua staging runtime custody already exists or is unreadable; no adoption")
	}
	if err = p.verifyJanuaPolicies(ctx, a, policies, nil); err != nil {
		return nil, err
	}
	options := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldValidation: metav1.FieldValidationStrict, FieldManager: JanuaStagingProfile}
	admitted, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(cluster.GetNamespace()).Create(ctx, cluster, options)
	if err != nil || !closedKarafielCluster(cluster, admitted) {
		return nil, fmt.Errorf("Janua staging Cluster dry admission refused")
	}
	for _, policy := range policies {
		if _, e := p.k8sClient.Kube().NetworkingV1().NetworkPolicies(policy.Namespace).Get(ctx, policy.Name, metav1.GetOptions{}); !k8serrors.IsNotFound(e) {
			return nil, fmt.Errorf("Janua staging policy name already exists or is unreadable")
		}
		admitted, e := p.k8sClient.Kube().NetworkingV1().NetworkPolicies(policy.Namespace).Create(ctx, policy, options)
		if e != nil || admitted == nil || !reflect.DeepEqual(admitted.Spec, policy.Spec) {
			return nil, fmt.Errorf("Janua staging policy dry admission refused")
		}
	}
	if dryRun {
		return nil, nil
	}
	options.DryRun = nil
	receipt := &JanuaStagingBirthReceipt{JanuaStagingNamespaceBinding: binding}
	for _, policy := range policies {
		if err = check(); err != nil {
			return nil, err
		}
		created, e := p.k8sClient.Kube().NetworkingV1().NetworkPolicies(policy.Namespace).Create(ctx, policy, options)
		if e != nil || created == nil || !karafielPolicyExact(created, policy) {
			return nil, fmt.Errorf("Janua staging policy create uncertain; operation held")
		}
		receipt.PolicyUIDs = append(receipt.PolicyUIDs, created.UID)
	}
	if err = p.verifyJanuaPolicies(ctx, a, policies, receipt.PolicyUIDs); err != nil {
		return nil, err
	}
	if err = check(); err != nil {
		return nil, err
	}
	secret, err := newRuntimeSecret(&ProvisionRequest{Addon: a, ProjectID: a.ProjectID, Namespace: cluster.GetNamespace()}, cluster.GetName())
	if err != nil {
		return nil, fmt.Errorf("Janua staging credential construction refused")
	}
	yes := true
	secret.Immutable = &yes
	secret.Annotations = map[string]string{"enclii.dev/staging-operation": operation.OperationID.String()}
	createdSecret, err := p.k8sClient.Kube().CoreV1().Secrets(cluster.GetNamespace()).Create(ctx, secret, options)
	if err != nil || createdSecret == nil || createdSecret.UID == "" || createdSecret.ResourceVersion == "" || !reflect.DeepEqual(createdSecret.Data, secret.Data) {
		return nil, fmt.Errorf("Janua staging runtime create uncertain; operation held")
	}
	if err = check(); err != nil {
		return nil, err
	}
	if err = p.verifyJanuaPolicies(ctx, a, policies, receipt.PolicyUIDs); err != nil {
		return nil, err
	}
	created, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(cluster.GetNamespace()).Create(ctx, cluster, options)
	if err != nil || created == nil || created.GetUID() == "" || !closedKarafielCluster(cluster, created) {
		return nil, fmt.Errorf("Janua staging Cluster create uncertain; operation held")
	}
	receipt.ClusterUID = created.GetUID()
	if err = check(); err != nil {
		return nil, err
	}
	// Bind the exact create result using resourceVersion CAS, not a second read
	// of a potentially replaced same-name Secret.
	createdSecret.OwnerReferences = []metav1.OwnerReference{{APIVersion: CloudNativePGAPIVersion, Kind: CloudNativePGKind, Name: cluster.GetName(), UID: receipt.ClusterUID}}
	owned, err := p.k8sClient.Kube().CoreV1().Secrets(cluster.GetNamespace()).Update(ctx, createdSecret, metav1.UpdateOptions{FieldManager: JanuaStagingProfile})
	if err != nil || owned == nil || owned.UID != createdSecret.UID || owned.ResourceVersion == "" || !ownedByCNPG(owned, cluster.GetName(), receipt.ClusterUID) || !reflect.DeepEqual(owned.Data, secret.Data) {
		return nil, fmt.Errorf("Janua staging runtime owner binding uncertain; operation held")
	}
	receipt.RuntimeSecretUID, receipt.RuntimeResourceVersion = owned.UID, owned.ResourceVersion
	if err = check(); err != nil {
		return nil, err
	}
	if err = p.verifyJanuaPolicies(ctx, a, policies, receipt.PolicyUIDs); err != nil {
		return nil, err
	}
	if err = WriteJanuaStagingBirthReceipt(ctx, p.k8sClient.Kube(), operation, journalUID, *receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (p *PostgresProvisioner) verifyJanuaNamespaces(ctx context.Context, a *types.DatabaseAddon, binding JanuaStagingNamespaceBinding) error {
	for name, uid := range map[string]k8stypes.UID{JanuaStagingNamespace: binding.ApplicationUID, "project-" + a.ProjectID.String()[:8]: binding.AddonUID} {
		current, err := p.k8sClient.Kube().CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil || uid == "" || current.UID != uid || current.DeletionTimestamp != nil || current.ResourceVersion == "" || current.Labels[k8s.LabelProjectNamespace] != a.ProjectID.String() || current.Labels["enclii.dev/verify-signatures"] != "true" || current.Labels["enclii.dev/staging-profile"] != JanuaStagingProfile {
			return fmt.Errorf("Janua staging namespace UID or ownership changed")
		}
		for _, key := range []string{"enclii.dev/data-access", "enclii.dev/postgres-consumer", "enclii.dev/redis-consumer"} {
			if current.Labels[key] != "false" {
				return fmt.Errorf("Janua staging namespace shared-data access refused")
			}
		}
	}
	return nil
}

func (p *PostgresProvisioner) verifyJanuaPolicies(ctx context.Context, a *types.DatabaseAddon, expected []*networkingv1.NetworkPolicy, uids []k8stypes.UID) error {
	if len(expected) != 3 || (uids != nil && len(uids) != 3) {
		return fmt.Errorf("Janua staging policy identity refused")
	}
	// NetworkPolicies are additive. Fail closed on any unreviewed rule in either
	// isolated namespace; matching our own policy is insufficient proof.
	for _, name := range []string{expected[0].Namespace, JanuaStagingNamespace} {
		list, err := p.k8sClient.Kube().NetworkingV1().NetworkPolicies(name).List(ctx, metav1.ListOptions{})
		if err != nil || list.Continue != "" {
			return fmt.Errorf("Janua staging effective policy inventory unavailable")
		}
		baselineFound := false
		for i := range list.Items {
			actual := &list.Items[i]
			matched := false
			for j, desired := range expected {
				if desired.Namespace == name && desired.Name == actual.Name {
					if uids == nil || actual.UID != uids[j] || !karafielPolicyExact(actual, desired) {
						return fmt.Errorf("Janua staging policy birth identity changed")
					}
					matched = true
				}
			}
			if !matched && !januaDNSBaseline(actual, a.ProjectID.String()) {
				return fmt.Errorf("foreign additive policy can broaden Janua staging access")
			}
			if !matched {
				baselineFound = true
			}
		}
		if !baselineFound {
			return fmt.Errorf("Janua staging DNS-only default-deny baseline unavailable")
		}
	}
	if uids != nil {
		for i, desired := range expected {
			actual, err := p.k8sClient.Kube().NetworkingV1().NetworkPolicies(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
			if err != nil || actual.UID != uids[i] || !karafielPolicyExact(actual, desired) {
				return fmt.Errorf("Janua staging policy birth proof unavailable")
			}
		}
	}
	return nil
}

func januaDNSBaseline(actual *networkingv1.NetworkPolicy, project string) bool {
	if actual == nil || actual.Name != "default-deny" || actual.UID == "" || actual.ResourceVersion == "" || actual.DeletionTimestamp != nil || actual.Labels[k8s.LabelProjectNamespace] != project || actual.Labels["enclii.dev/managed-by"] != "onboarding-api" {
		return false
	}
	copy := actual.Spec.DeepCopy()
	for i := range copy.Egress {
		for j := range copy.Egress[i].To {
			peer := &copy.Egress[i].To[j]
			if !reflect.DeepEqual(peer.PodSelector, &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}}) {
				return false
			}
			peer.PodSelector = nil // shared DNS-only checker is otherwise exact
		}
	}
	return len(copy.Egress) == 1 && len(copy.PodSelector.MatchLabels) == 0 && len(copy.PodSelector.MatchExpressions) == 0 && karafielDenyDNSOnly(*copy)
}
