package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Cloudflare for SaaS provisioning for client-owned domains.
//
// The zone+CNAME path in domain_provisioner.go requires MADFAM to control the
// domain's nameservers. A client who keeps their own registrar cannot use it.
// For those domains we register a Cloudflare custom hostname on our
// fallback-origin zone; the client then adds a CNAME plus a verification TXT
// record on their own DNS and Cloudflare issues a per-hostname certificate.
//
// Nothing in here declares success on our own authority: the stored status is
// whatever Cloudflare last reported, and the domain is only marked verified
// when Cloudflare says both the hostname and its certificate are active.
//
// Two properties are load-bearing and easy to lose:
//
//  1. The fallback-origin zone is SHARED by every tenant. A hostname string is
//     therefore never evidence of ownership; every operation on a custom
//     hostname carries the project claiming it and refuses when another
//     project already holds that hostname.
//  2. The mechanism decision fails closed. "We could not reach Cloudflare" is
//     not "this domain is client-owned", so an undecidable lookup aborts that
//     domain's provisioning instead of moving it onto another mechanism.

// domainProvisioningMechanism identifies how a domain reaches our edge.
type domainProvisioningMechanism string

const (
	// mechanismZoneCNAME is the pre-existing path: a Cloudflare zone we
	// control plus a proxied CNAME to the tunnel.
	mechanismZoneCNAME domainProvisioningMechanism = "zone-cname"
	// mechanismCustomHostname is the Cloudflare for SaaS path for
	// client-owned domains.
	mechanismCustomHostname domainProvisioningMechanism = "custom-hostname"
	// mechanismUndetermined means the decision could not be made — typically
	// a Cloudflare zone lookup that failed for a reason other than "no such
	// zone". Provisioning aborts for that domain and the existing record is
	// left as it is; nothing is switched on a guess.
	mechanismUndetermined domainProvisioningMechanism = "undetermined"
)

// domainProvisioningResult is the typed outcome of provisioning one domain.
// Deploy-path provisioning does not abort a deploy on failure (that is by
// design), so Err is carried here and persisted on the domain record instead
// of being swallowed by a log line.
type domainProvisioningResult struct {
	Domain            string                      `json:"domain"`
	Mechanism         domainProvisioningMechanism `json:"mechanism"`
	CustomHostnameID  string                      `json:"custom_hostname_id,omitempty"`
	HostnameStatus    string                      `json:"custom_hostname_status,omitempty"`
	SSLStatus         string                      `json:"custom_hostname_ssl_status,omitempty"`
	PendingDNSRecords []types.PendingDNSRecord    `json:"pending_dns_records,omitempty"`
	Err               error                       `json:"-"`
	ErrorMessage      string                      `json:"error,omitempty"`

	// CustomHostnameReleased reports that a stale custom hostname was
	// successfully deleted at Cloudflare during this pass, which is the only
	// condition under which the stored hostname id may be cleared.
	CustomHostnameReleased bool `json:"-"`
}

// WaitingOnClient reports whether the domain owner still has records to add.
func (r *domainProvisioningResult) WaitingOnClient() bool {
	return r != nil && len(r.PendingDNSRecords) > 0
}

// setErr records a failure on the result in both typed and rendered form.
func (r *domainProvisioningResult) setErr(err error) {
	if err == nil {
		return
	}
	r.Err = err
	r.ErrorMessage = err.Error()
}

// domainOwner identifies the project (and service) claiming a hostname.
// Every custom-hostname operation carries one.
type domainOwner struct {
	ProjectID uuid.UUID
	ServiceID uuid.UUID
}

// ownerFromService derives the claiming project from the service being
// provisioned. Returns nil for a nil service, which every ownership check
// treats as "ownership unknown" and refuses.
func ownerFromService(service *types.Service) *domainOwner {
	if service == nil {
		return nil
	}
	return &domainOwner{ProjectID: service.ProjectID, ServiceID: service.ID}
}

// customHostnameZone returns the configured fallback-origin zone id and the
// hostname clients CNAME to. ok is false when Cloudflare for SaaS is not
// configured, in which case the custom-hostname path must not be attempted.
func (h *Handler) customHostnameZone() (zoneID, fallbackOrigin string, ok bool) {
	if h == nil || h.config == nil {
		return "", "", false
	}
	zoneID = strings.TrimSpace(h.config.CloudflareFallbackOriginZoneID)
	fallbackOrigin = strings.TrimSpace(h.config.CloudflareFallbackOriginHostname)
	return zoneID, fallbackOrigin, zoneID != "" && fallbackOrigin != ""
}

// zoneResolver is the slice of the Cloudflare client the mechanism decision
// needs: "is this domain's apex a zone we already control?". Declared as an
// interface so the decision is testable without a live Cloudflare API.
type zoneResolver interface {
	FindZoneForDomain(ctx context.Context, domain string) (*cloudflare.Zone, error)
}

// resolveDomainMechanism decides which provisioning path a domain takes.
//
//   - external declared true  → custom hostname, always
//   - external declared false → zone + CNAME, always (unchanged behaviour)
//   - external absent         → zone + CNAME when the apex already has a zone
//     in our Cloudflare account, custom hostname when Cloudflare confirmed it
//     does not. If Cloudflare for SaaS is not configured, absent falls back to
//     the pre-existing zone path so that an unconfigured platform behaves
//     exactly as it does today.
//
// A zone lookup that fails for any reason other than "no such zone" — a 5xx,
// a 429, a timeout, an expired token, a truncated pagination — yields
// mechanismUndetermined and an error. That case must NOT be read as "the
// domain is client-owned": doing so would move a live MADFAM domain onto the
// custom-hostname path and rewrite its record on the strength of a blip.
func (h *Handler) resolveDomainMechanism(
	ctx context.Context,
	zones zoneResolver,
	domain string,
	external *bool,
) (domainProvisioningMechanism, error) {
	if external != nil {
		if *external {
			return mechanismCustomHostname, nil
		}
		return mechanismZoneCNAME, nil
	}

	if _, _, saasConfigured := h.customHostnameZone(); !saasConfigured {
		return mechanismZoneCNAME, nil
	}

	if zones == nil {
		return mechanismZoneCNAME, nil
	}

	_, err := zones.FindZoneForDomain(ctx, domain)
	switch {
	case err == nil:
		// A zone we already control keeps the zone+CNAME path so no existing
		// MADFAM-owned domain changes mechanism.
		return mechanismZoneCNAME, nil
	case errors.Is(err, cloudflare.ErrZoneNotFound):
		// Cloudflare positively reported that the account holds no zone for
		// this domain, so it is client-owned.
		return mechanismCustomHostname, nil
	default:
		return mechanismUndetermined, fmt.Errorf(
			"cannot decide how to provision %s: the Cloudflare zone lookup neither found a zone nor reported one absent, "+
				"so whether we control this domain's nameservers is unknown: %w", domain, err)
	}
}

// hostnameOwner resolves the project that holds the custom_domains record for
// a hostname.
//
//	(uuid.Nil, false, nil) — no record exists; nobody holds this hostname
//	(id,       true,  nil) — project id holds it
//	(_,        _,     err) — could not tell; callers must fail closed
func (h *Handler) hostnameOwner(ctx context.Context, domain string) (uuid.UUID, bool, error) {
	if h == nil || h.repos == nil || h.repos.CustomDomains == nil || h.repos.Services == nil {
		return uuid.Nil, false, fmt.Errorf(
			"cannot establish which project owns %s: the domain repositories are unavailable", domain)
	}

	record, err := h.repos.CustomDomains.GetByDomain(ctx, domain)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("failed to look up the owner of %s: %w", domain, err)
	}
	if record == nil {
		return uuid.Nil, false, nil
	}

	service, err := h.repos.Services.GetByID(record.ServiceID)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("failed to resolve the project that owns %s: %w", domain, err)
	}
	if service == nil {
		return uuid.Nil, false, fmt.Errorf(
			"custom domain %s references service %s, which no longer exists", domain, record.ServiceID)
	}

	return service.ProjectID, true, nil
}

// assertHostnameClaimableBy refuses when another project already holds the
// custom_domains record for this hostname.
//
// The fallback-origin zone is shared, so without this check any project could
// register — or silently adopt — a hostname another project is already being
// served on, and have its own row marked verified off the back of the other
// project's certificate.
func (h *Handler) assertHostnameClaimableBy(ctx context.Context, domain string, owner *domainOwner) error {
	if owner == nil || owner.ProjectID == uuid.Nil {
		return fmt.Errorf(
			"refusing to provision a custom hostname for %s: the claiming project could not be determined", domain)
	}

	ownerProjectID, held, err := h.hostnameOwner(ctx, domain)
	if err != nil {
		return err
	}
	if held && ownerProjectID != owner.ProjectID {
		return fmt.Errorf(
			"refusing to claim custom hostname %s for project %s: it is already registered to project %s",
			domain, owner.ProjectID, ownerProjectID)
	}

	return nil
}

// ensureCustomHostname registers (idempotently) a Cloudflare for SaaS custom
// hostname for a client-owned domain and reports what the client still owes.
//
// owner is the project claiming the hostname. It is checked before Cloudflare
// is called and again, through the client's adoption guard, before an existing
// registration is handed back as ours.
func (h *Handler) ensureCustomHostname(ctx context.Context, domain string, owner *domainOwner) domainProvisioningResult {
	result := domainProvisioningResult{
		Domain:    domain,
		Mechanism: mechanismCustomHostname,
	}

	zoneID, fallbackOrigin, ok := h.customHostnameZone()
	if !ok {
		result.setErr(fmt.Errorf(
			"cloudflare for saas is not configured: set ENCLII_CLOUDFLARE_FALLBACK_ORIGIN_ZONE_ID and ENCLII_CLOUDFLARE_FALLBACK_ORIGIN_HOSTNAME to provision client-owned domain %s", domain))
		return result
	}

	// Ownership before reachability: whether another project already holds
	// this hostname is the answer regardless of whether Cloudflare is up, and
	// it is the cheaper question.
	if err := h.assertHostnameClaimableBy(ctx, domain, owner); err != nil {
		result.setErr(err)
		return result
	}

	if h.domainSyncService == nil {
		result.setErr(fmt.Errorf("domain sync service unavailable, cannot provision custom hostname for %s", domain))
		return result
	}

	cfClient := h.domainSyncService.GetCloudflareClient()
	if cfClient == nil {
		result.setErr(fmt.Errorf("cloudflare client unavailable, cannot provision custom hostname for %s", domain))
		return result
	}

	// TXT validation works before the client cuts their CNAME over, so a live
	// domain can be migrated without a certificate gap. HTTP validation would
	// require the traffic to already point at us.
	hostname, created, err := cfClient.EnsureCustomHostname(ctx, zoneID, domain, &cloudflare.CreateCustomHostnameOptions{
		SSLMethod: cloudflare.SSLMethodTXT,
		// Re-checked at the moment of adoption: the pre-check above and the
		// Cloudflare read are not one transaction, and adopting a hostname is
		// exactly the operation that would otherwise inherit another
		// project's verified certificate.
		AdoptGuard: func(*cloudflare.CustomHostname) error {
			return h.assertHostnameClaimableBy(ctx, domain, owner)
		},
	})
	if err != nil {
		result.setErr(fmt.Errorf("failed to provision custom hostname for %s: %w", domain, err))
		return result
	}

	result.CustomHostnameID = hostname.ID
	result.HostnameStatus = string(hostname.Status)
	result.SSLStatus = string(hostname.SSL.Status)
	result.PendingDNSRecords = toPendingDNSRecords(hostname.PendingClientDNSRecords(fallbackOrigin))

	fields := []logging.Field{
		logging.String("domain", domain),
		logging.String("custom_hostname_id", hostname.ID),
		logging.String("status", string(hostname.Status)),
		logging.String("ssl_status", string(hostname.SSL.Status)),
		logging.Int("pending_client_dns_records", len(result.PendingDNSRecords)),
	}
	if created {
		h.logger.Info(ctx, "Cloudflare custom hostname registered for client-owned domain", fields...)
	} else {
		h.logger.Debug(ctx, "Cloudflare custom hostname already registered", fields...)
	}

	return result
}

// refreshCustomHostnameState re-reads a known custom hostname from Cloudflare.
// Used by the verify path, where the caller is asking "is it live yet?" and
// only Cloudflare can answer.
func (h *Handler) refreshCustomHostnameState(ctx context.Context, domain, customHostnameID string) domainProvisioningResult {
	result := domainProvisioningResult{
		Domain:           domain,
		Mechanism:        mechanismCustomHostname,
		CustomHostnameID: customHostnameID,
	}

	zoneID, fallbackOrigin, ok := h.customHostnameZone()
	if !ok {
		result.setErr(fmt.Errorf("cloudflare for saas is not configured, cannot read custom hostname state for %s", domain))
		return result
	}

	if h.domainSyncService == nil {
		result.setErr(fmt.Errorf("domain sync service unavailable, cannot read custom hostname state for %s", domain))
		return result
	}

	cfClient := h.domainSyncService.GetCloudflareClient()
	if cfClient == nil {
		result.setErr(fmt.Errorf("cloudflare client unavailable, cannot read custom hostname state for %s", domain))
		return result
	}

	hostname, err := cfClient.GetCustomHostname(ctx, zoneID, customHostnameID)
	if err != nil {
		result.setErr(fmt.Errorf("failed to read custom hostname state for %s: %w", domain, err))
		return result
	}

	result.HostnameStatus = string(hostname.Status)
	result.SSLStatus = string(hostname.SSL.Status)
	result.PendingDNSRecords = toPendingDNSRecords(hostname.PendingClientDNSRecords(fallbackOrigin))
	return result
}

// releaseCustomHostnameForProject removes the custom hostname serving domain,
// but only after proving the caller's project is the one entitled to remove
// it. Junctions store no hostname id, so the hostname is resolved from the
// project's own custom_domains record, or — when there is none and no other
// project holds one either — by hostname on the fallback-origin zone.
//
// Every uncertain case refuses. Deleting a custom hostname takes the client's
// domain offline at the edge; doing that to the wrong tenant is worse than
// leaving a registration behind for an operator to reap.
func (h *Handler) releaseCustomHostnameForProject(ctx context.Context, domain string, owner *domainOwner) error {
	zoneID, _, ok := h.customHostnameZone()
	if !ok {
		// Nothing could have been provisioned this way, so there is nothing
		// to delete and nothing to complain about.
		return nil
	}
	if h.domainSyncService == nil {
		return nil
	}
	cfClient := h.domainSyncService.GetCloudflareClient()
	if cfClient == nil {
		return nil
	}

	if owner == nil || owner.ProjectID == uuid.Nil {
		return fmt.Errorf(
			"refusing to release the custom hostname for %s: the requesting project could not be determined", domain)
	}

	ownerProjectID, held, err := h.hostnameOwner(ctx, domain)
	if err != nil {
		return err
	}
	if held && ownerProjectID != owner.ProjectID {
		return fmt.Errorf(
			"refusing to release custom hostname %s on behalf of project %s: it belongs to project %s",
			domain, owner.ProjectID, ownerProjectID)
	}

	hostname, err := cfClient.FindCustomHostname(ctx, zoneID, domain)
	if err != nil {
		return fmt.Errorf("failed to look up custom hostname for %s: %w", domain, err)
	}
	if hostname == nil {
		return nil
	}

	return cfClient.DeleteCustomHostname(ctx, zoneID, hostname.ID)
}

// deleteCustomHostname removes a custom hostname during domain teardown, using
// the id stored on the domain record. The record is the ownership proof here:
// the caller loaded it from the project's own row.
func (h *Handler) deleteCustomHostname(ctx context.Context, domain, customHostnameID string) error {
	if customHostnameID == "" {
		return nil
	}

	zoneID, _, ok := h.customHostnameZone()
	if !ok {
		return fmt.Errorf("cloudflare for saas is not configured, cannot delete custom hostname for %s", domain)
	}
	if h.domainSyncService == nil {
		return fmt.Errorf("domain sync service unavailable, cannot delete custom hostname for %s", domain)
	}
	cfClient := h.domainSyncService.GetCloudflareClient()
	if cfClient == nil {
		return fmt.Errorf("cloudflare client unavailable, cannot delete custom hostname for %s", domain)
	}

	return cfClient.DeleteCustomHostname(ctx, zoneID, customHostnameID)
}

// toPendingDNSRecords converts the Cloudflare client's record list into the
// transport/storage type. Kept explicit so the sdk-go types package does not
// take a dependency on the internal cloudflare package.
func toPendingDNSRecords(records []cloudflare.ClientDNSRecord) []types.PendingDNSRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]types.PendingDNSRecord, 0, len(records))
	for _, record := range records {
		out = append(out, types.PendingDNSRecord{
			Purpose: record.Purpose,
			Type:    record.Type,
			Name:    record.Name,
			Value:   record.Value,
		})
	}
	return out
}

// persistDomainProvisioningResult writes the provisioning outcome onto the
// CustomDomain row so a failure or an outstanding client action is visible on
// the domain read path instead of only in the logs.
//
// Domains with no CustomDomain row (junctions provision by hostname alone) are
// a no-op here; their result is returned to the caller instead.
func (h *Handler) persistDomainProvisioningResult(ctx context.Context, result domainProvisioningResult) {
	if h == nil || h.repos == nil || h.repos.CustomDomains == nil || result.Domain == "" {
		return
	}

	record, err := h.repos.CustomDomains.GetByDomain(ctx, result.Domain)
	if err != nil {
		h.logger.Warn(ctx, "Failed to load custom domain for provisioning state update",
			logging.String("domain", result.Domain),
			logging.Error("error", err))
		return
	}
	if record == nil {
		return
	}

	// A domain that flipped from `external: true` back to the zone path keeps
	// its custom hostname registered at Cloudflare until we release it. The
	// stored id is only cleared once Cloudflare confirmed the delete, so a
	// failure here leaves the id in place for teardown to retry rather than
	// orphaning the registration.
	if result.Mechanism == mechanismZoneCNAME && record.CustomHostnameID != "" {
		if delErr := h.deleteCustomHostname(ctx, record.Domain, record.CustomHostnameID); delErr != nil {
			h.logger.Warn(ctx, "Failed to release the custom hostname of a domain that moved back to the zone path",
				logging.String("domain", record.Domain),
				logging.String("custom_hostname_id", record.CustomHostnameID),
				logging.Error("error", delErr))
		} else {
			result.CustomHostnameReleased = true
			h.logger.Info(ctx, "Released the custom hostname of a domain that moved back to the zone path",
				logging.String("domain", record.Domain),
				logging.String("custom_hostname_id", record.CustomHostnameID))
		}
	}

	applyProvisioningResult(record, result, time.Now())

	if err := h.repos.CustomDomains.UpdateCustomHostnameState(ctx, record); err != nil {
		h.logger.Warn(ctx, "Failed to persist domain provisioning state",
			logging.String("domain", result.Domain),
			logging.Error("error", err))
	}
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
	record.CustomHostnameID = result.CustomHostnameID
	record.CustomHostnameStatus = result.HostnameStatus
	record.CustomHostnameSSLStatus = result.SSLStatus
	record.PendingDNSRecords = result.PendingDNSRecords
	record.ProvisioningError = result.ErrorMessage
	record.ProvisioningCheckedAt = &now

	switch {
	case result.Err != nil:
		record.Status = types.DomainStatusError
		record.Verified = false
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

// describePendingClientAction renders the outstanding client action as a
// single operator-readable sentence.
//
// It includes record VALUES — the ownership token and the DCV challenge — so
// it is for the domain owner, over an authenticated response. Never log it;
// use describePendingClientRecordNames for that.
func describePendingClientAction(result domainProvisioningResult) string {
	if result.Err != nil {
		return fmt.Sprintf("Provisioning failed for %s: %s", result.Domain, result.ErrorMessage)
	}
	if !result.WaitingOnClient() {
		if result.Mechanism == mechanismCustomHostname {
			return fmt.Sprintf("Cloudflare reports %s as %s (certificate %s).",
				result.Domain, result.HostnameStatus, result.SSLStatus)
		}
		return ""
	}

	parts := make([]string, 0, len(result.PendingDNSRecords))
	for _, record := range result.PendingDNSRecords {
		parts = append(parts, fmt.Sprintf("%s %s -> %s", record.Type, record.Name, record.Value))
	}
	return fmt.Sprintf(
		"Waiting on the domain owner to add %d DNS record(s) on %s: %s",
		len(parts), result.Domain, strings.Join(parts, "; "))
}

// describePendingClientRecordNames renders the outstanding client action with
// record names only. The values are secrets in the useful sense — the
// `_cf-custom-hostname` ownership token and the `_acme-challenge` DCV value
// are what proves control of the hostname — and logs are the one place they
// must not be reproduced.
func describePendingClientRecordNames(result domainProvisioningResult) string {
	if !result.WaitingOnClient() {
		return ""
	}

	names := make([]string, 0, len(result.PendingDNSRecords))
	for _, record := range result.PendingDNSRecords {
		names = append(names, fmt.Sprintf("%s %s", record.Type, record.Name))
	}
	return fmt.Sprintf("%d DNS record(s) on %s: %s",
		len(names), result.Domain, strings.Join(names, "; "))
}
