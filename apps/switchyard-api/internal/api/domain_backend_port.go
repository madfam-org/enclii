package api

// Deciding which port a hostname's tunnel route should target.
//
// Five call sites hardcoded 80, and that is why `enclii domains add
// crea-erp.madfam.io --service nauta-web` produced a DNS record and a 404: the
// route spec named nauta-web:80, resolveTunnelBackend read the live Service,
// found it exposes 3000, and correctly refused to write a rule pointing at a
// port nothing listens on. The DNS half succeeded, the routing half was refused,
// and the hostname landed on cloudflared's catch-all — HTTP 404 with
// `server: cloudflare`, which is exactly what was observed.
//
// 80 was never a safe default; it was a guess that happens to be right for
// services that publish on 80 and silently wrong for every other. The live
// Service is the authority on which port it serves, so ask it.

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fallbackTunnelBackendPort is what we use when the cluster cannot tell us
// anything. Unchanged from the previous hardcoded value so this cannot regress
// a hostname that was working: where 80 was right before, it is still chosen.
const fallbackTunnelBackendPort = 80

// resolveTunnelBackendPort picks the port a hostname's tunnel route should
// target, preferring, in order:
//
//  1. an explicit port the caller already resolved (a manifest's
//     `spec.domains[].port` or `spec.runtime.port`) — the declaration wins,
//     because it is the thing the author actually asked for;
//  2. the single port the live Kubernetes Service exposes;
//  3. 80.
//
// A Service exposing several ports is NOT guessed at: picking one would be a
// coin flip with an outage on the wrong side of it, so the fallback is used and
// the ambiguity is logged for a human to settle with an explicit declaration.
//
// namespace is passed IN rather than resolved here. Resolving it internally
// would issue a second environment/project lookup on paths that have already
// done one, which both doubles the query load and reorders the queries these
// handlers make — enough on its own to break callers that depend on the
// sequence. The caller already knows the namespace; asking it is cheaper and
// truthful.
func (h *Handler) resolveTunnelBackendPort(
	ctx context.Context,
	service *types.Service,
	namespace string,
	declaredPort int,
) int {
	if declaredPort > 0 {
		return declaredPort
	}
	if h == nil || service == nil || h.k8sClient == nil || h.k8sClient.Kube() == nil {
		return fallbackTunnelBackendPort
	}
	if strings.TrimSpace(namespace) == "" {
		return fallbackTunnelBackendPort
	}

	live, err := h.k8sClient.Kube().CoreV1().Services(namespace).Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil || live == nil {
		// Not an error worth failing on: resolveTunnelBackend runs later and
		// refuses the write properly if the backend really is unusable. This is
		// only about choosing a better number than 80.
		h.logger.Debug(ctx, "Could not read the live Service to resolve a tunnel backend port; falling back",
			logging.String("service", service.Name),
			logging.String("namespace", namespace),
			logging.Int("fallback_port", fallbackTunnelBackendPort))
		return fallbackTunnelBackendPort
	}

	ports := live.Spec.Ports
	if len(ports) == 1 {
		return int(ports[0].Port)
	}
	if len(ports) > 1 {
		// If one of them IS 80, the historical default is at least a defensible
		// choice and keeps existing behaviour. Otherwise say so out loud.
		for _, port := range ports {
			if int(port.Port) == fallbackTunnelBackendPort {
				return fallbackTunnelBackendPort
			}
		}
		h.logger.Warn(ctx, "Service exposes several ports and none is 80; declare `port:` on the domain in enclii.yaml to choose one",
			logging.String("service", service.Name),
			logging.String("namespace", namespace),
			logging.String("exposed_ports", describeServicePorts(ports)),
			logging.Int("fallback_port", fallbackTunnelBackendPort))
	}

	return fallbackTunnelBackendPort
}
