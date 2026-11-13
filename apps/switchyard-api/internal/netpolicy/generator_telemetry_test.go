package netpolicy

import (
	"reflect"
	"testing"
)

// RFC 0057: the telemetry egress reaches only the gate pods, in the telemetry
// namespace, on 8080/TCP, and only for the service that asks for it.
func TestTelemetryEgress_ClosedPeerAndPort(t *testing.T) {
	policies := decodePolicies(t, NetworkSpec{Services: []ServiceSpec{
		{Name: "product-web", Port: 3000, Egress: []string{"dns", "janua", "telemetry"}},
		{Name: "other-web", Port: 3000, Egress: []string{"dns", "janua"}},
	}})

	web := policies["product-web-egress"]
	var found bool
	for _, rule := range web.Spec.Egress {
		for _, port := range rule.Ports {
			if port.Port == nil || port.Port.IntVal != 8080 || len(rule.To) != 1 {
				continue
			}
			peer := rule.To[0]
			if peer.NamespaceSelector == nil || peer.PodSelector == nil {
				continue
			}
			if !reflect.DeepEqual(peer.NamespaceSelector.MatchLabels, map[string]string{"kubernetes.io/metadata.name": "telemetry"}) {
				continue
			}
			if !reflect.DeepEqual(peer.PodSelector.MatchLabels, map[string]string{"app.kubernetes.io/name": "telemetry-gate"}) {
				t.Fatalf("only the telemetry gate may be reached: %#v", peer.PodSelector)
			}
			if port.Protocol == nil || *port.Protocol != "TCP" {
				t.Fatal("telemetry uses only TCP")
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a telemetry gate peer on 8080: %#v", web.Spec.Egress)
	}

	for _, rule := range policies["other-web-egress"].Spec.Egress {
		for _, peer := range rule.To {
			if peer.NamespaceSelector != nil && reflect.DeepEqual(peer.NamespaceSelector.MatchLabels, map[string]string{"kubernetes.io/metadata.name": "telemetry"}) {
				t.Fatalf("a service that did not ask for telemetry gained it: %#v", rule)
			}
		}
	}
}
