---
title: CLI Authentication Setup
description: Configure OAuth authentication for the Enclii CLI with Janua SSO
sidebar_position: 15
tags: [cli, authentication, oauth, janua]
---

# Enclii CLI Authentication Setup

This document describes how to set up OAuth authentication for the Enclii CLI.

## Related Documentation

- **Prerequisites**: [CLI Installation](/cli/)
- **SSO Provider**: [Janua SSO Integration](/integrations/sso)
- **Troubleshooting**: [Authentication Problems](/troubleshooting/auth-problems)

## Prerequisites

- Admin access to Janua SSO (admin.madfam.io)
- Or admin credentials to run the registration script

## Option 1: Via Janua Admin Dashboard

1. Go to https://admin.madfam.io
2. Navigate to **OAuth Clients** section
3. Click **Create New Client**
4. Fill in the following details:

| Field | Value |
|-------|-------|
| Name | Enclii CLI |
| Description | Official Enclii command-line interface |
| Is Confidential | **No** (public client for PKCE) |
| Redirect URIs | `http://127.0.0.1:8080/callback` and `http://127.0.0.1:3000/callback` |
| Grant Types | `authorization_code`, `refresh_token` |
| Allowed Scopes | `openid`, `profile`, `email`, `offline_access` |
| Website URL | `https://enclii.dev` |

5. Save and note the `client_id` (will be auto-generated)

## Option 2: Via Registration Script

```bash
# Set admin credentials
export JANUA_ADMIN_EMAIL=admin@madfam.io
export JANUA_ADMIN_PASSWORD=your-password

# Run the registration script
cd /path/to/enclii
python scripts/register-oauth-client.py
```

The script will output the `client_id` - save it for CLI configuration.

## Option 3: Via Janua API (curl)

```bash
# First, login to get an access token
TOKEN=$(curl -s -X POST https://auth.madfam.io/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "admin@madfam.io", "password": "YOUR_PASSWORD"}' \
  | jq -r '.access_token')

# Create the OAuth client
curl -X POST https://auth.madfam.io/api/v1/oauth/clients \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Enclii CLI",
    "description": "Official Enclii command-line interface",
    "redirect_uris": ["http://127.0.0.1:8080/callback", "http://127.0.0.1:3000/callback"],
    "allowed_scopes": ["openid", "profile", "email", "offline_access"],
    "grant_types": ["authorization_code", "refresh_token"],
    "is_confidential": false,
    "website_url": "https://enclii.dev"
  }'
```

## Using the CLI After Setup

Once the OAuth client is registered:

Install a [release](https://github.com/madfam-org/enclii/releases) (see
[Installation](/cli/)) or build from source (Go 1.26+), then:

```bash
# Login (opens browser for OAuth flow)
enclii login

# Verify authentication (whoami prints on stderr)
enclii whoami 2>&1

# Use CLI commands
enclii deploy
enclii logs my-service
```

To hold more than one identity, or to sign in as someone other than the
browser's current account, see
[`enclii login`](/cli/commands/login#several-identities-profiles-and-account-switching)
(`--profile`, `--prompt`, `--no-browser`).

## Custom Client ID

If using a different client_id than the built-in Enclii CLI client:

```bash
# Login with custom client ID
enclii login --client-id your-custom-client-id

# Token refresh reads its client ID from the environment, so set the same value
export ENCLII_OIDC_CLIENT_ID=your-custom-client-id
```

## Troubleshooting

### "invalid_client: Unknown client_id"
The OAuth client hasn't been registered in Janua. Follow the setup steps above.

### "redirect_uri mismatch"
Janua compares the redirect URI exactly, including the port. The CLI listens on
`http://127.0.0.1:8080/callback`, or on port 3000 when 8080 is busy, so the
client must register both.

### Token expired
Run `enclii login` again to refresh your credentials.

## Security Notes

- The CLI uses OAuth 2.0 PKCE flow (secure for public clients)
- Credentials are stored with 600 permissions at `~/.enclii/credentials.json`, or at `~/.enclii/profiles/<name>/credentials.json` for `--profile <name>`
- Access tokens are automatically refreshed when possible
- Run `enclii logout` to remove the active profile's stored credentials (it does not end the browser's Janua session)
