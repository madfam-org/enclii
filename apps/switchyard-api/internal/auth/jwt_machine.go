package auth

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Janua mints two shapes of access token:
//
//   - HUMAN (authorization_code / refresh): a UUID `sub`, `email`, roles; no
//     `token_use`, no `actor_type`.
//   - MACHINE (client_credentials): `token_use: "client_credentials"`,
//     `actor_type: "service_account"`, `client_id` and a space-delimited
//     `scope`. A client bound to an organization also carries `tenant_id` and
//     `org_id` (both the organization id) and `org_slug`.
//
// The session middleware (AuthMiddleware) serves people and links every
// external token to a local user. Machine tokens never take that path: the
// native MCP (internal/mcp) reads them with ValidateExternalMachineClaims.
const (
	// TokenUseClientCredentials is the `token_use` value on machine tokens.
	TokenUseClientCredentials = "client_credentials"
	// ActorTypeServiceAccount is the `actor_type` value on machine tokens.
	ActorTypeServiceAccount = "service_account"
	// ServiceAccountSubjectPrefix prefixes the `sub` of machine tokens.
	ServiceAccountSubjectPrefix = "service-account:"
)

// ScopeClaim is the OAuth `scope` claim. Janua emits a space-delimited string;
// the JSON-array form some issuers use is accepted too.
type ScopeClaim []string

// UnmarshalJSON accepts a space-delimited string, an array of strings or null.
func (s *ScopeClaim) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" || trimmed == "" {
		*s = nil
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("scope claim: %w", err)
		}
		out := make([]string, 0, len(list))
		for _, v := range list {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		*s = out
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("scope claim: %w", err)
	}
	*s = strings.Fields(str)
	return nil
}

// Has reports whether the claim holds exactly want (no wildcard, no prefix).
func (s ScopeClaim) Has(want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// MachineClaims are the claims a Janua machine token carries, beside the
// registered ones.
type MachineClaims struct {
	ClientID  string     `json:"client_id,omitempty"`
	Scope     ScopeClaim `json:"scope,omitempty"`
	TokenUse  string     `json:"token_use,omitempty"`
	ActorType string     `json:"actor_type,omitempty"`
	TenantID  string     `json:"tenant_id,omitempty"`
	OrgID     string     `json:"org_id,omitempty"`
	jwt.RegisteredClaims
}

// IsMachine reports whether the claims came from a client_credentials grant.
// Any one machine marker is enough, so a partial claim set is treated as a
// machine (and must then pass the scope gate) rather than as a person.
func (c *MachineClaims) IsMachine() bool {
	if c == nil {
		return false
	}
	return c.TokenUse == TokenUseClientCredentials ||
		c.ActorType == ActorTypeServiceAccount ||
		strings.HasPrefix(c.Subject, ServiceAccountSubjectPrefix)
}

// ServiceClientID is the machine client's id: the `client_id` claim, else
// the `sub` without its service-account prefix.
func (c *MachineClaims) ServiceClientID() string {
	if c == nil {
		return ""
	}
	if c.ClientID != "" {
		return c.ClientID
	}
	return strings.TrimPrefix(c.Subject, ServiceAccountSubjectPrefix)
}

// Organization is the Janua organization the client is bound to (`tenant_id`,
// else `org_id`), or "" for a client bound to none.
func (c *MachineClaims) Organization() string {
	if c == nil {
		return ""
	}
	if c.TenantID != "" {
		return c.TenantID
	}
	return c.OrgID
}

// ValidateExternalMachineClaims verifies a Janua bearer exactly as
// ValidateExternalToken does (JWKS signature, expiry, issuer, the API's
// audience ENCLII_JANUA_AUDIENCE) and returns its machine-token claims. It
// refuses to run without a configured issuer and audience, so a machine edge
// never accepts "any token Janua signed". Whether the token IS a machine
// token is the caller's check (IsMachine), so it can answer a person's token
// with 403 rather than 401.
func (j *JWTManager) ValidateExternalMachineClaims(tokenString string) (*MachineClaims, error) {
	if j.externalIssuer == "" || j.externalAudience == "" {
		return nil, fmt.Errorf("machine tokens need a configured external issuer and audience")
	}
	claims := &MachineClaims{}
	if err := j.verifyExternal(tokenString, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// ValidateExternalMachineClaims delegates to the embedded JWT manager, which
// holds the external JWKS in OIDC mode.
func (o *OIDCManager) ValidateExternalMachineClaims(tokenString string) (*MachineClaims, error) {
	if o.jwtManager == nil {
		return nil, fmt.Errorf("external JWKS validation not configured")
	}
	return o.jwtManager.ValidateExternalMachineClaims(tokenString)
}

// ExternalAudience is the audience external tokens must carry.
func (j *JWTManager) ExternalAudience() string { return j.externalAudience }

// ExternalAudience is the audience external tokens must carry.
func (o *OIDCManager) ExternalAudience() string {
	if o.jwtManager == nil {
		return ""
	}
	return o.jwtManager.ExternalAudience()
}
