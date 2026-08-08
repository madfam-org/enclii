package api

// Writing a provisioning outcome back onto a domain record.
//
// Split out of domain_provisioner_custom_hostname.go, which was already in the
// 600-line band, and kept together here because these three functions share one
// invariant that is easy to lose:
//
//	a provisioning result may only ever be written to a row belonging to the
//	project that was being provisioned.
//
// The outcome used to be keyed on the hostname string alone. A hostname is not
// an owner — the Cloudflare for SaaS fallback-origin zone is shared by every
// tenant, and custom_domains is unique only per service+environment — so
// "load the row named app.victim.com and write this result to it" resolved to
// whichever project's row came back first. Three separate paths reached that
// write with an attacker's outcome and a victim's row:
//
//   - the day-one configuration, where the custom-hostname path returns at the
//     unconfigured-zone guard and never runs its ownership check at all;
//   - the zone-path conflict branch, which correctly REFUSES to route and then
//     recorded the refusal on the victim's record;
//   - the undetermined-mechanism branch, same shape.
//
// The last two are the sharpest lesson: refusing to act is not the same as
// writing nothing, and a refusal branch that still writes is still a
// cross-tenant write.

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// persistDomainProvisioningResult writes the provisioning outcome onto the
// CustomDomain rows the provisioned service owns, so a failure or an
// outstanding client action is visible on the domain read path instead of only
// in the logs.
//
// owner identifies the service whose provisioning produced this result, and is
// the ONLY key used to find a row to write. Consequences, all deliberate:
//
//   - a result with no owner writes nothing, rather than falling back to the
//     hostname;
//   - a hostname whose only rows belong to other projects writes nothing, on
//     every branch including the ones that refused to provision it;
//   - domains with no CustomDomain row at all (junctions provision by hostname
//     alone) remain a no-op here; their result is returned to the caller.
func (h *Handler) persistDomainProvisioningResult(
	ctx context.Context, result domainProvisioningResult, owner *domainOwner,
) {
	if h == nil || h.repos == nil || h.repos.CustomDomains == nil || result.Domain == "" {
		return
	}

	// Fail closed. An outcome that cannot name the service it belongs to names
	// no row it is entitled to write either.
	if owner == nil || owner.ServiceID == uuid.Nil {
		h.logger.Warn(ctx, "Not recording a domain provisioning outcome: the service it belongs to could not be determined",
			logging.String("domain", result.Domain),
			logging.String("mechanism", string(result.Mechanism)))
		return
	}

	records, err := h.repos.CustomDomains.ListByDomainForService(ctx, result.Domain, owner.ServiceID)
	if err != nil {
		h.logger.Warn(ctx, "Failed to load custom domain for provisioning state update",
			logging.String("domain", result.Domain),
			logging.Error("error", err))
		return
	}
	if len(records) == 0 {
		// Either a junction-provisioned hostname (no row of its own), or a
		// hostname whose rows belong to somebody else. Neither is ours to write.
		return
	}

	now := time.Now()
	for i := range records {
		record := &records[i]

		// Per-record copy: releasing the stale hostname is a fact about THIS
		// row, and applyZoneProvisioningResult keys the state-clearing on it.
		recordResult := result
		recordResult.CustomHostnameReleased = h.releaseStaleCustomHostname(ctx, record, recordResult)

		applyProvisioningResult(record, recordResult, now)

		if err := h.repos.CustomDomains.UpdateCustomHostnameState(ctx, record); err != nil {
			h.logger.Warn(ctx, "Failed to persist domain provisioning state",
				logging.String("domain", record.Domain),
				logging.Error("error", err))
		}
	}
}

// releaseStaleCustomHostname deletes the Cloudflare registration of a domain
// that has moved back to the zone path, and reports whether Cloudflare
// confirmed it.
//
// A domain that flipped from `external: true` back to the zone path keeps its
// custom hostname registered at Cloudflare until we release it. The stored id is
// only cleared once Cloudflare confirmed the delete, so a failure here leaves
// the id in place for teardown to retry rather than orphaning the registration.
func (h *Handler) releaseStaleCustomHostname(
	ctx context.Context, record *types.CustomDomain, result domainProvisioningResult,
) bool {
	if result.Mechanism != mechanismZoneCNAME || record.CustomHostnameID == "" {
		return result.CustomHostnameReleased
	}

	if err := h.deleteCustomHostname(ctx, record.Domain, record.CustomHostnameID); err != nil {
		h.logger.Warn(ctx, "Failed to release the custom hostname of a domain that moved back to the zone path",
			logging.String("domain", record.Domain),
			logging.String("custom_hostname_id", record.CustomHostnameID),
			logging.Error("error", err))
		return false
	}

	h.logger.Info(ctx, "Released the custom hostname of a domain that moved back to the zone path",
		logging.String("domain", record.Domain),
		logging.String("custom_hostname_id", record.CustomHostnameID))
	return true
}

// applyProvisioningResult maps a provisioning outcome onto a domain record.
//
// Verified is only ever set from a Cloudflare-reported active hostname AND an
// active certificate. It is never inferred from a successful API call, and it
// is cleared again if Cloudflare later reports the hostname as no longer
// active (e.g. the client moved their CNAME away).
func applyProvisioningResult(record *types.CustomDomain, result domainProvisioningResult, now time.Time) {
	if record == nil {
		return
	}

	switch result.Mechanism {
	case mechanismUndetermined:
		// We could not work out how this domain reaches the edge, so we change
		// nothing about how it currently does. Only the diagnosis is written:
		// status, verification, TLS provider and every custom-hostname field
		// keep the values the last conclusive pass left. Rewriting them here
		// is how a transient Cloudflare error turns a live domain into a
		// pending one.
		record.ProvisioningError = result.ErrorMessage
		record.ProvisioningCheckedAt = &now
		return
	case mechanismZoneCNAME:
		applyZoneProvisioningResult(record, result, now)
		return
	}

	record.TLSProvider = types.TLSProviderCloudflareForSaaS
	record.ProvisioningError = result.ErrorMessage
	record.ProvisioningCheckedAt = &now

	if result.Err != nil {
		// The pass failed, so it learned nothing about the hostname itself.
		// Its identifier and last-known Cloudflare state are left alone:
		// blanking the stored hostname id here would strand the registration
		// at Cloudflare with nothing left to release it by. Only the outcome
		// of THIS pass is recorded.
		record.Status = types.DomainStatusError
		record.Verified = false
		return
	}

	record.CustomHostnameID = result.CustomHostnameID
	record.CustomHostnameStatus = result.HostnameStatus
	record.CustomHostnameSSLStatus = result.SSLStatus
	record.PendingDNSRecords = result.PendingDNSRecords

	switch {
	case result.HostnameStatus == string(cloudflare.CustomHostnameStatusActive) &&
		result.SSLStatus == string(cloudflare.CustomHostnameSSLActive):
		record.Status = types.DomainStatusActive
		record.Verified = true
		if record.VerifiedAt == nil {
			verifiedAt := now
			record.VerifiedAt = &verifiedAt
		}
	case result.HostnameStatus == string(cloudflare.CustomHostnameStatusMoved),
		result.HostnameStatus == string(cloudflare.CustomHostnameStatusDeleted),
		result.HostnameStatus == string(cloudflare.CustomHostnameStatusBlocked):
		record.Status = types.DomainStatusError
		record.Verified = false
	default:
		// pending / pending_validation / certificate not issued yet:
		// we are waiting on the domain owner, not on ourselves.
		record.Status = types.DomainStatusPending
		record.Verified = false
	}
}

// applyZoneProvisioningResult records the outcome of the zone + CNAME path.
//
// This path writes to the record, which the pre-Cloudflare-for-SaaS code did
// not do. That is deliberate — a deploy-path DNS failure used to exist only in
// a log line — but it obliges the write to be idempotent in BOTH directions:
// a pass that succeeds has to be able to undo the error a previous pass wrote,
// otherwise one bad minute leaves `status = error` forever.
func applyZoneProvisioningResult(record *types.CustomDomain, result domainProvisioningResult, now time.Time) {
	record.ProvisioningError = result.ErrorMessage
	record.ProvisioningCheckedAt = &now

	if result.Err != nil {
		record.Status = types.DomainStatusError
		return
	}

	// Only an error (or an unset) status is rewritten. An active, pending or
	// verifying domain keeps the status its own lifecycle put there — the zone
	// path proves the DNS record exists, not that the domain is serving.
	if record.Status == types.DomainStatusError || record.Status == "" {
		if record.Verified {
			record.Status = types.DomainStatusActive
		} else {
			record.Status = types.DomainStatusPending
		}
	}

	// A domain that used to be provisioned as a custom hostname and is now on
	// the zone path must stop pointing at a hostname registration — otherwise
	// teardown branches on a stale id and never deletes the zone DNS record.
	// Cleared only when Cloudflare confirmed the registration was released.
	if result.CustomHostnameReleased {
		record.CustomHostnameID = ""
		record.CustomHostnameStatus = ""
		record.CustomHostnameSSLStatus = ""
		record.PendingDNSRecords = nil
		if record.TLSProvider == types.TLSProviderCloudflareForSaaS {
			record.TLSProvider = types.TLSProviderCertManager
		}
	}
}
