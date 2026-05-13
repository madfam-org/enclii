# Self-Hosting Enclii

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


Enclii is licensed under AGPL-3.0. You can deploy the full platform on your own infrastructure with no capability limits.

## Prerequisites

- Kubernetes cluster (v1.27+) — any provider or bare metal
- PostgreSQL 15+
- Redis 7+
- Domain name with DNS control
- Container registry (GHCR, Docker Hub, ECR, etc.)
- kubectl and Helm 3 installed locally

## Architecture Overview

```
Internet → Ingress (Cloudflare Tunnel / nginx / Traefik)
            ↓
         K8s Services
            ├── switchyard-api   (Go, port 4200)   — Control plane API
            ├── switchyard-ui    (Next.js, port 4201) — Web dashboard
            ├── reconciler       — Deployment controller
            └── roundhouse       — Build worker

         Data Layer
            ├── PostgreSQL       — Primary datastore
            └── Redis            — Cache + session store
```

## Quick Start

### Option A: Hetzner (Terraform provided)

Enclii includes Terraform modules for Hetzner dedicated servers with k3s:

```bash
cd infra/terraform
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars with your Hetzner + Cloudflare credentials

terraform init
terraform plan
terraform apply
```

See `scripts/deploy-production.sh` for the full automated deployment flow.

### Option B: Bring Your Own Cluster

Any Kubernetes cluster works. You need:

1. A namespace for Enclii services
2. PostgreSQL accessible from the cluster
3. Redis accessible from the cluster
4. An ingress controller or tunnel for external access

## Core Services

### 1. switchyard-api

The control plane API. Manages projects, environments, services, deployments.

```bash
# Environment variables (minimum)
ENCLII_DB_URL=postgres://user:pass@host:5432/enclii
ENCLII_REDIS_HOST=redis-host
ENCLII_REDIS_PORT=6379
ENCLII_OIDC_ISSUER=https://your-oidc-provider
ENCLII_REGISTRY=ghcr.io/your-org
ENCLII_PORT=4200
```

K8s manifests are in `apps/switchyard-api/k8s/`.

### 2. switchyard-ui

The web dashboard (Next.js).

```bash
NEXT_PUBLIC_API_URL=https://api.your-domain.com
NEXTAUTH_URL=https://app.your-domain.com
```

### 3. Reconciler

Watches the database for pending deployments and applies them to Kubernetes. Runs as part of the switchyard-api process.

### 4. Roundhouse (Build Worker)

Builds container images from source using Buildpacks or Dockerfiles. Can run in-process or as a separate worker.

## Authentication

### Option A: Janua SSO (OIDC)

Enclii is built to work with [Janua](https://github.com/madfam-org/janua), our open-source SSO server. Deploy Janua separately and configure:

```bash
ENCLII_AUTH_MODE=oidc
ENCLII_OIDC_ISSUER=https://auth.your-domain.com
ENCLII_OIDC_CLIENT_ID=enclii
ENCLII_OIDC_CLIENT_SECRET=your-secret
```

### Option B: Any OIDC Provider

Any OpenID Connect provider works (Auth0, Keycloak, Okta, etc.):

```bash
ENCLII_AUTH_MODE=oidc
ENCLII_OIDC_ISSUER=https://your-provider.com
ENCLII_OIDC_CLIENT_ID=your-client-id
ENCLII_OIDC_CLIENT_SECRET=your-secret
```

### Option C: Local JWT (Development)

For development or air-gapped environments:

```bash
ENCLII_AUTH_MODE=jwt
ENCLII_JWT_SECRET=your-256-bit-secret
```

## Storage

### Persistent Volumes

Enclii uses PVCs for:
- Build cache (roundhouse)
- Artifact storage

Any CSI driver works. The included configuration uses [Longhorn](https://longhorn.io/) — see `infra/helm/longhorn/`.

### Object Storage

SBOMs and build artifacts are stored in S3-compatible storage. The included configuration uses Cloudflare R2:

```bash
ENCLII_S3_ENDPOINT=https://your-account.r2.cloudflarestorage.com
ENCLII_S3_BUCKET=enclii-artifacts
ENCLII_S3_ACCESS_KEY=your-key
ENCLII_S3_SECRET_KEY=your-secret
```

## Ingress

### Option A: Cloudflare Tunnel (provided)

Zero exposed ports, DDoS protection included. Configuration in `infra/k8s/production/cloudflared-unified.yaml`.

### Option B: Any Ingress Controller

nginx, Traefik, HAProxy, etc. Point your ingress to the K8s services on port 80.

## GitOps (Optional)

Enclii includes an ArgoCD App-of-Apps configuration for GitOps-driven deployment:

```bash
# Install ArgoCD
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

# Apply root application
kubectl apply -f infra/argocd/root-application.yaml
```

Individual app definitions are in `infra/argocd/apps/`.

## Database Migrations

Migrations run automatically on API startup. For manual application:

```bash
kubectl exec -n enclii deploy/switchyard-api -- \
  psql "$DATABASE_URL" -f /app/migrations/001_genesis.sql
```

Migration files are in `apps/switchyard-api/internal/db/migrations/` and `apps/switchyard-api/migrations/`.

## What You Get

- Full platform with no artificial capability limits
- Project, environment, and service management
- Build pipeline (Buildpacks + Dockerfile)
- Canary and blue-green deployment strategies
- Automatic rollback on failure
- Custom domain provisioning
- RBAC with admin/developer/viewer roles
- API keys for CI/CD integration
- Webhook notifications (Slack, Discord, Telegram)
- Database add-ons (PostgreSQL, Redis, MySQL provisioning)
- Serverless functions with scale-to-zero

## What's Not Included

- **Managed support** — available under commercial license (contact legal@madfam.io)
- **MADFAM proprietary data** — Forgesight industry datasets are separately licensed

## AGPL-3.0 Obligations

When self-hosting, the AGPL-3.0 network clause (Section 13) applies: if you modify the source code and provide it as a network service, you must make the modified source available to your users. Unmodified deployments have no additional obligations beyond the license terms.

## Reference Documentation

| Topic | Location |
|-------|----------|
| Production deployment roadmap | `docs/production/PRODUCTION_DEPLOYMENT_ROADMAP.md` |
| Production readiness checklist | `docs/production/PRODUCTION_CHECKLIST.md` |
| Infrastructure anatomy | `docs/infrastructure/INFRA_ANATOMY.md` |
| Capacity planning | `docs/infrastructure/CAPACITY_ROADMAP.md` |
| GitOps setup | `docs/infrastructure/GITOPS.md` |
| Storage configuration | `docs/infrastructure/STORAGE.md` |
| Cloudflare integration | `docs/infrastructure/CLOUDFLARE.md` |
| Troubleshooting | `docs/troubleshooting/` |
