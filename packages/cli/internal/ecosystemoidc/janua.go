package ecosystemoidc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultJanuaAPIURL = "https://auth.madfam.io"

// JanuaClient is a minimal Janua OAuth admin/register HTTP client.
type JanuaClient struct {
	BaseURL        string
	AdminToken     string
	InternalAPIKey string
	HTTP           *http.Client
}

func NewJanuaClient(adminToken, internalAPIKey string) *JanuaClient {
	base := strings.TrimRight(os.Getenv("ENCLII_JANUA_API_URL"), "/")
	if base == "" {
		base = strings.TrimRight(os.Getenv("JANUA_API_URL"), "/")
	}
	if base == "" {
		base = defaultJanuaAPIURL
	}
	return &JanuaClient{
		BaseURL:        base,
		AdminToken:     strings.TrimSpace(adminToken),
		InternalAPIKey: strings.TrimSpace(internalAPIKey),
		HTTP:           &http.Client{Timeout: 30 * time.Second},
	}
}

type remoteOAuthClient struct {
	ID           string   `json:"id"`
	ClientID     string   `json:"client_id"`
	ClientSecret *string  `json:"client_secret"`
	Name         string   `json:"name"`
	Audience     *string  `json:"audience"`
	ClientKey    *string  `json:"client_key"`
	RedirectURIs []string `json:"redirect_uris"`
}

type rotateSecretResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (c *JanuaClient) registerOrReconcile(ctx context.Context, spec JanuaClientSpec) (remoteOAuthClient, bool, error) {
	body := map[string]interface{}{
		"name":            spec.Name,
		"description":     spec.Description,
		"redirect_uris":   spec.RedirectURIs,
		"allowed_scopes":  spec.AllowedScopes,
		"grant_types":     spec.GrantTypes,
		"audience":        spec.Audience,
		"client_key":      spec.ClientKey,
		"website_url":     spec.WebsiteURL,
		"is_confidential": spec.confidential(),
	}
	if spec.ClientID != "" {
		body["client_id"] = spec.ClientID
	}

	if c.InternalAPIKey != "" {
		var out remoteOAuthClient
		status, err := c.doJSON(ctx, http.MethodPost, "/api/v1/oauth/clients/register", body, &out, true)
		if err != nil {
			return remoteOAuthClient{}, false, err
		}
		return out, status == http.StatusCreated, nil
	}
	if c.AdminToken == "" {
		return remoteOAuthClient{}, false, fmt.Errorf("auth required for Janua: run `enclii login` as admin@madfam.io or set JANUA_INTERNAL_API_KEY")
	}

	existing, err := c.findExisting(ctx, spec)
	if err != nil {
		return remoteOAuthClient{}, false, err
	}
	if existing != nil {
		return *existing, false, nil
	}

	var created remoteOAuthClient
	status, err := c.doJSON(ctx, http.MethodPost, "/api/v1/oauth/clients", body, &created, false)
	if err != nil {
		return remoteOAuthClient{}, false, err
	}
	return created, status == http.StatusCreated, nil
}

func (c *JanuaClient) findExisting(ctx context.Context, spec JanuaClientSpec) (*remoteOAuthClient, error) {
	if c.InternalAPIKey != "" {
		var out remoteOAuthClient
		path := "/api/v1/oauth/clients/internal/by-name/" + url.PathEscape(spec.Name)
		status, err := c.doJSON(ctx, http.MethodGet, path, nil, &out, true)
		if err != nil {
			if status == http.StatusNotFound {
				return nil, nil
			}
			return nil, err
		}
		return &out, nil
	}

	for pageNum := 1; pageNum <= 20; pageNum++ {
		var page struct {
			Clients []remoteOAuthClient `json:"clients"`
			Total   int                 `json:"total"`
		}
		path := fmt.Sprintf("/api/v1/oauth/clients/admin/all?page=%d&per_page=100", pageNum)
		if _, err := c.doJSON(ctx, http.MethodGet, path, nil, &page, false); err != nil {
			return nil, err
		}
		for i := range page.Clients {
			item := page.Clients[i]
			if spec.ClientID != "" && item.ClientID == spec.ClientID {
				return &item, nil
			}
			if item.Name == spec.Name {
				return &item, nil
			}
			if spec.ClientKey != "" && item.ClientKey != nil && *item.ClientKey == spec.ClientKey {
				return &item, nil
			}
			if item.Audience != nil && *item.Audience == spec.Audience {
				return &item, nil
			}
		}
		if len(page.Clients) == 0 || pageNum*100 >= page.Total {
			break
		}
	}
	return nil, nil
}

func (c *JanuaClient) rotateSecret(ctx context.Context, internalUUID string) (string, error) {
	var out rotateSecretResponse
	path := "/api/v1/oauth/clients/" + url.PathEscape(internalUUID) + "/rotate"
	_, err := c.doJSON(ctx, http.MethodPost, path, map[string]interface{}{}, &out, false)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out.ClientSecret) == "" {
		return "", fmt.Errorf("rotate returned empty client_secret")
	}
	return out.ClientSecret, nil
}

func (c *JanuaClient) doJSON(ctx context.Context, method, path string, body interface{}, out interface{}, internal bool) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if internal {
		req.Header.Set("X-Internal-API-Key", c.InternalAPIKey)
	} else if c.AdminToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AdminToken)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return resp.StatusCode, fmt.Errorf("request to Janua failed: %s %s: %s", method, path, msg)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode Janua response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// ResolveClientSecret returns a plaintext secret from create response or rotate.
func (c *JanuaClient) ResolveClientSecret(ctx context.Context, remote remoteOAuthClient, created bool, rotateIfMissing bool) (string, error) {
	if remote.ClientSecret != nil && strings.TrimSpace(*remote.ClientSecret) != "" {
		return strings.TrimSpace(*remote.ClientSecret), nil
	}
	if !rotateIfMissing {
		return "", fmt.Errorf("client %s exists without retrievable secret — re-run with --rotate-secret", remote.ClientID)
	}
	if strings.TrimSpace(remote.ID) == "" {
		return "", fmt.Errorf("cannot rotate secret: missing internal client UUID")
	}
	return c.rotateSecret(ctx, remote.ID)
}
