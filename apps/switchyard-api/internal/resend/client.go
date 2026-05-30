package resend

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

const defaultBaseURL = "https://api.resend.com"

// Client wraps the Resend REST API.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// Config configures a Resend API client.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient returns a Resend client. Empty API key means Configured() is false.
func NewClient(cfg Config) *Client {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		apiKey:     strings.TrimSpace(cfg.APIKey),
		baseURL:    strings.TrimRight(base, "/"),
		httpClient: hc,
	}
}

// Configured reports whether the client can call the Resend API.
func (c *Client) Configured() bool {
	return c != nil && c.apiKey != ""
}

// HTTPClient exposes the underlying client (for test overrides).
func (c *Client) HTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	return c.httpClient
}

// SetHTTPClient replaces the HTTP client (tests).
func (c *Client) SetHTTPClient(hc *http.Client) {
	if c != nil {
		c.httpClient = hc
	}
}

// Domain represents a Resend domain resource.
type Domain struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Region    string         `json:"region"`
	CreatedAt string         `json:"created_at"`
	Records   []DomainRecord `json:"records,omitempty"`
}

// DomainRecord is a DNS record required for domain verification.
type DomainRecord struct {
	Record   string `json:"record"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	TTL      string `json:"ttl"`
	Priority int    `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// EmailMessage is a sent email summary from Resend.
type EmailMessage struct {
	ID        string   `json:"id"`
	From      string   `json:"from"`
	To        []string `json:"to"`
	Subject   string   `json:"subject"`
	CreatedAt string   `json:"created_at"`
	LastEvent string   `json:"last_event"`
}

// SendEmailRequest is the outbound email payload.
type SendEmailRequest struct {
	From    string
	To      []string
	Subject string
	HTML    string
	Text    string
}

// SendEmailResponse is returned after a successful send.
type SendEmailResponse struct {
	ID string `json:"id"`
}

type listDomainsResponse struct {
	Data []Domain `json:"data"`
}

type listEmailsResponse struct {
	Data []EmailMessage `json:"data"`
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if !c.Configured() {
		return fmt.Errorf("resend client is not configured")
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var errBody map[string]any
		_ = json.Unmarshal(raw, &errBody)
		return fmt.Errorf("resend API %s %s: status %d: %s", method, path, resp.StatusCode, string(raw))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ListDomains returns all domains in the Resend account.
func (c *Client) ListDomains(ctx context.Context) ([]Domain, error) {
	var resp listDomainsResponse
	if err := c.do(ctx, http.MethodGet, "/domains", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetDomain returns a domain by ID.
func (c *Client) GetDomain(ctx context.Context, id string) (*Domain, error) {
	var domain Domain
	if err := c.do(ctx, http.MethodGet, "/domains/"+url.PathEscape(id), nil, &domain); err != nil {
		return nil, err
	}
	return &domain, nil
}

// GetDomainByName finds a domain by apex name.
func (c *Client) GetDomainByName(ctx context.Context, name string) (*Domain, error) {
	domains, err := c.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for i := range domains {
		if strings.EqualFold(domains[i].Name, want) {
			return &domains[i], nil
		}
	}
	return nil, nil
}

// CreateDomain registers a new sending domain.
func (c *Client) CreateDomain(ctx context.Context, name, region string) (*Domain, error) {
	payload := map[string]string{
		"name":   strings.TrimSpace(name),
		"region": strings.TrimSpace(region),
	}
	var domain Domain
	if err := c.do(ctx, http.MethodPost, "/domains", payload, &domain); err != nil {
		return nil, err
	}
	return &domain, nil
}

// VerifyDomain triggers DNS verification for a domain.
func (c *Client) VerifyDomain(ctx context.Context, id string) (*Domain, error) {
	var domain Domain
	if err := c.do(ctx, http.MethodPost, "/domains/"+url.PathEscape(id)+"/verify", map[string]any{}, &domain); err != nil {
		return nil, err
	}
	return &domain, nil
}

// ListEmails returns recent sent emails, optionally filtered by domain query param.
func (c *Client) ListEmails(ctx context.Context, domain string) ([]EmailMessage, error) {
	path := "/emails"
	if d := strings.TrimSpace(domain); d != "" {
		path += "?domain=" + url.QueryEscape(d)
	}
	var resp listEmailsResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// SendEmail sends a transactional email.
func (c *Client) SendEmail(ctx context.Context, req SendEmailRequest) (*SendEmailResponse, error) {
	payload := map[string]any{
		"from":    req.From,
		"to":      req.To,
		"subject": req.Subject,
	}
	if req.HTML != "" {
		payload["html"] = req.HTML
	}
	if req.Text != "" {
		payload["text"] = req.Text
	}
	var resp SendEmailResponse
	if err := c.do(ctx, http.MethodPost, "/emails", payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
