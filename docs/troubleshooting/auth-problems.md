---
title: Authentication Problems
description: Troubleshoot login, tokens, SSO, and session issues
sidebar_position: 5
tags: [troubleshooting, authentication, oauth, sso, janua]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# Authentication Problems Troubleshooting

This guide helps resolve authentication and authorization issues with Enclii.

## Prerequisites

- [CLI installed](/cli/)
- Network access to auth.madfam.io and api.enclii.dev

## Quick Diagnosis

```bash
# Check current auth status
enclii whoami

# Verify token validity
enclii auth verify

# Check auth configuration
enclii auth status
```

## Common Authentication Errors

### "Invalid or Expired Token"

**Symptom**: API returns 401, CLI operations fail

**Causes**:
- Access token expired (29 days)
- Token revoked
- Wrong token type used

**Solutions**:

```bash
# Re-authenticate
enclii logout
enclii login

# Verify new token works
enclii whoami
```

For API clients:
```bash
# Refresh token if available
curl -X POST https://auth.madfam.io/api/v1/oauth/token \
  -d "grant_type=refresh_token" \
  -d "refresh_token=$REFRESH_TOKEN" \
  -d "client_id=enclii-cli"
```

### "Unknown client_id"

**Symptom**: OAuth flow fails with "invalid_client"

**Causes**:
- OAuth client not registered in Janua
- Wrong client_id configured
- Client deleted or disabled

**Solutions**:

1. **Use default client ID**:
```bash
enclii login  # Uses default enclii-cli
```

2. **Register OAuth client** (admin required):
   - See [CLI Auth Setup](/guides/cli-auth-setup)

3. **Verify client exists**:
```bash
# Check Janua admin panel
# https://admin.madfam.io → OAuth Clients
```

### "Redirect URI Mismatch"

**Symptom**: OAuth callback fails with redirect error

**Causes**:
- Registered redirect URI doesn't match actual
- Port changed during callback
- Protocol mismatch (http vs https)

**Solutions**:

The CLI uses `http://127.0.0.1:<port>/callback` with dynamic port.

1. **Verify registered URIs** include:
   - `http://127.0.0.1/callback` (for CLI)
   - `https://app.enclii.dev/auth/callback` (for web)

2. **Update OAuth client** if needed:
```bash
# Via Janua API
curl -X PATCH https://auth.madfam.io/api/v1/oauth/clients/<client-id> \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"redirect_uris": ["http://127.0.0.1/callback"]}'
```

### Browser Doesn't Open

**Symptom**: `enclii login` doesn't open browser

**Causes**:
- No default browser configured
- SSH session without display
- WSL environment

**Solutions**:

1. **Manual login** with device flow:
```bash
# Copy the URL and open manually
enclii login --no-browser
# Then open the displayed URL in any browser
```

2. **Set browser explicitly** (macOS/Linux):
```bash
export BROWSER=/usr/bin/firefox
enclii login
```

### SSO Login Fails

**Symptom**: Redirected to Janua but login fails

**Causes**:
- Invalid Janua credentials
- Account disabled
- MFA required but not configured

**Solutions**:

1. **Test Janua login directly**:
```bash
curl -X POST https://auth.madfam.io/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "you@example.com", "password": "yourpassword"}'
```

2. **Check account status** in Janua admin

3. **Reset password** via Janua if needed

### "Permission Denied" (403 Forbidden)

**Symptom**: Authenticated but can't access resource

**Causes**:
- Insufficient role permissions
- Resource in different organization
- API key scope too narrow

**Solutions**:

```bash
# Check your role
enclii whoami

# List your organizations
enclii orgs list

# Request elevated permissions from org admin
```

**Role hierarchy**:
- `viewer` - Read-only access
- `developer` - Deploy and manage services
- `admin` - Full project management
- `owner` - Organization owner

### Token Stored But Commands Fail

**Symptom**: Credentials exist but API calls fail

**Causes**:
- Token file corrupted
- Wrong OIDC issuer configured
- Network issues reaching auth server

**Solutions**:

```bash
# Clear stored credentials
rm ~/.enclii/credentials.json
enclii login

# Or specify credentials location
export ENCLII_CREDENTIALS_PATH=~/.enclii/credentials.json
enclii login
```

### JWKS Validation Errors

**Symptom**: API returns "JWT signature validation failed"

**Causes**:
- JWKS endpoint unreachable
- Key rotation in progress
- Clock skew between servers

**Solutions**:

1. **Verify JWKS endpoint**:
```bash
curl https://auth.madfam.io/.well-known/jwks.json
```

2. **Check server clock** (admin):
```bash
kubectl exec -n enclii deploy/switchyard-api -- date
```

3. **Wait and retry** if key rotation is happening

### API Key Issues

**Symptom**: API key authentication fails

**Causes**:
- API key expired
- Key revoked
- Wrong scope

**Solutions**:

```bash
# List your API keys
enclii api-keys list

# Create new API key
enclii api-keys create --name "CI/CD" --scopes "deploy,read"

# Use API key
curl -H "X-API-Key: ek_live_xxxxx" https://api.enclii.dev/v1/users/me
```

## Session Management

### View Active Sessions

```bash
# List all active sessions
enclii sessions list
```

### Revoke Sessions

```bash
# Revoke specific session
enclii sessions revoke <session-id>

# Revoke all sessions (log out everywhere)
enclii sessions revoke-all
```

### SSO Logout

```bash
# Log out from CLI and SSO provider
enclii logout --sso

# This terminates:
# - Local CLI session
# - Janua SSO session
# - All linked sessions
```

## Configuration

### Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `ENCLII_CLIENT_ID` | OAuth client ID | `enclii-cli` |
| `ENCLII_OIDC_ISSUER` | SSO provider URL | `https://auth.madfam.io` |
| `ENCLII_API_URL` | API endpoint | `https://api.enclii.dev` |
| `ENCLII_CREDENTIALS_PATH` | Credentials file | `~/.enclii/credentials.json` |

### Credentials File

Located at `~/.enclii/credentials.json`:
```json
{
  "access_token": "eyJ...",
  "refresh_token": "eyJ...",
  "token_type": "Bearer",
  "expires_at": "2026-02-24T00:00:00Z"
}
```

**Security**: File should have `600` permissions:
```bash
chmod 600 ~/.enclii/credentials.json
```

## Testing Authentication

### Verify Token Manually

```bash
# Decode JWT (doesn't verify signature)
echo $TOKEN | cut -d. -f2 | base64 -d | jq

# Verify with API
curl -H "Authorization: Bearer $TOKEN" \
  https://api.enclii.dev/v1/users/me
```

### Test OAuth Flow

```bash
# Start auth flow manually
open "https://auth.madfam.io/oauth/authorize?\
client_id=enclii-cli&\
response_type=code&\
redirect_uri=http://127.0.0.1:8080/callback&\
scope=openid%20profile%20email"
```

## Related Documentation

- **CLI Auth Setup**: [Authentication Setup Guide](/guides/cli-auth-setup)
- **SSO Deployment**: [SSO Deployment Instructions](/guides/sso-deployment)
- **SSO Integration**: [SSO Integration](/integrations/sso)
- **API Errors**: [API Error Reference](./api-errors)
