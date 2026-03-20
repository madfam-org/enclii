---
title: SSO/OIDC Integration
description: Enterprise authentication with Janua SSO using OAuth 2.0 and OpenID Connect
sidebar_position: 2
tags: [integrations, sso, oidc, oauth, authentication, janua]
---

# SSO/OIDC Integration

Enclii integrates with Janua SSO for enterprise-grade authentication using OAuth 2.0 and OpenID Connect (OIDC).

## Overview

Enclii uses [Janua](https://github.com/madfam-org/janua) as its identity provider:
- **Protocol**: OAuth 2.0 / OpenID Connect
- **Algorithm**: RS256 (RSA-2048 asymmetric keys)
- **Provider**: auth.madfam.io
- **Features**: MFA, passkeys, device verification, session management

---

## Authentication Flow

### CLI Authentication (PKCE)

```
┌─────────┐                    ┌─────────┐                    ┌─────────┐
│   CLI   │                    │  Janua  │                    │ Enclii  │
│ Client  │                    │   SSO   │                    │   API   │
└────┬────┘                    └────┬────┘                    └────┬────┘
     │                              │                              │
     │  1. Generate PKCE verifier   │                              │
     │  2. Open browser with authz  │                              │
     │─────────────────────────────►│                              │
     │                              │                              │
     │                              │  3. User authenticates       │
     │                              │     (password, OAuth, etc)   │
     │                              │                              │
     │  4. Redirect with auth code  │                              │
     │◄─────────────────────────────│                              │
     │                              │                              │
     │  5. Exchange code for tokens │                              │
     │─────────────────────────────►│                              │
     │                              │                              │
     │  6. Access + Refresh tokens  │                              │
     │◄─────────────────────────────│                              │
     │                              │                              │
     │  7. API requests with token  │                              │
     │─────────────────────────────────────────────────────────────►│
     │                              │                              │
     │                              │  8. Validate JWT (JWKS)      │
     │                              │◄─────────────────────────────│
     │                              │                              │
     │  9. API response             │                              │
     │◄─────────────────────────────────────────────────────────────│
```

### Token Format

Access tokens are RS256-signed JWTs:

```json
{
  "header": {
    "alg": "RS256",
    "typ": "JWT",
    "kid": "key-id-123"
  },
  "payload": {
    "sub": "usr_abc123",
    "email": "developer@example.com",
    "name": "Developer",
    "iss": "https://auth.madfam.io",
    "aud": "enclii",
    "iat": 1704067200,
    "exp": 1704068100,
    "roles": ["developer"],
    "teams": ["acme-corp"]
  }
}
```

---

## Configuration

### Enclii API Configuration

The Enclii API validates tokens against Janua's JWKS endpoint:

```yaml
# Environment variables
ENCLII_OIDC_ISSUER: https://auth.madfam.io
ENCLII_OIDC_AUDIENCE: enclii
ENCLII_OIDC_JWKS_URL: https://auth.madfam.io/.well-known/jwks.json
```

### CLI Configuration

The CLI is pre-configured for Janua SSO:

```yaml
# ~/.enclii/config.yaml (managed automatically)
auth:
  issuer: https://auth.madfam.io
  client_id: enclii-cli
  redirect_uri: http://localhost:9999/callback
```

---

## Token Validation

### JWKS (JSON Web Key Set)

Enclii validates tokens using Janua's public keys:

```
GET https://auth.madfam.io/.well-known/jwks.json

{
  "keys": [
    {
      "kty": "RSA",
      "kid": "key-id-123",
      "use": "sig",
      "alg": "RS256",
      "n": "...",
      "e": "AQAB"
    }
  ]
}
```

### Validation Steps

1. **Signature Verification**: Verify JWT signature with JWKS public key
2. **Issuer Check**: `iss` must be `https://auth.madfam.io`
3. **Audience Check**: `aud` must include `enclii`
4. **Expiration Check**: `exp` must be in the future
5. **Not Before Check**: `nbf` (if present) must be in the past

### Go Validation Example

```go
import (
    "github.com/golang-jwt/jwt/v5"
    "github.com/MicahParks/keyfunc/v2"
)

// Initialize JWKS
jwks, err := keyfunc.Get("https://auth.madfam.io/.well-known/jwks.json", keyfunc.Options{
    RefreshInterval: time.Hour,
})

// Validate token
token, err := jwt.Parse(tokenString, jwks.Keyfunc,
    jwt.WithIssuer("https://auth.madfam.io"),
    jwt.WithAudience("enclii"),
    jwt.WithValidMethods([]string{"RS256"}),
)

if err != nil {
    return fmt.Errorf("invalid token: %w", err)
}

claims := token.Claims.(jwt.MapClaims)
userID := claims["sub"].(string)
email := claims["email"].(string)
```

---

## Role-Based Access Control (RBAC)

### Roles

| Role | Permissions |
|------|-------------|
| `admin` | Full access to all resources |
| `developer` | Deploy, manage services, view logs |
| `viewer` | Read-only access to dashboards and logs |

### Token Claims

Roles are included in the JWT:

```json
{
  "sub": "usr_abc123",
  "roles": ["developer"],
  "teams": ["acme-corp"],
  "permissions": [
    "services:read",
    "services:deploy",
    "logs:read"
  ]
}
```

### API Authorization

```go
// Check permission
func requirePermission(permission string) gin.HandlerFunc {
    return func(c *gin.Context) {
        claims := getClaims(c)

        if !claims.HasPermission(permission) {
            c.JSON(403, gin.H{"error": "forbidden"})
            c.Abort()
            return
        }

        c.Next()
    }
}

// Usage
router.POST("/api/v1/deployments",
    requirePermission("services:deploy"),
    createDeployment,
)
```

---

## Session Management

### Token Refresh

Access tokens expire after 15 minutes. The CLI automatically refreshes:

```go
// Check if token needs refresh
if time.Now().After(token.ExpiresAt.Add(-5 * time.Minute)) {
    newToken, err := refreshToken(refreshToken)
    if err != nil {
        // Prompt re-login
        return login()
    }
    saveToken(newToken)
}
```

### Logout

Logging out terminates both local and SSO sessions:

```bash
enclii logout
```

This:
1. Clears local tokens from `~/.enclii/config.yaml`
2. Initiates RP-Initiated Logout with Janua
3. Janua terminates the SSO session

### Session Revocation

Revoke all sessions for security:

```bash
# Via Janua dashboard
# or
curl -X POST https://auth.madfam.io/api/v1/sessions/revoke-all \
  -H "Authorization: Bearer $TOKEN"
```

---

## Multi-Factor Authentication

Janua supports multiple MFA methods:

### TOTP (Time-based One-Time Password)

```bash
# Enable TOTP
enclii auth mfa enable totp
# Scan QR code with authenticator app
```

### WebAuthn/Passkeys

```bash
# Register a passkey
enclii auth passkey register
# Follow browser prompts
```

### Device Verification

New devices require verification:

1. Login from new device
2. Janua sends verification email
3. Click verification link
4. Device is trusted for 30 days

---

## API Token Authentication

For CI/CD and programmatic access, use API tokens instead of OIDC:

### Create API Token

```bash
enclii tokens create --name "ci-deploy" --scopes "deploy,read"
```

**Output:**
```
Token created successfully!

Token:   enclii_abc123xyz...  (copy this - shown only once!)
Name:    ci-deploy
Scopes:  deploy, read
Expires: Never
```

### Use API Token

```bash
# Environment variable
export ENCLII_TOKEN="enclii_abc123xyz..."

# Or header
curl -H "Authorization: Bearer enclii_abc123xyz..." \
  https://api.enclii.dev/api/v1/projects
```

### Token Scopes

| Scope | Permissions |
|-------|-------------|
| `read` | Read projects, services, deployments |
| `deploy` | Create deployments, trigger builds |
| `admin` | Full administrative access |
| `logs` | Stream and fetch logs |
| `secrets` | Manage environment variables |

---

## Custom OIDC Provider

For enterprise installations, configure a custom OIDC provider:

### Requirements

Your OIDC provider must support:
- Authorization Code flow with PKCE
- RS256 token signing
- JWKS endpoint for public keys
- Standard OIDC claims (`sub`, `email`, `name`)

### Configuration

```yaml
# Enclii API environment
ENCLII_OIDC_ISSUER: https://your-idp.example.com
ENCLII_OIDC_AUDIENCE: enclii
ENCLII_OIDC_JWKS_URL: https://your-idp.example.com/.well-known/jwks.json
ENCLII_OIDC_CLIENT_ID: enclii-api
ENCLII_OIDC_CLIENT_SECRET: secret  # For backend-to-backend if needed
```

### CLI Configuration

```yaml
# ~/.enclii/config.yaml
auth:
  issuer: https://your-idp.example.com
  client_id: enclii-cli
  redirect_uri: http://localhost:9999/callback
  scopes: ["openid", "profile", "email"]
```

---

## Troubleshooting

### Token Validation Fails

1. **Check issuer**: Ensure `iss` claim matches configuration
2. **Check audience**: Ensure `aud` includes `enclii`
3. **Check expiration**: Token may be expired
4. **Check JWKS**: Verify JWKS endpoint is accessible

```bash
# Test JWKS endpoint
curl https://auth.madfam.io/.well-known/jwks.json
```

### Login Loop

1. Clear local tokens:
   ```bash
   rm ~/.enclii/config.yaml
   enclii login
   ```

2. Check browser cookies for auth.madfam.io

### API Returns 401

1. Verify token is being sent:
   ```bash
   enclii whoami
   ```

2. Check token expiration:
   ```bash
   enclii tokens info
   ```

3. Refresh token:
   ```bash
   enclii auth refresh
   ```

---

## Security Best Practices

1. **Use API Tokens for CI/CD**: Don't embed user credentials in pipelines
2. **Rotate Tokens**: Regularly rotate long-lived API tokens
3. **Enable MFA**: Require MFA for production access
4. **Audit Logs**: Review authentication logs regularly
5. **Least Privilege**: Grant minimum required permissions
6. **Token Expiration**: Use short-lived tokens where possible

---

## Related Documentation

- **Getting Started**: [Quick Start Guide](/getting-started/QUICKSTART)
- **CLI**: [CLI Reference](/cli/) | [Login Command](/cli/commands/login)
- **Guides**: [CLI Auth Setup](/guides/cli-auth-setup) | [SSO Deployment](/guides/sso-deployment)
- **Troubleshooting**: [Auth Problems](/troubleshooting/auth-problems)
- **Security FAQ**: [Security Questions](/faq/security)
- **Other Integrations**: [GitHub Integration](/integrations/github)
- **External**: [Janua Documentation](https://docs.janua.dev)
