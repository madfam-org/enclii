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
		HTTP:           &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

type remoteOAuthClient struct {
	ID             string   `json:"id"`
	ClientID       string   `json:"client_id"`
	ClientSecret   *string  `json:"client_secret"`
	Name           string   `json:"name"`
	Audience       *string  `json:"audience"`
	ClientKey      *string  `json:"client_key"`
	RedirectURIs   []string `json:"redirect_uris"`
	OrganizationID *string  `json:"organization_id"`
	AllowedScopes  []string `json:"allowed_scopes"`
	GrantTypes     []string `json:"grant_types"`
	IsConfidential bool     `json:"is_confidential"`
	IsActive       bool     `json:"is_active"`
}

type rotateSecretResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (c *JanuaClient) registerOrReconcile(ctx context.Context, spec JanuaClientSpec) (remoteOAuthClient, bool, error) {
	// Org-bound machine clients are distinct consumers even when they share an
	// API audience. Check identity and privileges before any registration/rotation.
	if spec.OrganizationID != "" {
		existing, err := c.findExisting(ctx, spec)
		if err != nil {
			return remoteOAuthClient{}, false, err
		}
		if existing != nil {
			if err := validateMachineClient(*existing, spec); err != nil {
				return remoteOAuthClient{}, false, err
			}
			return *existing, false, nil
		}
	}

	body := map[string]interface{}{
		"name":            spec.Name,
		"description":     spec.Description,
		"allowed_scopes":  spec.AllowedScopes,
		"grant_types":     spec.GrantTypes,
		"audience":        spec.Audience,
		"client_key":      spec.ClientKey,
		"website_url":     spec.WebsiteURL,
		"is_confidential": spec.confidential(),
	}
	// A login client carries redirect_uris; a client_credentials machine client
	// has none. Sending an empty/null redirect_uris on a machine client invites
	// a validation error from Janua, so send the field only when it has values.
	if len(spec.RedirectURIs) > 0 {
		body["redirect_uris"] = spec.RedirectURIs
	}
	// Org-bound machine clients (nauta-symbiosis-hcm) pass their tenant so Janua
	// scopes the service identity — and, per Janua #595, emits its app:role
	// scopes verbatim into the roles claim. Omitted for unbound login clients.
	if spec.OrganizationID != "" {
		body["organization_id"] = spec.OrganizationID
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
		if spec.OrganizationID != "" {
			if err := validateMachineClient(out, spec); err != nil {
				return remoteOAuthClient{}, false, err
			}
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
		if spec.OrganizationID != "" {
			if err := validateMachineClient(*existing, spec); err != nil {
				return remoteOAuthClient{}, false, err
			}
		}
		return *existing, false, nil
	}

	var created remoteOAuthClient
	status, err := c.doJSON(ctx, http.MethodPost, "/api/v1/oauth/clients", body, &created, false)
	if err != nil {
		return remoteOAuthClient{}, false, err
	}
	if spec.OrganizationID != "" {
		if err := validateMachineClient(created, spec); err != nil {
			return remoteOAuthClient{}, false, err
		}
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
				if spec.ClientID != "" {
					return nil, fmt.Errorf("pinned Janua client was not found")
				}
				return nil, nil
			}
			return nil, err
		}
		if spec.ClientID != "" && out.ClientID != spec.ClientID {
			return nil, fmt.Errorf("Janua client name does not match pinned identity")
		}
		return &out, nil
	}

	var match *remoteOAuthClient
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
			// A pin is exclusive. A name/key collision may not override it.
			selected := spec.ClientID != "" && item.ClientID == spec.ClientID
			if spec.ClientID == "" {
				// Janua currently aliases client_key to audience; it is not an identity.
				selected = item.Name == spec.Name
			}
			if selected {
				if match != nil && match.ClientID != item.ClientID {
					return nil, fmt.Errorf("ambiguous Janua client identity; pin the reviewed client_id")
				}
				candidate := item
				match = &candidate
			}
		}
		if len(page.Clients) == 0 || pageNum*100 >= page.Total {
			if match == nil && spec.ClientID != "" {
				return nil, fmt.Errorf("pinned Janua client was not found; refusing duplicate creation")
			}
			return match, nil
		}
	}
	return nil, fmt.Errorf("Janua client inventory exceeded page limit; identity could not be established")
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	sortStrings(aa)
	sortStrings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func validateMachineClient(remote remoteOAuthClient, spec JanuaClientSpec) error {
	if !remote.IsActive || remote.ClientID == "" || remote.ID == "" || remote.Name != spec.Name || remote.OrganizationID == nil || *remote.OrganizationID != spec.OrganizationID ||
		remote.Audience == nil || *remote.Audience != spec.Audience ||
		remote.IsConfidential != spec.confidential() ||
		!sameStrings(remote.AllowedScopes, spec.AllowedScopes) ||
		!sameStrings(remote.GrantTypes, spec.GrantTypes) || len(remote.RedirectURIs) != 0 {
		return fmt.Errorf("existing Janua machine client differs from reviewed organization, audience, scope or grant; refusing credential reuse or rotation")
	}
	if spec.ClientID != "" && remote.ClientID != spec.ClientID {
		return fmt.Errorf("Janua client differs from pinned identity")
	}
	return nil
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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
