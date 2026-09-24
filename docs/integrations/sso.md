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

### Web Console Sign-in (account switching)

The Enclii web consoles — **admin.enclii.dev** (admin/DISPATCH) and
**app.enclii.dev** (Switchyard UI) — sign operators in through the same OIDC
PKCE flow, and both offer explicit account-switching controls on their login
pages. The controls differ only by the OIDC `prompt` parameter appended to
Janua's `authorize` URL:

| Control | `prompt` value | Effect |
|---------|----------------|--------|
| **Sign in with Janua SSO** | *(omitted)* | Default. Janua may silently reuse an existing SSO session. |
| **Switch account** | `select_account` | Janua shows its account chooser — for an operator holding more than one MADFAM account. |
| **Sign in as someone else** | `login` | Forces fresh re-authentication, ignoring any existing SSO session. |

`prompt` is appended **only when supplied**, so the default sign-in keeps the
silent-session-reuse behavior. Signing out uses Janua's RP-Initiated Logout
(`end_session`) so the shared SSO session is actually ended, not just the local
cookies cleared — see [Logout](#logout).

> **Ecosystem directive:** every MADFAM platform adopts this «Switch account /
> Sign in as someone else» sign-in model, **except Crea Tu Mundo MAP**.

### CLI sign-in (account switching)

The `enclii` CLI offers the same two choices from `v1.0.0-alpha.10`:
`enclii login --prompt select_account` and `enclii login --prompt login`. It can
also hold several identities at once, because each `--profile` keeps its own
login. An operator can keep an everyday account as the default profile and an
administrator account in `--profile admin`, logging that one in with
`--no-browser` in a private window so the browser's session is left as it is.
See [`enclii login`](../cli/commands/login.md#several-identities-profiles-and-account-switching).

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

The CLI is pre-configured for Janua SSO and needs no configuration file:

| Setting | Default | Override |
|---------|---------|----------|
| Issuer | `https://auth.madfam.io` | `--issuer` or `ENCLII_OIDC_ISSUER` |
| OAuth client | the built-in public (PKCE) Enclii CLI client | `enclii login --client-id`; token refresh reads `ENCLII_OIDC_CLIENT_ID` |
| Redirect URI | `http://127.0.0.1:8080/callback` (port 3000 if 8080 is busy) | none |
| Credentials | `~/.enclii/credentials.json`, or `~/.enclii/profiles/<name>/credentials.json` for `--profile <name>` | `--profile` or `ENCLII_PROFILE` |

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

Access tokens are short-lived. On each invocation the CLI refreshes the active
profile's access token when it is within 60 seconds of expiry and a refresh
token is stored, then writes the new token back to the same profile's file
(`packages/cli/internal/config/config.go`). A failed refresh is not fatal: the
CLI keeps the old token, the API answers 401 once it has expired, and
`enclii login` (with the same `--profile`) starts over.

### Logout

The web consoles sign out with Janua's RP-Initiated Logout (`end_session`), so
the shared SSO session ends as well as the console's own cookies.

The CLI's logout is local only:

```bash
enclii logout                   # deletes ~/.enclii/credentials.json
enclii --profile admin logout   # deletes only the "admin" profile's credentials
```

It does not revoke the tokens server-side and does not end the Janua browser
session; sign out in the browser, or revoke sessions as below, for that.

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

MFA is set up in Janua, not in the Enclii CLI: sign in at `auth.madfam.io` and
use the account's security settings.

### TOTP (Time-based One-Time Password)

Enable TOTP in Janua's security settings and scan the QR code with an
authenticator app.

### WebAuthn/Passkeys

Register a passkey in Janua's security settings and follow the browser prompts.

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
# Environment variable (legacy ENCLII_TOKEN is also accepted)
export ENCLII_API_TOKEN="enclii_abc123xyz..."

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

The CLI has no configuration file for this; point it at the provider with the
environment and a client ID:

```bash
export ENCLII_OIDC_ISSUER=https://your-idp.example.com
export ENCLII_OIDC_CLIENT_ID=<cli-client-id>   # used for token refresh
enclii login --client-id <cli-client-id>
```

Register the client as public (PKCE) with the redirect URIs
`http://127.0.0.1:8080/callback` and `http://127.0.0.1:3000/callback`; the CLI
listens on 8080, or on 3000 when 8080 is busy.

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

1. Clear the active profile's local tokens and log in again:
   ```bash
   enclii logout
   enclii login
   ```

2. Check browser cookies for auth.madfam.io

### API Returns 401

1. Check which identity and expiry the active profile holds (stderr):
   ```bash
   enclii whoami 2>&1
   ```

2. Check that an `ENCLII_API_TOKEN` (or `--api-token`) is not overriding the
   login: an explicit token always wins over stored credentials.

3. The CLI refreshes the access token by itself near expiry. If the refresh
   token was revoked, log in again:
   ```bash
   enclii login
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
