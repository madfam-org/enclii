# Enclii - System Context

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Version:** 1.0.0
**Last Updated:** 2026-01-21

## Overview

Enclii is MADFAM's open source DevOps platform for deploying, scaling, and operating containerized services. It provides streamlined deployment experience on bare metal infrastructure with GitOps automation and zero vendor lock-in.

## Architecture

| Component | Port | Domain | Description |
|-----------|------|--------|-------------|
| Switchyard API | 4200 (container) / 80 (K8s service) | api.enclii.dev | Control plane API |
| Switchyard UI | 4201 (container) / 80 (K8s service) | app.enclii.dev | User dashboard |
| Dispatch | 4203 (container) / 80 (K8s service) | admin.enclii.dev | Infrastructure control tower |
| Landing Page | 80 (container) / 80 (K8s service) | enclii.dev, www.enclii.dev | Marketing site |
| Docs | 80 (container) / 80 (K8s service) | docs.enclii.dev | Documentation |

## Key Components

### Dispatch (Admin UI)
**Purpose:** Superuser interface for managing infrastructure - domains, tunnels, and ecosystem resources.

**Authorization Model:**
- Email domain must be in `ALLOWED_ADMIN_DOMAINS` (default: `@madfam.io`)
- User role must be in `ALLOWED_ADMIN_ROLES` (default: `superadmin,admin,operator`)

**Key Files:**
| Purpose | Location |
|---------|----------|
| Middleware | `apps/admin-console/middleware.ts` |
| Auth Context | `apps/admin-console/contexts/AuthContext.tsx` |
| K8s Deployment | `apps/admin-console/k8s/deployment.yaml` |

### Cloudflare Tunnel
**Purpose:** Zero-trust ingress routing all external traffic through Cloudflare.

**Configuration:** `infra/k8s/production/cloudflared-unified.yaml`

**Architecture:**
```
Internet → Cloudflare Edge → cloudflared pods → K8s Service:80 → Container:4xxx
           (TLS, DDoS)        (2 replicas)       (ClusterIP)      (targetPort)
```

## Kubernetes Resources

| Resource | Namespace | Purpose |
|----------|-----------|---------|
| `switchyard-api` | enclii | Control plane API |
| `switchyard-ui` | enclii | User dashboard |
| `dispatch` | enclii | Admin interface |
| `cloudflared` | cloudflare-tunnel | Ingress tunnel |

## Authentication

Enclii uses **Janua** for all authentication:
- `NEXT_PUBLIC_JANUA_URL`: `https://auth.madfam.io`
- OIDC flow with RS256 JWT validation

## Critical Configuration

### Dispatch Environment Variables
```yaml
ALLOWED_ADMIN_DOMAINS: "@madfam.io"
ALLOWED_ADMIN_ROLES: "superadmin,admin,operator"
NEXT_PUBLIC_JANUA_URL: "https://auth.madfam.io"
```

### Cloudflare Tunnel Routing
Key routes in `cloudflared-unified.yaml`:
- `admin.enclii.dev` → `dispatch.enclii.svc.cluster.local:80`
- `app.enclii.dev` → `switchyard-ui.enclii.svc.cluster.local:80`
- `api.enclii.dev` → `switchyard-api.enclii.svc.cluster.local:80`

## Troubleshooting

### Check Dispatch health
```bash
curl -s https://admin.enclii.dev/api/health
```

### View Dispatch logs
```bash
kubectl logs -n enclii -l app=dispatch -f
```

### Check Cloudflare tunnel
```bash
kubectl logs -n cloudflare-tunnel -l app=cloudflared -f
```

### Restart Cloudflare tunnel (after config changes)
```bash
kubectl rollout restart deployment/cloudflared -n cloudflare-tunnel
```

## Related Documentation
- [CLAUDE.md](./CLAUDE.md) - Full development guide
- [llms.txt](./llms.txt) - LLM context (compact)
- [llms-full.txt](./llms-full.txt) - LLM context (full)
- [Janua System Context](../janua/SYSTEM_CONTEXT.md)
- [Dhanam System Context](../dhanam/SYSTEM_CONTEXT.md)

---

## Operation Ratchet - Rules of Engagement

**Purpose:** Stability enforcement to prevent configuration drift and regression.

### Rule 1: Always Validate Before Confirming Task Complete

```bash
./scripts/validate.sh --golden
```

Run this before confirming any task is complete. The script checks:
- Critical config keys in K8s manifests
- Golden config drift detection
- Go linting (when available)

### Rule 2: Never Remove Protected Config Keys

The following keys are **protected** and must never be removed from manifests:

| Key | File | Impact |
|-----|------|--------|
| `ENCLII_OIDC_ISSUER` | environment-patch.yaml | Breaks SSO |
| `ENCLII_OIDC_CLIENT_ID` | environment-patch.yaml | Breaks SSO |
| `ENCLII_OIDC_CLIENT_SECRET` | environment-patch.yaml | Breaks SSO |
| `ENCLII_EXTERNAL_JWKS_URL` | environment-patch.yaml | Breaks JWT validation |
| `ALLOWED_ADMIN_DOMAINS` | dispatch/deployment.yaml | Breaks admin access |
| `ALLOWED_ADMIN_ROLES` | dispatch/deployment.yaml | Breaks admin access |
| `imagePullSecrets:` | roundhouse.yaml, dispatch/deployment.yaml | Breaks image pulls |
| `hostname: api.enclii.dev` | cloudflared-unified.yaml | Breaks API routing |
| `hostname: app.enclii.dev` | cloudflared-unified.yaml | Breaks UI routing |
| `hostname: admin.enclii.dev` | cloudflared-unified.yaml | Breaks Dispatch routing |
| `hostname: auth.madfam.io` | cloudflared-unified.yaml | Breaks Janua SSO |

### Rule 3: Check for NEXT_PUBLIC_* Env Vars in Dockerfiles

Next.js build-time environment variables (`NEXT_PUBLIC_*`) must be defined in:
- `apps/switchyard-ui/Dockerfile`
- `apps/admin-console/Dockerfile`

Specifically verify:
- `NEXT_PUBLIC_JANUA_URL` - Required for SSO to work

### Rule 4: If Golden Config Fails, Fix or Explicitly Update

When `./scripts/check-golden.sh` fails:

1. **Review the diff** - Understand what changed
2. **If intentional:** Run `./scripts/update-golden.sh` to update snapshots
3. **If unintentional:** Revert the manifest changes

**Never** update golden configs just to make CI pass without understanding the changes.

### Protected Manifests

Golden snapshots are maintained for:

| Manifest | Golden Snapshot | Purpose |
|----------|-----------------|---------|
| `infra/k8s/production/environment-patch.yaml` | `tests/golden/k8s/production/environment-patch.yaml.golden` | SSO/OIDC config |
| `infra/k8s/production/cloudflared-unified.yaml` | `tests/golden/k8s/production/cloudflared-unified.yaml.golden` | Tunnel routes |
| `infra/k8s/production/security-patch.yaml` | `tests/golden/k8s/production/security-patch.yaml.golden` | Security context |
| `infra/k8s/base/roundhouse.yaml` | `tests/golden/k8s/base/roundhouse.yaml.golden` | Build pipeline |
| `apps/admin-console/k8s/deployment.yaml` | `tests/golden/k8s/apps/admin-console-deployment.yaml.golden` | Admin UI |

### CI Integration

Two CI jobs enforce these rules:

1. **manifest-audit** - Checks for protected config keys
2. **golden-config** - Compares manifests against golden snapshots

Both must pass for CI to succeed.

### Quick Reference

```bash
# Run full validation
./scripts/validate.sh --golden

# Update golden configs (after intentional changes)
./scripts/update-golden.sh

# Check golden configs only
./scripts/check-golden.sh
```
