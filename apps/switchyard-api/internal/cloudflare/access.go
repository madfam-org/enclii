package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// AccessApplication represents a Cloudflare Access application.
type AccessApplication struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Domain   string         `json:"domain"`
	Type     string         `json:"type"` // "self_hosted"
	AUD      string         `json:"aud"`
	Policies []AccessPolicy `json:"policies,omitempty"`
}

// AccessPolicy represents a Cloudflare Access policy attached to an application.
type AccessPolicy struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Decision string `json:"decision"` // "allow", "deny"
}

// accessApplicationResponse wraps the CF API result field.
type accessApplicationResponse struct {
	Result AccessApplication `json:"result"`
}

// CreateAccessApplication creates a Cloudflare Access self-hosted application
// for the given domain. Returns the created application (with its ID and AUD).
func (c *Client) CreateAccessApplication(ctx context.Context, name, domain string) (*AccessApplication, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":                 name,
		"domain":               domain,
		"type":                 "self_hosted",
		"session_duration":     "24h",
		"auto_redirect_to_idp": false,
	})

	path := fmt.Sprintf("/accounts/%s/access/apps", c.accountID)
	var resp accessApplicationResponse
	if err := c.post(ctx, path, bytes.NewReader(payload), &resp); err != nil {
		return nil, fmt.Errorf("create access application: %w", err)
	}
	return &resp.Result, nil
}

// DeleteAccessApplication removes a Cloudflare Access application by ID.
func (c *Client) DeleteAccessApplication(ctx context.Context, appID string) error {
	path := fmt.Sprintf("/accounts/%s/access/apps/%s", c.accountID, appID)
	if err := c.httpDelete(ctx, path, nil); err != nil {
		return fmt.Errorf("delete access application: %w", err)
	}
	return nil
}

// CreateAccessPolicy attaches an "allow" policy (e.g. email domain rule) to an Access app.
func (c *Client) CreateAccessPolicy(ctx context.Context, appID, policyName string, emailDomains []string) (*AccessPolicy, error) {
	// Build "include" rules — one email_domain per entry
	include := make([]map[string]interface{}, 0, len(emailDomains))
	for _, d := range emailDomains {
		include = append(include, map[string]interface{}{
			"email_domain": map[string]string{"domain": d},
		})
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"name":     policyName,
		"decision": "allow",
		"include":  include,
	})

	path := fmt.Sprintf("/accounts/%s/access/apps/%s/policies", c.accountID, appID)
	var resp struct {
		Result AccessPolicy `json:"result"`
	}
	if err := c.post(ctx, path, bytes.NewReader(payload), &resp); err != nil {
		return nil, fmt.Errorf("create access policy: %w", err)
	}
	return &resp.Result, nil
}
