# Enclii

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


> **Deploy, scale, and operate — on infrastructure you own.**
> *Open source DevOps platform with production-grade Kubernetes on Hetzner + Cloudflare.*

[![Production Readiness](https://img.shields.io/badge/status-production--running%20beta-yellowgreen)](./docs/production/PRODUCTION_CHECKLIST.md)
[![Infrastructure](https://img.shields.io/badge/infrastructure-Hetzner%20%2B%20Cloudflare-blue)](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md)
[![Auth](https://img.shields.io/badge/auth-OIDC%20%2F%20Janua%20SSO-success)](./docs/production/PRODUCTION_READINESS_AUDIT.md)
[![Cost](https://img.shields.io/badge/monthly%20cost-%2455-success)](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md)

**Status:** Production-running beta | [Production Checklist →](./docs/production/PRODUCTION_CHECKLIST.md)
**Authentication:** OIDC via Janua SSO (RS256 JWT) - **Integrated**
**Infrastructure:** Hetzner Dedicated + Cloudflare - **Running**

---

## What is Enclii?

Enclii is an **open source DevOps platform** for deploying, scaling, and operating containerized services with enterprise-grade security, GitOps automation, and zero vendor lock-in.

### Self-Hosted Production

> "We run our entire platform on Enclii, authenticated by Janua. We are our own most demanding customer."

**Production Services:**
- ✅ **Control Plane API** (`api.enclii.dev`)
- ✅ **Web Dashboard** (`app.enclii.dev`)
- ✅ **Admin Platform** (`admin.enclii.dev`)
- ✅ **Authentication** (`auth.madfam.io`) → Janua SSO (OIDC)
- ✅ **Documentation** (`docs.enclii.dev`)
- ✅ **Landing Page** (`enclii.dev`)
- ✅ **Status Page** (`status.enclii.dev`, `status.madfam.io`)

All services deploy via zero-touch onboarding — K8s manifests and CI workflows live in each repo, not here. [See onboarding guide →](./docs/guides/ONBOARDING_GUIDE.md)

### MADFAM Operations Doctrine

Enclii is the required control plane for MADFAM DevOps and provisioning:

- Enclii web, API, and CLI are mandatory for routine production provisioning, deployment, observability, domains, secrets, provider operations, scaling, rollback, and remediation.
- `enclii ops` replaces routine `kubectl`, ArgoCD, Longhorn, Kyverno, ExternalSecrets, Vault, and ARC manipulation.
- `enclii providers` replaces routine `gh`, Cloudflare, Porkbun, and Hetzner manipulation.
- Switchyard API is the agent-facing contract Selva and other agents should call.
- Raw `kubectl`, `helm`, SSH, provider CLIs/APIs, `docker exec`, and direct container access are allowed only for platform bootstrap or documented break-glass emergencies when Enclii is unavailable or lacks an implemented adapter.
- Missing adapter gaps must be recorded and remediated in Enclii rather than normalized as routine operator procedure.

---

## Key Features

### 🏗️ Production-Running Beta Infrastructure

**Current Hetzner Topology:**

| Surface | Role | Hardware |
|---------|------|----------|
| **The Sanctuary** | Production workloads | Hetzner dedicated server |
| **The Forge** | CI/CD builder capacity | Hetzner Cloud VPS |

**Cost-Optimized Stack:**
- **Cloudflare Tunnel** - Zero-trust ingress (replaces load balancers)
- **Cloudflare for SaaS** - 100 custom domains FREE
- **Cloudflare R2** - Zero-egress object storage
- **Self-hosted PostgreSQL** - In-cluster with persistent storage
- **Self-hosted Redis** - In-cluster caching (Sentinel ready for multi-node)

> **Builder Node Targeting**: Build workloads are isolated on "The Forge" via Kubernetes taints (`builder=true:NoSchedule`). Production apps run exclusively on "The Sanctuary".

> **Infrastructure Audit (Jan 2026)**: Evaluated Ubicloud managed PostgreSQL and Redis Sentinel. **Decision: NOT NEEDED** for 99.5% SLA / 24-hour RPO. Sentinel manifests staged for future multi-node deployment.

Self-hosted infrastructure is significantly cheaper than equivalent SaaS platforms, but the platform is still a production-running beta until the remaining blockers below are closed.

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
├── docs/                      # Documentation
└── examples/                  # Sample service specs
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

### Current Status: Production-Running Beta

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

**Remaining blockers before full production-ready status:**
- ⚠️ Load testing validation against expected production traffic and failure modes
- ⚠️ Final security audit with documented remediation status
- ⚠️ Documented backup restore drill for PostgreSQL, Redis, and critical platform state
- ⚠️ HA/multi-node expansion plan and failover runbook for components that still depend on single-instance capacity

[View production checklist →](./docs/production/PRODUCTION_CHECKLIST.md)

---

## Quick Start

### Deploy your first service (5 minutes)

If you just want to ship an app to Enclii, the fastest path is:

```bash
brew install enclii/tap/enclii   # or: curl -sSL https://get.enclii.dev | bash
enclii login
cd my-app && enclii init
enclii deploy
# → Live at https://dev.my-app.enclii.dev
```

**[Full 5-minute quickstart → docs.enclii.dev/quickstart](https://docs.enclii.dev/quickstart)**

Migrating from another platform?

- [From Vercel →](./docs/guides/migrating-from-vercel.md)
- [From Railway →](./docs/guides/migrating-from-railway.md)
- [From Heroku →](./docs/guides/migrating-from-heroku.md)

### Run your own Enclii cluster

This repository contains the full source for Enclii itself. If you're bootstrapping a self-hosted cluster (bare-metal or cloud), continue below. Most users don't need to do this — [app.enclii.dev](https://app.enclii.dev) is the hosted control plane.

<details>
<summary><b>Local development prerequisites</b></summary>

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

**NPM Registry Configuration**

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

See [NPM Registry](./docs/infrastructure/npm-registry.md) for details.

</details>

<details>
<summary><b>Local development (10 minutes)</b></summary>

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

[Detailed platform-contributor setup →](./docs/getting-started/QUICKSTART.md)

</details>

<details>
<summary><b>Self-hosted production deployment</b></summary>

See [Production Deployment Roadmap](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md) for the complete 8-week implementation plan.

**Bootstrap:**
```bash
# Provision Hetzner cluster
hcloud server create --name enclii-node-{1,2,3} --type cpx31

# Configure Cloudflare Tunnel
cloudflared tunnel create enclii-production

# Bootstrap-only: first cluster/platform install before Enclii can manage itself.
kubectl apply -k infra/k8s/production
```

</details>

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
- [5-minute Quickstart](./docs/quickstart.md) - Deploy your first service
- [Template Catalog](./docs/templates.md) - Starter templates by framework
- [Migration Index](./docs/guides/migrating.md) - From Vercel / Railway / Heroku
- [Onboarding Guide](./docs/guides/ONBOARDING_GUIDE.md) - Zero-touch repo onboarding
- [Platform Contributor Setup](./docs/getting-started/QUICKSTART.md) - Local dev in 10 minutes
- [Production Deployment Roadmap](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md) - 8-week plan

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

- ✅ Self-hosted (Enclii deploys itself)
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

## The Vision: Self-Hosted as Competitive Advantage

We run our entire production infrastructure on Enclii, authenticated by Janua.

When prospects ask **"Can Enclii handle production?"** — we answer with verifiable proof:
> "We run our entire production on Enclii. Here's our status page showing 99.95% uptime. We deploy 10-20 times per day with zero downtime using our own platform."

**Production services running today:**
- Control Plane API at api.enclii.dev
- Web Dashboard at app.enclii.dev
- Admin Platform at admin.enclii.dev
- Janua Auth at auth.madfam.io
- Status pages at status.enclii.dev, status.madfam.io

**Why this matters:**
- Customer confidence: "If they trust it, we can too"
- Product quality: We find bugs before customers do
- Sales credibility: Authentic production usage metrics

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
- **Onboarding Guide:** [ONBOARDING_GUIDE.md](./docs/guides/ONBOARDING_GUIDE.md)

---

**Questions?** Open an issue or contact the team at [engineering@enclii.dev](mailto:engineering@enclii.dev)

**Ready to deploy?** Start with [PRODUCTION_DEPLOYMENT_ROADMAP.md](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md) 🚀
