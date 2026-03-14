# Enclii Dogfooding Service Specs

> ⚠️ **PLANNED IMPLEMENTATION** - These are service specifications for **future deployment** (Weeks 5-6).
> **Current Status:** Specs are ready. Awaiting infrastructure (Weeks 1-2) and Janua integration (Weeks 3-4).
> **Not Yet Deployed:** These services are NOT yet running in production.

This directory contains **production-ready service specifications** for deploying Enclii's own infrastructure using Enclii itself.

## Why This Will Matter

> **Goal (Weeks 5-6):** "We'll run our entire platform on Enclii, authenticated by Janua. We'll be our own most demanding customer."

These service specs will demonstrate:
- 🔲 Enclii deploys Enclii (self-hosting) - PLANNED
- 🔲 Janua authenticates Enclii (eating our own auth solution) - PLANNED
- ✅ Multi-repo support (Enclii + Janua from separate GitHub repos) - SPECS READY
- ✅ Full production features (HA, autoscaling, monitoring, custom domains) - SPECS READY

## Service Specs

### Core Platform

- **`switchyard-api.yaml`** - Control plane REST API
  - Built from: `github.com/madfam-org/enclii`
  - Exposed at: `api.enclii.io`
  - 3 replicas (HA)
  - Autoscaling: 3-10 pods based on CPU/memory

- **`switchyard-ui.yaml`** - Web dashboard (Next.js)
  - Built from: `github.com/madfam-org/enclii`
  - Exposed at: `app.enclii.io`
  - 2 replicas
  - Autoscaling: 2-8 pods

### Janua (Authentication Platform)

- **`janua-api.yaml`** - Authentication API (OAuth/OIDC)
  - Built from: `github.com/madfam-org/janua` → `apps/api`
  - Exposed at: `api.janua.dev`
  - Port: 8000 (per PORT_REGISTRY.md)
  - 3 replicas (auth is critical)
  - Autoscaling: 3-10 pods

- **`janua-dashboard.yaml`** - Authentication Dashboard UI
  - Built from: `github.com/madfam-org/janua` → `apps/dashboard`
  - Exposed at: `app.janua.dev`
  - Port: 3002 (per PORT_REGISTRY.md)
  - 2 replicas
  - Autoscaling: 2-5 pods

- **`janua-landing.yaml`** - Janua Marketing Site
  - Built from: `github.com/madfam-org/janua` → `apps/landing`
  - Exposed at: `janua.dev`, `www.janua.dev`
  - Port: 3001 (per PORT_REGISTRY.md)
  - 2 replicas

### Public Services

- **`landing-page.yaml`** - Marketing site
  - Exposed at: `enclii.io`
  - Static Next.js export
  - Aggressive caching (24 hours)

- **`docs-site.yaml`** - Documentation
  - Exposed at: `docs.enclii.io`
  - Docusaurus or similar
  - Caching: 1 hour

- **`status-page.yaml`** - Public status page
  - Exposed at: `status.enclii.io`
  - Monitors all Enclii services
  - Connected to Prometheus

## How to Use

### Prerequisites

1. **Bootstrap infrastructure** (Hetzner dedicated server + Cloudflare)
2. **Deploy Enclii control plane manually** (one time)
3. **Configure secrets** in Kubernetes

See [DOGFOODING_GUIDE.md](../DOGFOODING_GUIDE.md) for full instructions.

### Deploy Services

```bash
# Create project
./bin/enclii project create enclii-platform

# Import service specs
./bin/enclii service create --file dogfooding/switchyard-api.yaml
./bin/enclii service create --file dogfooding/switchyard-ui.yaml
./bin/enclii service create --file dogfooding/janua-api.yaml
./bin/enclii service create --file dogfooding/janua-dashboard.yaml
./bin/enclii service create --file dogfooding/janua-landing.yaml
./bin/enclii service create --file dogfooding/landing-page.yaml
./bin/enclii service create --file dogfooding/docs-site.yaml
./bin/enclii service create --file dogfooding/status-page.yaml

# Deploy to production
./bin/enclii deploy --service switchyard-api --env production
./bin/enclii deploy --service switchyard-ui --env production
./bin/enclii deploy --service janua-api --env production
./bin/enclii deploy --service janua-dashboard --env production
./bin/enclii deploy --service janua-landing --env production
./bin/enclii deploy --service landing-page --env production
./bin/enclii deploy --service docs-site --env production
./bin/enclii deploy --service status-page --env production

# Check status
./bin/enclii services list
```

### Continuous Deployment

All services have `autoDeploy: true`, which means:

1. **Developer pushes to `main`** (either Enclii or Janua repo)
2. **GitHub webhook triggers Enclii**
3. **Enclii builds new image** (with provenance + SBOM)
4. **Enclii deploys with canary** (10% → 50% → 100%)
5. **Automatic rollback on failure** (if error rate > 2%)

## Architecture Highlights

### Multi-Repository Support

```yaml
# Enclii API (from Enclii repo)
source:
  git:
    repository: https://github.com/madfam-org/enclii
    branch: main

# Janua (from separate Janua repo)
source:
  git:
    repository: https://github.com/madfam-org/janua
    branch: main
```

This demonstrates Enclii can build from **any GitHub repository**, not just monorepos.

### Authentication Flow

```
User → app.enclii.io
  ↓
Redirect to auth.enclii.io (Janua)
  ↓
Login with password/SSO
  ↓
Janua issues RS256 JWT
  ↓
Redirect to app.enclii.io/callback
  ↓
Store tokens
  ↓
API requests to api.enclii.io
  ↓
Switchyard validates JWT via Janua JWKS
  ↓
✅ Authenticated
```

**Key point:** Enclii authenticates its own users with Janua. Total dogfooding.

### Infrastructure

- **Kubernetes:** Hetzner dedicated server (single-node k3s)
- **Ingress:** Cloudflare Tunnel (replaces LoadBalancer)
- **Database:** Self-hosted PostgreSQL in-cluster (daily backups to R2)
- **Cache:** Single Redis instance (Sentinel config staged for multi-node)
- **Storage:** Cloudflare R2 (SBOMs, artifacts)
- **DNS:** Cloudflare for SaaS (100 free domains)

> **Note:** Currently single-node. Longhorn CSI and Redis Sentinel configs are ready for multi-node scaling when needed.

**Cost:** See internal-devops for cost breakdown

## Secrets Required

Before deploying, create these secrets:

```bash
# Enclii secrets
kubectl create secret generic enclii-secrets \
  --from-literal=database-url="postgres://..." \
  --from-literal=redis-url="redis://..." \
  --from-literal=r2-endpoint="https://..." \
  --from-literal=r2-access-key-id="..." \
  --from-literal=r2-secret-access-key="..." \
  --from-literal=prometheus-url="http://prometheus.monitoring:9090" \
  -n enclii-platform

# JWT signing keys (RS256)
kubectl create secret generic jwt-secrets \
  --from-file=private-key=keys/rsa-private.pem \
  --from-file=public-key=keys/rsa-public.pem \
  -n enclii-platform

# Janua secrets
kubectl create secret generic janua-secrets \
  --from-literal=database-url="postgres://..." \
  --from-literal=redis-url="redis://..." \
  --from-literal=session-secret="$(openssl rand -base64 32)" \
  --from-literal=smtp-host="smtp.sendgrid.net" \
  --from-literal=smtp-port="587" \
  --from-literal=smtp-user="apikey" \
  --from-literal=smtp-password="SG...." \
  -n enclii-platform
```

## Monitoring

All services emit metrics to Prometheus:

- **Control Plane API:** `/metrics` endpoint
- **Web UI:** `/api/metrics` endpoint
- **Janua:** `/metrics` endpoint

Grafana dashboards available at: `grafana.enclii.io`

## Status Page

Public status page monitors:
- ✅ Control Plane API (`api.enclii.io/health`)
- ✅ Web Dashboard (`app.enclii.io/api/health`)
- ✅ Janua API (`api.janua.dev/health`)
- ✅ Janua Dashboard (`app.janua.dev/api/health`)
- ✅ Documentation (`docs.enclii.io`)

View at: https://status.enclii.io

## Next Steps

1. **Read [DOGFOODING_GUIDE.md](../DOGFOODING_GUIDE.md)** for full implementation guide
2. **Bootstrap infrastructure** (Week 1-2)
3. **Deploy Enclii manually** (Week 3)
4. **Deploy Janua** (Week 4)
5. **Migrate to self-service** (Week 5)

---

**Questions?** See [DOGFOODING_GUIDE.md](../DOGFOODING_GUIDE.md) or open an issue.
