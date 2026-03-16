# GitHub Webhook Setup Guide

**Last Updated:** Feb 3, 2026 (Wave 13)
**Purpose:** Enable automatic deployments for all MADFAM ecosystem repositories

---

## Overview

This guide covers setting up GitHub webhooks to trigger Enclii's build and deployment pipeline for:
- `madfam-org/janua` → Janua SSO (auth.madfam.io)
- `madfam-org/dhanam` → Dhanam Finance (api.dhan.am, app.dhan.am)
- `madfam-org/enclii` → Enclii Platform (already configured)
- `madfam-org/solarpunk-foundry` → Infrastructure

---

## Prerequisites

1. **GitHub Personal Access Token (PAT)** with `admin:repo_hook` permission
2. **Enclii API Token** for project registration
3. **Cluster access** to retrieve webhook secret

---

## Quick Setup (Automated)

The fastest way to set up webhooks for all repositories:

```bash
# Set environment variables
export ENCLII_API_TOKEN="your-enclii-api-token"
export GITHUB_TOKEN="your-github-pat"

# Run automated setup
./scripts/setup-auto-deploy.sh
```

This script will:
1. Create projects in Enclii for each repository
2. Create production environments
3. Configure GitHub webhooks with HMAC secrets
4. Display the webhook secrets for each repo

---

## Manual Setup

### Step 1: Get Webhook Secret

```bash
# From the enclii namespace
kubectl get secret enclii-github-webhook -n enclii -o jsonpath='{.data.secret}' | base64 -d
```

Save this secret — you'll need it for each webhook configuration.

### Step 2: Configure Webhook in GitHub

For each repository:

1. Go to **Settings** → **Webhooks** → **Add webhook**
2. Configure:

| Setting | Value |
|---------|-------|
| **Payload URL** | `https://api.enclii.dev/v1/webhooks/github` |
| **Content type** | `application/json` |
| **Secret** | (from Step 1) |
| **SSL verification** | Enable |
| **Events** | `push` events only |

3. Click **Add webhook**

### Step 3: Verify Webhook

After adding the webhook:

1. Make a small commit and push to the `main` branch
2. Check webhook delivery in GitHub: **Settings** → **Webhooks** → **Recent Deliveries**
3. Verify deployment started: `curl https://api.enclii.dev/v1/deployments?project=<project-slug>`

---

## Repository-Specific Configuration

### Janua (madfam-org/janua)

**Webhook URL:** `https://api.enclii.dev/v1/webhooks/github`
**Events:** Push to `main` branch
**Project slug:** `janua`

**Build triggers:**
- Changes to `apps/janua-api/**`
- Changes to `infra/k8s/**`

**Deployment targets:**
- `janua-api` → auth.madfam.io
- `janua-dashboard` → app.janua.dev
- `janua-admin` → admin.janua.dev

### Dhanam (madfam-org/dhanam)

**Webhook URL:** `https://api.enclii.dev/v1/webhooks/github`
**Events:** Push to `main` branch
**Project slug:** `dhanam`

**Build triggers:**
- Changes to `apps/api/**`
- Changes to `apps/web/**`
- Changes to `apps/admin/**`
- Changes to `infra/k8s/**`

**Deployment targets:**
- `dhanam-api` → api.dhan.am
- `dhanam-web` → app.dhan.am
- `dhanam-admin` → admin.dhan.am

### Enclii (madfam-org/enclii)

**Webhook URL:** `https://api.enclii.dev/v1/webhooks/github`
**Events:** Push to `main` branch
**Project slug:** `enclii`

**Build triggers:**
- Changes to `apps/switchyard-api/**`
- Changes to `apps/switchyard-ui/**`
- Changes to `apps/dispatch/**`
- Changes to `apps/roundhouse/**`
- Changes to `apps/status/**`

**Deployment targets:**
- `switchyard-api` → api.enclii.dev
- `switchyard-ui` → app.enclii.dev
- `dispatch` → admin.enclii.dev
- `docs-site` → docs.enclii.dev
- `landing-page` → enclii.dev
- `status-enclii` → status.enclii.dev
- `status-madfam` → status.madfam.io

---

## Troubleshooting

### Webhook Not Triggering

1. **Check GitHub delivery status:**
   - Go to repo Settings → Webhooks → Recent Deliveries
   - Look for HTTP 200 responses

2. **Verify Enclii API health:**
   ```bash
   curl -s https://api.enclii.dev/health | jq
   ```

3. **Check webhook secret match:**
   ```bash
   # On cluster
   kubectl get secret enclii-github-webhook -n enclii -o jsonpath='{.data.secret}' | base64 -d
   ```
   Compare with GitHub webhook secret.

### Build Not Starting

1. **Check Roundhouse worker:**
   ```bash
   kubectl get pods -n enclii -l app=roundhouse
   kubectl logs -n enclii -l app=roundhouse --tail=50
   ```

2. **Verify project exists:**
   ```bash
   curl -s -H "Authorization: Bearer $ENCLII_API_TOKEN" \
     https://api.enclii.dev/v1/projects/<project-slug>
   ```

3. **Check build queue:**
   ```bash
   curl -s -H "Authorization: Bearer $ENCLII_API_TOKEN" \
     https://api.enclii.dev/v1/builds?status=pending
   ```

### Deployment Not Proceeding

1. **Check ArgoCD sync:**
   ```bash
   kubectl get applications -n argocd | grep <service>
   ```

2. **Verify image pushed:**
   ```bash
   # Check GHCR for latest image
   curl -s -H "Authorization: Bearer $(echo $GITHUB_TOKEN)" \
     https://ghcr.io/v2/madfam-org/<image>/tags/list
   ```

3. **Check Kyverno policy compliance:**
   ```bash
   kubectl get policyreport -n <namespace>
   ```

---

## Webhook Security

### HMAC Verification

All webhooks are verified using HMAC-SHA256:
1. GitHub signs the payload with the shared secret
2. Enclii verifies the signature before processing
3. Mismatched signatures are rejected with HTTP 401

### Secret Rotation

To rotate the webhook secret:

```bash
# 1. Generate new secret
NEW_SECRET=$(openssl rand -hex 32)

# 2. Update Enclii secret
kubectl create secret generic enclii-github-webhook -n enclii \
  --from-literal=secret=$NEW_SECRET \
  --dry-run=client -o yaml | kubectl apply -f -

# 3. Restart Switchyard API
kubectl rollout restart deploy/switchyard-api -n enclii

# 4. Update each GitHub webhook with the new secret
echo "New secret: $NEW_SECRET"
```

---

## Current Status (Wave 13)

| Repository | Webhook | Auto-Deploy | Notes |
|------------|---------|-------------|-------|
| madfam-org/enclii | ✅ Configured | ✅ Working | Primary platform |
| madfam-org/janua | ⚠️ Manual | ⚠️ ArgoCD only | Needs webhook setup |
| madfam-org/dhanam | ⚠️ Manual | ⚠️ ArgoCD only | Needs webhook setup |
| madfam-org/solarpunk-foundry | ⚠️ Manual | ⚠️ ArgoCD only | Infrastructure repo |

**Action Required:** Run `./scripts/setup-auto-deploy.sh` with `GITHUB_TOKEN` to configure missing webhooks.

---

## Related Documentation

- [Onboarding Guide](./ONBOARDING_GUIDE.md)
- [Build Pipeline](../production/BUILD_PIPELINE.md)
- [Janua Deployment Hardening](../cross-repo/JANUA_DEPLOYMENT_PROMPT.md)
- [Dhanam Deployment Hardening](../cross-repo/DHANAM_DEPLOYMENT_PROMPT.md)
