package netpolicy

import (
	"io"
	"reflect"
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestSelvaEgress_ClosedPeerAndTargetPort(t *testing.T) {
	policies := decodePolicies(t, NetworkSpec{Services: []ServiceSpec{
		{Name: "training-gateway", Port: 8080, Egress: []string{"dns", "janua", "selva"}},
		{Name: "training-kernel", Port: 3000, Egress: []string{"dns", "janua"}},
	}})

	gateway := policies["training-gateway-egress"]
	if !reflect.DeepEqual(gateway.Spec.PodSelector.MatchLabels, map[string]string{"app": "training-gateway"}) {
		t.Fatalf("inference permission must select only the opted-in service: %#v", gateway.Spec.PodSelector)
	}
	if len(gateway.Spec.Egress) != 3 {
		t.Fatalf("expected DNS, Janua and Selva only, got %#v", gateway.Spec.Egress)
	}
	var selvaRule *networkingv1.NetworkPolicyEgressRule
	for i := range gateway.Spec.Egress {
		rule := &gateway.Spec.Egress[i]
		for _, port := range rule.Ports {
			if port.Port == nil {
				t.Fatal("an allow rule must name a port")
			}
			if port.Port.IntVal == 4306 {
				selvaRule = rule
			} else if port.Port.IntVal != 53 && port.Port.IntVal != 8080 {
				t.Fatalf("unrequested egress port: %v", port.Port)
			}
		}
	}
	if selvaRule == nil || len(selvaRule.To) != 1 || len(selvaRule.Ports) != 1 {
		t.Fatalf("expected one narrow Selva peer and port: %#v", selvaRule)
	}
	peer := selvaRule.To[0]
	if peer.NamespaceSelector == nil || peer.PodSelector == nil || peer.IPBlock != nil {
		t.Fatalf("namespace and pod selectors must be ANDed in one peer: %#v", peer)
	}
	if !reflect.DeepEqual(peer.NamespaceSelector.MatchLabels, map[string]string{"kubernetes.io/metadata.name": "selva"}) || len(peer.NamespaceSelector.MatchExpressions) != 0 {
		t.Fatalf("namespace must be closed to Selva: %#v", peer.NamespaceSelector)
	}
	if !reflect.DeepEqual(peer.PodSelector.MatchLabels, map[string]string{"app.kubernetes.io/name": "inference-gateway"}) || len(peer.PodSelector.MatchExpressions) != 0 {
		t.Fatalf("only the inference gateway may be reached: %#v", peer.PodSelector)
	}
	if selvaRule.Ports[0].Protocol == nil || *selvaRule.Ports[0].Protocol != "TCP" {
		t.Fatal("inference uses only TCP")
	}

	for _, rule := range policies["training-kernel-egress"].Spec.Egress {
		for _, port := range rule.Ports {
			if port.Port == nil || (port.Port.IntVal != 53 && port.Port.IntVal != 8080) {
				t.Fatalf("kernel gained unrequested inference or public egress: %#v", rule)
			}
		}
	}
}

func TestSelvaEgress_IsOptInAndDoesNotAddPublicIngress(t *testing.T) {
	policies := decodePolicies(t, NetworkSpec{Services: []ServiceSpec{
		{Name: "model-worker", Egress: []string{"selva"}},
	}})
	if len(policies) != 3 {
		t.Fatalf("expected default denies and worker egress only: %#v", policies)
	}
	if _, ok := policies["model-worker-ingress"]; ok {
		t.Fatal("inference egress must not open ingress")
	}
	without := decodePolicies(t, NetworkSpec{Services: []ServiceSpec{
		{Name: "model-worker", Egress: []string{"dns"}},
	}})
	for _, rule := range without["model-worker-egress"].Spec.Egress {
		for _, port := range rule.Ports {
			if port.Port == nil || port.Port.IntVal != 53 {
				t.Fatalf("Selva must not be granted by default: %#v", rule)
			}
		}
	}
}

func decodePolicies(t *testing.T, spec NetworkSpec) map[string]networkingv1.NetworkPolicy {
	t.Helper()
	out, err := GeneratePolicies("project-fixture", "project-fixture", spec)
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(out)), 4096)
	policies := map[string]networkingv1.NetworkPolicy{}
	for {
		var policy networkingv1.NetworkPolicy
		if err := decoder.Decode(&policy); err != nil {
			if err == io.EOF {
				return policies
			}
			t.Fatal(err)
		}
		if policy.Name != "" {
			policies[policy.Name] = policy
		}
	}
}
