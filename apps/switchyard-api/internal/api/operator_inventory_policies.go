package api

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type inventoryPolicy struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	Selector        map[string]string `json:"pod_selector"`
	SelectorSummary string            `json:"pod_selector_summary"`
	Types           []string          `json:"policy_types"`
	IngressRules    int               `json:"ingress_rules"`
	EgressRules     int               `json:"egress_rules"`
	Ingress         []string          `json:"ingress_summary"`
	Egress          []string          `json:"egress_summary"`
	Created         *string           `json:"created_at"`
}
type inventoryPolicyGroup struct {
	Namespace string            `json:"namespace"`
	Policies  []inventoryPolicy `json:"policies"`
}
type inventoryPolicies struct {
	Groups     []inventoryPolicyGroup `json:"groups"`
	Policies   int                    `json:"total_policies"`
	Namespaces int                    `json:"total_namespaces"`
	SyncedAt   string                 `json:"synced_at"`
}

func inventorySelector(selector metav1.LabelSelector) string {
	parts := []string{}
	for k, v := range selector.MatchLabels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	// Expressions must not be misrepresented as an unrestricted selector.
	for _, exp := range selector.MatchExpressions {
		parts = append(parts, fmt.Sprintf("%s %s (%s)", exp.Key, exp.Operator, strings.Join(exp.Values, ",")))
	}
	if len(parts) == 0 {
		return "all pods"
	}
	return strings.Join(parts, ",")
}
func inventoryPeer(peer networkingv1.NetworkPolicyPeer) string {
	if peer.IPBlock != nil {
		if len(peer.IPBlock.Except) > 0 {
			return fmt.Sprintf("%s (except %s)", peer.IPBlock.CIDR, strings.Join(peer.IPBlock.Except, ","))
		}
		return peer.IPBlock.CIDR
	}
	parts := []string{}
	if peer.NamespaceSelector != nil {
		parts = append(parts, "ns:"+inventorySelector(*peer.NamespaceSelector))
	}
	if peer.PodSelector != nil {
		parts = append(parts, "pod:"+inventorySelector(*peer.PodSelector))
	}
	if len(parts) == 0 {
		return "unrestricted"
	}
	return strings.Join(parts, " ")
}
func inventoryPorts(ports []networkingv1.NetworkPolicyPort) string {
	if len(ports) == 0 {
		return "any-port"
	}
	out := []string{}
	for _, port := range ports {
		protocol := "TCP"
		if port.Protocol != nil {
			protocol = string(*port.Protocol)
		}
		value := "any"
		if port.Port != nil {
			value = port.Port.String()
		}
		if port.EndPort != nil {
			value += fmt.Sprintf("-%d", *port.EndPort)
		}
		out = append(out, protocol+"/"+value)
	}
	return strings.Join(out, ",")
}
func inventoryPeers(peers []networkingv1.NetworkPolicyPeer, fallback string) string {
	if len(peers) == 0 {
		return fallback
	}
	out := []string{}
	for _, peer := range peers {
		out = append(out, inventoryPeer(peer))
	}
	return strings.Join(out, " OR ")
}
func (h *Handler) readInventoryNetworkPolicies(ctx context.Context) (inventoryPolicies, error) {
	out := inventoryPolicies{Groups: []inventoryPolicyGroup{}}
	list, err := h.opsKubeClient().NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	groups := map[string][]inventoryPolicy{}
	for _, policy := range list.Items {
		item := inventoryPolicy{Name: policy.Name, Namespace: policy.Namespace, Selector: policy.Spec.PodSelector.MatchLabels, SelectorSummary: inventorySelector(policy.Spec.PodSelector), Types: []string{}, Ingress: []string{}, Egress: []string{}, IngressRules: len(policy.Spec.Ingress), EgressRules: len(policy.Spec.Egress), Created: inventoryTimestamp(policy.CreationTimestamp.Time)}
		if len(item.Selector) == 0 {
			item.Selector = nil
		}
		for _, kind := range policy.Spec.PolicyTypes {
			item.Types = append(item.Types, string(kind))
		}
		for _, rule := range policy.Spec.Ingress {
			item.Ingress = append(item.Ingress, inventoryPeers(rule.From, "all sources")+" -> "+inventoryPorts(rule.Ports))
		}
		for _, rule := range policy.Spec.Egress {
			item.Egress = append(item.Egress, inventoryPorts(rule.Ports)+" -> "+inventoryPeers(rule.To, "all destinations"))
		}
		groups[policy.Namespace] = append(groups[policy.Namespace], item)
	}
	for ns, policies := range groups {
		sort.Slice(policies, func(i, j int) bool { return policies[i].Name < policies[j].Name })
		out.Groups = append(out.Groups, inventoryPolicyGroup{ns, policies})
	}
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Namespace < out.Groups[j].Namespace })
	out.Policies = len(list.Items)
	out.Namespaces = len(out.Groups)
	out.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	return out, nil
}
