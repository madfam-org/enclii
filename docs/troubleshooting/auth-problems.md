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

- [CLI installed](/cli/). Profiles and `enclii login --prompt` need `v1.0.0-alpha.10` or later.
- Network access to auth.madfam.io and api.enclii.dev

## Quick Diagnosis

```bash
# Which identity is the active profile logged in as? (whoami prints on stderr)
enclii whoami 2>&1

# The same for a named profile
enclii --profile admin whoami 2>&1

# Which CLI build is this?
enclii version 2>&1
```

`whoami` reads the stored token and does not call the API. To check that the
token works against the API, run any read command, for example
`enclii projects list`.

## Common Authentication Errors

### "Invalid or Expired Token"

**Symptom**: API returns 401, CLI operations fail

**Causes**:
- The access token expired and could not be refreshed (no refresh token, or it was revoked)
- Token revoked
- A stale `ENCLII_API_TOKEN` (or `--api-token`): an explicit token always wins over the stored login

**Solutions**:

```bash
# Log the active profile in again
enclii login

# Verify
enclii whoami 2>&1

# If a token is set in the environment, it overrides the login
unset ENCLII_API_TOKEN ENCLII_TOKEN
```

The CLI refreshes the stored token itself when it is within 60 seconds of
expiry and writes the new one back to the profile's credentials file. For CI,
use a personal API token instead of a login (see [API Token Issues](#api-token-issues)).

### Logged In as the Wrong Account

**Symptom**: `enclii whoami` shows an account you did not mean to use, or
administrative calls fail with 403 (or 404) although you are logged in

**Cause**: a plain `enclii login` lets Janua answer from the browser's current
session, so the CLI receives whichever account the browser is signed in as

**Solutions**:

```bash
# Pick the account from Janua's chooser
enclii login --prompt select_account

# Sign in as someone else
enclii login --prompt login

# Keep a second identity in its own profile, without changing the browser's
# account: open the printed URL in a private window
enclii --profile admin login --no-browser --prompt login
enclii --profile admin whoami 2>&1
```

Janua answers 404 rather than 403 for some resources a caller may not see (for
example another owner's OAuth client), so a 404 on an administrative call can
also mean the wrong identity. See
[`enclii login`](/cli/commands/login#several-identities-profiles-and-account-switching).

### "Unknown client_id"

**Symptom**: OAuth flow fails with "invalid_client"

**Causes**:
- OAuth client not registered in Janua
- Wrong `--client-id`
- Client deleted or disabled

**Solutions**:

1. **Use the built-in client**:
```bash
enclii login  # no --client-id: uses the built-in Enclii CLI client
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

**Cause**: Janua compares redirect URIs exactly, including the port. The CLI
listens on `http://127.0.0.1:8080/callback`, or on port 3000 when 8080 is busy.

**Solutions**:

1. **Verify the CLI client's registered URIs** include both
   `http://127.0.0.1:8080/callback` and `http://127.0.0.1:3000/callback`.
2. **Update the OAuth client** in the Janua admin panel if needed; see
   [CLI Auth Setup](/guides/cli-auth-setup).

### Browser Doesn't Open

**Symptom**: `enclii login` doesn't open browser

**Causes**:
- No default browser configured
- SSH session without display
- WSL environment

**Solutions**:

1. **Print the URL instead**:
```bash
enclii login --no-browser
# Then open the printed URL in a browser on the same machine:
# Janua redirects to the CLI's callback on 127.0.0.1
```

2. **Choose the command that opens it** (the CLI appends the URL as the last argument):
```bash
export ENCLII_BROWSER='firefox --private-window'
enclii login
```

### SSO Login Fails

**Symptom**: Redirected to Janua but login fails

**Causes**:
- Invalid Janua credentials
- Account disabled
- MFA required but not configured

**Solutions**:

1. **Sign in at https://auth.madfam.io in the browser** to see Janua's own error
2. **Check account status** in Janua admin
3. **Reset password** via Janua if needed

### "Permission Denied" (403 Forbidden)

**Symptom**: Authenticated but can't access resource

**Causes**:
- Logged in as a different account than intended (see [Logged In as the Wrong Account](#logged-in-as-the-wrong-account))
- Insufficient role permissions
- Resource in a different team or organization
- API token scope too narrow

**Solutions**:

```bash
# Which identity is in use?
enclii whoami 2>&1

# Your teams
enclii teams list

# Request elevated permissions from a team admin
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
- Wrong OIDC issuer configured (`ENCLII_OIDC_ISSUER`)
- The CLI is calling the wrong API: unless `ENCLII_API_ENDPOINT` is set, the
  default `development` environment targets `http://localhost:4200`
- Network issues reaching auth server

**Solutions**:

```bash
# Log in again: this rewrites the active profile's credentials file
enclii logout
enclii login

# Point the CLI at the hosted API
export ENCLII_API_ENDPOINT=https://api.enclii.dev
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

### API Token Issues

**Symptom**: authentication with a personal API token fails

**Causes**:
- Token expired
- Token revoked
- Scope too narrow

**Solutions**:

```bash
# List your API tokens (metadata only, never the value)
enclii tokens list

# Create a new one; the value is shown once
enclii tokens create --name ci-deploy --expires-in 90d --scopes deploy,logs

# Use it: an explicit token wins over any stored login
export ENCLII_API_TOKEN=<token>
enclii projects list
```

See [`enclii tokens`](/cli/commands/tokens).

## Session Management

The CLI does not manage Janua sessions. `enclii logout` deletes only the active
profile's local credentials; it neither revokes the tokens nor ends the
browser's Janua session.

- To end the browser's SSO session, sign out of a web console (it uses Janua's
  RP-Initiated Logout) or of Janua itself.
- To review or revoke sessions, use the Janua dashboard.

## Configuration

### Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `ENCLII_OIDC_ISSUER` | SSO provider URL | `https://auth.madfam.io` |
| `ENCLII_OIDC_CLIENT_ID` | OAuth client used for token refresh | the built-in Enclii CLI client |
| `ENCLII_API_ENDPOINT` | API endpoint | `http://localhost:4200` in the default `development` environment, `https://api.enclii.dev` otherwise |
| `ENCLII_API_TOKEN` | API token; wins over any stored login (legacy `ENCLII_TOKEN` also accepted) | unset |
| `ENCLII_PROFILE` | Stored identity to use, like `--profile` | `default` |
| `ENCLII_BROWSER` | Command that opens the login URL | the operating system's opener |

The full list is in the [CLI reference](/cli/#environment-variables).

### Credentials File

| Profile | File |
|---------|------|
| `default` | `~/.enclii/credentials.json` |
| any other name, for example `admin` | `~/.enclii/profiles/admin/credentials.json` |

```json
{
  "access_token": "eyJ...",
  "refresh_token": "...",
  "token_type": "Bearer",
  "expires_at": "2026-02-24T00:00:00Z",
  "issuer": "https://auth.madfam.io"
}
```

**Security**: the CLI writes these files with `600` permissions. If you copied
one, restore them:
```bash
chmod 600 ~/.enclii/credentials.json
```

## Testing Authentication

### Verify Token Manually

```bash
# Decode the active profile's access token claims (doesn't verify the signature)
jq -r .access_token ~/.enclii/credentials.json | cut -d. -f2 | base64 -d 2>/dev/null | jq

# Exercise the token against the API
enclii projects list
```

### Test OAuth Flow

```bash
# Print the exact authorize URL the CLI uses (PKCE included) without opening it
enclii login --no-browser
```

## Related Documentation

- **CLI Auth Setup**: [Authentication Setup Guide](/guides/cli-auth-setup)
- **SSO Deployment**: [SSO Deployment Instructions](/guides/sso-deployment)
- **SSO Integration**: [SSO Integration](/integrations/sso)
- **API Errors**: [API Error Reference](./api-errors)
