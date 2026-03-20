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
| Redirect URIs | `http://127.0.0.1/callback` |
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
    "redirect_uris": ["http://127.0.0.1/callback"],
    "allowed_scopes": ["openid", "profile", "email", "offline_access"],
    "grant_types": ["authorization_code", "refresh_token"],
    "is_confidential": false,
    "website_url": "https://enclii.dev"
  }'
```

## Using the CLI After Setup

Once the OAuth client is registered:

```bash
# Build the CLI
cd packages/cli
go build -o enclii ./cmd/enclii

# Login (opens browser for OAuth flow)
./enclii login

# Verify authentication
./enclii whoami

# Use CLI commands
./enclii deploy
./enclii logs my-service
```

## Custom Client ID

If using a different client_id than the default (`enclii-cli`):

```bash
# Login with custom client ID
enclii login --client-id your-custom-client-id

# Or set via environment
export ENCLII_CLIENT_ID=your-custom-client-id
enclii login
```

## Troubleshooting

### "invalid_client: Unknown client_id"
The OAuth client hasn't been registered in Janua. Follow the setup steps above.

### "redirect_uri mismatch"
The redirect URI must exactly match what's registered. The CLI uses `http://127.0.0.1:<port>/callback` where port is dynamically assigned.

### Token expired
Run `enclii login` again to refresh your credentials.

## Security Notes

- The CLI uses OAuth 2.0 PKCE flow (secure for public clients)
- Credentials are stored at `~/.enclii/credentials.json` with 600 permissions
- Access tokens are automatically refreshed when possible
- Run `enclii logout` to remove stored credentials
