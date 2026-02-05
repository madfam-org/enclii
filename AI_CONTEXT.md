# AI_CONTEXT.md - Enclii Operations Platform

## Architecture
- **Stack**: Go (Gin) backend + Next.js 16 frontend + K3s + ArgoCD
- **Pattern**: GitOps, App-of-Apps, zero-trust ingress via Cloudflare Tunnel
- **Self-Deployment**: Enclii deploys itself (dogfooding)
- **Cluster**: 2-node k3s v1.33.6+k3s1 (foundry-core + foundry-builder-01)
- **Last Audit**: Jan 26, 2026 — all critical issues resolved, 79 pods running, 0 errors

## God Files (Critical Paths)
| Purpose | Path |
|---------|------|
| API Entry | `apps/switchyard-api/cmd/api/main.go` |
| CLI Entry | `packages/cli/cmd/enclii/main.go` |
| Login Flow | `packages/cli/internal/cmd/login.go` |
| Terraform | `infra/terraform/` |
| K8s Manifests | `infra/k8s/production/` |
| ArgoCD Root | `infra/argocd/root-application.yaml` |
| ArgoCD Apps | `infra/argocd/apps/*.yaml` (13 apps) |
| Tunnel Config | `infra/k8s/production/cloudflared-unified.yaml` (28 domains) |
| Kyverno Policies | `infra/k8s/base/kyverno/policies/` |
| Golden Tests | `tests/golden/` (pre-commit validation) |
| Enclii Config | `.enclii.yml` |
| Dispatch API | `apps/dispatch/` |
| Infra Anatomy | `docs/infrastructure/INFRA_ANATOMY.md` |
| LLM Context (compact) | `llms.txt` |
| LLM Context (full) | `llms-full.txt` |

## Port Allocation
- 4200: Switchyard API (api.enclii.dev)
- 4201: Switchyard UI (app.enclii.dev)
- 4203: Dispatch (superuser infrastructure control)

## The Tripod
- **Depends On**: Janua for authentication (auth.madfam.io)
- **Orchestrates**: All MADFAM services via K8s/ArgoCD
- **Does NOT use**: @madfam/ui (independent Radix UI components)
- **Self-Deploys**: Control plane runs on Enclii itself

## Agent Directives
1. ALWAYS run `golangci-lint run` before Go commits
2. ALWAYS run `pnpm typecheck` before TypeScript commits
3. CHECK `git status && git branch` at session start
4. USE feature branches for all changes
5. READ this file at session start
6. **The "Proof of Life" Standard:** No deployment, refactor, or fix is considered "Complete" until you have successfully `curl`ed the public endpoint (e.g., `https://api.enclii.dev/health`) and received a `200 OK` (or `401 Unauthorized` for protected routes).
   - **Principle:** "Kubernetes Applied" is NOT "Done." "Endpoint Reachable" is "Done."
   - **Failure Protocol:** If the curl fails (502/503/Connection Refused), you MUST diagnose the logs immediately. Do not report success.

## Secret Management Protocols (Safe-Patch Mode)
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
   - `your_key_here`
   - `placeholder`
   - `example`
   - `xxx` or `TODO`
   into active config files (`.env`, `.env.local`)

## Testing Commands
```bash
# Go linting
cd apps/switchyard-api && golangci-lint run

# TypeScript validation
pnpm typecheck
pnpm lint

# Full validation
pnpm build
```

## Self-Deployment Flow
```
Git push → GitHub webhook → Roundhouse build → ArgoCD sync → K8s deployment
```

## Dispatch Capabilities
- Zone management (list, create, delete)
- DNS record management (CRUD)
- Tunnel management (list, get)
- Domain commissioning (Sovereign Registrar flow)
- Subdomain routing to tunnels

## Local Development
```bash
# Start with madfam script
~/labspace/madfam start

# Or start services individually
docker compose up -d
```
