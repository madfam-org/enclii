---
title: SSO Deployment Instructions
description: Deploy and configure Janua SSO integration with Enclii
sidebar_position: 16
tags: [sso, janua, authentication, deployment]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# Enclii-Janua SSO Integration Deployment

## Related Documentation

- **SSO Integration**: [SSO Overview](/integrations/sso)
- **CLI Auth**: [CLI Authentication Setup](/guides/cli-auth-setup)
- **Troubleshooting**: [Authentication Problems](/troubleshooting/auth-problems)

## Status: Configuration Ready, Pending Production Deployment

**Date**: 2025-12-10

## Completed Steps

### 1. OAuth Client Created in Janua Production
- **Client ID**: `<GET_FROM_JANUA_ADMIN_OR_SECRETS_MANAGER>`
- **Client Secret**: `<GET_FROM_JANUA_ADMIN_OR_SECRETS_MANAGER>` (NEVER commit actual secrets)
- **Redirect URIs**:
  - `https://api.enclii.dev/v1/auth/callback`
  - `https://app.enclii.dev/auth/callback`
- **Scopes**: openid, profile, email
- **Grant Types**: authorization_code, refresh_token

### 2. K8s Manifests Updated
- `infra/k8s/production/environment-patch.yaml` - Updated with OIDC env vars
- `infra/k8s/production/oidc-secrets.yaml` - Template for K8s secret
- `infra/k8s/production/oidc-secrets.local.yaml` - Actual credentials (gitignored)
- `infra/k8s/production/kustomization.yaml` - Updated to include OIDC resources

### 3. OIDC Configuration
```yaml
ENCLII_AUTH_MODE: "oidc"
ENCLII_OIDC_ISSUER: "https://auth.madfam.io"
ENCLII_EXTERNAL_JWKS_URL: "https://auth.madfam.io/.well-known/jwks.json"
ENCLII_EXTERNAL_ISSUER: "https://auth.madfam.io"
ENCLII_OIDC_REDIRECT_URL: "https://api.enclii.dev/v1/auth/callback"
```

## Pending Manual Steps

### Step 1: Apply OIDC Secret to Production Cluster

SSH into the production server and run:

```bash
# Create the OIDC credentials secret
# Get credentials from Janua admin panel or secrets manager
kubectl -n enclii create secret generic enclii-oidc-credentials \
  --from-literal=client-id=$JANUA_CLIENT_ID \
  --from-literal=client-secret=$JANUA_CLIENT_SECRET \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Step 2: Update Switchyard API Deployment

```bash
# Update environment variables
kubectl -n enclii set env deployment/switchyard-api \
  ENCLII_AUTH_MODE=oidc \
  ENCLII_OIDC_ISSUER=https://auth.madfam.io \
  ENCLII_EXTERNAL_JWKS_URL=https://auth.madfam.io/.well-known/jwks.json \
  ENCLII_EXTERNAL_ISSUER=https://auth.madfam.io \
  ENCLII_OIDC_REDIRECT_URL=https://api.enclii.dev/v1/auth/callback

# Add secret references
kubectl -n enclii patch deployment switchyard-api --type='json' -p='[
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {"name": "ENCLII_OIDC_CLIENT_ID", "valueFrom": {"secretKeyRef": {"name": "enclii-oidc-credentials", "key": "client-id"}}}},
  {"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {"name": "ENCLII_OIDC_CLIENT_SECRET", "valueFrom": {"secretKeyRef": {"name": "enclii-oidc-credentials", "key": "client-secret"}}}}
]'
```

### Step 3: Verify Rollout

```bash
# Watch rollout progress
kubectl -n enclii rollout status deployment/switchyard-api --timeout=120s

# Verify pods are healthy
kubectl -n enclii get pods -l app=switchyard-api

# Check API health
curl https://api.enclii.dev/health
```

### Step 4: Test Token Validation

```bash
# 1. Get a token from Janua
curl -X POST https://auth.madfam.io/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"enclii-admin@madfam.io","password":"EncliiSSO2024!"}'

# 2. Use the access_token with Enclii API
curl -X GET https://api.enclii.dev/api/v1/users/me \
  -H "Authorization: Bearer <ACCESS_TOKEN>"
```

## Rollback Instructions

If SSO integration causes issues:

```bash
# Revert to local auth mode
kubectl -n enclii set env deployment/switchyard-api \
  ENCLII_AUTH_MODE=local

# Watch rollout
kubectl -n enclii rollout status deployment/switchyard-api
```

## Security Notes

- The `oidc-secrets.local.yaml` file contains actual credentials and is gitignored
- Never commit the client_secret to version control
- Rotate credentials periodically via Janua's OAuth client management
- The client_secret shown once on creation and should be stored securely

## Architecture

```
┌─────────────────────┐     ┌─────────────────────┐
│  Enclii UI (4201)   │────▶│  Enclii API (4200)  │
│  app.enclii.dev     │     │  api.enclii.dev     │
└─────────────────────┘     └──────────┬──────────┘
                                       │
                                       │ JWKS Validation
                                       │ Token Introspection
                                       ▼
                            ┌─────────────────────┐
                            │   Janua API (4100)  │
                            │  auth.madfam.io     │
                            │  auth.madfam.io      │
                            └─────────────────────┘
```

## Related Files

- `apps/switchyard-api/internal/auth/oidc.go` - OIDC authentication manager
- `apps/switchyard-api/internal/config/config.go` - Configuration parsing
- `infra/k8s/production/environment-patch.yaml` - K8s env config
- `infra/k8s/production/oidc-secrets.yaml` - Secret template
