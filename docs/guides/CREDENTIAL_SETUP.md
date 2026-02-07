# Cloudflare Credential Setup Guide

This guide explains how to configure Cloudflare API credentials for Enclii domain provisioning scripts.

## Why This Matters

Enclii uses Cloudflare for:
- **DNS Management**: Creating zones and DNS records
- **Tunnel Routing**: Directing traffic through Cloudflare Tunnel to Kubernetes services
- **TLS/SSL**: Automatic certificate management via Cloudflare proxy

The scripts require properly-scoped API tokens to perform these operations securely.

## Quick Start

### Option 1: Interactive Setup (Recommended)

```bash
./scripts/cloudflare-zone-create.sh --setup
```

This creates `~/.enclii/credentials` with your Cloudflare credentials.

### Option 2: Environment Variables

```bash
export CLOUDFLARE_API_TOKEN="your-api-token"
export CLOUDFLARE_ACCOUNT_ID="your-account-id"
export TUNNEL_ID="your-tunnel-uuid"
```

### Option 3: Kubernetes Secret

```bash
kubectl create secret generic enclii-cloudflare-credentials \
    -n enclii \
    --from-literal=api-token="your-api-token" \
    --from-literal=account-id="your-account-id" \
    --from-literal=tunnel-id="your-tunnel-uuid"
```

## Creating a Properly-Scoped API Token

### Step 1: Access Cloudflare Dashboard

1. Go to [Cloudflare Dashboard](https://dash.cloudflare.com)
2. Navigate to **My Profile** → **API Tokens**
3. Click **Create Token**

### Step 2: Create Custom Token

Select **Create Custom Token** (not a template).

### Step 3: Configure Permissions

Add the following permissions:

| Permission | Access Level | Scope | Purpose |
|------------|-------------|-------|---------|
| Zone | Edit | Account: All zones | Create/manage zones |
| DNS | Edit | Account: All zones | Create/update DNS records |
| Zone Settings | Edit | Account: All zones | HTTPS enforcement, TLS version, etc. |
| SSL and Certificates | Edit | Account: All zones | SSL mode, certificate management |
| Cloudflare Tunnel | Edit | Account | Tunnel configuration |

### Step 4: Set Token Name and TTL

- **Name**: `enclii-domain-provisioning`
- **TTL**: Recommended to leave as "No expiration" for automation, or set reasonable expiration with rotation plan

### Step 5: Create and Save

1. Click **Continue to summary**
2. Review permissions
3. Click **Create Token**
4. **IMPORTANT**: Copy the token immediately - it won't be shown again

## Credential File Format

Location: `~/.enclii/credentials`

```ini
# Enclii Cloudflare Credentials
# WARNING: Keep this file secure. Do not commit to version control.

[cloudflare]
api_token = your-api-token-here
account_id = your-account-id-here
tunnel_id = your-tunnel-uuid-here
```

**Security**: This file should have permissions `600` (owner read/write only):

```bash
chmod 600 ~/.enclii/credentials
```

## Finding Your Cloudflare IDs

### Account ID

1. Go to [Cloudflare Dashboard](https://dash.cloudflare.com)
2. Select any domain (or the overview page)
3. Look in the right sidebar under **API** → **Account ID**

Or via API:
```bash
curl -X GET "https://api.cloudflare.com/client/v4/accounts" \
    -H "Authorization: Bearer YOUR_TOKEN" \
    -H "Content-Type: application/json" | jq '.result[].id'
```

### Tunnel ID

1. Go to **Zero Trust** → **Networks** → **Tunnels**
2. Click on your tunnel (e.g., `enclii-production`)
3. The UUID is shown in the URL or tunnel details

Or via API:
```bash
curl -X GET "https://api.cloudflare.com/client/v4/accounts/YOUR_ACCOUNT_ID/tunnels" \
    -H "Authorization: Bearer YOUR_TOKEN" \
    -H "Content-Type: application/json" | jq '.result[] | {name, id}'
```

## Verifying Credentials

Test your credentials:

```bash
./scripts/cloudflare-zone-create.sh --test
```

Or check status:

```bash
./scripts/cloudflare-zone-create.sh --status
```

## Credential Loading Priority

Scripts load credentials in this order:

1. **Environment Variables** (highest priority)
   - `CLOUDFLARE_API_TOKEN`
   - `CLOUDFLARE_ACCOUNT_ID`
   - `TUNNEL_ID`

2. **Local Credential File**
   - `~/.enclii/credentials`
   - Customizable via `ENCLII_CREDENTIALS_FILE` env var

3. **Kubernetes Secret** (requires kubectl access)
   - Secret: `enclii-cloudflare-credentials`
   - Namespace: `enclii` (customizable via `CLOUDFLARE_SECRET_NAMESPACE`)

## Common Issues

### "Authentication error" from Cloudflare API

**Cause**: The API token doesn't have required permissions.

**Solution**: Create a new token with all permissions listed above. The tunnel-specific token from `~/.cloudflared/cert.pem` only works for tunnel operations, not zone/DNS management.

### "Zone creation failed"

**Cause**: Token missing `Zone:Edit` permission.

**Solution**: Ensure your token has `Zone:Edit` for `Account: All zones`.

### "DNS record creation failed"

**Cause**: Token missing `DNS:Edit` permission or zone not active.

**Solution**:
1. Verify token has `DNS:Edit` permission
2. Check zone status (must update nameservers at registrar first)

### Scripts can't find credentials

**Cause**: Credential file not in expected location or wrong format.

**Solution**:
1. Check file exists: `ls -la ~/.enclii/credentials`
2. Verify format matches INI-style with `[cloudflare]` section
3. Ensure no extra whitespace around `=` signs

## Security Best Practices

### Do

- Store credentials in `~/.enclii/credentials` with `600` permissions
- Use least-privilege tokens (only permissions needed)
- Rotate tokens periodically
- Use Kubernetes secrets for production workloads

### Don't

- Commit credentials to version control
- Share tokens across environments
- Use account-level API keys (use scoped tokens instead)
- Store tokens in scripts

## Scripts Using Credentials

| Script | kubectl Required | Purpose |
|--------|------------------|---------|
| `cloudflare-zone-create.sh` | No | Create zones and DNS records only |
| `cloudflare-zone-settings.sh` | No | Manage zone settings (HTTPS, TLS, security) |
| `provision-domain.sh` | Yes | Full provisioning including tunnel routing |
| `deploy-client.sh` | Yes | Complete client deployment orchestration |

## Kubernetes Secret YAML (Production)

For production deployments, create the secret via manifest:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: enclii-cloudflare-credentials
  namespace: enclii
  labels:
    app.kubernetes.io/name: cloudflare-credentials
    app.kubernetes.io/part-of: enclii
type: Opaque
stringData:
  api-token: "your-api-token-here"
  account-id: "your-account-id-here"
  tunnel-id: "your-tunnel-uuid-here"
```

Apply with:
```bash
kubectl apply -f cloudflare-credentials.yaml
```

## Future: External Secrets Integration

When External Secrets Operator is deployed, credentials will be sourced from Vault:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: cloudflare-credentials
  namespace: enclii
spec:
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  data:
    - secretKey: api-token
      remoteRef:
        key: enclii/cloudflare
        property: api-token
    - secretKey: account-id
      remoteRef:
        key: enclii/cloudflare
        property: account-id
    - secretKey: tunnel-id
      remoteRef:
        key: enclii/cloudflare
        property: tunnel-id
```

See `docs/infrastructure/EXTERNAL_SECRETS.md` for implementation status.

## Related Documentation

- [Agency Model Deployment](./AGENCY_MODEL_DEPLOYMENT.md) - Multi-tenant client deployment
- [Dogfooding Guide](./DOGFOODING_GUIDE.md) - Running Enclii on Enclii
- [External Secrets](../infrastructure/EXTERNAL_SECRETS.md) - Vault integration roadmap
