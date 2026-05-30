package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"
)

// ListDNSRecords retrieves all DNS records for the configured zone
func (c *Client) ListDNSRecords(ctx context.Context) ([]DNSRecord, error) {
	return c.ListDNSRecordsForZone(ctx, c.zoneID)
}

// ListDNSRecordsForZone retrieves all DNS records for a specific zone
func (c *Client) ListDNSRecordsForZone(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	var allRecords []DNSRecord
	page := 1
	perPage := 100

	for {
		query := url.Values{}
		query.Set("page", fmt.Sprintf("%d", page))
		query.Set("per_page", fmt.Sprintf("%d", perPage))

		var resp APIResponse[[]DNSRecord]
		path := fmt.Sprintf("/zones/%s/dns_records", zoneID)

		if err := c.get(ctx, path, query, &resp); err != nil {
			return nil, fmt.Errorf("failed to list DNS records: %w", err)
		}

		if !resp.Success {
			if len(resp.Errors) > 0 {
				return nil, fmt.Errorf("API error: %s", resp.Errors[0].Message)
			}
			return nil, fmt.Errorf("unknown API error")
		}

		allRecords = append(allRecords, resp.Result...)

		if resp.ResultInfo == nil || page >= resp.ResultInfo.TotalPages {
			break
		}
		page++
	}

	logrus.WithField("count", len(allRecords)).Debug("Retrieved DNS records from Cloudflare")
	return allRecords, nil
}

// GetDNSRecord retrieves a specific DNS record by domain name.
// Uses FindZoneForDomain to support multi-zone lookups (e.g., madfam.io, tezca.mx).
func (c *Client) GetDNSRecord(ctx context.Context, domain string) (*DNSRecord, error) {
	zone, err := c.FindZoneForDomain(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to find zone for %s: %w", domain, err)
	}

	query := url.Values{}
	query.Set("name", domain)

	var resp APIResponse[[]DNSRecord]
	path := fmt.Sprintf("/zones/%s/dns_records", zone.ID)

	if err := c.get(ctx, path, query, &resp); err != nil {
		return nil, fmt.Errorf("failed to get DNS record for %s: %w", domain, err)
	}

	if !resp.Success || len(resp.Result) == 0 {
		return nil, nil // Record not found
	}

	return &resp.Result[0], nil
}

// GetDNSRecordByType retrieves a DNS record by name and type.
// Uses FindZoneForDomain to support multi-zone lookups.
func (c *Client) GetDNSRecordByType(ctx context.Context, domain, recordType string) (*DNSRecord, error) {
	zone, err := c.FindZoneForDomain(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to find zone for %s: %w", domain, err)
	}

	query := url.Values{}
	query.Set("name", domain)
	query.Set("type", recordType)

	var resp APIResponse[[]DNSRecord]
	path := fmt.Sprintf("/zones/%s/dns_records", zone.ID)

	if err := c.get(ctx, path, query, &resp); err != nil {
		return nil, fmt.Errorf("failed to get %s record for %s: %w", recordType, domain, err)
	}

	if !resp.Success || len(resp.Result) == 0 {
		return nil, nil
	}

	return &resp.Result[0], nil
}

// VerifyDomainDNS checks if a domain's DNS is correctly configured to point to the tunnel
func (c *Client) VerifyDomainDNS(ctx context.Context, domain, expectedCNAME string) (*DNSVerificationResult, error) {
	result := &DNSVerificationResult{
		Domain:          domain,
		ExpectedContent: expectedCNAME,
	}

	// First, try to find a CNAME record
	record, err := c.GetDNSRecordByType(ctx, domain, "CNAME")
	if err != nil {
		return nil, fmt.Errorf("failed to verify DNS for %s: %w", domain, err)
	}

	if record != nil {
		result.RecordExists = true
		result.RecordType = "CNAME"
		result.RecordContent = record.Content
		result.Proxied = record.Proxied
		result.IsCorrect = strings.EqualFold(record.Content, expectedCNAME)
		return result, nil
	}

	// If no CNAME, check for A record (proxied domains might use A records)
	record, err = c.GetDNSRecordByType(ctx, domain, "A")
	if err != nil {
		return nil, fmt.Errorf("failed to verify DNS for %s: %w", domain, err)
	}

	if record != nil {
		result.RecordExists = true
		result.RecordType = "A"
		result.RecordContent = record.Content
		result.Proxied = record.Proxied
		// A records with proxied enabled might still be correctly configured
		result.IsCorrect = record.Proxied
		return result, nil
	}

	// No record found
	result.RecordExists = false
	result.IsCorrect = false
	return result, nil
}

// VerifyDomainTXTRecord checks for a specific TXT verification record
func (c *Client) VerifyDomainTXTRecord(ctx context.Context, domain, expectedValue string) (bool, error) {
	record, err := c.GetDNSRecordByType(ctx, domain, "TXT")
	if err != nil {
		return false, fmt.Errorf("failed to verify TXT record for %s: %w", domain, err)
	}

	if record == nil {
		return false, nil
	}

	return strings.Contains(record.Content, expectedValue), nil
}

// CheckDomainExists checks if a domain has any DNS records in the zone
func (c *Client) CheckDomainExists(ctx context.Context, domain string) (bool, error) {
	record, err := c.GetDNSRecord(ctx, domain)
	if err != nil {
		return false, err
	}
	return record != nil, nil
}

// CreateDNSRecord creates a new DNS record in the configured zone
func (c *Client) CreateDNSRecord(ctx context.Context, name, recordType, content string, proxied bool) (*DNSRecord, error) {
	return c.CreateDNSRecordInZone(ctx, c.zoneID, name, recordType, content, proxied)
}

// CreateDNSRecordInZone creates a new DNS record in a specific zone
func (c *Client) CreateDNSRecordInZone(ctx context.Context, zoneID, name, recordType, content string, proxied bool) (*DNSRecord, error) {
	payload := struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		Proxied bool   `json:"proxied"`
		TTL     int    `json:"ttl"`
		Comment string `json:"comment,omitempty"`
	}{
		Type:    recordType,
		Name:    name,
		Content: content,
		Proxied: proxied,
		TTL:     1, // Auto
		Comment: "Managed by Enclii platform",
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DNS record: %w", err)
	}

	var resp APIResponse[DNSRecord]
	path := fmt.Sprintf("/zones/%s/dns_records", zoneID)

	if err := c.post(ctx, path, bytes.NewReader(payloadBytes), &resp); err != nil {
		return nil, fmt.Errorf("failed to create DNS record for %s: %w", name, err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error creating DNS record: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error creating DNS record")
	}

	logrus.WithFields(logrus.Fields{
		"name":    name,
		"type":    recordType,
		"content": content,
		"proxied": proxied,
	}).Info("Created DNS record in Cloudflare")

	return &resp.Result, nil
}

// CreateDNSRecordInZoneWithPriority creates a DNS record, including MX priority when set (>0).
func (c *Client) CreateDNSRecordInZoneWithPriority(ctx context.Context, zoneID, name, recordType, content string, proxied bool, priority int) (*DNSRecord, error) {
	payload := map[string]any{
		"type":    recordType,
		"name":    name,
		"content": content,
		"proxied": proxied,
		"ttl":     1,
		"comment": "Managed by Enclii platform (Resend DNS)",
	}
	if strings.EqualFold(recordType, "MX") && priority > 0 {
		payload["priority"] = priority
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DNS record: %w", err)
	}

	var resp APIResponse[DNSRecord]
	path := fmt.Sprintf("/zones/%s/dns_records", zoneID)

	if err := c.post(ctx, path, bytes.NewReader(payloadBytes), &resp); err != nil {
		return nil, fmt.Errorf("failed to create DNS record for %s: %w", name, err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error creating DNS record: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error creating DNS record")
	}

	return &resp.Result, nil
}

// UpdateDNSRecordInZone updates an existing DNS record in a specific zone.
func (c *Client) UpdateDNSRecordInZone(ctx context.Context, zoneID string, record DNSRecord, content string, proxied bool) (*DNSRecord, error) {
	if record.ID == "" {
		return nil, fmt.Errorf("record ID is required")
	}
	if record.Name == "" {
		return nil, fmt.Errorf("record name is required")
	}

	recordType := record.Type
	if recordType == "" {
		recordType = "CNAME"
	}
	ttl := record.TTL
	if ttl == 0 {
		ttl = 1
	}

	payload := struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		Proxied bool   `json:"proxied"`
		TTL     int    `json:"ttl"`
		Comment string `json:"comment,omitempty"`
	}{
		Type:    recordType,
		Name:    record.Name,
		Content: content,
		Proxied: proxied,
		TTL:     ttl,
		Comment: "Managed by Enclii platform",
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DNS record update: %w", err)
	}

	var resp APIResponse[DNSRecord]
	path := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, record.ID)

	if err := c.put(ctx, path, bytes.NewReader(payloadBytes), &resp); err != nil {
		return nil, fmt.Errorf("failed to update DNS record for %s: %w", record.Name, err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error updating DNS record: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error updating DNS record")
	}

	logrus.WithFields(logrus.Fields{
		"name":    record.Name,
		"type":    recordType,
		"content": content,
		"proxied": proxied,
	}).Info("Updated DNS record in Cloudflare")

	return &resp.Result, nil
}

// DeleteDNSRecord deletes a DNS record by its ID in the configured zone
func (c *Client) DeleteDNSRecord(ctx context.Context, recordID string) error {
	return c.DeleteDNSRecordInZone(ctx, c.zoneID, recordID)
}

// DeleteDNSRecordInZone deletes a DNS record by its ID in a specific zone
func (c *Client) DeleteDNSRecordInZone(ctx context.Context, zoneID, recordID string) error {
	var resp APIResponse[struct {
		ID string `json:"id"`
	}]
	path := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)

	if err := c.httpDelete(ctx, path, &resp); err != nil {
		return fmt.Errorf("failed to delete DNS record %s: %w", recordID, err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return fmt.Errorf("API error deleting DNS record: %s", resp.Errors[0].Message)
		}
		return fmt.Errorf("unknown API error deleting DNS record")
	}

	logrus.WithField("record_id", recordID).Info("Deleted DNS record from Cloudflare")
	return nil
}

// ListAccountZones lists all zones accessible by the API token (any status).
func (c *Client) ListAccountZones(ctx context.Context) ([]Zone, error) {
	return c.listZonesPaginated(ctx, url.Values{})
}

// ListZones lists active zones accessible by the API token.
func (c *Client) ListZones(ctx context.Context) ([]Zone, error) {
	query := url.Values{}
	query.Set("status", "active")
	return c.listZonesPaginated(ctx, query)
}

func (c *Client) listZonesPaginated(ctx context.Context, baseQuery url.Values) ([]Zone, error) {
	var allZones []Zone
	page := 1
	perPage := 50

	for {
		query := url.Values{}
		for k, v := range baseQuery {
			for _, item := range v {
				query.Add(k, item)
			}
		}
		query.Set("page", fmt.Sprintf("%d", page))
		query.Set("per_page", fmt.Sprintf("%d", perPage))

		var resp APIResponse[[]Zone]
		if err := c.get(ctx, "/zones", query, &resp); err != nil {
			return nil, fmt.Errorf("failed to list zones: %w", err)
		}

		if !resp.Success {
			if len(resp.Errors) > 0 {
				return nil, fmt.Errorf("API error: %s", resp.Errors[0].Message)
			}
			return nil, fmt.Errorf("unknown API error")
		}

		allZones = append(allZones, resp.Result...)

		if resp.ResultInfo == nil || page >= resp.ResultInfo.TotalPages {
			break
		}
		page++
	}

	return allZones, nil
}

// FindZoneForDomain finds the Cloudflare zone that manages a given domain
// For example, "api.qubic.quest" would match zone "qubic.quest"
func (c *Client) FindZoneForDomain(ctx context.Context, domain string) (*Zone, error) {
	zones, err := c.ListZones(ctx)
	if err != nil {
		return nil, err
	}

	// Find the most specific matching zone (longest suffix match)
	var bestMatch *Zone
	bestLen := 0

	for i, zone := range zones {
		if domain == zone.Name || strings.HasSuffix(domain, "."+zone.Name) {
			if len(zone.Name) > bestLen {
				bestMatch = &zones[i]
				bestLen = len(zone.Name)
			}
		}
	}

	if bestMatch == nil {
		return nil, fmt.Errorf("no Cloudflare zone found for domain %s", domain)
	}

	return bestMatch, nil
}

// CreateZone creates a new Cloudflare zone for a domain.
// Uses jump_start to auto-scan for existing DNS records.
func (c *Client) CreateZone(ctx context.Context, name string) (*Zone, error) {
	payload := struct {
		Name    string `json:"name"`
		Account struct {
			ID string `json:"id"`
		} `json:"account"`
		JumpStart bool `json:"jump_start"`
	}{
		Name:      name,
		JumpStart: true,
	}
	payload.Account.ID = c.accountID

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal zone creation request: %w", err)
	}

	var resp APIResponse[Zone]
	if err := c.post(ctx, "/zones", bytes.NewReader(payloadBytes), &resp); err != nil {
		return nil, fmt.Errorf("failed to create zone %s: %w", name, err)
	}

	if !resp.Success {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("API error creating zone %s: %s", name, resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("unknown API error creating zone %s", name)
	}

	logrus.WithFields(logrus.Fields{
		"zone_id":      resp.Result.ID,
		"zone_name":    resp.Result.Name,
		"status":       resp.Result.Status,
		"name_servers": resp.Result.NameServers,
	}).Info("Created Cloudflare zone")

	return &resp.Result, nil
}

// EnsureZoneForDomain finds the Cloudflare zone for a domain, creating it if missing.
// Extracts the apex domain (last 2 segments) from the FQDN for zone creation.
func (c *Client) EnsureZoneForDomain(ctx context.Context, domain string) (*Zone, error) {
	zone, err := c.FindZoneForDomain(ctx, domain)
	if err == nil {
		return zone, nil
	}

	// Extract apex domain: "api.tezca.mx" → "tezca.mx"
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("cannot extract apex domain from %s", domain)
	}
	apex := strings.Join(parts[len(parts)-2:], ".")

	logrus.WithFields(logrus.Fields{
		"domain": domain,
		"apex":   apex,
	}).Info("Zone not found, creating new Cloudflare zone")

	return c.CreateZone(ctx, apex)
}

// EnsureDNSRecord creates a CNAME record for the domain if it doesn't already exist.
// If a record exists with different content, it is left unchanged.
// Returns the record and whether it was created.
func (c *Client) EnsureDNSRecord(ctx context.Context, domain, cnameTarget string) (*DNSRecord, bool, error) {
	// Find which zone manages this domain
	zone, err := c.FindZoneForDomain(ctx, domain)
	if err != nil {
		return nil, false, err
	}

	// Check if record already exists in that zone
	query := url.Values{}
	query.Set("name", domain)
	query.Set("type", "CNAME")

	var resp APIResponse[[]DNSRecord]
	path := fmt.Sprintf("/zones/%s/dns_records", zone.ID)

	if err := c.get(ctx, path, query, &resp); err != nil {
		return nil, false, fmt.Errorf("failed to check DNS record for %s: %w", domain, err)
	}

	if resp.Success && len(resp.Result) > 0 {
		record := resp.Result[0]
		if !strings.EqualFold(record.Content, cnameTarget) || !record.Proxied {
			updated, err := c.UpdateDNSRecordInZone(ctx, zone.ID, record, cnameTarget, true)
			if err != nil {
				return nil, false, err
			}
			return updated, false, nil
		}

		return &record, false, nil
	}

	// Create the record
	record, err := c.CreateDNSRecordInZone(ctx, zone.ID, domain, "CNAME", cnameTarget, true)
	if err != nil {
		return nil, false, err
	}

	return record, true, nil
}
