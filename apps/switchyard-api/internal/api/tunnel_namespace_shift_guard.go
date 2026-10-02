package api

// The recorded-namespace guard on the automatic junction reconcile.
//
// ServiceRepository.GetByID did not read services.k8s_namespace until this
// guard was added. Every route planner that loaded a service by id derived the
// production namespace from the environment record or the project slug, even
// for a service that was adopted from, and still runs in, a namespace of its
// own. Reading the column fixes that derivation, and it also means a hostname
// can now resolve to a different backend than it did before the change.
//
// The operator paths show or guard that difference: tunnels-apply notes it on
// the plan row and keeps its repoint guard, all-or-nothing apply and
// --expect-plan; `domains add` and the junction create write only the hostname
// they were asked for, through ensureTunnelRoute's resolve-before-write and
// repoint guard. The project-wide junction reconcile that runs as a side effect
// of every junction create has no review step, so for it a backend that changed
// ONLY because the recorded namespace became visible is refused: it logs the
// refusal and writes nothing for that hostname, not even a dead-route repair or
// a first-time add. The route is moved, if it should be, through tunnels-apply:
// dry run, review, then apply with --expect-plan.

import (
	"context"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// recordedNamespaceShift is a namespace that differs only because the
// service's recorded k8s_namespace is now read.
type recordedNamespaceShift struct {
	// Recorded is the namespace the route resolves to with the record.
	Recorded string
	// Derived is the namespace it resolved to without it: the environment's
	// recorded namespace, else the project slug.
	Derived string
}

// recordedNamespaceShiftFor reports whether the service's recorded
// k8s_namespace changes the namespace resolveServiceNamespace returns for
// envName. A service with no recorded namespace never shifts.
func (h *Handler) recordedNamespaceShiftFor(
	ctx context.Context, service *types.Service, envName string,
) (recordedNamespaceShift, bool) {
	if h == nil || service == nil || service.K8sNamespace == nil || strings.TrimSpace(*service.K8sNamespace) == "" {
		return recordedNamespaceShift{}, false
	}
	recorded := h.resolveServiceNamespace(ctx, service, envName)
	withoutRecord := *service
	withoutRecord.K8sNamespace = nil
	derived := h.resolveServiceNamespace(ctx, &withoutRecord, envName)
	if recorded == derived {
		return recordedNamespaceShift{}, false
	}
	return recordedNamespaceShift{Recorded: recorded, Derived: derived}, true
}

// refuseRecordedNamespaceShift is the guard the automatic junction reconcile
// runs before it provisions a hostname. It returns true when the reconcile
// must leave the hostname alone.
//
// It lets the reconcile through only when the recorded namespace changes
// nothing, or when the live rule already targets exactly the backend the
// recorded namespace resolves to, in which case ensureTunnelRoute writes
// nothing either. Every other case would be a route change caused by reading
// the column, so it is refused and logged.
func (h *Handler) refuseRecordedNamespaceShift(
	ctx context.Context, hostname string, service *types.Service, envName string,
) bool {
	if h == nil || h.tunnelRoutesService == nil {
		// No tunnel is wired up, so no route can be written either way.
		return false
	}
	shift, shifted := h.recordedNamespaceShiftFor(ctx, service, envName)
	if !shifted {
		return false
	}

	spec := &services.RouteSpec{
		Hostname:         hostname,
		ServiceName:      service.Name,
		ServiceNamespace: shift.Recorded,
		ServicePort:      h.resolveTunnelBackendPort(ctx, service, shift.Recorded, 0),
	}
	existing, known := h.existingTunnelRoute(ctx, hostname)
	if known && existing != nil &&
		(existing.Service == tunnelRouteServiceURL(spec) || sameClusterBackend(existing.Service, spec)) {
		return false
	}

	incumbent := "none"
	switch {
	case !known:
		incumbent = "unknown (tunnel config could not be read)"
	case existing != nil:
		incumbent = existing.Service
	}
	h.logger.Error(ctx, "REFUSED: the automatic junction reconcile does not move a route onto a service's recorded namespace",
		logging.String("domain", hostname),
		logging.String("service", service.Name),
		logging.String("environment", envName),
		logging.String("recorded_namespace", shift.Recorded),
		logging.String("derived_namespace", shift.Derived),
		logging.String("existing_backend", incumbent),
		logging.String("attempted_backend", tunnelRouteServiceURL(spec)),
		logging.String("remedy", "dry-run `enclii providers cloudflare tunnels-apply --project <slug>`, review every row, then apply with --expect-plan"))
	return true
}

// tunnelRouteTargetsService reports whether hostname's live rule targets the
// named service in namespace, on any port.
func (h *Handler) tunnelRouteTargetsService(ctx context.Context, hostname, serviceName, namespace string) bool {
	existing, known := h.existingTunnelRoute(ctx, hostname)
	if !known || existing == nil {
		return false
	}
	backend, ok := parseClusterBackend(existing.Service)
	return ok && backend.Service == serviceName && backend.Namespace == namespace
}
