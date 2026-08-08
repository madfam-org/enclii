package cloudflare

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

// GetSSLCertificatePacks retrieves all SSL certificate packs for the zone
func (c *Client) GetSSLCertificatePacks(ctx context.Context) ([]SSLCertificatePack, error) {
	return c.GetSSLCertificatePacksForZone(ctx, c.zoneID)
}

// GetSSLCertificatePacksForZone retrieves SSL certificate packs for a specific zone
func (c *Client) GetSSLCertificatePacksForZone(ctx context.Context, zoneID string) ([]SSLCertificatePack, error) {
	var resp APIResponse[[]SSLCertificatePack]
	path := fmt.Sprintf("/zones/%s/ssl/certificate_packs", zoneID)

	if err := c.get(ctx, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to get SSL certificate packs: %w", err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error")
	}

	return resp.Result, nil
}

// GetSSLVerificationStatus retrieves SSL verification status for hostnames
func (c *Client) GetSSLVerificationStatus(ctx context.Context) ([]SSLVerificationStatus, error) {
	var resp APIResponse[[]SSLVerificationStatus]
	path := fmt.Sprintf("/zones/%s/ssl/verification", c.zoneID)

	if err := c.get(ctx, path, nil, &resp); err != nil {
		return nil, fmt.Errorf("failed to get SSL verification status: %w", err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error")
	}

	return resp.Result, nil
}

// GetSSLStatus returns the SSL status for a specific domain
func (c *Client) GetSSLStatus(ctx context.Context, domain string) (*SSLStatus, error) {
	result := &SSLStatus{
		Domain:            domain,
		HasCertificate:    false,
		CertificateStatus: "none",
		IsCloudflareSSL:   false,
	}

	// Get certificate packs
	packs, err := c.GetSSLCertificatePacks(ctx)
	if err != nil {
		logrus.WithError(err).WithField("domain", domain).Warn("Failed to get SSL certificate packs")
		// Don't fail, just return default status
		return result, nil
	}

	// Check if domain is covered by any certificate pack
	for _, pack := range packs {
		for _, host := range pack.Hosts {
			if matchesDomain(host, domain) {
				result.HasCertificate = true
				result.CertificateStatus = pack.Status
				result.Issuer = pack.CertificateAuthority
				result.IsCloudflareSSL = true

				if pack.Status == "active" && !pack.ExpiresOn.IsZero() {
					result.ExpiresAt = &pack.ExpiresOn
				}

				logrus.WithFields(logrus.Fields{
					"domain": domain,
					"status": pack.Status,
					"issuer": pack.CertificateAuthority,
				}).Debug("Found SSL certificate for domain")

				return result, nil
			}
		}
	}

	return result, nil
}

// VerifyTLS checks if a domain has an active TLS certificate
func (c *Client) VerifyTLS(ctx context.Context, domain string) (bool, error) {
	status, err := c.GetSSLStatus(ctx, domain)
	if err != nil {
		return false, err
	}

	return status.HasCertificate && status.CertificateStatus == "active", nil
}

// matchesDomain checks if a certificate host matches a domain.
// Handles wildcards (*.example.com) and exact matches.
//
// A wildcard covers EXACTLY ONE label, per RFC 6125 and per what Cloudflare
// Universal SSL actually serves: "*.madfam.io" matches "a.madfam.io" but NOT
// "a.b.madfam.io". Reporting a match for a nested host would claim a valid
// certificate for a name whose TLS handshake fails at the edge.
func matchesDomain(certHost, domain string) bool {
	// Exact match
	if strings.EqualFold(certHost, domain) {
		return true
	}

	// Wildcard match (*.example.com matches sub.example.com, one level only)
	if strings.HasPrefix(certHost, "*.") {
		baseDomain := strings.ToLower(certHost[2:]) // Remove "*."
		if baseDomain == "" {
			return false
		}

		lowerDomain := strings.ToLower(domain)
		suffix := "." + baseDomain
		if !strings.HasSuffix(lowerDomain, suffix) {
			return false
		}

		// The wildcard consumes a single label: what is left must be one
		// non-empty label with no further dots.
		label := lowerDomain[:len(lowerDomain)-len(suffix)]
		return label != "" && !strings.Contains(label, ".")
	}

	return false
}
