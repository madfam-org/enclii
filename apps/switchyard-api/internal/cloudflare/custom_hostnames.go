package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"
)

// Cloudflare for SaaS custom hostnames.
//
// A client-owned domain (e.g. "cto.creatumundo.mx") cannot use the zone+CNAME
// path in dns.go because that path requires the domain's nameservers to be
// delegated to our Cloudflare account. Custom hostnames solve exactly that:
// we register the client's hostname against ONE zone we already own — the
// "fallback origin" zone — and Cloudflare issues a per-hostname DV certificate
// and proxies the traffic to that zone's origin.
//
// The client keeps their registrar and nameservers, and only adds two records:
//
//	CNAME <hostname>                  -> <fallback-origin hostname>
//	TXT   _cf-custom-hostname.<host>  -> <ownership verification value>
//
// (plus, when the SSL method is "txt", a TXT on _acme-challenge.<hostname>).
//
// Nothing here marks a hostname as usable on our say-so: a hostname is serving
// traffic only when Cloudflare reports status "active" AND ssl.status "active".

// CustomHostnameStatus is the hostname-level state reported by Cloudflare.
type CustomHostnameStatus string

const (
	// CustomHostnameStatusPending means Cloudflare has accepted the hostname
	// but has not yet seen the ownership verification record.
	CustomHostnameStatusPending CustomHostnameStatus = "pending"
	// CustomHostnameStatusPendingValidation means verification is in flight.
	CustomHostnameStatusPendingValidation CustomHostnameStatus = "pending_validation"
	// CustomHostnameStatusActive means Cloudflare is serving the hostname.
	CustomHostnameStatusActive CustomHostnameStatus = "active"
	// CustomHostnameStatusMoved means the hostname stopped pointing at us
	// (the client's CNAME no longer resolves to the fallback origin).
	CustomHostnameStatusMoved CustomHostnameStatus = "moved"
	// CustomHostnameStatusDeleted means the hostname was removed.
	CustomHostnameStatusDeleted CustomHostnameStatus = "deleted"
	// CustomHostnameStatusPendingDeletion means removal is in flight.
	CustomHostnameStatusPendingDeletion CustomHostnameStatus = "pending_deletion"
	// CustomHostnameStatusBlocked means Cloudflare refused the hostname.
	CustomHostnameStatusBlocked CustomHostnameStatus = "blocked"
)

// CustomHostnameSSLStatus is the certificate-level state reported by Cloudflare.
type CustomHostnameSSLStatus string

const (
	// CustomHostnameSSLInitializing is the state right after creation.
	CustomHostnameSSLInitializing CustomHostnameSSLStatus = "initializing"
	// CustomHostnameSSLPendingValidation means the DCV record is still missing.
	CustomHostnameSSLPendingValidation CustomHostnameSSLStatus = "pending_validation"
	// CustomHostnameSSLPendingIssuance means the CA is issuing the certificate.
	CustomHostnameSSLPendingIssuance CustomHostnameSSLStatus = "pending_issuance"
	// CustomHostnameSSLPendingDeployment means the certificate is being pushed to the edge.
	CustomHostnameSSLPendingDeployment CustomHostnameSSLStatus = "pending_deployment"
	// CustomHostnameSSLActive means the certificate is live at the edge.
	CustomHostnameSSLActive CustomHostnameSSLStatus = "active"
	// CustomHostnameSSLDeleted means the certificate was removed.
	CustomHostnameSSLDeleted CustomHostnameSSLStatus = "deleted"
	// CustomHostnameSSLExpired means the certificate is past its validity window.
	CustomHostnameSSLExpired CustomHostnameSSLStatus = "expired"
)

// SSL validation methods supported by Cloudflare for DV certificates.
const (
	// SSLMethodHTTP validates over HTTP (/.well-known/...). It only works once
	// the client's CNAME already resolves to the fallback origin.
	SSLMethodHTTP = "http"
	// SSLMethodTXT validates via a TXT record on _acme-challenge.<hostname>.
	// It works before the CNAME is cut over, so it is the safer default for
	// a live domain that is being migrated with no downtime.
	SSLMethodTXT = "txt"
)

// defaultMinTLSVersion is the floor we set on every issued certificate.
const defaultMinTLSVersion = "1.2"

// DNS record purposes reported back to the caller so a UI/CLI can explain
// which record does what.
const (
	// DNSRecordPurposeRouting is the CNAME that sends traffic to us.
	DNSRecordPurposeRouting = "routing"
	// DNSRecordPurposeOwnership is the TXT that proves the client controls the name.
	DNSRecordPurposeOwnership = "ownership"
	// DNSRecordPurposeSSLValidation is the TXT that lets the CA issue the cert.
	DNSRecordPurposeSSLValidation = "ssl_validation"
)

// CustomHostname is a Cloudflare for SaaS custom hostname.
type CustomHostname struct {
	ID                        string                    `json:"id"`
	Hostname                  string                    `json:"hostname"`
	Status                    CustomHostnameStatus      `json:"status"`
	SSL                       CustomHostnameSSL         `json:"ssl"`
	OwnershipVerification     OwnershipVerification     `json:"ownership_verification"`
	OwnershipVerificationHTTP OwnershipVerificationHTTP `json:"ownership_verification_http"`
	VerificationErrors        []string                  `json:"verification_errors,omitempty"`
	CustomOriginServer        string                    `json:"custom_origin_server,omitempty"`
	CreatedAt                 string                    `json:"created_at,omitempty"`
	CustomMetadata            map[string]string         `json:"custom_metadata,omitempty"`
}

// CustomHostnameSSL is the certificate sub-resource of a custom hostname.
type CustomHostnameSSL struct {
	ID                string                     `json:"id,omitempty"`
	Type              string                     `json:"type,omitempty"`   // always "dv" for us
	Method            string                     `json:"method,omitempty"` // "http" or "txt"
	Status            CustomHostnameSSLStatus    `json:"status,omitempty"`
	ValidationRecords []SSLValidationRecord      `json:"validation_records,omitempty"`
	ValidationErrors  []SSLValidationError       `json:"validation_errors,omitempty"`
	Settings          *CustomHostnameSSLSettings `json:"settings,omitempty"`
}

// CustomHostnameSSLSettings carries the per-certificate TLS settings.
type CustomHostnameSSLSettings struct {
	MinTLSVersion string `json:"min_tls_version,omitempty"`
}

// SSLValidationRecord is one DCV challenge Cloudflare expects to be satisfied.
type SSLValidationRecord struct {
	TxtName  string   `json:"txt_name,omitempty"`
	TxtValue string   `json:"txt_value,omitempty"`
	HTTPURL  string   `json:"http_url,omitempty"`
	HTTPBody string   `json:"http_body,omitempty"`
	Emails   []string `json:"emails,omitempty"`
}

// SSLValidationError is a certificate validation failure reported by Cloudflare.
type SSLValidationError struct {
	Message string `json:"message"`
}

// OwnershipVerification is the TXT record proving control of the hostname.
type OwnershipVerification struct {
	Type  string `json:"type,omitempty"` // "txt"
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// OwnershipVerificationHTTP is the HTTP alternative to the ownership TXT record.
type OwnershipVerificationHTTP struct {
	HTTPURL  string `json:"http_url,omitempty"`
	HTTPBody string `json:"http_body,omitempty"`
}

// ClientDNSRecord is a DNS record the *domain owner* has to create on their
// own nameservers. We can never create these ourselves — that is the whole
// point of the custom-hostname path.
type ClientDNSRecord struct {
	Purpose string `json:"purpose"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Value   string `json:"value"`
}

// CreateCustomHostnameOptions tunes hostname creation. The zero value is valid
// and yields a TXT-validated DV certificate with a TLS 1.2 floor.
type CreateCustomHostnameOptions struct {
	// SSLMethod is "txt" (default) or "http".
	SSLMethod string
	// MinTLSVersion defaults to "1.2".
	MinTLSVersion string
	// CustomOriginServer overrides the zone's fallback origin for this
	// hostname only. Leave empty to use the zone fallback origin.
	CustomOriginServer string
	// AdoptGuard is consulted by EnsureCustomHostname before an already
	// registered custom hostname is returned as this caller's own. The zone
	// is shared by every tenant, so a hostname match alone proves nothing
	// about who owns it; returning an error here refuses the adoption and the
	// error is surfaced to the caller unchanged.
	//
	// nil means "adopt any existing registration", which is only safe for
	// callers that have already established ownership themselves.
	AdoptGuard func(existing *CustomHostname) error
}

func (o *CreateCustomHostnameOptions) sslMethod() string {
	if o == nil || o.SSLMethod == "" {
		return SSLMethodTXT
	}
	return o.SSLMethod
}

func (o *CreateCustomHostnameOptions) minTLSVersion() string {
	if o == nil || o.MinTLSVersion == "" {
		return defaultMinTLSVersion
	}
	return o.MinTLSVersion
}

func (o *CreateCustomHostnameOptions) customOriginServer() string {
	if o == nil {
		return ""
	}
	return o.CustomOriginServer
}

// guardAdoption runs the caller's ownership check against an existing
// registration. No guard means no objection.
func (o *CreateCustomHostnameOptions) guardAdoption(existing *CustomHostname) error {
	if o == nil || o.AdoptGuard == nil {
		return nil
	}
	return o.AdoptGuard(existing)
}

// ListCustomHostnamesFilter narrows a custom hostname listing.
type ListCustomHostnamesFilter struct {
	// Hostname filters by exact hostname (Cloudflare treats this as an
	// exact match on the `hostname` query parameter).
	Hostname string
	// ID filters by custom hostname id.
	ID string
}

func (f *ListCustomHostnamesFilter) query() url.Values {
	query := url.Values{}
	if f == nil {
		return query
	}
	if f.Hostname != "" {
		query.Set("hostname", f.Hostname)
	}
	if f.ID != "" {
		query.Set("id", f.ID)
	}
	return query
}

// IsActive reports whether Cloudflare is actually serving this hostname with a
// live certificate. Both the hostname and its certificate must be active —
// a 200 from the API means nothing on its own.
func (h *CustomHostname) IsActive() bool {
	if h == nil {
		return false
	}
	return h.Status == CustomHostnameStatusActive && h.SSL.Status == CustomHostnameSSLActive
}

// PendingClientDNSRecords returns the records the domain owner still has to
// create for this hostname to go active. fallbackOrigin is the hostname the
// client should CNAME to (our fallback-origin zone record); pass "" to omit
// the routing record.
//
// The list is empty when Cloudflare reports the hostname fully active.
func (h *CustomHostname) PendingClientDNSRecords(fallbackOrigin string) []ClientDNSRecord {
	if h == nil {
		return nil
	}

	var records []ClientDNSRecord

	if h.Status != CustomHostnameStatusActive && fallbackOrigin != "" && h.Hostname != "" {
		records = append(records, ClientDNSRecord{
			Purpose: DNSRecordPurposeRouting,
			Type:    "CNAME",
			Name:    h.Hostname,
			Value:   fallbackOrigin,
		})
	}

	if h.Status != CustomHostnameStatusActive &&
		strings.EqualFold(h.OwnershipVerification.Type, "txt") &&
		h.OwnershipVerification.Name != "" {
		records = append(records, ClientDNSRecord{
			Purpose: DNSRecordPurposeOwnership,
			Type:    "TXT",
			Name:    h.OwnershipVerification.Name,
			Value:   h.OwnershipVerification.Value,
		})
	}

	if h.SSL.Status != CustomHostnameSSLActive {
		for _, record := range h.SSL.ValidationRecords {
			if record.TxtName == "" {
				continue
			}
			records = append(records, ClientDNSRecord{
				Purpose: DNSRecordPurposeSSLValidation,
				Type:    "TXT",
				Name:    record.TxtName,
				Value:   record.TxtValue,
			})
		}
	}

	return records
}

// CreateCustomHostname registers a client-owned hostname on the given
// fallback-origin zone and requests a DV certificate for it.
func (c *Client) CreateCustomHostname(ctx context.Context, zoneID, hostname string, opts *CreateCustomHostnameOptions) (*CustomHostname, error) {
	if zoneID == "" {
		return nil, fmt.Errorf("custom hostname: zone ID is required")
	}
	if hostname == "" {
		return nil, fmt.Errorf("custom hostname: hostname is required")
	}

	payload := map[string]interface{}{
		"hostname": hostname,
		"ssl": map[string]interface{}{
			"method": opts.sslMethod(),
			"type":   "dv",
			"settings": map[string]interface{}{
				"min_tls_version": opts.minTLSVersion(),
			},
		},
	}
	if origin := opts.customOriginServer(); origin != "" {
		payload["custom_origin_server"] = origin
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal custom hostname request: %w", err)
	}

	var resp APIResponse[CustomHostname]
	path := fmt.Sprintf("/zones/%s/custom_hostnames", zoneID)

	if err := c.post(ctx, path, bytes.NewReader(payloadBytes), &resp); err != nil {
		return nil, fmt.Errorf("failed to create custom hostname %s: %w", hostname, err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error creating custom hostname %s: %s", hostname, resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error creating custom hostname %s", hostname)
	}

	if resp.Result.ID == "" {
		return nil, fmt.Errorf("custom hostname %s: Cloudflare returned no hostname id", hostname)
	}

	logrus.WithFields(logrus.Fields{
		"zone_id":            zoneID,
		"hostname":           resp.Result.Hostname,
		"custom_hostname_id": resp.Result.ID,
		"status":             resp.Result.Status,
		"ssl_status":         resp.Result.SSL.Status,
		"ssl_method":         opts.sslMethod(),
	}).Info("Created Cloudflare custom hostname")

	return &resp.Result, nil
}

// GetCustomHostname reads the current validation and certificate state of a
// custom hostname. This is the only source of truth for "is it live yet".
func (c *Client) GetCustomHostname(ctx context.Context, zoneID, customHostnameID string) (*CustomHostname, error) {
	if zoneID == "" {
		return nil, fmt.Errorf("custom hostname: zone ID is required")
	}
	if customHostnameID == "" {
		return nil, fmt.Errorf("custom hostname: custom hostname ID is required")
	}

	var resp APIResponse[CustomHostname]
	path := fmt.Sprintf("/zones/%s/custom_hostnames/%s", zoneID, customHostnameID)

	if err := c.get(ctx, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to get custom hostname %s: %w", customHostnameID, err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error getting custom hostname %s: %s", customHostnameID, resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error getting custom hostname %s", customHostnameID)
	}

	return &resp.Result, nil
}

// ListCustomHostnames lists the custom hostnames on a zone, following
// pagination the same way ListDNSRecordsForZone does.
func (c *Client) ListCustomHostnames(ctx context.Context, zoneID string, filter *ListCustomHostnamesFilter) ([]CustomHostname, error) {
	if zoneID == "" {
		return nil, fmt.Errorf("custom hostname: zone ID is required")
	}

	var all []CustomHostname
	page := 1
	perPage := 50
	baseQuery := filter.query()

	for {
		query := url.Values{}
		for key, values := range baseQuery {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		query.Set("page", fmt.Sprintf("%d", page))
		query.Set("per_page", fmt.Sprintf("%d", perPage))

		var resp APIResponse[[]CustomHostname]
		path := fmt.Sprintf("/zones/%s/custom_hostnames", zoneID)

		if err := c.get(ctx, path, query, &resp); err != nil {
			return nil, fmt.Errorf("failed to list custom hostnames: %w", err)
		}

		if !resp.Success {
			if len(resp.Errors) > 0 {
				return nil, fmt.Errorf("API error listing custom hostnames: %s", resp.Errors[0].Message)
			}
			return nil, fmt.Errorf("unknown API error listing custom hostnames")
		}

		all = append(all, resp.Result...)

		if resp.ResultInfo == nil || page >= resp.ResultInfo.TotalPages {
			break
		}
		page++
	}

	return all, nil
}

// FindCustomHostname returns the custom hostname registered for hostname on
// the zone, or (nil, nil) when there is none.
func (c *Client) FindCustomHostname(ctx context.Context, zoneID, hostname string) (*CustomHostname, error) {
	if hostname == "" {
		return nil, fmt.Errorf("custom hostname: hostname is required")
	}

	hostnames, err := c.ListCustomHostnames(ctx, zoneID, &ListCustomHostnamesFilter{Hostname: hostname})
	if err != nil {
		return nil, err
	}

	for i := range hostnames {
		if strings.EqualFold(hostnames[i].Hostname, hostname) {
			return &hostnames[i], nil
		}
	}

	return nil, nil
}

// EnsureCustomHostname is the idempotent entry point used by provisioning:
// it returns the existing custom hostname when one is already registered and
// creates it otherwise. The bool reports whether it was created now.
//
// Provisioning runs on every deploy, so this must never duplicate a hostname
// and must never report a fresh "pending" for one Cloudflare already serves.
//
// The fallback-origin zone is shared by every tenant, so an existing
// registration for this hostname is NOT evidence that it belongs to the
// caller. opts.AdoptGuard is the caller's ownership check and is run before
// any existing registration is handed back; when it refuses, its error is
// returned unchanged and nothing is created.
func (c *Client) EnsureCustomHostname(ctx context.Context, zoneID, hostname string, opts *CreateCustomHostnameOptions) (*CustomHostname, bool, error) {
	existing, err := c.FindCustomHostname(ctx, zoneID, hostname)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		if guardErr := opts.guardAdoption(existing); guardErr != nil {
			return nil, false, guardErr
		}
		return existing, false, nil
	}

	created, createErr := c.CreateCustomHostname(ctx, zoneID, hostname, opts)
	if createErr == nil {
		return created, true, nil
	}

	// A concurrent provisioning pass (or a hostname registered on this zone
	// outside the window our list filter covered) can lose the create race.
	// Re-read before surfacing the failure so we do not report an error for
	// a hostname that is in fact registered. The ownership guard applies to
	// this adoption too — losing a race to another tenant is exactly the case
	// it exists for.
	if raced, lookupErr := c.FindCustomHostname(ctx, zoneID, hostname); lookupErr == nil && raced != nil {
		if guardErr := opts.guardAdoption(raced); guardErr != nil {
			return nil, false, guardErr
		}
		logrus.WithFields(logrus.Fields{
			"zone_id":  zoneID,
			"hostname": hostname,
		}).Debug("Custom hostname create lost a race, using the existing registration")
		return raced, false, nil
	}

	return nil, false, createErr
}

// DeleteCustomHostname removes a custom hostname from the zone. The client's
// domain stops being served by Cloudflare immediately.
func (c *Client) DeleteCustomHostname(ctx context.Context, zoneID, customHostnameID string) error {
	if zoneID == "" {
		return fmt.Errorf("custom hostname: zone ID is required")
	}
	if customHostnameID == "" {
		return fmt.Errorf("custom hostname: custom hostname ID is required")
	}

	// Cloudflare's custom hostname delete does not wrap its result in the
	// standard {success, errors, result} envelope: it answers with a bare
	// {"id": "<deleted id>"}. We accept that documented shape and a proper
	// success envelope, and nothing else — a 2xx carrying `{}` or `null`
	// confirms no deletion and must not be reported as one.
	var resp struct {
		Success *bool      `json:"success"`
		Errors  []APIError `json:"errors"`
		ID      string     `json:"id"`
		Result  *struct {
			ID string `json:"id"`
		} `json:"result"`
	}

	path := fmt.Sprintf("/zones/%s/custom_hostnames/%s", zoneID, customHostnameID)
	if err := c.httpDelete(ctx, path, &resp); err != nil {
		return fmt.Errorf("failed to delete custom hostname %s: %w", customHostnameID, err)
	}

	if len(resp.Errors) > 0 {
		return fmt.Errorf("API error deleting custom hostname %s: %s", customHostnameID, resp.Errors[0].Message)
	}
	if resp.Success != nil && !*resp.Success {
		return fmt.Errorf("unknown API error deleting custom hostname %s", customHostnameID)
	}

	deleteConfirmed := resp.ID != "" ||
		(resp.Result != nil && resp.Result.ID != "") ||
		(resp.Success != nil && *resp.Success)
	if !deleteConfirmed {
		return fmt.Errorf(
			"custom hostname %s: Cloudflare returned no confirmation of the delete; treating it as still registered",
			customHostnameID)
	}

	logrus.WithFields(logrus.Fields{
		"zone_id":            zoneID,
		"custom_hostname_id": customHostnameID,
	}).Info("Deleted Cloudflare custom hostname")

	return nil
}

// IsAPIError reports whether err carries a Cloudflare API error code and, when
// it does, returns that error. Callers use it to branch on well-known codes
// instead of matching on message strings.
func IsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
