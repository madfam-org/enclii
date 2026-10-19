package ecosystemoidc

import (
	"context"
	"fmt"
	"strings"
)

// ClientMetadata is an allowlist of public OAuth registration facts. It cannot
// carry a credential, and inspection never resolves, rotates or intakes one.
type ClientMetadata struct {
	ID             string   `json:"id"`
	ClientID       string   `json:"client_id"`
	Name           string   `json:"name"`
	Audience       *string  `json:"audience"`
	OrganizationID *string  `json:"organization_id"`
	RedirectURIs   []string `json:"redirect_uris"`
	AllowedScopes  []string `json:"allowed_scopes"`
	GrantTypes     []string `json:"grant_types"`
	IsConfidential bool     `json:"is_confidential"`
	IsActive       bool     `json:"is_active"`
}

// InspectClient uses only the managed admin session's GET inventory/detail
// paths. Never fall back to internal credentials, registration or reconciliation.
func (c *JanuaClient) InspectClient(ctx context.Context, spec JanuaClientSpec) (ClientMetadata, error) {
	if err := validateClientSpec(spec); err != nil {
		return ClientMetadata{}, fmt.Errorf("invalid metadata inspection profile")
	}
	if strings.TrimSpace(c.AdminToken) == "" {
		return ClientMetadata{}, fmt.Errorf("metadata inspection requires a managed Janua admin session")
	}
	reader := *c
	reader.InternalAPIKey = ""
	reader.metadataOnly = true
	actual, err := reader.findExisting(ctx, spec)
	if err != nil {
		return ClientMetadata{}, fmt.Errorf("Janua metadata inspection unavailable; no changes made")
	}
	if actual == nil {
		return ClientMetadata{}, fmt.Errorf("Janua metadata client not found; no changes made")
	}
	if actual.Name != spec.Name || !canonicalOrganization(actual.ID) || !returnedClientShape.MatchString(actual.ClientID) {
		return ClientMetadata{}, fmt.Errorf("Janua metadata identity refused; no changes made")
	}
	return ClientMetadata{ID: actual.ID, ClientID: actual.ClientID, Name: actual.Name, Audience: actual.Audience, OrganizationID: actual.OrganizationID, RedirectURIs: actual.RedirectURIs, AllowedScopes: actual.AllowedScopes, GrantTypes: actual.GrantTypes, IsConfidential: actual.IsConfidential, IsActive: actual.IsActive}, nil
}

// Reject even unexpected credential-bearing fields before decoding the DTO.
// Null fields in ordinary provider DTOs carry no credential and are allowed.
func containsCredential(value interface{}) bool {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, nested := range v {
			normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
			if nested != nil && (strings.Contains(normalized, "secret") || strings.Contains(normalized, "password") || normalized == "accesstoken" || normalized == "refreshtoken" || normalized == "idtoken" || normalized == "internalapikey" || normalized == "apikey" || normalized == "token" || normalized == "credential" || normalized == "credentials" || normalized == "authorization") {
				return true
			}
			if containsCredential(nested) {
				return true
			}
		}
	case []interface{}:
		for _, nested := range v {
			if containsCredential(nested) {
				return true
			}
		}
	}
	return false
}
