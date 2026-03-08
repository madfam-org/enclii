# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Enclii is an open source DevOps platform for deploying, scaling, and operating containerized services with enterprise-grade security, GitOps automation, and zero vendor lock-in.

**Current Status:** 🟢 v0.1.0 - Production Beta (95% ready) ([checklist](./docs/production/PRODUCTION_CHECKLIST.md))
**Infrastructure:** Hetzner Dedicated (2-node k3s) + Cloudflare (~$55/month) - **Running**
**Authentication:** OIDC via Janua SSO (RS256 JWT) - **Integrated**
**Dogfooding:** Core services deployed ([api.enclii.dev](https://api.enclii.dev), [app.enclii.dev](https://app.enclii.dev))
**Build Pipeline:** GitHub webhook CI/CD with Buildpacks - **Operational**
**GitOps:** ArgoCD App-of-Apps (17 apps: 10 ApplicationSet + 7 static) with self-heal - **Operational** (Jan 2026)
**Storage:** Longhorn CSI v1.7.2 (single-replica; ready for multi-node) - **Operational** (Jan 2026)
**Last Audit:** Feb 6, 2026 — 82 pods, 0 errors, 12 endpoints 100% healthy ([report](./docs/infrastructure/INFRA_ANATOMY.md))

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
- **Functions**: Serverless functions with scale-to-zero - **API + UI complete**, KEDA runtime pending deployment
- **Junctions**: Routing/ingress + certs + DNS - **Planned**
- **Timetable**: Cron and one-off jobs - **Planned**
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

## Production Infrastructure

### Current Production Stack (~$55/month)

Enclii runs on a 2-node k3s cluster with infrastructure prepared for further scaling:

**Compute & Kubernetes (2-node cluster):**
- **foundry-core** (Hetzner AX41-NVME) - Control plane (AMD Ryzen 5 3600, 64GB RAM, 2x512GB NVMe) - ~$50/month
- **foundry-builder-01** (VPS "The Forge") - Worker node for CI builds (taint: builder=true:NoSchedule)
- **k3s v1.33.6+k3s1** - Lightweight Kubernetes (both nodes must match k3s version)
- **Cloudflare Tunnel** - Zero-trust ingress (replaces LoadBalancer) - $0

> **Note:** 2-node cluster since Jan 2026. Builder node runs only ARC GitHub Actions runners. Longhorn CSI operates in single-replica mode; ready for multi-replica when additional storage nodes are added.

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
- **Self-hosted PostgreSQL** - In-cluster deployment with PVC storage, daily backups to R2 - $0
- **Self-hosted Redis** - Single instance in-cluster (Sentinel config ready for multi-node) - $0

> **Infrastructure Audit (Jan 2026)**: Evaluated Ubicloud managed PostgreSQL ($50/mo) and Redis Sentinel HA. Conclusion: **NOT NEEDED** for current 99.5% SLA / 24-hour RPO requirements. Self-hosted meets targets at $0 cost. Redis Sentinel manifests staged at `infra/k8s/production/redis-sentinel.yaml` for multi-node deployment.

**Storage & Networking:**
- **Cloudflare R2** - Zero-egress object storage (SBOMs, artifacts) - $5/month
- **Cloudflare for SaaS** - First 100 custom domains FREE - $0

**GitOps & Orchestration (Deployed Jan 2026):**
- **ArgoCD** - GitOps engine with App-of-Apps pattern
- **Pull-based sync** with automatic drift correction (self-heal)
- Configuration: `infra/argocd/` (root-application.yaml, apps/*.yaml)
- Access: `kubectl port-forward svc/argocd-server -n argocd 8080:443`

**Cluster Storage (Deployed Jan 2026):**
- **Longhorn CSI** - Block storage (prepared for multi-node replication)
- StorageClasses: `longhorn` (single replica on single-node; ready for HA when nodes added)
- Configuration: `infra/helm/longhorn/`

**GPU Node Preparation (Ready to Deploy):**
- **NVIDIA Device Plugin** - DaemonSet for GPU discovery
- **Tolerations/Affinity** - Web workloads avoid GPU nodes
- Configuration: `infra/k8s/base/gpu/`

**Secure Build Pipeline (Ready to Deploy):**
- **Kaniko** - Rootless container builds (replaces Docker-in-Docker)
- Pod Security `restricted`, NetworkPolicy isolation
- Configuration: `apps/roundhouse/k8s/kaniko-job-template.yaml`

**vs Traditional SaaS Stack:** $2,220/month (Railway $2,000 + Auth0 $220)
**5-Year Savings:** $129,900

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
| Auth Context (client-side) | `apps/dispatch/contexts/AuthContext.tsx` |
| K8s Deployment | `apps/dispatch/k8s/deployment.yaml` |
| Dockerfile | `apps/dispatch/Dockerfile` |

### Dogfooding Status (In Progress)

**Goal:** Run our entire platform on Enclii, authenticated by Janua.

> **Current State:** "We run our core production services on Enclii. We are our own most demanding customer."

**Production Services** (running at enclii.dev):
- ✅ `switchyard-api` → api.enclii.dev (control plane)
- ✅ `switchyard-ui` → app.enclii.dev (web dashboard)
- ✅ `dispatch` → admin.enclii.dev (admin control platform)
- ✅ `janua` → auth.madfam.io (SSO authentication)
- ✅ `docs-site` → docs.enclii.dev (documentation)
- ✅ `landing-page` → enclii.dev (deployed)
- ✅ `status-page` → status.enclii.dev (deployed, 24h timeline history)
- ✅ `status-page-madfam` → status.madfam.io (deployed, 24h timeline history)

**Build Pipeline Status:**
- ✅ GitHub webhook configured with HMAC verification
- ✅ Real build pipeline (Buildpacks/Dockerfile detection)
- ✅ Container registry push (ghcr.io/madfam-org)
- ✅ Kubernetes reconciler for deployments
- ✅ Deployment lifecycle event tracking (push → build → deploy → healthy)
- ✅ Self-service repo onboarding API

See [DOGFOODING_GUIDE.md](./docs/guides/DOGFOODING_GUIDE.md) for complete implementation plan.

**Why This Matters:**
- **Customer Confidence:** "If they trust it, we can too"
- **Product Quality:** We find bugs before customers do
- **Sales Credibility:** Authentic production usage metrics

---

## Common Workflows

### Adding a New API Endpoint

1. **Define handler** in `apps/switchyard-api/internal/api/`
2. **Add route** in `apps/switchyard-api/internal/api/router.go`
3. **Update OpenAPI spec** in `docs/api/openapi.yaml`
4. **Add tests** in `apps/switchyard-api/internal/api/*_test.go`
5. **Run validation**: `make lint && make test`

> **Note:** Domain provisioning hooks into the webhook flow (not the router). See `enclii_yaml.go` and `domain_provisioner.go` for the auto-provisioning pipeline triggered by GitHub push events.

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

```bash
# Create migration
go run apps/switchyard-api/cmd/migrate/main.go create <name>

# Apply locally
go run apps/switchyard-api/cmd/migrate/main.go up

# Apply to production (via kubectl)
kubectl exec -n enclii deploy/switchyard-api -- /app/migrate up
```

---

## Debugging Guide

### API Issues

```bash
# Check API health
curl https://api.enclii.dev/health

# View API logs
enclii logs switchyard-api -f --level error

# Check database connectivity
kubectl exec -n enclii deploy/switchyard-api -- /app/healthcheck db

# Inspect pod status
kubectl get pods -n enclii -l app=switchyard-api
kubectl describe pod -n enclii <pod-name>
```

### Build Failures

```bash
# View build logs
enclii builds logs --latest

# Check Roundhouse worker status
kubectl logs -n enclii -l app=roundhouse -f

# Inspect build job
kubectl get jobs -n enclii-builds
kubectl logs -n enclii-builds job/<job-name>
```

### Deployment Issues

```bash
# Check deployment status
enclii ps --wide

# View reconciler logs
kubectl logs -n enclii -l app=reconciler -f

# Inspect Kubernetes deployment
kubectl get deploy -n <namespace>
kubectl describe deploy -n <namespace> <service>
kubectl rollout status deploy/<service> -n <namespace>
```

### Auth/SSO Issues

```bash
# Test JWKS endpoint
curl https://auth.madfam.io/.well-known/jwks.json | jq

# Verify token (CLI)
enclii auth verify

# Check Janua logs
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
# Login: admin / (kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d)

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
| Router setup | `apps/switchyard-api/internal/api/router.go` |
| Middleware | `apps/switchyard-api/internal/api/middleware/` |
| Models | `apps/switchyard-api/internal/models/` |
| Services | `apps/switchyard-api/internal/service/` |
| Admin handlers | `apps/switchyard-api/internal/api/*_handlers.go` (bare_metal, cluster_admin, cost, drift, managed_resource, propagation, virtual_cluster, admin_topology) |
| Admin services | `apps/switchyard-api/internal/services/` (bare_metal, cluster_admin, infrastructure, vcluster, placement, drift, cost_tracking) |
| Admin types | `packages/sdk-go/pkg/types/admin.go` |
| Admin migrations | `apps/switchyard-api/internal/db/migrations/002_admin_foundation.*.sql` |
| enclii.yaml parser | `apps/switchyard-api/internal/api/enclii_yaml.go` |
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
| Config | `packages/cli/internal/config/` |

### UI (Next.js)

| Purpose | Location |
|---------|----------|
| App router | `apps/switchyard-ui/app/` |
| Components | `apps/switchyard-ui/components/` |
| API calls | `apps/switchyard-ui/lib/api/` |
| Hooks | `apps/switchyard-ui/hooks/` |
| Types | `apps/switchyard-ui/types/` |

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
| GPU setup | `infra/k8s/base/gpu/` |
| Kaniko builds | `apps/roundhouse/k8s/kaniko-job-template.yaml` |

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
make migrate-up
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
| Build stuck | `kubectl get jobs -n enclii-builds` | Restart Roundhouse worker |
| Auth fails | `curl .../jwks.json` | Check Janua status, OIDC config |
| Deploy timeout | `kubectl describe deploy` | Check resource limits, probes |
| Preview not created | Webhook logs | Verify GitHub integration |
| SSL errors | Cert-manager logs | Check issuer, DNS |

---

## Deferred Items (Not Implemented — Tracked Here)

| Item | Status | Notes |
|------|--------|-------|
| ESO CRD migration (0.9.11 to 0.16.2) | Deferred | Requires maintenance window, CRD v1beta1 to v1 migration. Current version stable |
| Digest pinning for ecosystem repos | Out of scope | Changes go in external repos (forgesight, karafiel, pravara-mes, madfam-site) |
| Timetable (user cron jobs) | Deferred | ~2 week feature effort. Q2 2026 per roadmap |
| Multi-region | Deferred | Explicitly out of scope for v1 per SOFTWARE_SPEC.md |
| Handler legacy pattern (repos to services) | Incremental | Migrate as handlers are touched for other work |
| Test coverage enforcement | Deferred | Add `-coverprofile` and threshold gate to CI in separate PR |

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
