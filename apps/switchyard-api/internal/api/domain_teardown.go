package api

// Removing a domain's infrastructure when its service is deleted.
//
// Split out of domain_provisioner.go on size. Teardown is the mirror of
// provisioning and has its own hazard: every record it deletes was resolved
// from the service's OWN rows, which is what entitles it to delete them. A
// teardown path that resolves anything by hostname alone would be deleting on
// the strength of a string, on a zone shared by every tenant.

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// cleanupDomainsForService removes tunnel routes and DNS records for all domains of a service.
// Called during service deletion.
func (h *Handler) cleanupDomainsForService(ctx context.Context, serviceID uuid.UUID) {
	// Get all domains for this service (across all environments)
	domains, err := h.repos.CustomDomains.GetByServiceID(ctx, serviceID.String())
	if err != nil {
		h.logger.Warn(ctx, "Failed to get domains for cleanup",
			logging.String("service_id", serviceID.String()),
			logging.Error("error", err))
		return
	}

	for _, domain := range domains {
		// Remove tunnel route
		if h.tunnelRoutesService != nil {
			if err := h.tunnelRoutesService.RemoveRoute(ctx, domain.Domain); err != nil {
				h.logger.Warn(ctx, "Failed to remove tunnel route during cleanup",
					logging.String("domain", domain.Domain),
					logging.Error("error", err))
			} else {
				h.logger.Info(ctx, "Tunnel route removed during cleanup",
					logging.String("domain", domain.Domain))
			}
		}

		// Remove the Cloudflare for SaaS custom hostname, if this domain was
		// provisioned that way. For a genuinely client-owned domain there is
		// no DNS record of ours to delete — the records live on the client's
		// nameservers — but a domain that was ever on the zone path can hold
		// BOTH a hostname id and a proxied CNAME in our zone, so the zone
		// cleanup below still has to run. Skipping it is how a dangling CNAME
		// to the tunnel survives service deletion for whoever claims the
		// hostname next.
		if domain.CustomHostnameID != "" {
			if err := h.deleteCustomHostname(ctx, domain.Domain, domain.CustomHostnameID); err != nil {
				h.logger.Warn(ctx, "Failed to delete custom hostname during cleanup",
					logging.String("domain", domain.Domain),
					logging.String("custom_hostname_id", domain.CustomHostnameID),
					logging.Error("error", err))
			} else {
				h.logger.Info(ctx, "Custom hostname deleted during cleanup",
					logging.String("domain", domain.Domain))
			}
		}

		// Remove DNS record (zone-aware: finds correct zone for each domain)
		if h.domainSyncService != nil {
			cfClient := h.domainSyncService.GetCloudflareClient()
			if cfClient != nil {
				zone, zoneErr := cfClient.FindZoneForDomain(ctx, domain.Domain)
				if zoneErr != nil {
					// A client-owned domain has no zone of ours by
					// definition, so "not found" is the expected answer and
					// not a cleanup failure.
					if errors.Is(zoneErr, cloudflare.ErrZoneNotFound) {
						h.logger.Debug(ctx, "No Cloudflare zone of ours for domain during cleanup, nothing to delete",
							logging.String("domain", domain.Domain))
					} else {
						h.logger.Warn(ctx, "Failed to find zone for domain during cleanup",
							logging.String("domain", domain.Domain),
							logging.Error("error", zoneErr))
					}
					continue
				}
				record, err := cfClient.GetDNSRecord(ctx, domain.Domain)
				if err != nil {
					h.logger.Warn(ctx, "Failed to look up DNS record during cleanup",
						logging.String("domain", domain.Domain),
						logging.Error("error", err))
					continue
				}
				if record != nil {
					if err := cfClient.DeleteDNSRecordInZone(ctx, zone.ID, record.ID); err != nil {
						h.logger.Warn(ctx, "Failed to delete DNS record during cleanup",
							logging.String("domain", domain.Domain),
							logging.Error("error", err))
					} else {
						h.logger.Info(ctx, "DNS record deleted during cleanup",
							logging.String("domain", domain.Domain))
					}
				}
			}
		}
	}
}
