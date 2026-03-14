# Enclii

> **Deploy, scale, and operate — on infrastructure you own.**
> *Open source DevOps platform with production-grade Kubernetes on Hetzner + Cloudflare.*

[![Production Readiness](https://img.shields.io/badge/production%20ready-95%25-brightgreen)](./docs/production/PRODUCTION_CHECKLIST.md)
[![Infrastructure](https://img.shields.io/badge/infrastructure-Hetzner%20%2B%20Cloudflare-blue)](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md)
[![Auth](https://img.shields.io/badge/auth-OIDC%20%2F%20Janua%20SSO-success)](./docs/production/PRODUCTION_READINESS_AUDIT.md)
[![Cost](https://img.shields.io/badge/monthly%20cost-%2455-success)](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md)

**Status:** Beta (95% production-ready) | [Production Checklist →](./docs/production/PRODUCTION_CHECKLIST.md)
**Authentication:** OIDC via Janua SSO (RS256 JWT) - **Integrated**
**Infrastructure:** Hetzner Dedicated + Cloudflare - **Running**

---

## What is Enclii?

Enclii is an **open source DevOps platform** for deploying, scaling, and operating containerized services with enterprise-grade security, GitOps automation, and zero vendor lock-in.

### The Dogfooding Strategy (In Progress)

> **Goal:** "We run our entire platform on Enclii, authenticated by Janua. We are our own most demanding customer."

**Current Production Services:**
- ✅ **Control Plane API** (`api.enclii.dev`) → Running on Enclii
- ✅ **Web Dashboard** (`app.enclii.dev`) → Running on Enclii
- ✅ **Authentication** (`auth.madfam.io`) → Janua SSO (OIDC)
- ✅ **Documentation** (`docs.enclii.dev`) → Running on Enclii
- 🔲 **Landing Page** (`enclii.dev`) → Pending
- 🔲 **Status Page** (`status.enclii.dev`) → Pending

**Current Status:** Core services are deployed and running in production. GitHub webhooks configured for CI/CD. Real build pipeline with Buildpacks/Dockerfile detection operational. [See dogfooding guide →](./docs/guides/DOGFOODING_GUIDE.md)

---

## Key Features

### 🏗️ Production-Ready Infrastructure

**2-Node Hetzner Cluster:**

| Node | Role | Hardware |
|------|------|----------|
| **The Sanctuary** | Production Workloads | Hetzner dedicated server |
| **The Forge** | CI/CD Builder | Hetzner Cloud VPS |

**Cost-Optimized Stack:**
- **Cloudflare Tunnel** - Zero-trust ingress (replaces load balancers)
- **Cloudflare for SaaS** - 100 custom domains FREE
- **Cloudflare R2** - Zero-egress object storage
- **Self-hosted PostgreSQL** - In-cluster with persistent storage
- **Self-hosted Redis** - In-cluster caching (Sentinel ready for multi-node)

> **Builder Node Targeting**: Build workloads are isolated on "The Forge" via Kubernetes taints (`builder=true:NoSchedule`). Production apps run exclusively on "The Sanctuary".

> **Infrastructure Audit (Jan 2026)**: Evaluated Ubicloud managed PostgreSQL and Redis Sentinel. **Decision: NOT NEEDED** for 99.5% SLA / 24-hour RPO. Sentinel manifests staged for future multi-node deployment.

Self-hosted infrastructure is significantly cheaper than equivalent SaaS platforms.

[View infrastructure details →](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md)

### 🔐 Authentication & Security (Production Ready)

**OIDC/OAuth 2.0 - Full Implementation:**
- ✅ **Janua SSO Integration** - Self-hosted OAuth 2.0/OIDC provider
- ✅ **RS256 JWT Signing** - RSA 2048-bit keys with JWKS rotation
- ✅ **External JWKS Validation** - Federated identity support
- ✅ **PKCE Support** - Secure authorization code flow
- ✅ **Token Refresh** - Automatic refresh with rotation

**Social & Enterprise Auth:**
- ✅ **GitHub OAuth** - Repository imports with linked accounts
- ✅ **Google OAuth** - One-click sign-in
- ✅ **Microsoft OAuth** - Azure AD integration ready
- ✅ **SAML 2.0 SSO** - Enterprise IdP support via Janua

**Access Control:**
- ✅ **RBAC** - Admin/Developer/Viewer roles with granular permissions
- ✅ **Multi-Tenant Organizations** - Namespace isolation per tenant
- ✅ **API Keys** - Service accounts for CI/CD pipelines
- ✅ **Session Management** - Redis-backed with secure cookies

**Security Hardening:**
- ✅ **Rate Limiting** - 1,000-10,000 req/min tiers
- ✅ **Security Headers** - HSTS, CSP, X-Frame-Options
- ✅ **Audit Logging** - Immutable security event trail
- ✅ **RP-Initiated Logout** - Full SSO session termination

**Cost Advantage:**
- ✅ **$0/month** vs Auth0 ($220+) or Clerk ($300+)
- ✅ **No per-MAU pricing** - Unlimited users
- ✅ **No vendor lock-in** - Own your auth infrastructure

[View auth architecture →](./docs/architecture/ARCHITECTURE.md)

### 🚀 Multi-Tenant SaaS Ready

**Cloudflare for SaaS** enables unlimited custom domains:
- ✅ First **100 domains FREE**
- ✅ $0.10/domain after that
- ✅ Auto-provisioned SSL in ~30 seconds
- ✅ No cert-manager rate limits
- ✅ No Kubernetes overhead

**Perfect for:** SaaS platforms serving multiple customers with custom domains.

### 📦 Complete Feature Set

**Developer Experience:**
- Intuitive CLI (`enclii init`, `enclii up`, `enclii deploy`)
- Auto-detect buildpacks (Nixpacks, Buildpacks, Dockerfile)
- Preview environments on every PR
- Real-time log streaming

**Security & Compliance:**
- RS256 JWT authentication with RSA signing
- RBAC with admin/developer/viewer roles
- Rate limiting (1,000-10,000 req/min)
- Security headers (HSTS, CSP, X-Frame-Options)
- Audit logging with immutable trail
- Image signing (Cosign) + SBOM (CycloneDX)

**Operations:**
- Canary deployments with auto-rollback
- Blue-green deployment strategy
- Horizontal pod autoscaling (HPA)
- Redis caching with tag-based invalidation
- PgBouncer connection pooling
- Prometheus + Grafana monitoring

**Multi-Tenancy:**
- NetworkPolicies (zero-trust networking)
- ResourceQuotas per tenant
- Per-tenant metrics and logging
- Cost tracking and showback

---

## Architecture

### Repository Structure (Monorepo)

```
enclii/
├── apps/
│   ├── switchyard-api/        # Control plane API (Go)
│   ├── switchyard-ui/         # Web dashboard (Next.js)
│   └── roundhouse/            # Build workers (Go)
├── packages/
│   └── cli/                   # `enclii` CLI (Go)
├── infra/
│   ├── k8s/                   # Kubernetes manifests
│   │   ├── base/              # Core infrastructure
│   │   ├── staging/           # Staging overlays
│   │   └── production/        # Production overlays
│   └── terraform/             # Infrastructure as Code
├── dogfooding/                # ⭐ Service specs for self-hosting
│   ├── switchyard-api.yaml    # Control plane (from this repo)
│   ├── switchyard-ui.yaml     # Web UI (from this repo)
│   ├── janua.yaml             # Auth (from github.com/madfam-org/janua)
│   ├── landing-page.yaml      # Marketing site
│   ├── docs-site.yaml         # Documentation
│   └── status-page.yaml       # Status monitoring
├── docs/                      # Documentation
├── examples/                  # Sample service specs
└── DOGFOODING_GUIDE.md        # Self-hosting strategy
```

### Component Names

**Production Names** (all railroad-themed 🚂):
- **Switchyard** - Control plane API
- **Conductor** - CLI (`enclii` command)
- **Roundhouse** - Build/provenance/signing workers
- **Junctions** - Ingress/routing/DNS/TLS
- **Timetable** - Cron jobs and scheduled tasks
- **Lockbox** - Secrets management (Vault client + ESO in production)
- **Signal** - Observability (implemented: `/v1/observability/*`, Prometheus + Grafana)
- **Waybill** - Infrastructure cost metering and usage showback

---

## Production Readiness

### Current Status: 95% Ready (Beta)

From [PRODUCTION_CHECKLIST.md](./docs/production/PRODUCTION_CHECKLIST.md):

**Infrastructure (Complete):**
- ✅ Hetzner Cloud k3s cluster running
- ✅ Cloudflare Tunnel integration
- ✅ PostgreSQL with health checks
- ✅ Redis for caching/sessions
- ✅ NetworkPolicies for zero-trust

**Authentication (Complete):**
- ✅ OIDC via Janua SSO (RS256 JWT)
- ✅ External JWKS validation
- ✅ GitHub OAuth linked accounts
- ✅ RBAC with role-based access

**Build Pipeline (Complete):**
- ✅ GitHub webhook CI/CD
- ✅ Buildpacks/Dockerfile detection
- ✅ Container registry push (ghcr.io)
- ✅ Real deployments (not simulated)

**Remaining (5%):**
- ⚠️ Load testing validation
- ⚠️ Final security audit

[View production checklist →](./docs/production/PRODUCTION_CHECKLIST.md)

---

## Quick Start

### Prerequisites

**Core:**
- Docker ≥ 24
- kubectl ≥ 1.29
- kind ≥ 0.23 (for local dev)
- Helm ≥ 3.14

**Languages:**
- Go ≥ 1.22
- Node.js ≥ 20
- pnpm ≥ 9

**macOS:**
```bash
brew install go node pnpm kind helm kubectl docker
```

### NPM Registry Configuration

Enclii uses MADFAM's private npm registry for internal packages. Configure your `.npmrc`:

```bash
# Add to your project's .npmrc or ~/.npmrc
@madfam:registry=https://npm.madfam.io
@enclii:registry=https://npm.madfam.io
@janua:registry=https://npm.madfam.io

# Auth token only needed for publishing (not for installing @enclii/* or @janua/* packages)
//npm.madfam.io/:_authToken=${NPM_MADFAM_TOKEN}
```

`@enclii/*` and `@janua/*` packages have public read access — no token required for `npm install`. The `NPM_MADFAM_TOKEN` is only needed for publishing or installing private scopes (`@madfam/*`, `@dhanam/*`, etc.).

**Note:** Enclii hosts the npm.madfam.io registry via Verdaccio. See [NPM Registry](./docs/infrastructure/npm-registry.md) for details.

### Local Development (10 minutes)

```bash
# 1. Clone and bootstrap
git clone https://github.com/madfam-org/enclii
cd enclii
make bootstrap  # Install dependencies

# 2. Start local Kubernetes
make kind-up         # Create kind cluster
make infra-dev       # Install NGINX Ingress, cert-manager, Prometheus
make dns-dev         # Configure dev DNS

# 3. Run the platform
make run-switchyard  # Control plane API on :8001
make run-ui          # Web UI on http://localhost:8030

# 4. Try the CLI
make build-cli
./bin/enclii init                  # Scaffold a service
./bin/enclii up                    # Deploy preview environment
./bin/enclii deploy --env prod     # Deploy to production
./bin/enclii logs api -f           # Tail logs
```

[View detailed setup →](./docs/getting-started/QUICKSTART.md)

### Production Deployment

See [Production Deployment Roadmap](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md) for the complete 8-week implementation plan.

**Bootstrap (Week 1-2):**
```bash
# Provision Hetzner cluster
hcloud server create --name enclii-node-{1,2,3} --type cpx31

# Configure Cloudflare Tunnel
cloudflared tunnel create enclii-production

# Deploy infrastructure
kubectl apply -k infra/k8s/production
```

**Dogfooding (Week 5-6):**
```bash
# Import service specs
./bin/enclii service create --file dogfooding/switchyard-api.yaml
./bin/enclii service create --file dogfooding/janua.yaml

# Deploy via Enclii itself
./bin/enclii deploy --service switchyard-api --env production
./bin/enclii deploy --service janua --env production

# ✅ Enclii now deploys Enclii!
```

---

## CLI Reference

```bash
enclii init              # Scaffold a new service from template
enclii up                # Build & deploy current branch (preview)
enclii deploy            # Deploy to production with canary
enclii logs <service>    # Stream logs
enclii ps                # List services, versions, health
enclii scale             # Configure autoscaling
enclii secrets set       # Manage secrets
enclii rollback          # Revert to previous release
enclii auth login        # Authenticate via Janua OAuth
```

**Common workflows:**

```bash
# Deploy with canary strategy
enclii deploy --env prod --strategy canary --wait

# Set secrets
enclii secrets set DATABASE_URL=postgres://... --env prod

# Custom domain
enclii routes add --host api.example.com --service api --env prod

# Scale to 5 replicas
enclii scale --min 5 --max 10 --service api --env prod
```

---

## Documentation

**📚 [Complete Documentation Index →](./docs/README.md)**

**Getting Started:**
- [Production Deployment Roadmap](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md) - 8-week plan
- [Production Readiness Audit](./docs/production/PRODUCTION_READINESS_AUDIT.md) - Current state
- [Dogfooding Guide](./docs/guides/DOGFOODING_GUIDE.md) - Self-hosting strategy
- [Quick Start](./docs/getting-started/QUICKSTART.md) - Local dev in 10 minutes

**Architecture:**
- [Architecture Overview](./docs/architecture/ARCHITECTURE.md) - System design
- [API Documentation](./docs/architecture/API.md) - REST API reference
- [Development Guide](./docs/getting-started/DEVELOPMENT.md) - Contributing guide

**Infrastructure (Jan 2026):**
- [GitOps with ArgoCD](./docs/infrastructure/GITOPS.md) - App-of-Apps pattern, self-heal
- [Storage with Longhorn](./docs/infrastructure/STORAGE.md) - Replicated CSI storage
- [Cloudflare Integration](./docs/infrastructure/CLOUDFLARE.md) - Zero-trust tunnel routing
- [External Secrets](./docs/infrastructure/EXTERNAL_SECRETS.md) - Secret synchronization

**Audits & Reports:**
- [Audit Navigation](./docs/audits/README.md) - Browse all audit reports
- [Master Audit Report](./docs/audits/MASTER_REPORT.md) - Comprehensive overview

**Operations:**
- [Deployment Guide](./infra/DEPLOYMENT.md) - Production ops
- [Secrets Management](./infra/SECRETS_MANAGEMENT.md) - Lockbox integration

---

## Key Differentiators

### vs Railway ($2,000+/month)

| Feature | Railway | Enclii |
|---------|---------|--------|
| **Cost** | Expensive | **Self-hosted (fraction of SaaS cost)** |
| **Custom Domains** | Limited, expensive | **100 FREE** (Cloudflare for SaaS) |
| **Vendor Lock-In** | Full lock-in | **None** (portable Kubernetes) |
| **Auth** | Bring your own (expensive) | **Janua included** |
| **Bandwidth** | Expensive egress | **Zero egress** (Cloudflare R2) |
| **Multi-Tenancy** | Not designed for it | **Built-in** (NetworkPolicies, quotas) |
| **Self-Hosting** | Impossible | **Fully self-hosted** |

### vs Vercel + Clerk

| Feature | Vercel + Clerk | Enclii |
|---------|----------------|--------|
| **Cost** | Expensive | **Self-hosted (fraction of cost)** |
| **Backend Support** | Limited (Functions) | **Full container support** |
| **Database** | Bring your own | **Self-hosted PostgreSQL included** |
| **Auth** | Clerk (expensive) | **Janua included** |
| **Control** | SaaS (no control) | **Full control** (self-hosted) |

### The Self-Hosted Advantage

**Why self-hosted infrastructure matters:**

1. **Cost Control** - Fraction of equivalent SaaS cost
2. **No Vendor Lock-In** - Portable Kubernetes, standard tools
3. **Data Sovereignty** - Your infrastructure, your rules
4. **Unlimited Scale** - No artificial SaaS limits
5. **Self-Hosted Auth** - No Auth0/Clerk dependency
6. **Custom Compliance** - Meet any regulatory requirement

---

## Roadmap

### Phase 1: Foundation (Complete - 100%)

- ✅ Control plane API (Switchyard)
- ✅ CLI (`enclii init/up/deploy/logs`)
- ✅ Web UI (Next.js dashboard)
- ✅ JWT authentication (RS256)
- ✅ RBAC (admin/developer/viewer)
- ✅ Preview environments
- ✅ Kubernetes reconciliation (embedded in control plane)
- ✅ Cloudflare Tunnel integration
- ✅ Redis caching

### Phase 2: Janua Integration (Complete - 100%)

- ✅ OIDC/JWKS provider via Janua
- ✅ External JWKS validation
- ✅ OAuth 2.0 handlers
- ✅ Frontend OIDC integration
- ✅ Janua running at auth.madfam.io
- ✅ GitHub OAuth linked accounts

### Phase 3: Production (Current - 95%)

- ✅ Dogfooding (Enclii deploys itself)
- ✅ Real build pipeline (Buildpacks/Dockerfile)
- ✅ GitHub webhook CI/CD
- ✅ Container registry push (ghcr.io)
- ✅ ArgoCD GitOps deployment (Jan 2026)
- ✅ Longhorn CSI storage (Jan 2026)
- ✅ Cloudflare tunnel route automation (Jan 2026)
- ⚠️ Load testing (1,000 RPS) - pending
- ⚠️ Final security audit - pending

### Phase 4: GA (Upcoming)

- Multi-region deployments
- KEDA autoscaling (custom metrics)
- Infrastructure cost showback and budget alerts
- Policy-as-code gates (OPA)
- Cron jobs and scheduled tasks
- SOC 2 compliance documentation

[View production checklist →](./docs/production/PRODUCTION_CHECKLIST.md)

---

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for the full guide. Quick checklist:

1. Read [CLAUDE.md](./CLAUDE.md) for project conventions
2. Run `make precommit` before pushing
3. Use conventional commits for changelog
4. Open draft PR early for feedback

---

## Security

**Supply Chain Security:**
- SBOM generation (CycloneDX format)
- Image signing (Cosign with RSA keys)
- Base image rotation every 30 days
- Vulnerability scanning (Trivy)

**Runtime Security:**
- Zero-trust networking (NetworkPolicies)
- Non-root containers (UID 65532)
- Read-only root filesystem
- Dropped Linux capabilities
- Seccomp profiles enabled

**Responsible Disclosure:**
Email: [security@enclii.dev](mailto:security@enclii.dev)

---

## The Vision: Dogfooding as Competitive Advantage

**Goal (Weeks 5-8):** Run our entire production infrastructure on Enclii, authenticated by Janua.

When we launch, prospects will ask **"Can Enclii handle production?"**

We'll answer with verifiable proof:
> "We run our entire production on Enclii. Here's our status page showing 99.95% uptime. We deploy 10-20 times per day with zero downtime using our own platform."

**What we're building (service specs ready in `dogfooding/`):**
- Control Plane API at api.enclii.dev
- Web Dashboard at app.enclii.dev
- Janua Auth at auth.janua.dev
- Public status page at status.enclii.dev

**Why this matters:**
- Customer confidence: "If they trust it, we can too"
- Product quality: We'll find bugs before customers do
- Sales credibility: Authentic production usage metrics

[See complete dogfooding plan →](./docs/guides/DOGFOODING_GUIDE.md)

---

## License

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)

This project is licensed under the **GNU Affero General Public License v3.0** (AGPL-3.0) to protect the sovereignty of the infrastructure and ensure that all modifications remain open source when deployed as a network service.

**Copyright (C) 2025 Innovaciones MADFAM SAS de CV**

This program is free software: you can redistribute it and/or modify it under the terms of the GNU Affero General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License along with this program. If not, see [LICENSE](./LICENSE) or visit https://www.gnu.org/licenses/agpl-3.0.html.

### Why AGPL-3.0?

The AGPL-3.0 license ensures that:

- **Network Copyleft**: Anyone running a modified version of Enclii as a network service must provide the source code to users
- **Infrastructure Sovereignty**: No vendor can take this code, modify it, and offer it as a proprietary service without sharing improvements
- **Community Protection**: All improvements and modifications must be contributed back to the community
- **Freedom Preservation**: Users retain the freedom to study, modify, and distribute the software

This aligns with the **MADFAM Manifesto Section IV**: protecting open infrastructure from proprietary capture.

---

## For AI Agents

This repository includes machine-readable context files following the [llmstxt.org](https://llmstxt.org) spec:

- **[llms.txt](./llms.txt)** — Compact overview with links to all key documentation
- **[llms-full.txt](./llms-full.txt)** — Full inline context including architecture, commands, debugging, and infrastructure details

---

## Links

- **Website:** [enclii.dev](https://enclii.dev)
- **Documentation:** [docs.enclii.dev](https://docs.enclii.dev)
- **Status Page:** [status.enclii.dev](https://status.enclii.dev)
- **Janua (Auth):** [janua.dev](https://janua.dev) | [GitHub](https://github.com/madfam-org/janua)
- **Production Roadmap:** [PRODUCTION_DEPLOYMENT_ROADMAP.md](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md)
- **Dogfooding Guide:** [DOGFOODING_GUIDE.md](./docs/guides/DOGFOODING_GUIDE.md)

---

**Questions?** Open an issue or contact the team at [engineering@enclii.dev](mailto:engineering@enclii.dev)

**Ready to deploy?** Start with [PRODUCTION_DEPLOYMENT_ROADMAP.md](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md) 🚀
