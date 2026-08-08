package api

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// domainProvisioningMechanism identifies how a domain reaches our edge.
type domainProvisioningMechanism string

const (
	// mechanismZoneCNAME is the pre-existing path: a Cloudflare zone we
	// control plus a proxied CNAME to the tunnel.
	mechanismZoneCNAME domainProvisioningMechanism = "zone-cname"
	// mechanismCustomHostname is the Cloudflare for SaaS path for
	// client-owned domains.
	mechanismCustomHostname domainProvisioningMechanism = "custom-hostname"
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
//     in our Cloudflare account, custom hostname otherwise. If Cloudflare for
//     SaaS is not configured, absent falls back to the pre-existing zone path
//     so that an unconfigured platform behaves exactly as it does today.
func (h *Handler) resolveDomainMechanism(
	ctx context.Context,
	zones zoneResolver,
	domain string,
	external *bool,
) domainProvisioningMechanism {
	if external != nil {
		if *external {
			return mechanismCustomHostname
		}
		return mechanismZoneCNAME
	}

	if _, _, saasConfigured := h.customHostnameZone(); !saasConfigured {
		return mechanismZoneCNAME
	}

	if zones == nil {
		return mechanismZoneCNAME
	}

	// A zone we already control keeps the zone+CNAME path so no existing
	// MADFAM-owned domain changes mechanism.
	if _, err := zones.FindZoneForDomain(ctx, domain); err == nil {
		return mechanismZoneCNAME
	}

	return mechanismCustomHostname
}

// ensureCustomHostname registers (idempotently) a Cloudflare for SaaS custom
// hostname for a client-owned domain and reports what the client still owes.
func (h *Handler) ensureCustomHostname(ctx context.Context, domain string) domainProvisioningResult {
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

// deleteCustomHostnameByDomain removes a custom hostname when the caller has
// no stored hostname id (junctions provision by hostname alone). It is a no-op
// when the hostname was never registered.
func (h *Handler) deleteCustomHostnameByDomain(ctx context.Context, domain string) error {
	zoneID, _, ok := h.customHostnameZone()
	if !ok {
		return nil
	}
	if h.domainSyncService == nil {
		return nil
	}
	cfClient := h.domainSyncService.GetCloudflareClient()
	if cfClient == nil {
		return nil
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

// deleteCustomHostname removes a custom hostname during domain teardown.
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

	if result.Mechanism == mechanismZoneCNAME {
		// The zone path is unchanged; only clear stale custom-hostname state
		// and record whether provisioning failed.
		record.ProvisioningError = result.ErrorMessage
		record.ProvisioningCheckedAt = &now
		if result.Err != nil {
			record.Status = types.DomainStatusError
		}
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

// describePendingClientAction renders the outstanding client action as a
// single operator-readable sentence.
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
