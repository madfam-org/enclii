package api

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func multiPortService(namespace, name string, ports ...int32) *corev1.Service {
	spec := make([]corev1.ServicePort, 0, len(ports))
	for _, port := range ports {
		spec = append(spec, corev1.ServicePort{Port: port})
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       corev1.ServiceSpec{Ports: spec},
	}
}

// The live failure: `enclii domains add crea-erp.madfam.io --service nauta-web`
// built a route spec naming nauta-web:80. nauta-web publishes 3000, so
// resolveTunnelBackend refused the write and the hostname served cloudflared's
// 404 catch-all with a DNS record already in place.
func TestResolveTunnelBackendPortReadsTheLiveService(t *testing.T) {
	h := &Handler{
		logger:    testLogger(t),
		k8sClient: fakeKubeWithServices(k8sService("nauta", "nauta-web", 3000)),
	}
	service := &types.Service{Name: "nauta-web"}

	if got := h.resolveTunnelBackendPort(context.Background(), service, "nauta", 0); got != 3000 {
		t.Fatalf("want the port the Service actually exposes (3000), got %d", got)
	}
}

// An explicit declaration always wins: it is what the manifest author asked for.
func TestResolveTunnelBackendPortPrefersTheDeclaredPort(t *testing.T) {
	h := &Handler{
		logger:    testLogger(t),
		k8sClient: fakeKubeWithServices(k8sService("nauta", "nauta-web", 3000)),
	}
	service := &types.Service{Name: "nauta-web"}

	if got := h.resolveTunnelBackendPort(context.Background(), service, "nauta", 8080); got != 8080 {
		t.Fatalf("want the declared port 8080, got %d", got)
	}
}

// Where 80 was right before, it must still be chosen: this change must not
// move a hostname that was already working.
func TestResolveTunnelBackendPortFallsBackTo80(t *testing.T) {
	service := &types.Service{Name: "janua-api"}

	// No Kubernetes client at all.
	bare := &Handler{logger: testLogger(t)}
	if got := bare.resolveTunnelBackendPort(context.Background(), service, "janua", 0); got != 80 {
		t.Fatalf("no kube client: want 80, got %d", got)
	}

	// Client present, Service absent.
	h := &Handler{logger: testLogger(t), k8sClient: fakeKubeWithServices()}
	if got := h.resolveTunnelBackendPort(context.Background(), service, "janua", 0); got != 80 {
		t.Fatalf("missing Service: want 80, got %d", got)
	}

	// Empty namespace.
	if got := h.resolveTunnelBackendPort(context.Background(), service, "", 0); got != 80 {
		t.Fatalf("empty namespace: want 80, got %d", got)
	}

	// Nil service.
	if got := h.resolveTunnelBackendPort(context.Background(), nil, "janua", 0); got != 80 {
		t.Fatalf("nil service: want 80, got %d", got)
	}

	// A Service genuinely on 80.
	onEighty := &Handler{logger: testLogger(t), k8sClient: fakeKubeWithServices(k8sService("janua", "janua-api", 80))}
	if got := onEighty.resolveTunnelBackendPort(context.Background(), service, "janua", 0); got != 80 {
		t.Fatalf("Service on 80: want 80, got %d", got)
	}
}

// Several ports is genuinely ambiguous. Picking one at random would put an
// outage on the wrong side of a coin flip, so 80 is kept when it is among them
// and used as the fallback when it is not.
func TestResolveTunnelBackendPortDoesNotGuessBetweenSeveralPorts(t *testing.T) {
	service := &types.Service{Name: "multi"}

	withEighty := &Handler{logger: testLogger(t), k8sClient: fakeKubeWithServices(multiPortService("ns", "multi", 8080, 80, 9090))}
	if got := withEighty.resolveTunnelBackendPort(context.Background(), service, "ns", 0); got != 80 {
		t.Fatalf("80 among several: want 80, got %d", got)
	}

	withoutEighty := &Handler{logger: testLogger(t), k8sClient: fakeKubeWithServices(multiPortService("ns", "multi", 8080, 9090))}
	if got := withoutEighty.resolveTunnelBackendPort(context.Background(), service, "ns", 0); got != 80 {
		t.Fatalf("no 80 among several: want the 80 fallback rather than a guess, got %d", got)
	}
}
