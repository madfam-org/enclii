package porkbun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.porkbun.com/api/json/v3"

type Config struct {
	APIKey       string
	SecretAPIKey string
	BaseURL      string
}

type Client struct {
	apiKey       string
	secretAPIKey string
	baseURL      string
	httpClient   *http.Client
}

// BasicResponse is the envelope every Porkbun response carries. `code` in
// particular has been observed both as a bare number and as a numeric
// string, so all four fields decode tolerantly.
type BasicResponse struct {
	Status    FlexibleString `json:"status"`
	Message   FlexibleString `json:"message,omitempty"`
	Code      FlexibleString `json:"code,omitempty"`
	RequestID FlexibleString `json:"requestId,omitempty"`
}

// Domain mirrors a Porkbun domain record.
//
// Every scalar decodes through the tolerant types in flexible.go: Porkbun
// has flipped the boolean-ish flags between JSON numbers and numeric
// strings without notice (see flexible.go), and the string fields are
// equally unguaranteed. Marshalling is unchanged — FlexibleInt still emits
// a number and FlexibleString still emits a string — so API consumers see
// the same payload they always have.
type Domain struct {
	Domain       FlexibleString `json:"domain"`
	Status       FlexibleString `json:"status"`
	TLD          FlexibleString `json:"tld"`
	CreateDate   FlexibleString `json:"createDate"`
	ExpireDate   FlexibleString `json:"expireDate"`
	SecurityLock FlexibleInt    `json:"securityLock"`
	WhoisPrivacy FlexibleInt    `json:"whoisPrivacy"`
	AutoRenew    FlexibleInt    `json:"autoRenew"`
	APIAccess    FlexibleInt    `json:"apiAccess"`
	NotLocal     FlexibleInt    `json:"notLocal"`
}

type ListDomainsResponse struct {
	BasicResponse
	Domains []Domain `json:"domains"`
}

type GetDomainResponse struct {
	BasicResponse
	Domain Domain `json:"domain"`
}

// NameserversResponse carries the authoritative nameserver list. `ns` is a
// JSON array of hostnames — there is no numeric value for a registrar to
// stringify — so it stays a plain []string.
type NameserversResponse struct {
	BasicResponse
	Nameservers []string `json:"ns"`
}

// DNSRecord mirrors a Porkbun DNS record.
//
// id/ttl/prio are numeric values that Porkbun returns as JSON strings, and
// FlexibleString decodes each of them from a number too, so a flip to real
// JSON numbers cannot reproduce the securityLock breakage. DNSRecord is
// also an *input* type (CreateDNSRecord builds its request body from these
// fields), so the tolerance is deliberately one-directional: it only
// affects decoding, and the fields still marshal as strings.
type DNSRecord struct {
	ID      FlexibleString `json:"id"`
	Name    FlexibleString `json:"name"`
	Type    FlexibleString `json:"type"`
	Content FlexibleString `json:"content"`
	TTL     FlexibleString `json:"ttl"`
	Prio    FlexibleString `json:"prio"`
	Notes   FlexibleString `json:"notes"`
}

type DNSRecordsResponse struct {
	BasicResponse
	Records []DNSRecord `json:"records"`
}

type CreateDNSRecordResponse struct {
	BasicResponse
	ID FlexibleString `json:"id,omitempty"`
}

func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		apiKey:       strings.TrimSpace(cfg.APIKey),
		secretAPIKey: strings.TrimSpace(cfg.SecretAPIKey),
		baseURL:      baseURL,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.apiKey != "" && c.secretAPIKey != ""
}

func (c *Client) ListDomains(ctx context.Context) (*ListDomainsResponse, error) {
	var out ListDomainsResponse
	if err := c.do(ctx, http.MethodGet, "/domain/listAll", nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetDomain(ctx context.Context, domain string) (*GetDomainResponse, error) {
	var out GetDomainResponse
	if err := c.do(ctx, http.MethodGet, "/domain/get/"+url.PathEscape(domain), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetNameservers(ctx context.Context, domain string) (*NameserversResponse, error) {
	var out NameserversResponse
	if err := c.do(ctx, http.MethodGet, "/domain/getNs/"+url.PathEscape(domain), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateNameservers(ctx context.Context, domain string, nameservers []string, idempotencyKey string) (*BasicResponse, error) {
	var out BasicResponse
	body := map[string]any{"ns": nameservers}
	if err := c.do(ctx, http.MethodPost, "/domain/updateNs/"+url.PathEscape(domain), body, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListDNSRecords(ctx context.Context, domain string) (*DNSRecordsResponse, error) {
	var out DNSRecordsResponse
	if err := c.do(ctx, http.MethodGet, "/dns/retrieve/"+url.PathEscape(domain), nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, domain string, record DNSRecord, idempotencyKey string) (*CreateDNSRecordResponse, error) {
	var out CreateDNSRecordResponse
	body := map[string]any{
		"type":    strings.ToUpper(strings.TrimSpace(record.Type.String())),
		"name":    strings.TrimSpace(record.Name.String()),
		"content": strings.TrimSpace(record.Content.String()),
	}
	if ttl := strings.TrimSpace(record.TTL.String()); ttl != "" {
		body["ttl"] = ttl
	}
	if prio := strings.TrimSpace(record.Prio.String()); prio != "" {
		body["prio"] = prio
	}
	if err := c.do(ctx, http.MethodPost, "/dns/create/"+url.PathEscape(domain), body, idempotencyKey, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method, path string, body map[string]any, idempotencyKey string, out any) error {
	if !c.Configured() {
		return fmt.Errorf("porkbun API credentials are not configured")
	}

	var reader io.Reader
	if method == http.MethodPost {
		if body == nil {
			body = map[string]any{}
		}
		body["apikey"] = c.apiKey
		body["secretapikey"] = c.secretAPIKey
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal porkbun request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodGet {
		req.Header.Set("X-API-Key", c.apiKey)
		req.Header.Set("X-Secret-API-Key", c.secretAPIKey)
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		req.Header.Set("Idempotency-Key", strings.TrimSpace(idempotencyKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("porkbun API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode porkbun response: %w", err)
	}
	if failed, message := responseFailed(out); failed {
		return fmt.Errorf("porkbun API error: %s", message)
	}
	return nil
}

func responseFailed(out any) (bool, string) {
	payload, err := json.Marshal(out)
	if err != nil {
		return false, ""
	}
	var basic BasicResponse
	if err := json.Unmarshal(payload, &basic); err != nil {
		return false, ""
	}
	message := basic.Message.String()
	code := basic.Code.String()
	if strings.EqualFold(basic.Status.String(), "ERROR") {
		if message != "" && code != "" {
			return true, code + ": " + message
		}
		if message != "" {
			return true, message
		}
		if code != "" {
			return true, code
		}
		return true, "unknown error"
	}
	return false, ""
}
