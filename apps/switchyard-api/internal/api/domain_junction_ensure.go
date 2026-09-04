package api

// Making a provisioned hostname visible to the routing model that reconciles it.
//
// Enclii keeps a hostname in two tables that were never linked:
//
//   - `custom_domains` — written by `domains add` and by the enclii.yaml
//     provisioner. Drives the Cloudflare DNS record.
//   - `junctions` — written ONLY by POST /v1/projects/:slug/junctions. Drives
//     every tunnel-route reconciliation there is
//     (planJunctionTunnelRoutes and reconcileJunctionTunnelRoutesForProject
//     both iterate `Junctions.ListByProject` and nothing else).
//
// A hostname added through `domains add` therefore got a DNS record and no
// junction, so `cloudflare tunnels-apply --project nauta` reported "no junction
// domains found" (count 0) while the hostname 404'd on cloudflared's catch-all.
// Reconciliation had nothing to reconcile because the hostname was invisible to
// the only model reconciliation reads.
//
// ensureJunctionForDomain closes that: every path that provisions a hostname
// also records the junction, so the hostname is thereafter reconcilable by the
// existing machinery. Idempotent, ownership-checked, and non-fatal — a hostname
// whose junction cannot be created is still routed, it just is not yet
// self-healing, and that is said out loud rather than silently.

import (
	"context"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// defaultJunctionPath matches CreateJunction's default. The domain+path
// uniqueness index is keyed on both, so using a different default here would
// mint a second junction for a hostname that already has one.
const defaultJunctionPath = "/"

// ensureJunctionForDomain records the junction for a hostname that is being
// provisioned, if one does not already exist.
//
// Returns whether a junction now exists for this hostname. Errors are logged
// and swallowed: this runs inside best-effort provisioning paths, and failing a
// domain that is otherwise routable because its bookkeeping row could not be
// written would be a worse outcome than the missing row.
func (h *Handler) ensureJunctionForDomain(ctx context.Context, domain string, service *types.Service) bool {
	if h == nil || h.repos == nil || h.repos.Junctions == nil || service == nil {
		return false
	}
	domain = canonicalDomain(domain)
	if domain == "" {
		return false
	}

	exists, err := h.repos.Junctions.ExistsByDomainPath(ctx, domain, defaultJunctionPath)
	if err != nil {
		h.logger.Warn(ctx, "Could not check for an existing junction; leaving the routing model untouched",
			logging.String("domain", domain),
			logging.Error("error", err))
		return false
	}
	if exists {
		return true
	}

	// Same ownership gate CreateJunction applies, and for the same reason: a
	// junction is what lets a project provision and later RELEASE a hostname's
	// edge infrastructure. Creating one for a hostname another project holds
	// would hand this project a lever over someone else's live domain. Fails
	// closed — no junction rather than a wrong one.
	owner := ownerFromService(service)
	if owner == nil {
		return false
	}
	if err := h.assertHostnameNotHeldByAnotherProject(ctx, domain, owner); err != nil {
		h.logger.Warn(ctx, "Not recording a junction: another project holds this hostname",
			logging.String("domain", domain),
			logging.String("service", service.Name),
			logging.Error("error", err))
		return false
	}

	junction := &types.Junction{
		ProjectID: service.ProjectID,
		ServiceID: service.ID,
		Domain:    domain,
		Path:      defaultJunctionPath,
		Protocol:  "https",
		TLS: &types.TLSConfig{
			Enabled:       true,
			Issuer:        "letsencrypt-prod",
			MinVersion:    "1.2",
			ForceRedirect: true,
		},
	}

	if err := h.repos.Junctions.Create(ctx, junction); err != nil {
		// A duplicate here is a race with a concurrent provisioning pass, not a
		// failure: the row we wanted exists.
		if strings.Contains(err.Error(), "duplicate key") {
			return true
		}
		h.logger.Warn(ctx, "Could not record a junction for a provisioned hostname; it will route but will not be reconciled by tunnels-apply until one exists",
			logging.String("domain", domain),
			logging.String("service", service.Name),
			logging.Error("error", err))
		return false
	}

	h.logger.Info(ctx, "Recorded a junction for a provisioned hostname so it is reconcilable",
		logging.String("domain", domain),
		logging.String("service", service.Name),
		logging.String("project_id", service.ProjectID.String()))
	return true
}
