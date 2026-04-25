# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Enclii is an open source DevOps platform for deploying, scaling, and operating containerized services with enterprise-grade security, GitOps automation, and zero vendor lock-in.

**Current Status:** 🟢 v0.1.0 - Production Beta (95% ready) ([checklist](./docs/production/PRODUCTION_CHECKLIST.md))
**Infrastructure:** Hetzner Dedicated (3-node k3s) + Cloudflare - **Running**
**Authentication:** OIDC via Janua SSO (RS256 JWT) - **Integrated**
**Self-Hosted:** Core services deployed ([api.enclii.dev](https://api.enclii.dev), [app.enclii.dev](https://app.enclii.dev))
**Build Pipeline:** GitHub webhook CI/CD with Buildpacks - **Operational**
**GitOps:** ArgoCD App-of-Apps (28 apps across 22 namespaces) with self-heal - **Operational** (Jan 2026)
**Storage:** Longhorn CSI v1.7.2 (17 volumes, 2-replica across nodes) - **Operational** (Jan 2026)
**Last Audit:** Apr 25, 2026 — Stability remediation session: emergency-deploy GHCR auth + multi-arch digest pipeline restored (#136, #137); ARC custom runner image rolled out (#138); ApplicationSet madlab dedup (#140); Selva cutover Layer 1 (#141); ARC max-replicas baseline 2→12 (#143); bitnami/kubectl → bitnamilegacy purge across 5 CronJobs (#144). 15 owner decisions captured in [internal-devops/decisions/2026-04-25-owner-decisions-log.md](https://github.com/madfam-org/internal-devops/blob/main/decisions/2026-04-25-owner-decisions-log.md). Earlier: Apr 7, 2026 — Production 530 recovery, S110 remediation ([report](./docs/infrastructure/INFRA_ANATOMY.md)) ([capacity](./docs/infrastructure/CAPACITY_ROADMAP.md))

### Port Allocation

Per [PORT_ALLOCATION.md](https://github.com/madfam-org/solarpunk-foundry/blob/main/docs/PORT_ALLOCATION.md), Enclii uses the 4200-4299 block.

| Service | Port | Container | Public Domain |
|---------|------|-----------|---------------|
| Switchyard API | 4200 | enclii-api | api.enclii.dev |
| Web UI | 4201 | enclii-ui | app.enclii.dev |
| Agent (Planned) | 4202 | enclii-agent | - |
| Dispatch | 4203 | dispatch | admin.enclii.dev |
| Status Page | 4204 | enclii-status | status.enclii.dev, status.madfam.io |
| Metrics | 4290 | enclii-metrics | - |

### Quick Start (Production Deployment)
```bash
# 1. Configure credentials
cp infra/terraform/terraform.tfvars.example infra/terraform/terraform.tfvars
# Edit terraform.tfvars with your Hetzner/Cloudflare credentials

# 2. Deploy infrastructure
./scripts/deploy-production.sh check    # Validate config
./scripts/deploy-production.sh init     # Initialize Terraform
./scripts/deploy-production.sh plan     # Review changes
./scripts/deploy-production.sh apply    # Create infrastructure
./scripts/deploy-production.sh kubeconfig    # Get cluster access
./scripts/deploy-production.sh post-deploy   # Setup tunnel & namespaces
./scripts/deploy-production.sh status   # Verify deployment
```

## Architecture

The project follows a monorepo structure with these key components:

- **Switchyard**: Control plane API (Go) - manages projects, environments, services, deployments
- **Conductor (CLI)**: Developer interface (`enclii` command) (Go)
- **Roundhouse**: Build/provenance/signing workers (Go)
- **UI**: Web interface (Next.js)
- **Dispatch**: Admin control platform (Next.js) - fleet management, topology visualization
- **Waybill**: Infrastructure cost metering and usage showback (Go). Customer billing handled by Dhanam.
- **Functions**: Serverless functions with scale-to-zero - **API + UI complete**, KEDA operator ArgoCD app staged (pending cluster deploy)
- **Junctions**: Routing/ingress + certs + DNS - **Implemented** (Session 103)
- **Timetable**: Cron and one-off jobs - **Implemented** (Session 103)
- **Lockbox**: Internal — Vault client + rotation controller in switchyard-api; ESO handles K8s secrets in production
- **Signal**: Implemented — `/v1/observability/*` endpoints, Prometheus + Grafana deployed

## Common Development Commands

### Setup & Bootstrap
```bash
make bootstrap          # Install hooks, dependencies, and configure workspaces
make kind-up           # Create local kind cluster
make infra-dev         # Install ingress, cert-manager, observability stack
make dns-dev           # Configure dev DNS entries
```

### Running Services Locally
```bash
make run-switchyard    # Start control plane API on :8080
make run-ui            # Start web UI on :3000
```

### Building
```bash
make build-all         # Build all components
make build-cli         # Build CLI only
```

### Testing & Quality
```bash
make test              # Run unit tests
make e2e               # Run end-to-end tests
make lint              # Run linters (golangci-lint, eslint, prettier)
make precommit         # Run all checks before committing
```

### CLI Operations
```bash
./bin/enclii init                  # Scaffold a new service
./bin/enclii up                     # Deploy preview environment
./bin/enclii deploy --env prod     # Deploy to production
./bin/enclii logs <service> -f     # Tail service logs
./bin/enclii rollback <service>    # Rollback to previous release
./bin/enclii onboard --repo org/name --db-name mydb --secrets-file .env  # Full project onboarding
```

## Key Technical Details

### Service Configuration
Services are defined using YAML specs (stored versioned in the control plane):
- Located at: Service spec embedded in control plane DB
- Format: `apiVersion: enclii.dev/v1`
- Includes: runtime config, health checks, routes, secrets, volumes, jobs, autoscaling

### Deployment Flow
1. Build via Nixpacks/Buildpacks or Dockerfile
2. Parse `enclii.yaml` — auto-provision custom domains (tunnel routes + DNS CNAMEs)
3. Create immutable Release with provenance (git SHA, SBOM, signature)
4. Deploy with canary/blue-green strategies
5. Automatic rollback on failure based on SLO metrics

### Environment Variables
Key vars for local development (set in `.env`):
- `ENCLII_DB_URL`: Control plane database URL
- `ENCLII_REGISTRY`: Container registry
- `ENCLII_OIDC_ISSUER`: Auth provider URL
- `ENCLII_DEFAULT_REGION`: Default deployment region
- `ENCLII_LOG_LEVEL`: Logging verbosity

### Testing Strategy
- **Unit tests**: Test control plane validation, CLI arguments, reconciler idempotency
- **Integration tests**: Test build→release→deploy pipeline, secret injection, TLS issuance
- **E2E tests**: Test preview environment creation, canary deployments, rollbacks, cron jobs

### Platform SLOs
- Control plane API availability: 99.95% monthly
- Build subsystem availability: 99.9% monthly
- Preview environment provisioning: P95 < 3 minutes
- Deploy to stage: P95 ≤ 8 minutes for typical Node/Go services

## Important Conventions

### Security
- Never commit secrets - use Lockbox/Vault
- All images must be signed with cosign
- SBOM required for all releases
- Admission policies enforced via Kyverno/OPA

### NetworkPolicy Architecture
- **Platform policies** live in `infra/k8s/policies/` — only for enclii core infrastructure (enclii, janua, data, keda, vault, logging, status, arc)
- **Ecosystem app policies** are defined in each repo's `enclii.yaml` `network:` section, generated by the control plane's netpolicy generator (`apps/switchyard-api/internal/netpolicy/generator.go`), and applied directly to the cluster via K8s API during onboarding
- **Supported egress types**: `dns`, `https`, `http`, `postgres`, `redis`, `janua`, `pgbouncer`
- **NEVER add ecosystem-specific NetworkPolicies to the enclii repo** — this violates the zero-touch onboarding policy
- **NEVER add ecosystem-specific ExternalSecrets to the enclii repo** — ExternalSecrets for ecosystem apps belong in each repo's own `infra/k8s/` directory (self-provisioning pattern). The 18 ExternalSecret files in `infra/k8s/base/external-secrets/vault-secrets/` are platform infrastructure only
- ArgoCD `network-policies` app has `prune: false` — removing files from git does NOT delete live cluster resources

### Git Workflow
- Trunk-based development on `main`
- Conventional commits for changelog generation
- Preview environments created automatically for PRs
- Canary deploys to stage, then manual approval to prod

### Error Handling
- Exit codes: 0 (success), 10 (validation), 20 (build failed), 30 (deploy failed), 40 (timeout), 50 (auth)
- Precise error messages with actionable context
- Automatic rollback triggers when error rate > 2% for 2 minutes

### Cost Management
- Resource usage tracked per project/environment/service
- Budget alerts at 80% threshold
- Hard throttle at 100% for non-production environments

### CLI Preference
- **ALWAYS prefer `enclii` CLI over `kubectl`** for operational tasks — the CLI is the canonical interface for both internal operations and client platforms:
  - Service status: `enclii ps` (not `kubectl get pods`)
  - Logs: `enclii logs <service> -f` (not `kubectl logs`)
  - Deployments: `enclii deploy --env <env>` (not `kubectl set image`)
  - Rollbacks: `enclii rollback <service>` (not `kubectl rollout undo`)
  - Secrets: `enclii secrets set KEY=VALUE --secret` (not `kubectl create secret`)
  - Domains/routing: `enclii domains add <domain>` (not manual tunnel config)
  - Onboarding: `enclii onboard --repo org/name` (not manual namespace/ArgoCD setup)
- **Use `kubectl` only when** no enclii CLI equivalent exists:
  - Kyverno PolicyExceptions and raw CRD management
  - ArgoCD application sync/patch operations
  - Storage/PVC operations, node management
  - Direct pod debugging (`kubectl exec`, `kubectl port-forward`)
  - Janua DB operations (no enclii equivalent)
- **Rationale**: The enclii CLI routes through the Switchyard API, providing audit logging, lifecycle event tracking, service-scoped context, and consistent behavior across all platforms that consume the CLI

## Production Infrastructure

### Current Production Stack

Enclii runs on a 3-node k3s cluster (2 dedicated servers + 1 builder VPS).

> **Operational details** (server IPs, hardware specs, costs, SSH access): see `madfam-org/internal-devops` (private repo).

**Compute & Kubernetes (3-node cluster):**
- **foundry-cp** - Control plane + primary workload node (Hetzner EX44: i5-13500, 14C/20T, 128GB, 2x512GB NVMe Gen4) — provisioned 2026-04-08
- **foundry-worker-01** - Worker node + Longhorn 2nd replica target (Hetzner AX41-NVMe: Ryzen 5 3600, 64GB)
- **foundry-builder-01** - CI builds only (Hetzner VPS: 2 vCPU, 4GB, taint: builder=true:NoSchedule)
- **k3s v1.33.7+k3s3** - Lightweight Kubernetes (all nodes must match k3s version)
- **Cloudflare Tunnel** - Zero-trust ingress (replaces LoadBalancer)

> **Note:** 3-node cluster since Apr 2026. Control-plane migrated from AX41 to EX44 on 2026-04-08. foundry-cp runs k3s server + production workloads; foundry-worker-01 handles spillover workloads and serves as Longhorn 2nd replica target. Builder node runs only ARC GitHub Actions runners. Longhorn CSI operates in 2-replica mode for storage redundancy across dedicated nodes.

**Ingress Architecture (Cloudflare Tunnel):**
```
Internet → Cloudflare Edge → cloudflared pods → K8s Service:80 → Container:4xxx
           (TLS, DDoS)        (2 replicas)       (ClusterIP)      (targetPort)
```
- Zero exposed node ports (all traffic through tunnel)
- Zero-downtime RollingUpdate deployments
- NetworkPolicy isolation per namespace
- Configuration: `infra/k8s/production/cloudflared-unified.yaml`

**Port Mapping Hierarchy** (Critical for tunnel configuration):
1. **Container Port**: What the app listens on (e.g., 4200, 4201, 4204)
2. **K8s Service Port**: What the service exposes (port 80)
3. **Cloudflare Route**: Must point to K8s Service port (80), NOT container port

> See `infra/DEPLOYMENT.md` for complete Service Routing table.

**Database & Caching:**
- **Self-hosted PostgreSQL** - In-cluster deployment with PVC storage, daily backups to R2
- **Self-hosted Redis** - Single instance in-cluster (Sentinel config ready for multi-node)

**Storage & Networking:**
- **Cloudflare R2** - Zero-egress object storage (SBOMs, artifacts)
- **Cloudflare for SaaS** - First 100 custom domains FREE

**GitOps & Orchestration (Deployed Jan 2026):**
- **ArgoCD** - GitOps engine with App-of-Apps pattern
- **Pull-based sync** with automatic drift correction (self-heal)
- Configuration: `infra/argocd/` (root-application.yaml, apps/*.yaml)

**Cluster Storage (Deployed Jan 2026):**
- **Longhorn CSI** - Block storage (prepared for multi-node replication)
- StorageClasses: `longhorn` (2 replicas across nodes for storage redundancy)
- Configuration: `infra/helm/longhorn/`

**GPU Node Preparation (Ready to Deploy):**
- **NVIDIA Device Plugin** - DaemonSet for GPU discovery
- **Tolerations/Affinity** - Web workloads avoid GPU nodes
- Configuration: `infra/k8s/base/gpu/`

**Secure Build Pipeline (Ready to Deploy):**
- **Kaniko** - Rootless container builds (replaces Docker-in-Docker)
- Pod Security `restricted`, NetworkPolicy isolation
- Configuration: `apps/roundhouse/k8s/kaniko-job-template.yaml`

See [PRODUCTION_DEPLOYMENT_ROADMAP.md](./docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md) for details.

### Authentication (Production)

**Current Implementation:**
- ✅ **OIDC/OAuth 2.0** via Janua SSO (auth.madfam.io)
- ✅ **External JWKS validation** for federated identity
- ✅ **GitHub OAuth integration** for repo imports
- ✅ **RBAC** with admin/developer/viewer roles
- ✅ **Session Management** via Redis
- ✅ **API Keys** for CI/CD integration
- ✅ **SSO Logout** - RP-Initiated Logout terminates Janua sessions (Jan 2026)

**Janua Integration (Complete):**
- **Repository:** [github.com/madfam-org/janua](https://github.com/madfam-org/janua)
- **Production URL:** https://auth.madfam.io
- **Protocol:** OAuth 2.0 / OIDC with RS256 JWT
- **Features:** Multi-tenant orgs, GitHub OAuth, JWKS rotation

### Dispatch (Admin Control Platform)

Dispatch is the superuser admin platform for managing the Enclii infrastructure control plane — fleet management (bare metal hosts), cluster registration, Crossplane-managed resources, virtual clusters, propagation policies, drift detection, cost tracking, and topology visualization.

**Access:** https://admin.enclii.dev

**Authorization Model:**
Access to Dispatch requires BOTH:
1. **Email Domain**: Must be from an allowed domain (default: `@madfam.io`)
2. **User Role**: Must have an operator role (`superadmin`, `admin`, or `operator`)

**Configuration (Environment Variables):**
| Variable | Description | Default |
|----------|-------------|---------|
| `ALLOWED_ADMIN_DOMAINS` | Comma-separated allowed email domains | `@madfam.io` |
| `ALLOWED_ADMIN_ROLES` | Comma-separated allowed roles | `superadmin,admin,operator` |

**Adding New Operators:**
1. Create user in Janua with appropriate role (`superadmin`, `admin`, or `operator`)
2. Ensure their email domain is in `ALLOWED_ADMIN_DOMAINS` (or add it)
3. No code changes required - configuration-driven authorization

**Key Files:**
| Purpose | Location |
|---------|----------|
| Middleware (server-side auth) | `apps/dispatch/middleware.ts` |
| Auth helpers (pure functions) | `apps/dispatch/lib/auth-helpers.ts` |
| Auth Context (client-side) | `apps/dispatch/contexts/AuthContext.tsx` |
| K8s Deployment | `apps/dispatch/k8s/deployment.yaml` |
| Dockerfile | `apps/dispatch/Dockerfile` |
| Analytics | `apps/dispatch/lib/analytics/posthog.ts`, `apps/dispatch/components/PostHogProvider.tsx` |
| Unit tests | `apps/dispatch/__tests__/` (10 files, 123 tests: auth, API, Cloudflare, components, analytics) |

### Production Services

All services deploy via the zero-touch onboarding pattern: K8s manifests, CI workflows, and `enclii.yaml` live in each repo. ArgoCD registration via `POST /v1/admin/onboard`. No deployment config lives in the enclii repo itself.

**Running at enclii.dev:**
- ✅ `switchyard-api` → api.enclii.dev (control plane)
- ✅ `switchyard-ui` → app.enclii.dev (web dashboard)
- ✅ `dispatch` → admin.enclii.dev (admin control platform)
- ✅ `janua` → auth.madfam.io (SSO authentication)
- ✅ `docs-site` → docs.enclii.dev (documentation)
- ✅ `landing-page` → enclii.dev (deployed)
- ✅ `status-page` → status.enclii.dev (deployed, 24h timeline with adaptive 5-60 min windows based on viewport, Atom feed, incidents API, auto-incident detection, uptime API, dark/light theme)
- ✅ `status-page-madfam` → status.madfam.io (deployed, 24h timeline with adaptive 5-60 min windows based on viewport, Atom feed, incidents API, auto-incident detection, uptime API, dark/light theme)
- ✅ `autoswarm-nexus-api` → agents-api.madfam.io (agent orchestration API)
- ✅ `autoswarm-office-ui` → agents.madfam.io (agent management console)
- ✅ `autoswarm-admin` → agents-admin.madfam.io (admin console)
- ✅ `autoswarm-colyseus` → agents-ws.madfam.io (real-time collaboration)
- ✅ `autoswarm-gateway` → (background worker, no public endpoint)
- ✅ `autoswarm-workers` → (background worker, no public endpoint)
- ✅ `pravara-admin` → mes-admin.madfam.io (MES admin console)
- ✅ `pravara-api` → mes-api.madfam.io (MES control plane)
- ✅ `pravara-ui` → mes.madfam.io (MES web dashboard)
- ✅ `deal-sniper` → sniper.madfam.io (Hetzner auction tracker dashboard)
- ✅ `yantra4d-landing` → yantra4d.com (3D engine landing page)
- ✅ `yantra4d-studio` → app.yantra4d.com (3D studio)
- ✅ `yantra4d-backend` → api.yantra4d.com (3D engine API)
- ✅ `yantra4d-admin` → admin.yantra4d.com (admin console)
- ✅ `karafiel-web` → karafiel.mx (marketplace)
- ✅ `karafiel-api` → api.karafiel.mx (marketplace API)
- ✅ `karafiel-admin` → admin.karafiel.mx (admin console)
- ✅ `forgesight-www` → forgesight.quest (project management)
- ✅ `forgesight-app` → app.forgesight.quest (app)
- ✅ `forgesight-api` → api.forgesight.quest (API)
- ✅ `forgesight-admin` → admin.forgesight.quest (admin console)
- ✅ `dhanam-web` → dhan.am (billing platform)
- ✅ `dhanam-api` → api.dhan.am (billing API)
- ✅ `dhanam-admin` → admin.dhan.am (admin console)
- ✅ `tezca-web` → tezca.mx (marketplace)
- ✅ `tezca-api` → api.tezca.mx (API)
- ✅ `tezca-admin` → admin.tezca.mx (admin console)
- ✅ `madfam-web` → madfam.io (org site)
- ✅ `madfam-cms` → cms.madfam.io (content management)
- ✅ `fortuna-api` → api.fortuna.tube (problem intelligence API)
- ✅ `fortuna-web` → fortuna.tube (problem intelligence dashboard)
- ✅ `avala-api` → api.avala.studio (learning verification API)
- ✅ `avala-web` → avala.studio (learning verification platform)
- ✅ `digifab-quoting-api` → api.cotiza.studio (quoting engine API)
- ✅ `digifab-quoting-web` → cotiza.studio (automated quoting)
- ✅ `primavera3d-web` → primavera3d.pro (3D portfolio)
- ✅ `ceq-studio` → ceq.lol (ComfyUI wrapper)
- ✅ `nuit-one-web` → nuit.one (audio platform)
- ✅ `forj-web` → forj.design (fabrication storefronts)
- ✅ `bloom-scroll-web` → almanac.solar (slow web aggregator)
- ✅ `coforma-studio-web` → coforma.studio (customer advisory boards)
- ✅ `blueprint-harvester-api` → blueprint.tube (3D model indexing)

**Build Pipeline Status:**
- ✅ GitHub webhook configured with HMAC verification
- ✅ Real build pipeline (Buildpacks/Dockerfile detection)
- ✅ Container registry push (ghcr.io/madfam-org)
- ✅ Kubernetes reconciler for deployments
- ✅ Deployment lifecycle event tracking (push → build → deploy → healthy)
- ✅ Self-service repo onboarding API

See [ONBOARDING_GUIDE.md](./docs/guides/ONBOARDING_GUIDE.md) for adding new repos.

---

## Common Workflows

### Adding a New API Endpoint

1. **Define handler** in `apps/switchyard-api/internal/api/` (routes are registered in `*_handlers.go` files — there is no centralized router file)
2. **Update OpenAPI spec** in `docs/api/openapi.yaml`
3. **Add tests** in `apps/switchyard-api/internal/api/*_test.go`
4. **Run validation**: `make lint && make test`

> **Note:** Domain provisioning hooks into the webhook flow (not the handler registration). See `enclii_yaml.go` and `domain_provisioner.go` for the auto-provisioning pipeline triggered by GitHub push events.

### Adding a New CLI Command

1. **Create command file** in `packages/cli/internal/cmd/`
2. **Register in root** in `packages/cli/internal/cmd/root.go`
3. **Add documentation** in `docs/cli/commands/`
4. **Test locally**: `go run ./cmd/enclii <command>`

### Auto-Deploy Pipeline (External Repos)

External repos (dhanam, janua, etc.) auto-deploy via GitOps:
1. Push to `main` triggers CI → builds Docker image → pushes to GHCR
2. CI commits image digest to `kustomization.yaml` via `kustomize edit set image`
3. ArgoCD detects the change and syncs the new digest to K8s
4. Lifecycle events tracked at each step via `POST /v1/callbacks/lifecycle-event`

See [EXTERNAL_REPO_DEPLOY.md](./docs/guides/EXTERNAL_REPO_DEPLOY.md) for the full pattern.
See [DEPLOYMENT_TRACKING.md](./docs/guides/DEPLOYMENT_TRACKING.md) for the lifecycle event API.
See [ONBOARDING_GUIDE.md](./docs/guides/ONBOARDING_GUIDE.md) for adding new repos.

### Preventive Status Hygiene (onboarding gates + stale-outage digest)

Onboarding hardening added after the Apr 2026 audit found 6 services silently in outage for >4 days because the status page was tracking targets whose first image had never been pushed to GHCR. Three preventive measures live in the platform now, all in `apps/status/` and `apps/switchyard-api/internal/checks/`:

1. **Image digest pinning gate** (`internal/checks/image_digest.go`) — rejects onboarding if any workload manifest references an image that isn't `@sha256:`-pinned. Blocks `:latest` and mutable tags; mirrors the cluster-side Kyverno `require-image-digest` policy but earlier. See `runImageGates()` in `apps/switchyard-api/internal/api/onboarding_image_gates.go`.
2. **GHCR package existence gate** (`internal/checks/image_exists.go`) — rejects onboarding if any `ghcr.io/madfam-org/*` image referenced by the manifests has no versions pushed yet (GHCR 404 or empty versions list). Non-GHCR registries are ignored. Avoids "status page tracks a target that has never shipped a pod".
3. **Stale-outage daily digest** — `apps/status/lib/stale-digest.ts` + `POST /api/status/stale-digest`. Scans `status_checks` for services that have been in outage/degraded for >24h continuously and posts a Slack-compatible summary to `STALE_DIGEST_WEBHOOK_URL` (or logs JSON to Loki when unset). Triggered daily at 14:00 UTC by `apps/status/k8s/{enclii,madfam}/stale-outage-digest.yaml` CronJobs. Silently no-ops when nothing is stale.

Both onboarding gates also expose a side-effect-free `GET /v1/admin/preflight?repo=owner/name` endpoint for CI callers. No bypass flag — if a legitimate exception arises, extend the gate rather than opt out per-request. Full contract: [ONBOARDING_GUIDE.md § Preventive Image Hygiene Gates](./docs/guides/ONBOARDING_GUIDE.md#preventive-image-hygiene-gates-auto-run-on-every-onboard).

### Deploying a Service Change

```bash
# Local testing
make run-switchyard  # Test API changes
make run-ui          # Test UI changes

# Deploy to staging
enclii deploy --env staging

# Verify
enclii logs <service> -f --env staging
enclii ps --env staging

# Deploy to production (after staging validation)
enclii deploy --env production --strategy canary --canary-percent 10
```

### Database Migration

Migrations are raw SQL files in two locations:
- `apps/switchyard-api/internal/db/migrations/` — Core schema (genesis, admin foundation, etc.)
- `apps/switchyard-api/migrations/` — Incremental migrations

```bash
# Apply to production (via kubectl exec into the API pod)
kubectl exec -n enclii deploy/switchyard-api -- psql "$DATABASE_URL" -f /path/to/migration.sql

# Verify migration applied
kubectl exec -n enclii deploy/switchyard-api -- psql "$DATABASE_URL" -c "\dt"
```

---

## Debugging Guide

> **Canonical troubleshooting docs**: `docs/troubleshooting/` — this section is a quick reference for AI agents.

### API Issues

```bash
# Check API health
curl https://api.enclii.dev/health

# View API logs (prefer enclii CLI)
enclii logs switchyard-api -f --level error

# Check database connectivity (kubectl — no CLI equivalent)
kubectl exec -n enclii deploy/switchyard-api -- /app/healthcheck db

# Inspect pod status (kubectl — direct pod debugging)
kubectl describe pod -n enclii <pod-name>
```

### Build Failures

```bash
# View build logs
enclii builds logs --latest

# Check Roundhouse worker status (kubectl — internal platform service)
kubectl logs -n enclii -l app=roundhouse -f

# Inspect build job (kubectl — raw job debugging)
kubectl get jobs -n enclii-builds
kubectl logs -n enclii-builds job/<job-name>
```

### Deployment Issues

```bash
# Check deployment status (prefer enclii CLI)
enclii ps --wide

# View service logs
enclii logs <service> -f

# Rollback if needed
enclii rollback <service>

# Direct pod debugging (kubectl — when CLI doesn't expose enough detail)
kubectl describe deploy -n <namespace> <service>
```

### Auth/SSO Issues

```bash
# Test JWKS endpoint
curl https://auth.madfam.io/.well-known/jwks.json | jq

# Verify token (CLI)
enclii auth verify

# Check Janua logs (kubectl — Janua is a separate service, no enclii equivalent)
kubectl logs -n janua -l app=janua-api -f
```

### GitOps/ArgoCD Issues

```bash
# Check ArgoCD sync status
kubectl get applications -n argocd

# View application details
kubectl describe application core-services -n argocd

# Check ArgoCD controller logs
kubectl logs -n argocd -l app.kubernetes.io/name=argocd-application-controller -f

# Access ArgoCD UI (port-forward)
kubectl port-forward svc/argocd-server -n argocd 8080:443
# Login: see internal-devops/access/argocd-access.md

# Force sync an application
kubectl patch application core-services -n argocd --type merge -p '{"operation":{"sync":{}}}'
```

### Storage/Longhorn Issues

```bash
# Check Longhorn pods
kubectl get pods -n longhorn-system

# View volume status
kubectl get volumes.longhorn.io -n longhorn-system

# Check PVC status
kubectl get pvc -A

# Access Longhorn UI (port-forward)
kubectl port-forward svc/longhorn-frontend -n longhorn-system 8081:80

# Check replica health
kubectl get replicas.longhorn.io -n longhorn-system
```

---

## Key File Locations

### API (Go)

| Purpose | Location |
|---------|----------|
| Entry point | `apps/switchyard-api/cmd/api/main.go` |
| HTTP handlers | `apps/switchyard-api/internal/api/*.go` |
| Route registration | `apps/switchyard-api/internal/api/*_handlers.go` (distributed, no centralized router) |
| Middleware | `apps/switchyard-api/internal/middleware/` |
| Services | `apps/switchyard-api/internal/services/` (projects, deployments, webhook, build, domain_sync, tunnel_routes, deployment_groups, auth, analyzer) |
| Admin handlers | `apps/switchyard-api/internal/api/*_handlers.go` (bare_metal, cluster_admin, cost, drift, managed_resource, propagation, virtual_cluster, admin_topology) |
| Admin services | `apps/switchyard-api/internal/services/` (bare_metal, cluster_admin, infrastructure, vcluster, placement, drift, cost_tracking) |
| Admin types | `packages/sdk-go/pkg/types/admin.go` |
| SDK client | `packages/sdk-go/pkg/client/` (projects, services, deployments, envvars, logs) |
| Timetable types | `packages/sdk-go/pkg/types/timetable.go` |
| Junction types | `packages/sdk-go/pkg/types/junction.go` |
| Timetable handlers | `apps/switchyard-api/internal/api/timetable_handlers.go` |
| Junction handlers | `apps/switchyard-api/internal/api/junction_handlers.go` |
| Timetable repos | `apps/switchyard-api/internal/db/cron_job_repository.go`, `cron_job_run_repository.go`, `one_off_job_repository.go` |
| Junction repo | `apps/switchyard-api/internal/db/junction_repository.go` |
| Timetable reconciler | `apps/switchyard-api/internal/reconciler/timetable_reconciler.go` |
| Timetable migrations | `apps/switchyard-api/migrations/007_timetable.up.sql` |
| Junction migrations | `apps/switchyard-api/migrations/008_junctions.up.sql` |
| Admin migrations | `apps/switchyard-api/internal/db/migrations/002_admin_foundation.*.sql` |
| enclii.yaml parser | `apps/switchyard-api/internal/api/enclii_yaml.go` |
| NetworkPolicy generator | `apps/switchyard-api/internal/netpolicy/generator.go` |
| Status handlers | `apps/switchyard-api/internal/api/status_handlers.go` |
| Domain provisioner | `apps/switchyard-api/internal/api/domain_provisioner.go` |
| Provisioning services | `apps/switchyard-api/internal/provisioning/` |
| Provisioning handlers | `apps/switchyard-api/internal/api/provisioning_handlers.go` |
| Migrations | `apps/switchyard-api/migrations/` |

### CLI (Go)

| Purpose | Location |
|---------|----------|
| Entry point | `packages/cli/cmd/enclii/main.go` |
| Commands | `packages/cli/internal/cmd/` |
| API client | `packages/cli/internal/api/` |
| Auth flow | `packages/cli/internal/auth/` |
| Command tests | `packages/cli/internal/cmd/*_test.go` (4 files, 47 tests: login, deploy, onboard, root) |
| Config | `packages/cli/internal/config/` |

### UI (Next.js)

| Purpose | Location |
|---------|----------|
| App router | `apps/switchyard-ui/app/` |
| Dashboard page | `apps/switchyard-ui/app/(protected)/page.tsx` |
| Components | `apps/switchyard-ui/components/` |
| Dashboard cards | `apps/switchyard-ui/components/dashboard/project-card-compact.tsx` |
| Framework detection | `apps/switchyard-ui/components/dashboard/framework-icon.tsx` (known-repo map + heuristic) |
| API calls | `apps/switchyard-ui/lib/api/` |
| Hooks | `apps/switchyard-ui/hooks/` |
| Types | `apps/switchyard-ui/types/` |
| Analytics | `apps/switchyard-ui/lib/analytics/posthog.ts`, `apps/switchyard-ui/lib/analytics/PostHogProvider.tsx` |
| Unit tests | `apps/switchyard-ui/**/*.test.ts` (6 files, 159 tests) |

### Infrastructure

| Purpose | Location |
|---------|----------|
| Terraform | `infra/terraform/` |
| K8s manifests | `infra/k8s/production/` |
| Cloudflare tunnel | `infra/k8s/production/cloudflared-unified.yaml` |
| Deploy scripts | `scripts/` |
| ArgoCD config | `infra/argocd/` |
| ArgoCD apps | `infra/argocd/apps/*.yaml` |
| Longhorn values | `infra/helm/longhorn/` |
| KEDA values | `infra/helm/keda/values.yaml` |
| GPU setup | `infra/k8s/base/gpu/` |
| Kaniko builds | `apps/roundhouse/k8s/kaniko-job-template.yaml` |
| Vault ExternalSecrets | `infra/k8s/base/external-secrets/vault-secrets/` (enclii, janua, data, cloudflare) |
| Kyverno policy exceptions | `infra/k8s/policies/` (keda, karafiel, vault, logging) |
| Backup CronJobs | `infra/k8s/production/backup/` (postgres, k3s-datastore, github-repos, cloudflare-config, argocd-secrets, verify, restore-drill) |
| Backup Kustomization | `infra/k8s/production/backup/kustomization.yaml` (ArgoCD-managed) |
| Backup coverage runbook | `docs/runbooks/BACKUP_COVERAGE.md` |
| Node maintenance | `infra/k8s/production/node-maintenance-cronjob.yaml` (daily GC + Prometheus metrics export) |
| Prometheus config + alerts | `infra/k8s/production/monitoring/prometheus.yaml` (scrape configs, alert rules ConfigMap) |
| Node exporter | `infra/k8s/production/monitoring/node-exporter.yaml` (DaemonSet + textfile collector) |
| Grafana dashboards | `infra/k8s/production/monitoring/dashboards/` (roundhouse, secrets-rotation, node-maintenance, longhorn-health, cluster-capacity, api-latency, cost-trends, argocd-sync) |
| Logging stack | `infra/k8s/production/logging/` (Fluent Bit DaemonSet + Loki StatefulSet, ArgoCD-managed via `infra/argocd/apps/logging.yaml`) |
| HPAs | `infra/k8s/production/hpa-switchyard-api.yaml`, `hpa-switchyard-ui.yaml`, `hpa-roundhouse-api.yaml` |
| Status K8s (base) | `apps/status/k8s/base/` (deployment, service, secret template) |
| Status K8s (overlays) | `apps/status/k8s/enclii/`, `apps/status/k8s/madfam/` (configmap, cronjob) |
| Status Atom feed | `apps/status/app/feed.xml/route.ts` |
| Status incidents API | `apps/status/app/api/incidents/route.ts` (ADMIN_SECRET auth) |
| Status auto-incidents | `apps/status/lib/auto-incidents.ts` (detects failures → creates/resolves incidents) |
| Status uptime API | `apps/status/app/api/status/uptime/route.ts` |
| Status shared config | `apps/status/lib/status-config.ts` (colors, labels, priority, incident config) |
| Status E2E tests | `apps/status/tests/e2e/status-pages.spec.ts` |
| Status unit tests | `apps/status/__tests__/lib/` (7 files, 129 tests: types, config, health-checker, auto-incidents, incidents, status-history, status-config) |
| Status stale-outage digest | `apps/status/lib/stale-digest.ts` + `apps/status/app/api/status/stale-digest/route.ts` + `apps/status/k8s/{enclii,madfam}/stale-outage-digest.yaml` |
| Onboarding image gates | `apps/switchyard-api/internal/checks/image_digest.go` (digest pin check), `image_exists.go` (GHCR package exists check), wiring in `apps/switchyard-api/internal/api/onboarding_image_gates.go` |

### Documentation

| Purpose | Location |
|---------|----------|
| API spec | `docs/api/openapi.yaml` |
| CLI reference | `docs/cli/` |
| Quickstart | `docs/quickstart/` |
| Integrations | `docs/integrations/` |
| Architecture | `docs/architecture/` |
| **Infrastructure (Jan 2026)** | |
| GitOps/ArgoCD | `docs/infrastructure/GITOPS.md` |
| Storage/Longhorn | `docs/infrastructure/STORAGE.md` |
| Cloudflare integration | `docs/infrastructure/CLOUDFLARE.md` |
| External secrets | `docs/infrastructure/EXTERNAL_SECRETS.md` |
| Capacity roadmap | `docs/infrastructure/CAPACITY_ROADMAP.md` |
| Longhorn recovery runbook | `docs/runbooks/LONGHORN_VOLUME_RECOVERY.md` |
| Cluster ops runbook | `docs/runbooks/CLUSTER_REMEDIATION_OPS.md` |
| Incident response runbook | `docs/runbooks/INCIDENT_RESPONSE.md` (severity classification, escalation matrix, 5 failure playbooks, postmortem process) |
| K3s upgrade runbook | `docs/runbooks/K3S_UPGRADE.md` (prerequisites, rolling upgrade sequence, CRD migration notes, rollback procedure) |
| Logging conventions | `docs/architecture/LOGGING_CONVENTIONS.md` (logrus/zap split, standards for new services, Fluent Bit parser config) |
| **Load Testing** | |
| k6 test scripts | `tests/load/` (health.js, api.js, stress.js, config.js) |
| Load test CI | `.github/workflows/load-test.yml` (weekly + manual dispatch) |
| **Integration Testing** | |
| K8s integration tests | `tests/integration/` (deploy pipeline, PVC persistence, routes, custom domains, service volumes) |
| Deploy pipeline tests | `apps/switchyard-api/internal/api/deploy_pipeline_test.go` (45 tests: webhook→build→deploy flow) |
| Deploy pipeline helpers | `apps/switchyard-api/internal/api/deploy_pipeline_helpers_test.go` (fixtures, HTTP helpers) |
| **Governance** | |
| License (full AGPL-3.0) | `LICENSE` |
| Commercial licensing notice | `COMMERCIAL_LICENSE.md` |
| Self-hosting guide | `docs/guides/SELF_HOSTING.md` |
| **LLM Context** | |
| LLM context (compact) | `llms.txt` |
| LLM context (full) | `llms-full.txt` |

---

## Environment-Specific Commands

### Local Development

```bash
# Start full stack
make run-all

# Start individual services
make run-switchyard   # API on :8080
make run-ui           # UI on :3000

# Database
docker-compose up -d postgres redis
```

### Staging Environment

```bash
# Deploy
enclii deploy --env staging

# Logs
enclii logs <service> --env staging -f

# Port forward for debugging
kubectl port-forward -n staging svc/switchyard-api 8080:8080
```

### Production Environment

```bash
# Deploy with canary
enclii deploy --env production --strategy canary --canary-percent 10

# Monitor
enclii ps --env production --watch

# Rollback if needed
enclii rollback <service> --env production

# Direct kubectl access
export KUBECONFIG=~/.kube/enclii-production
kubectl get pods -n enclii
```

---

## Testing Workflows

### Unit Tests

```bash
# All tests
make test

# Specific package
go test ./apps/switchyard-api/internal/api/...

# Admin services and handlers
go test ./apps/switchyard-api/internal/services/... ./apps/switchyard-api/internal/api/...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration Tests

```bash
# Requires running services
make integration-test

# Specific test
go test -tags=integration -run TestDeploymentFlow ./...
```

### E2E Tests

```bash
# Full E2E suite
make e2e

# UI E2E (Playwright)
cd apps/switchyard-ui
pnpm test:e2e
```

---

## Troubleshooting Quick Reference

| Symptom | Check | Fix |
|---------|-------|-----|
| API 500 errors | `enclii logs switchyard-api` | Check DB connection, env vars |
| Build stuck | `enclii builds logs --latest` | Restart Roundhouse worker |
| Auth fails | `curl .../jwks.json` | Check Janua status, OIDC config |
| Deploy timeout | `enclii ps --wide` | Check resource limits, probes |
| Preview not created | Webhook logs | Verify GitHub integration |
| SSL errors | Cert-manager logs | Check issuer, DNS |

---

## Deferred Items (Not Implemented — Tracked Here)

| Item | Status | Notes |
|------|--------|-------|
| ESO CRD migration (0.9.11 to 0.16.2) | Deferred | Requires maintenance window, CRD v1beta1 to v1 migration. Current version stable |
| Digest pinning for ecosystem repos | Out of scope | Changes go in external repos (forgesight, karafiel, pravara-mes, madfam-site) |
| Timetable (user cron jobs) | **Implemented** | 7 API endpoints, 3 DB tables, K8s CronJob reconciler, CLI `enclii jobs` commands. Session 103 |
| Junction (routing/ingress) | **Implemented** | 4 API endpoints, 1 DB table, Cloudflare tunnel + cert-manager integration, CLI `enclii junctions` commands. Session 103 |
| Multi-region | Deferred | Explicitly out of scope for v1 per SOFTWARE_SPEC.md |
| Handler legacy pattern (repos to services) | **In Progress** | WebhookService + BuildService created (S109). 2 of ~8 service extractions done. Handlers coexist with h.repos.* — incremental migration continues S111-114 |
| Test coverage enforcement | Active | CI threshold 50%. `--passWithNoTests` removed from mandatory suites (switchyard-ui, dispatch, status) in S106. Tests: db/, reconciler/ (30 S106 + 35 S107), services/ (+ webhook 19, build 10 S109), roundhouse (21), waybill (14), CLI (82 + 33 S107), SDK (30), dispatch (123), status (129), switchyard-ui (159), shared-lib (19), ui-components (18), provenance (42), signing (8), addons (6), timetable (62+19 S103), cron_job_run (14 S106), topology (43 S107), rotation (18 S107), backup (16 S107), **deploy pipeline (45 API + 3 integration S109)**. 220+ tests added in S107, ~140 in S109. RBAC namespace bug fixed S106 (`default`->`enclii`). Integration tests go.mod aligned to 1.25.0 |
| Vault (secret management) | Ready | Helm values + ArgoCD app (health probes fixed S106: `uninitcode=200&sealedcode=200`) + ESO ClusterSecretStore + NetworkPolicies + tunnel route (vault.madfam.io) + ExternalSecret manifests (18 files, 15 namespaces, ~155 keys) + ESO reader policy + migration script. Pod can now run uninitialized/sealed. Needs cluster deploy (init, unseal, configure, migrate). `JANUA_SECRET_KEY` ExternalSecrets for karafiel, autoswarm, and pravara-mes live in their own repos (self-provisioning pattern, not in enclii) |
| PostHog (analytics) | Removed (S106, cleaned S110) | Self-host abandoned S106. Zombie namespace + ArgoCD app + policy files + ExternalSecret removed S110. Helm values archived to `infra/archive/posthog/`. Analytics via Cloudflare Worker proxy: `analytics.madfam.io` → PostHog Cloud (still active at `infra/cloudflare/posthog-proxy/`). Client-side PostHog re-enabled for switchyard-ui via the Cloudflare Worker proxy at `analytics.madfam.io` |
| react-sdk pre-existing test failures | Resolved | Session 102: Replaced empty div mocks with interactive form mocks, expanded useJanua context. 12/12 suites, 123/123 tests pass (janua `bdb7a31b`) |
| Onboarding handler K8s API migration | Completed | Session 97: NetworkPolicies now applied via K8s API (`k8s.ApplyNetworkPolicies`) instead of git commit. No more writes to `infra/k8s/policies/` during onboarding |
| pgbouncer egress type | Completed | Session 97: Added to netpolicy generator (port 6432 to data namespace). Available in `enclii.yaml` `network.services[].egress` |

---

## Agent Session Protocol (Level 5 Autonomy)

This section defines the operating protocol for AI agents (Claude Code, GitHub Copilot, etc.) when working in this repository.

### Session Start
1. **READ AI_CONTEXT.md** in the repository root for critical paths and directives
2. Run `git status && git branch` to verify clean state and current branch
3. Check for existing TodoWrite items from previous sessions
4. Load any Serena memories: `list_memories()` → `read_memory()`

### During Session
1. **ALWAYS use feature branches** - never commit directly to main/master
2. **ALWAYS run validation** before commits:
   - Go: `golangci-lint run` in each Go module
   - TypeScript: `pnpm typecheck && pnpm lint`
3. **UPDATE TodoWrite** after completing each task
4. **CHECKPOINT every 30 minutes** via `write_memory()` for session persistence
5. **PREFER `enclii` CLI over `kubectl`** for all operational tasks — see "CLI Preference" under Important Conventions

### Secret Management Protocols (Safe-Patch Mode)
**High-Value Targets**: You are PERMITTED to edit `.env` and `.env.local` files, but MUST adhere to:

1. **Backup First**: Before ANY modification to a secret file:
   ```bash
   cp .env .env.bak  # Create immediate restore point
   ```

2. **Patch, Don't Purge**: NEVER overwrite with `> .env` (deletes existing keys). ALWAYS use:
   ```bash
   sed -i '' 's/OLD_VALUE/NEW_VALUE/' .env  # Modify specific key
   echo "NEW_KEY=value" >> .env             # Append new key
   ```

3. **Placeholder Ban**: FORBIDDEN from writing values containing:
   - `your_key_here`, `placeholder`, `example`, `xxx`, `TODO`
   into active config files (`.env`, `.env.local`)

### Session End
1. Verify all TodoWrite items completed or documented
2. Run final validation: `make lint && make test`
3. Save session state: `write_memory("session_summary", outcomes)`
4. **DO NOT leave uncommitted changes** without explicit user approval

### Validation Requirements
| Change Type | Required Validation |
|-------------|---------------------|
| Go code | `golangci-lint run ./...` in module directory |
| TypeScript | `npm run lint && npm run test` |
| K8s manifests | `kubectl apply --dry-run=client -f <file>` |
| Terraform | `terraform plan` |
| Configuration | Manual review required |

---
