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

type BasicResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Code      string `json:"code,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

type Domain struct {
	Domain       string `json:"domain"`
	Status       string `json:"status"`
	TLD          string `json:"tld"`
	CreateDate   string `json:"createDate"`
	ExpireDate   string `json:"expireDate"`
	SecurityLock int    `json:"securityLock"`
	WhoisPrivacy int    `json:"whoisPrivacy"`
	AutoRenew    int    `json:"autoRenew"`
	APIAccess    int    `json:"apiAccess"`
	NotLocal     int    `json:"notLocal"`
}

type ListDomainsResponse struct {
	BasicResponse
	Domains []Domain `json:"domains"`
}

type GetDomainResponse struct {
	BasicResponse
	Domain Domain `json:"domain"`
}

type NameserversResponse struct {
	BasicResponse
	Nameservers []string `json:"ns"`
}

type DNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	TTL     string `json:"ttl"`
	Prio    string `json:"prio"`
	Notes   string `json:"notes"`
}

type DNSRecordsResponse struct {
	BasicResponse
	Records []DNSRecord `json:"records"`
}

type CreateDNSRecordResponse struct {
	BasicResponse
	ID string `json:"id,omitempty"`
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
		"type":    strings.ToUpper(strings.TrimSpace(record.Type)),
		"name":    strings.TrimSpace(record.Name),
		"content": strings.TrimSpace(record.Content),
	}
	if strings.TrimSpace(record.TTL) != "" {
		body["ttl"] = strings.TrimSpace(record.TTL)
	}
	if strings.TrimSpace(record.Prio) != "" {
		body["prio"] = strings.TrimSpace(record.Prio)
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
	if strings.EqualFold(basic.Status, "ERROR") {
		if basic.Message != "" && basic.Code != "" {
			return true, basic.Code + ": " + basic.Message
		}
		if basic.Message != "" {
			return true, basic.Message
		}
		if basic.Code != "" {
			return true, basic.Code
		}
		return true, "unknown error"
	}
	return false, ""
}
