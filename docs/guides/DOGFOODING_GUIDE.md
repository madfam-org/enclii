---
title: Dogfooding Guide
description: How Enclii deploys itself using its own platform with Janua authentication
sidebar_position: 1
tags: [guides, dogfooding, deployment, self-hosting, janua]
---

# Enclii + Janua Dogfooding Strategy

> ✅ **ACTIVE** - Enclii is now self-hosting with automated deployment pipeline.
> **Current Status:** Production services deployed, GitHub webhook configured, auto-deploy enabled.
> **Last Updated:** February 2026

---

> **Achieved:** "We run our entire platform on Enclii, authenticated by Janua. We are our own most demanding customer."

This document describes how Enclii deploys **itself** using its own platform, and how we use **Janua** (our own auth solution) to authenticate the Enclii control plane. This is critical for product quality, customer confidence, and sales credibility.

## Current Production Status

### Enclii Services (github.com/madfam-org/enclii)
| Service | URL | Port | Status | Auto-Deploy |
|---------|-----|------|--------|-------------|
| Switchyard API | api.enclii.dev | 4200 | ✅ Running | ✅ Enabled |
| Switchyard UI | app.enclii.dev | 4201 | ✅ Running | ✅ Enabled |
| Docs Site | docs.enclii.dev | - | ✅ Running | ✅ Enabled |
| Landing Page | enclii.dev | - | ✅ Running | ✅ Enabled |
| Status Page | status.enclii.dev | - | ✅ Running | ✅ Enabled |

### Janua Services (github.com/madfam-org/janua)
| Service | URL | Port | Status | Auto-Deploy |
|---------|-----|------|--------|-------------|
| Janua API | api.janua.dev | 80 | ✅ Running | ✅ Enabled |
| Janua Dashboard | app.janua.dev | 80 | ✅ Running | ✅ Enabled |
| Janua Admin | admin.janua.dev | 80 | ✅ Running | ✅ Enabled |
| Janua Docs | docs.janua.dev | 80 | ✅ Running | ✅ Enabled |
| Janua Website | janua.dev | 80 | ✅ Running | ✅ Enabled |

### Solarpunk Foundry Services (github.com/madfam-org/solarpunk-foundry)
| Service | URL | Port | Status | Auto-Deploy |
|---------|-----|------|--------|-------------|
| Solarpunk Docs | docs.madfam.io | 3000 | 🔲 Pending | ✅ Enabled |
| npm Registry | npm.madfam.io | 4873 | ✅ Running | Manual (image-based) |

### GitHub Webhook Status

| Repository | Webhook Configured | Events |
|------------|-------------------|--------|
| madfam-org/enclii | ✅ Active | push, pull_request |
| madfam-org/janua | ✅ Active | push, pull_request |

**Webhook Endpoint:** `POST /v1/webhooks/github`
**Events:** Push (triggers auto-deploy on main branch)

---

## Table of Contents

1. [Why Dogfooding Matters](#why-dogfooding-matters)
2. [Current State](#current-state)
3. [Dogfooding Architecture](#dogfooding-architecture)
4. [Deployment Strategy](#deployment-strategy)
5. [Repository Structure](#repository-structure)
6. [Step-by-Step Implementation](#step-by-step-implementation)
7. [The Confidence Signal](#the-confidence-signal)
8. [Troubleshooting](#troubleshooting)

---

## Why Dogfooding Matters

### The Problem We're Solving

**Before Dogfooding:**
- ❌ Enclii deployed via raw Kubernetes manifests (`kubectl apply -k infra/k8s/base`)
- ❌ Not using our own platform (can't validate our own product)
- ❌ Missing customer pain points (we don't experience what they do)
- ❌ No confidence signal ("If they don't use it, why should we?")
- ❌ Janua built but unused (we don't authenticate with our own solution)

**After Dogfooding:**
- ✅ Enclii deploys Enclii (using `enclii deploy` commands)
- ✅ Janua authenticates Enclii (OAuth/OIDC flows battle-tested daily)
- ✅ We experience every customer pain point first
- ✅ Powerful sales narrative: "We run production on Enclii + Janua"
- ✅ Product quality improves (we fix issues before customers see them)

### Business Impact

**Customer Confidence:**
- "If Enclii trusts Enclii for their own production, so can we"
- Removes #1 objection: "Is this actually production-ready?"

**Sales Credibility:**
- Authentic testimonials: "We've deployed 50+ times this month using Enclii"
- Technical demos show real production usage, not toy examples

**Product Quality:**
- Engineering team uses Enclii daily (bugs found and fixed faster)
- Edge cases discovered organically (complex auth flows, networking, etc.)

**Team Alignment:**
- Everyone experiences the developer experience daily
- Product decisions informed by real usage, not assumptions

---

## Current State

### What We Have (Active)

**Enclii Repository:** https://github.com/madfam-org/enclii
- ✅ Control plane API (Switchyard) - **DEPLOYED** at api.enclii.dev
- ✅ Web UI (Next.js dashboard) - **DEPLOYED** at app.enclii.dev
- ✅ CLI (`enclii` command) - **OPERATIONAL**
- ✅ Kubernetes reconcilers - **RUNNING**
- ✅ GitHub webhook for auto-deploy - **CONFIGURED** (ID: 585841923)

**Janua Repository:** https://github.com/madfam-org/janua
- ✅ OAuth 2.0 / OIDC provider - **DEPLOYED** at auth.madfam.io
- ✅ RS256 JWT signing - **ACTIVE** (JWKS validated)
- ✅ Multi-tenant organization support - **WORKING**
- ✅ Password + SSO authentication - **INTEGRATED**

**Self-Deployment Pipeline:**
- ✅ Services registered with auto_deploy: true
- ✅ GitHub webhook configured with HMAC SHA-256 signature verification
- ✅ Webhook handler processing push events → triggers builds
- ✅ Build pipeline with release creation and K8s reconciliation

### What's Still Pending

**Remaining Work:**
- ✅ Landing page (enclii.dev) - deployed and running
- ✅ Status page (status.enclii.dev) - deployed and running
- 🔲 Full end-to-end test with actual push event (awaiting next commit to main)

---

## Dogfooding Architecture

### Service Topology

```
┌─────────────────────────────────────────────────────────────────┐
│                       Enclii Platform                           │
│                (Deployed on Hetzner + Cloudflare)               │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│  Public Internet                                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │  enclii.dev   │  │ app.enclii.dev│  │auth.enclii.dev│         │
│  │ (Landing)    │  │   (Web UI)   │  │   (Janua)   │         │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘         │
│         │                 │                  │                  │
└─────────┼─────────────────┼──────────────────┼──────────────────┘
          │                 │                  │
          │                 │                  │
┌─────────┼─────────────────┼──────────────────┼──────────────────┐
│         ▼                 ▼                  ▼                  │
│  ┌────────────────────────────────────────────────────┐        │
│  │         Cloudflare Tunnel (Replaces LB)            │        │
│  └────────────────────────────────────────────────────┘        │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Kubernetes Cluster (Hetzner AX41-NVME single-node)      │  │
│  │                                                           │  │
│  │  Namespace: enclii-platform                              │  │
│  │  ┌─────────────────────────────────────────────────┐    │  │
│  │  │  Switchyard API (3 replicas)                    │    │  │
│  │  │  └─> api.enclii.dev                              │    │  │
│  │  │  └─> Built from: github.com/madfam-org/enclii   │    │  │
│  │  │  └─> Deployed via: enclii deploy                │    │  │
│  │  └─────────────────────────────────────────────────┘    │  │
│  │                                                           │  │
│  │  ┌─────────────────────────────────────────────────┐    │  │
│  │  │  Switchyard UI (2 replicas)                     │    │  │
│  │  │  └─> app.enclii.dev                              │    │  │
│  │  │  └─> Built from: github.com/madfam-org/enclii   │    │  │
│  │  │  └─> Deployed via: enclii deploy                │    │  │
│  │  └─────────────────────────────────────────────────┘    │  │
│  │                                                           │  │
│  │  ┌─────────────────────────────────────────────────┐    │  │
│  │  │  Janua (3 replicas)                            │    │  │
│  │  │  └─> auth.enclii.dev                             │    │  │
│  │  │  └─> Built from: github.com/madfam-org/janua   │    │  │
│  │  │  └─> Deployed via: enclii deploy                │    │  │
│  │  │  └─> Authenticates: Enclii itself!              │    │  │
│  │  └─────────────────────────────────────────────────┘    │  │
│  │                                                           │  │
│  │  ┌─────────────────────────────────────────────────┐    │  │
│  │  │  Landing Page (2 replicas)                      │    │  │
│  │  │  └─> enclii.dev                                  │    │  │
│  │  │  └─> Deployed via: enclii deploy                │    │  │
│  │  └─────────────────────────────────────────────────┘    │  │
│  │                                                           │  │
│  │  ┌─────────────────────────────────────────────────┐    │  │
│  │  │  Docs Site (2 replicas)                         │    │  │
│  │  │  └─> docs.enclii.dev                             │    │  │
│  │  │  └─> Deployed via: enclii deploy                │    │  │
│  │  └─────────────────────────────────────────────────┘    │  │
│  │                                                           │  │
│  │  ┌─────────────────────────────────────────────────┐    │  │
│  │  │  Status Page (2 replicas)                       │    │  │
│  │  │  └─> status.enclii.dev                           │    │  │
│  │  │  └─> Deployed via: enclii deploy                │    │  │
│  │  └─────────────────────────────────────────────────┘    │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Shared Infrastructure                                   │  │
│  │  ┌────────────────────────────────────────────────┐     │  │
│  │  │  Self-hosted PostgreSQL (in-cluster)          │     │  │
│  │  │  └─> Used by: Enclii + Janua                 │     │  │
│  │  └────────────────────────────────────────────────┘     │  │
│  │                                                           │  │
│  │  ┌────────────────────────────────────────────────┐     │  │
│  │  │  Single Redis instance (Sentinel staged)      │     │  │
│  │  │  └─> Used by: Enclii + Janua                 │     │  │
│  │  └────────────────────────────────────────────────┘     │  │
│  │                                                           │  │
│  │  ┌────────────────────────────────────────────────┐     │  │
│  │  │  Cloudflare R2 (object storage)                │     │  │
│  │  │  └─> Used for: SBOMs, artifacts, build cache   │     │  │
│  │  └────────────────────────────────────────────────┘     │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Authentication Flow

```
User visits app.enclii.dev
    │
    ├─> Redirected to auth.enclii.dev (Janua)
    │       │
    │       ├─> User logs in (password or SSO)
    │       │
    │       └─> Janua issues ID token (RS256 JWT)
    │
    ├─> Redirect back to app.enclii.dev/callback
    │       │
    │       ├─> Exchange code for tokens
    │       │
    │       └─> Store tokens in browser
    │
    ├─> User makes API request to api.enclii.dev
    │       │
    │       ├─> Include ID token in Authorization header
    │       │
    │       ├─> Switchyard API validates token via Janua JWKS
    │       │
    │       └─> Request succeeds (user authenticated!)
```

**Key Point:** Enclii authenticates its own users via Janua. We eat our own dog food.

---

## Deployment Strategy

### Phase 1: Bootstrap (One-Time Setup)

The **first deployment** of Enclii must be manual (chicken-and-egg problem). After that, Enclii deploys itself forever.

**Bootstrap Steps:**

1. **Deploy Infrastructure** (Self-hosted PostgreSQL, Redis, R2)
2. **Deploy Enclii Control Plane Manually** (using `kubectl apply -k infra/k8s/base`)
3. **Deploy Janua Manually** (using `kubectl apply -f dogfooding/janua.yaml`)
4. **Configure Janua** (create OAuth clients for Enclii)
5. **Switch to Self-Service** (all future deploys via `enclii deploy`)

### Phase 2: Dogfooding (Forever After)

Once bootstrapped, **all deployments** happen via Enclii itself:

```bash
# Deploy Switchyard API (from GitHub)
./bin/enclii deploy --service switchyard-api --env production

# Deploy Switchyard UI (from GitHub)
./bin/enclii deploy --service switchyard-ui --env production

# Deploy Janua (from separate repo!)
./bin/enclii deploy --service janua --env production

# Deploy landing page
./bin/enclii deploy --service landing-page --env production

# Deploy docs
./bin/enclii deploy --service docs-site --env production

# Deploy status page
./bin/enclii deploy --service status-page --env production
```

**Result:** Enclii deploys Enclii. We're our own customer.

---

## Repository Structure

### Enclii Repository (`github.com/madfam-org/enclii`)

```
enclii/
├── apps/
│   ├── switchyard-api/          # Control plane API (Go)
│   ├── switchyard-ui/           # Web dashboard (Next.js)
│   ├── landing/                 # Marketing site (Next.js)
│   ├── status/                  # Status page
│   └── ...
├── packages/
│   └── cli/                     # enclii CLI
├── infra/
│   ├── k8s/
│   │   ├── base/                # Raw Kubernetes manifests (bootstrap only)
│   │   ├── staging/
│   │   └── production/
│   └── terraform/               # Infrastructure as code (Hetzner, Cloudflare)
├── dogfooding/                  # ⭐ Service specs for self-hosting
│   ├── switchyard-api.yaml      # Enclii API spec
│   ├── switchyard-ui.yaml       # Enclii UI spec
│   ├── janua.yaml              # Janua spec (separate repo!)
│   ├── landing-page.yaml        # Landing page spec
│   ├── docs-site.yaml           # Docs spec
│   └── status-page.yaml         # Status page spec
└── DOGFOODING_GUIDE.md          # This file
```

### Janua Repository (`github.com/madfam-org/janua`)

```
janua/
├── src/                         # Janua source code
├── Dockerfile                   # Container build
├── docker-compose.yml           # Local dev
└── README.md
```

**Key Insight:** Janua lives in a **separate repository**, but is deployed on Enclii via the `dogfooding/janua.yaml` spec. This demonstrates Enclii's ability to build from any GitHub repository.

---

## Step-by-Step Implementation

### Prerequisites

- Hetzner AX41-NVME dedicated server (single-node k3s)
- Cloudflare account with Tunnel configured
- Self-hosted PostgreSQL in-cluster (or Ubicloud for HA)
- GitHub accounts with access to `madfam-org/enclii` and `madfam-org/janua`

### Step 1: Bootstrap Infrastructure (Week 1)

Follow the [PRODUCTION_DEPLOYMENT_ROADMAP.md](./PRODUCTION_DEPLOYMENT_ROADMAP.md) to set up:

1. **Hetzner dedicated server** (AX41-NVME, single-node k3s)
2. **Cloudflare Tunnel** (replaces LoadBalancer)
3. **Cloudflare for SaaS** (100 free custom domains)
4. **Self-hosted PostgreSQL** (in-cluster with daily backups)
5. **Single Redis instance** (Sentinel staged for multi-node)
6. **Cloudflare R2** (object storage)

**Result:** Infrastructure ready, but Enclii not deployed yet.

### Step 2: Bootstrap Enclii Control Plane (Week 2)

Deploy Enclii manually **one time** using raw Kubernetes manifests:

```bash
# Clone Enclii repository
git clone https://github.com/madfam-org/enclii
cd enclii

# Configure secrets
kubectl create secret generic enclii-secrets \
  --from-literal=database-url="postgres://..." \
  --from-literal=redis-url="redis://..." \
  --from-literal=r2-endpoint="https://..." \
  --from-literal=r2-access-key-id="..." \
  --from-literal=r2-secret-access-key="..." \
  -n enclii-platform

kubectl create secret generic jwt-secrets \
  --from-file=private-key=keys/rsa-private.pem \
  --from-file=public-key=keys/rsa-public.pem \
  -n enclii-platform

# Deploy control plane
kubectl apply -k infra/k8s/production

# Wait for readiness
kubectl wait --for=condition=ready pod -l app=switchyard-api -n enclii-platform --timeout=300s

# Verify
curl https://api.enclii.dev/health
# {"status": "ok"}
```

**Result:** Enclii control plane running, but not self-hosted yet.

### Step 3: Bootstrap Janua (Week 3)

Deploy Janua manually **one time**:

```bash
# Clone Janua repository
git clone https://github.com/madfam-org/janua
cd janua

# Configure secrets
kubectl create secret generic janua-secrets \
  --from-literal=database-url="postgres://..." \
  --from-literal=redis-url="redis://..." \
  --from-literal=session-secret="$(openssl rand -base64 32)" \
  --from-literal=smtp-host="smtp.sendgrid.net" \
  --from-literal=smtp-port="587" \
  --from-literal=smtp-user="apikey" \
  --from-literal=smtp-password="SG...." \
  -n enclii-platform

# Deploy Janua
kubectl apply -f ../enclii/dogfooding/janua.yaml

# Wait for readiness
kubectl wait --for=condition=ready pod -l app=janua -n enclii-platform --timeout=300s

# Verify
curl https://auth.enclii.dev/health
# {"status": "ok"}
```

**Result:** Janua running on Enclii infrastructure.

### Step 4: Configure Janua OAuth Clients (Week 3)

Create OAuth clients in Janua for Enclii:

```bash
# Create Enclii Web UI client (public)
curl -X POST https://auth.enclii.dev/v1/clients \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $JANUA_ADMIN_TOKEN" \
  -d '{
    "client_id": "enclii-web-ui",
    "client_name": "Enclii Web Dashboard",
    "redirect_uris": [
      "${APP_URL}/callback",
      "${DASHBOARD_URL}/callback",
      "http://localhost:3000/callback"
    ],
    "grant_types": ["authorization_code", "refresh_token"],
    "response_types": ["code"],
    "scope": "openid profile email",
    "token_endpoint_auth_method": "none",
    "application_type": "web"
  }'

# Create Enclii API client (confidential)
curl -X POST https://auth.enclii.dev/v1/clients \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $JANUA_ADMIN_TOKEN" \
  -d '{
    "client_id": "enclii-api",
    "client_name": "Enclii Control Plane API",
    "client_secret": "<generated-secret>",
    "grant_types": ["client_credentials"],
    "scope": "api:read api:write",
    "token_endpoint_auth_method": "client_secret_basic",
    "application_type": "service"
  }'
```

**Result:** Janua configured to authenticate Enclii.

### Step 5: Update Enclii to Use Janua (Week 4)

Update Switchyard API to validate Janua tokens:

```bash
# apps/switchyard-api/main.go
jwksProvider, _ := auth.NewJWKSProvider("https://auth.enclii.dev/.well-known/jwks.json")
jwtManager := auth.NewJWTManager(jwksProvider)

r.Use(jwtManager.AuthMiddleware())
```

Update Switchyard UI to use Janua OAuth:

```bash
# apps/switchyard-ui/lib/auth-config.ts
export const authConfig = {
  authority: 'https://auth.enclii.dev',
  client_id: 'enclii-web-ui',
  redirect_uri: '${APP_URL}/callback',
  scope: 'openid profile email',
  response_type: 'code',
}
```

**Result:** Enclii authenticates via Janua (but still deployed manually).

### Step 6: Migrate to Self-Service Deployment (Week 5)

Now the **critical transition**: Deploy Enclii components using **Enclii itself**.

```bash
cd enclii

# Create project in Enclii
./bin/enclii project create enclii-platform

# Import service specs
./bin/enclii service create --file dogfooding/switchyard-api.yaml
./bin/enclii service create --file dogfooding/switchyard-ui.yaml
./bin/enclii service create --file dogfooding/janua.yaml
./bin/enclii service create --file dogfooding/landing-page.yaml
./bin/enclii service create --file dogfooding/docs-site.yaml
./bin/enclii service create --file dogfooding/status-page.yaml

# Deploy everything via Enclii
./bin/enclii deploy --service switchyard-api --env production
./bin/enclii deploy --service switchyard-ui --env production
./bin/enclii deploy --service janua --env production
./bin/enclii deploy --service landing-page --env production
./bin/enclii deploy --service docs-site --env production
./bin/enclii deploy --service status-page --env production

# Verify all services
./bin/enclii services list
# NAME              STATUS     REPLICAS  AGE
# switchyard-api    Running    3/3       5m
# switchyard-ui     Running    2/2       5m
# janua            Running    3/3       5m
# landing-page      Running    2/2       5m
# docs-site         Running    2/2       5m
# status-page       Running    2/2       5m
```

**Result:** ✅ **Enclii deploys Enclii. Dogfooding complete!**

### Step 7: Enable Continuous Deployment (Week 5)

Configure GitHub webhooks so that **every push to main** triggers a deploy:

```yaml
# dogfooding/switchyard-api.yaml
spec:
  build:
    source:
      git:
        repository: https://github.com/madfam-org/enclii
        branch: main
        autoDeploy: true  # ⭐ Auto-deploy on push
```

**Workflow:**
1. Developer pushes to `main` branch
2. GitHub webhook notifies Enclii control plane
3. Enclii builds new image (with provenance)
4. Enclii creates new release (with SBOM)
5. Enclii deploys with canary strategy
6. If healthy after 5 minutes, promotes to 100%
7. If unhealthy, automatic rollback

**Result:** ✅ **Continuous deployment for Enclii itself.**

---

## The Confidence Signal

### What We Can Now Say

**To Customers:**
> "Enclii's entire production infrastructure runs on Enclii itself. Our control plane, web dashboard, authentication service, landing page, documentation, and status page are all deployed via `enclii deploy`. We've performed 200+ production deployments using our own platform. We're our own most demanding customer."

**To Investors:**
> "We dogfood our own product ruthlessly. Every feature we ship is battle-tested in our own production environment before customers see it. This ensures product quality and reduces support burden."

**To Engineering Candidates:**
> "You'll use Enclii every day to deploy your own work. It's not a side project—it's how we run our entire company."

### Sales Narrative

**Before Dogfooding:**
- Sales call: "Can Enclii handle production workloads?"
- Us: "Uh... we think so? Our test suite passes..."
- Customer: 😬

**After Dogfooding:**
- Sales call: "Can Enclii handle production workloads?"
- Us: "We run our entire production on Enclii. Here's our status page showing 99.95% uptime. We deploy 10-20 times per day with zero downtime. Want to see our deployment logs?"
- Customer: 🤝

### Authenticity Matters

Customers can **verify** our claims:

```bash
# Customer checks our public API
curl https://api.enclii.dev/health

# Customer checks Janua JWKS endpoint
curl https://auth.enclii.dev/.well-known/jwks.json

# Customer checks status page
curl https://status.enclii.dev
# Shows real uptime data for Enclii services
```

They can see we're not lying. We really do run on Enclii.

---

## Troubleshooting

### Issue: "Enclii API won't start after Janua integration"

**Symptoms:**
- Switchyard API returns 401 Unauthorized
- Logs show: "failed to fetch JWKS from Janua"

**Root Cause:**
- Janua not accessible from Switchyard API pods
- NetworkPolicy blocking traffic

**Fix:**
```bash
# Check NetworkPolicy
kubectl get netpol -n enclii-platform

# Verify Janua is reachable
kubectl exec -it -n enclii-platform deployment/switchyard-api -- \
  curl http://janua.enclii-platform.svc.cluster.local:8000/.well-known/jwks.json

# If blocked, update NetworkPolicy to allow egress to Janua
```

### Issue: "Circular dependency during bootstrap"

**Symptoms:**
- Can't deploy Enclii via Enclii (chicken-and-egg)

**Root Cause:**
- First deployment must be manual

**Fix:**
- Follow **Step 2: Bootstrap Enclii Control Plane** exactly
- Deploy manually **once**, then migrate to self-service
- Don't try to skip the bootstrap phase

### Issue: "Auto-deploy triggers too frequently"

**Symptoms:**
- Every commit triggers a deploy (even docs changes)
- Deploys happen during business hours (risky)

**Fix:**
```yaml
# dogfooding/switchyard-api.yaml
spec:
  build:
    source:
      git:
        autoDeploy: true
        deployFilter:
          paths:
            - "apps/switchyard-api/**"  # Only deploy on API changes
          excludePaths:
            - "**/*.md"  # Ignore docs
        deploySchedule:
          onlyAfter: "22:00 UTC"  # Only deploy after 10pm UTC
          onlyBefore: "06:00 UTC"  # Only deploy before 6am UTC
```

### Issue: "Janua tokens not validating"

**Symptoms:**
- User logs into Janua successfully
- Switchyard API rejects tokens with "invalid signature"

**Root Cause:**
- JWKS cache stale
- Clock skew between services

**Fix:**
```bash
# Check JWKS cache age
curl https://api.enclii.dev/debug/jwks/cache
# {"last_refresh": "2025-11-20T10:30:00Z", "next_refresh": "2025-11-20T10:45:00Z"}

# Force JWKS refresh
curl -X POST https://api.enclii.dev/debug/jwks/refresh \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Check clock skew
kubectl exec -it -n enclii-platform deployment/switchyard-api -- date
kubectl exec -it -n enclii-platform deployment/janua -- date
# Should be within 1-2 seconds
```

---

## Progress Tracker

### Phase 1: Infrastructure Setup ✅ COMPLETE
- [x] Provision Hetzner dedicated server (AX41-NVME)
- [x] Deploy Cloudflare Tunnel
- [x] Set up self-hosted PostgreSQL (in-cluster)
- [x] Deploy single Redis instance (Sentinel staged)
- [x] Configure Cloudflare R2

### Phase 2: Bootstrap Enclii ✅ COMPLETE
- [x] Deploy Switchyard API manually
- [x] Deploy Switchyard UI manually
- [x] Configure secrets and networking
- [x] Verify control plane health (api.enclii.dev/health → OK)

### Phase 3: Bootstrap Janua ✅ COMPLETE
- [x] Deploy Janua (auth.madfam.io)
- [x] Create OAuth clients for Enclii
- [x] Update Enclii to use Janua auth (OIDC mode)
- [x] Test full OAuth flow

### Phase 4: Self-Deployment Pipeline ✅ COMPLETE
- [x] Register services with auto_deploy: true
- [x] Configure GitHub webhook (ID: 585841923)
- [x] Implement webhook handler with HMAC verification
- [x] Build pipeline with release creation
- [x] K8s reconciliation integration

### Phase 5: Remaining Work 🔲 IN PROGRESS
- [x] Deploy landing page via Enclii
- [x] Implement status page
- [ ] Full end-to-end test with production push
- [ ] Load test to 1000 RPS
- [ ] Update sales materials with dogfooding narrative

---

## Conclusion

Dogfooding is **not optional**—it's a critical competitive advantage. By running Enclii on Enclii and authenticating with Janua, we:

1. **Validate our product** before customers do
2. **Build customer confidence** through authentic usage
3. **Improve product quality** by experiencing pain points first
4. **Enable powerful sales narratives** with real production metrics
5. **Align the team** around a shared experience

The service specs in `dogfooding/` are not toy examples—they're **production-ready configurations** that deploy our entire platform. Follow this guide to make Enclii its own best customer.

---

**Questions?** Open an issue or ask in #engineering on Slack.

**Ready to dogfood?** Start with [Step 1: Bootstrap Infrastructure](#step-1-bootstrap-infrastructure-week-1).

---

## Related Documentation

- **Getting Started**: [Quick Start Guide](/docs/getting-started/QUICKSTART) | [Development Guide](/docs/getting-started/DEVELOPMENT)
- **Architecture**: [Platform Architecture](/docs/architecture/ARCHITECTURE)
- **Infrastructure**: [Infrastructure Overview](/docs/infrastructure/) | [GitOps](/docs/infrastructure/GITOPS)
- **Production**: [Production Checklist](/docs/production/PRODUCTION_CHECKLIST) | [Deployment Roadmap](/docs/production/PRODUCTION_DEPLOYMENT_ROADMAP)
- **CLI**: [CLI Reference](/docs/cli/) | [Deploy Command](/docs/cli/commands/deploy)
- **Integrations**: [SSO Integration](/docs/integrations/sso) | [GitHub Integration](/docs/integrations/github)
- **Troubleshooting**: [Deployment Issues](/docs/troubleshooting/deployment-issues) | [Auth Problems](/docs/troubleshooting/auth-problems)
