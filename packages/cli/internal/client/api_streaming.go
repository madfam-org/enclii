package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// ==================== Custom Domains ====================

// CustomDomainRequest represents a request to add a custom domain
type CustomDomainRequest struct {
	Domain        string `json:"domain"`
	Environment   string `json:"environment,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`
	TLSEnabled    bool   `json:"tls_enabled"`
	TLSIssuer     string `json:"tls_issuer,omitempty"`
}

// CustomDomainResponse represents a custom domain in API responses
type CustomDomainResponse struct {
	ID               uuid.UUID  `json:"id"`
	ServiceID        uuid.UUID  `json:"service_id"`
	EnvironmentID    *uuid.UUID `json:"environment_id,omitempty"`
	Domain           string     `json:"domain"`
	Verified         bool       `json:"verified"`
	TLSEnabled       bool       `json:"tls_enabled"`
	TLSIssuer        string     `json:"tls_issuer,omitempty"`
	TLSProvider      string     `json:"tls_provider,omitempty"`
	Status           string     `json:"status"`
	DNSCNAME         string     `json:"dns_cname,omitempty"`
	IsPlatformDomain bool       `json:"is_platform_domain"`
	ZeroTrustEnabled bool       `json:"zero_trust_enabled"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	VerifiedAt       *time.Time `json:"verified_at,omitempty"`
}

type addCustomDomainResponse struct {
	CustomDomainResponse
}

func (r *addCustomDomainResponse) UnmarshalJSON(data []byte) error {
	var envelope struct {
		Domain json.RawMessage `json:"domain"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Domain) > 0 && envelope.Domain[0] == '{' {
		var wrapped struct {
			Domain CustomDomainResponse `json:"domain"`
		}
		if err := json.Unmarshal(data, &wrapped); err != nil {
			return err
		}
		r.CustomDomainResponse = wrapped.Domain
		return nil
	}

	var domain CustomDomainResponse
	if err := json.Unmarshal(data, &domain); err != nil {
		return err
	}
	r.CustomDomainResponse = domain
	return nil
}

// DomainVerifyResponse represents the verification response
type DomainVerifyResponse struct {
	Message           string                `json:"message"`
	Domain            *CustomDomainResponse `json:"domain,omitempty"`
	VerificationValue string                `json:"verification_value,omitempty"`
	Error             string                `json:"error,omitempty"`
}

// ListCustomDomains returns all custom domains for a service
func (c *APIClient) ListCustomDomains(ctx context.Context, serviceID string) ([]CustomDomainResponse, error) {
	var response struct {
		Domains []CustomDomainResponse `json:"domains"`
	}

	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/domains", serviceID), &response); err != nil {
		return nil, fmt.Errorf("failed to list domains: %w", err)
	}

	return response.Domains, nil
}

// AddCustomDomain adds a custom domain to a service
func (c *APIClient) AddCustomDomain(ctx context.Context, serviceID string, req CustomDomainRequest) (*CustomDomainResponse, error) {
	var domain addCustomDomainResponse
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/domains", serviceID), req, &domain); err != nil {
		return nil, fmt.Errorf("failed to add domain: %w", err)
	}

	return &domain.CustomDomainResponse, nil
}

// GetCustomDomain gets a specific custom domain
func (c *APIClient) GetCustomDomain(ctx context.Context, serviceID, domainID string) (*CustomDomainResponse, error) {
	var domain CustomDomainResponse
	if err := c.get(ctx, fmt.Sprintf("/v1/services/%s/domains/%s", serviceID, domainID), &domain); err != nil {
		return nil, fmt.Errorf("failed to get domain: %w", err)
	}

	return &domain, nil
}

// DeleteCustomDomain removes a custom domain from a service
func (c *APIClient) DeleteCustomDomain(ctx context.Context, serviceID, domainID string) error {
	resp, err := c.makeRequest(ctx, "DELETE", fmt.Sprintf("/v1/services/%s/domains/%s", serviceID, domainID), nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return nil
}

// VerifyCustomDomain verifies domain ownership via DNS TXT record
func (c *APIClient) VerifyCustomDomain(ctx context.Context, serviceID, domainID string) (*DomainVerifyResponse, error) {
	var response DomainVerifyResponse
	if err := c.post(ctx, fmt.Sprintf("/v1/services/%s/domains/%s/verify", serviceID, domainID), nil, &response); err != nil {
		return nil, fmt.Errorf("failed to verify domain: %w", err)
	}

	return &response, nil
}

// ==================== Log Streaming (WebSocket) ====================

// LogStreamMessage represents a log message from WebSocket streaming
type LogStreamMessage struct {
	Type      string    `json:"type"` // "log", "error", "info", "connected", "disconnected"
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// StreamLogsOptions configures log streaming behavior
type StreamLogsOptions struct {
	Lines      int
	Timestamps bool
	Since      *time.Time
}

// StreamLogs establishes a WebSocket connection to stream logs in real-time.
// It returns a channel of LogStreamMessage and an error channel.
// The caller should select on both channels and handle the context cancellation.
func (c *APIClient) StreamLogs(ctx context.Context, serviceID string, envName string, opts StreamLogsOptions) (<-chan LogStreamMessage, <-chan error, error) {
	// Build WebSocket URL
	wsURL, err := c.buildWSURL(serviceID, envName, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build WebSocket URL: %w", err)
	}

	// Set up WebSocket dialer with auth header
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	header := http.Header{}
	if c.token != "" {
		header.Set("Authorization", "Bearer "+c.token)
	}
	header.Set("User-Agent", c.userAgent)

	// Connect to WebSocket
	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil && resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("WebSocket connection failed (%d): %s", resp.StatusCode, string(body))
		}
		return nil, nil, fmt.Errorf("failed to connect to log stream: %w", err)
	}

	// Create channels
	logChan := make(chan LogStreamMessage, 100)
	errChan := make(chan error, 1)

	// Start goroutine to read messages
	go func() {
		defer close(logChan)
		defer close(errChan)
		defer func() { _ = conn.Close() }()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				_, message, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						return
					}
					if ctx.Err() != nil {
						return // Context cancelled, don't report as error
					}
					select {
					case errChan <- fmt.Errorf("read error: %w", err):
					default:
					}
					return
				}

				var msg LogStreamMessage
				if err := json.Unmarshal(message, &msg); err != nil {
					// Skip malformed messages
					continue
				}

				select {
				case logChan <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return logChan, errChan, nil
}

// buildWSURL constructs the WebSocket URL for log streaming
func (c *APIClient) buildWSURL(serviceID, envName string, opts StreamLogsOptions) (string, error) {
	// Parse base URL and convert to WebSocket scheme
	baseURL := c.baseURL
	if strings.HasPrefix(baseURL, "https://") {
		baseURL = "wss://" + strings.TrimPrefix(baseURL, "https://")
	} else if strings.HasPrefix(baseURL, "http://") {
		baseURL = "ws://" + strings.TrimPrefix(baseURL, "http://")
	}

	// Build path with query parameters
	path := fmt.Sprintf("/v1/services/%s/logs/stream", serviceID)

	params := url.Values{}
	if envName != "" {
		params.Set("env", envName)
	}
	if opts.Lines > 0 {
		params.Set("lines", fmt.Sprintf("%d", opts.Lines))
	}
	if opts.Timestamps {
		params.Set("timestamps", "true")
	}
	if opts.Since != nil {
		params.Set("since", opts.Since.Format(time.RFC3339))
	}

	fullURL := baseURL + path
	if params.Encode() != "" {
		fullURL += "?" + params.Encode()
	}

	return fullURL, nil
}

// StreamDeploymentLogs streams logs for a specific deployment (alternative endpoint)
func (c *APIClient) StreamDeploymentLogs(ctx context.Context, deploymentID string, opts StreamLogsOptions) (<-chan LogStreamMessage, <-chan error, error) {
	// Build WebSocket URL
	baseURL := c.baseURL
	if strings.HasPrefix(baseURL, "https://") {
		baseURL = "wss://" + strings.TrimPrefix(baseURL, "https://")
	} else if strings.HasPrefix(baseURL, "http://") {
		baseURL = "ws://" + strings.TrimPrefix(baseURL, "http://")
	}

	path := fmt.Sprintf("/v1/deployments/%s/logs/stream", deploymentID)

	params := url.Values{}
	if opts.Lines > 0 {
		params.Set("lines", fmt.Sprintf("%d", opts.Lines))
	}
	if opts.Timestamps {
		params.Set("timestamps", "true")
	}
	if opts.Since != nil {
		params.Set("since", opts.Since.Format(time.RFC3339))
	}

	wsURL := baseURL + path
	if params.Encode() != "" {
		wsURL += "?" + params.Encode()
	}

	// Set up WebSocket dialer with auth header
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	header := http.Header{}
	if c.token != "" {
		header.Set("Authorization", "Bearer "+c.token)
	}
	header.Set("User-Agent", c.userAgent)

	// Connect to WebSocket
	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil && resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("WebSocket connection failed (%d): %s", resp.StatusCode, string(body))
		}
		return nil, nil, fmt.Errorf("failed to connect to log stream: %w", err)
	}

	// Create channels
	logChan := make(chan LogStreamMessage, 100)
	errChan := make(chan error, 1)

	// Start goroutine to read messages
	go func() {
		defer close(logChan)
		defer close(errChan)
		defer func() { _ = conn.Close() }()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				_, message, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						return
					}
					if ctx.Err() != nil {
						return
					}
					select {
					case errChan <- fmt.Errorf("read error: %w", err):
					default:
					}
					return
				}

				var msg LogStreamMessage
				if err := json.Unmarshal(message, &msg); err != nil {
					continue
				}

				select {
				case logChan <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return logChan, errChan, nil
}
