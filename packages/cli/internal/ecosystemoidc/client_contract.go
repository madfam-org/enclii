package ecosystemoidc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

type clientKind uint8

const (
	humanClient clientKind = iota + 1
	machineClient
)

var scopeShape = regexp.MustCompile(`^[a-z][a-z0-9._-]*:[a-z][a-z0-9_-]*$`)
var pinnedClientShape = regexp.MustCompile(`^jnc_[A-Za-z0-9_-]{8,56}$`)
var returnedClientShape = regexp.MustCompile(`^jnc_[A-Za-z0-9_-]+$`)

func boundedText(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}
func canonicalOrganization(s string) bool {
	id, err := uuid.Parse(s)
	return err == nil && id.String() == s && id != uuid.Nil
}
func distinct(values []string, maxItems, maxBytes int) bool {
	if len(values) > maxItems {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, s := range values {
		if !boundedText(s, maxBytes) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func (s JanuaClientSpec) kind() (clientKind, error) {
	if !distinct(s.GrantTypes, 2, 64) {
		return 0, fmt.Errorf("invalid or duplicate OAuth grants")
	}
	if sameStrings(s.GrantTypes, []string{"client_credentials"}) {
		return machineClient, nil
	}
	if sameStrings(s.GrantTypes, []string{"authorization_code"}) || sameStrings(s.GrantTypes, []string{"authorization_code", "refresh_token"}) {
		return humanClient, nil
	}
	return 0, fmt.Errorf("OAuth grants must declare an ordinary code client or a machine client, never both")
}
func validateClientSpec(s JanuaClientSpec) error {
	kind, err := s.kind()
	if err != nil {
		return err
	}
	if !boundedText(s.Name, 255) || strings.ContainsAny(s.Name, "/\\") || !boundedText(s.Audience, 255) || (s.ClientKey != "" && !boundedText(s.ClientKey, 255)) {
		return fmt.Errorf("invalid OAuth client identity or audience")
	}
	if len(s.Description) > 1000 || len(s.WebsiteURL) > 500 || !utf8.ValidString(s.Description) || !utf8.ValidString(s.WebsiteURL) {
		return fmt.Errorf("client metadata exceeds supported schema")
	}
	if s.ClientID != "" && !pinnedClientShape.MatchString(s.ClientID) {
		return fmt.Errorf("invalid pinned Janua client identity")
	}
	if s.OrganizationID != "" && !canonicalOrganization(s.OrganizationID) {
		return fmt.Errorf("organization must be a canonical nonzero UUID")
	}
	if !distinct(s.RedirectURIs, 64, 2048) || !distinct(s.AllowedScopes, 128, 128) || len(s.AllowedScopes) == 0 {
		return fmt.Errorf("invalid or duplicate callback/scopes")
	}
	for _, scope := range s.AllowedScopes {
		switch scope {
		case "openid", "profile", "email", "offline_access", "api", "admin":
		default:
			if !scopeShape.MatchString(scope) {
				return fmt.Errorf("invalid OAuth scope shape")
			}
		}
	}
	if kind == machineClient {
		if !s.confidential() || len(s.RedirectURIs) != 0 {
			return fmt.Errorf("machine clients must be confidential with no callbacks")
		}
	} else {
		if len(s.RedirectURIs) == 0 {
			return fmt.Errorf("ordinary code clients require callbacks")
		}
		for _, callback := range s.RedirectURIs {
			u, err := url.Parse(callback)
			if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
				return fmt.Errorf("invalid OAuth callback")
			}
		}
	}
	return nil
}
func validatePlatform(p Platform) error {
	if err := validateClientSpec(p.JanuaClient); err != nil {
		return err
	}
	if !p.publicLogin() && !boundedText(p.IntakeTarget, 255) {
		return fmt.Errorf("confidential client requires an intake target")
	}
	if p.IntakeTarget != "" && !boundedText(p.IntakeTarget, 255) {
		return fmt.Errorf("invalid intake target")
	}
	for key, source := range p.IntakeKeyMap {
		if !boundedText(key, 255) || !boundedText(source, 2048) {
			return fmt.Errorf("invalid intake key map")
		}
	}
	if len(p.IntakeKeyMap) > 0 {
		if _, err := canonicalIntakeKeyNames(mapKeys(p.IntakeKeyMap)); err != nil {
			return err
		}
	}

	return nil
}

// WithOrganization changes ONLY tenant scope in an embedded reviewed profile.
// The CLI rejects mutable registry/all combinations before calling this method.
// A UUID declaration is not proof of membership or permission to create it.
func (r *Registry) WithOrganization(platformID, organization string) (*Registry, error) {
	if !canonicalOrganization(organization) {
		return nil, fmt.Errorf("--organization-id must be a canonical nonzero UUID")
	}
	p, ok := r.Platforms[platformID]
	if !ok {
		return nil, fmt.Errorf("unknown platform")
	}
	kind, err := p.JanuaClient.kind()
	if err != nil || kind != humanClient {
		return nil, fmt.Errorf("organization override requires an ordinary reviewed code profile")
	}
	if p.JanuaClient.OrganizationID != "" && p.JanuaClient.OrganizationID != organization {
		return nil, fmt.Errorf("reviewed profile already belongs to a different organization")
	}
	p.JanuaClient.OrganizationID = organization
	if err := validatePlatform(p); err != nil {
		return nil, err
	}
	result := &Registry{Issuer: r.Issuer, Platforms: make(map[string]Platform, len(r.Platforms))}
	for key, value := range r.Platforms {
		result.Platforms[key] = value
	}
	result.Platforms[platformID] = p
	return result, nil
}

func validateRemoteIdentity(r remoteOAuthClient, s JanuaClientSpec) error {
	id, err := uuid.Parse(r.ID)
	if err != nil || id == uuid.Nil || id.String() != r.ID || !returnedClientShape.MatchString(r.ClientID) || len(r.ClientID) > 64 || !r.IsActive || r.Name != s.Name || !r.organizationPresent || r.IsConfidential != s.confidential() {
		return fmt.Errorf("janua client identity, active state or confidentiality differs from declaration")
	}
	if s.ClientID != "" && r.ClientID != s.ClientID {
		return fmt.Errorf("janua client differs from pinned identity")
	}
	if (s.OrganizationID == "" && r.OrganizationID != nil) || (s.OrganizationID != "" && (r.OrganizationID == nil || *r.OrganizationID != s.OrganizationID)) {
		return fmt.Errorf("janua client organization differs; refusing adoption or rebinding")
	}
	actualKind, err := (JanuaClientSpec{GrantTypes: r.GrantTypes}).kind()
	expectedKind, _ := s.kind()
	if err != nil || actualKind != expectedKind {
		return fmt.Errorf("janua client grant kind differs; refusing human/machine conversion")
	}
	return nil
}
func validateRemoteClient(r remoteOAuthClient, s JanuaClientSpec) error {
	if !s.confidential() && r.ClientSecret != nil {
		return fmt.Errorf("public client returned an unexpected credential")
	}
	if err := validateRemoteIdentity(r, s); err != nil {
		return err
	}
	if r.Audience == nil || *r.Audience != s.Audience || !sameStrings(r.RedirectURIs, s.RedirectURIs) || !sameStrings(r.AllowedScopes, s.AllowedScopes) || !sameStrings(r.GrantTypes, s.GrantTypes) {
		return fmt.Errorf("janua returned callback, audience, scope or grants differ from declaration")
	}
	return nil
}

// Native admin lists omit organization_id; only detail responses attest it.
func (r *remoteOAuthClient) UnmarshalJSON(data []byte) error {
	type plain remoteOAuthClient
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return fmt.Errorf("invalid Janua client response")
	}
	for _, key := range []string{"id", "client_id", "name", "audience", "redirect_uris", "allowed_scopes", "grant_types", "is_confidential", "is_active"} {
		value, ok := keys[key]
		if !ok || (key != "audience" && bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
			return fmt.Errorf("partial Janua client response")
		}
	}
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("invalid Janua client response types")
	}
	*r = remoteOAuthClient(value)
	_, r.organizationPresent = keys["organization_id"]
	return nil
}

// Bounded transport calls this before typed decoding. Reject duplicate keys,
// invalid UTF-8 and trailing JSON, without returning response content.
func decodeProviderJSON(data []byte, out interface{}) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid provider JSON")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return fmt.Errorf("provider JSON is too deep")
		}
		token, err := d.Token()
		if err != nil {
			return fmt.Errorf("invalid provider JSON")
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return fmt.Errorf("invalid provider JSON")
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate provider JSON field")
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid provider JSON")
		}
		if _, err = d.Token(); err != nil {
			return fmt.Errorf("invalid provider JSON")
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if d.More() {
		return fmt.Errorf("trailing provider JSON")
	}
	if _, err := d.Token(); err == nil {
		return fmt.Errorf("trailing provider JSON")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("invalid provider response types")
	}
	return nil
}
