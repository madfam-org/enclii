---
title: Migrating from Railway
description: 10-minute path from a Railway project to Enclii
sidebar_position: 2
tags: [migration, railway, guides]
---

# Migrating from Railway to Enclii

Railway and Enclii have similar mental models — a project contains services, services get URLs, secrets are per-environment. This guide walks the concrete mapping. For advanced topics (private networking, cron, databases), see the [full Railway migration guide](./RAILWAY_MIGRATION_GUIDE.md).

## Feature parity at a glance

| Capability | Railway | Enclii |
|---|---|---|
| Git-push deploys | Native | Native (GitHub App) |
| Preview environments per PR | Native | Native (P1.7) |
| Custom domains + TLS | Native | Native (Cloudflare for SaaS) |
| Environment variables | Native | `enclii secrets set` |
| Managed Postgres | Native | `enclii addon` (managed Postgres addon) |
| Managed Redis | Native | P3.1 (managed-cache addon, landing) |
| Private service networking | Native | Native (cluster-internal DNS) |
| Cron / scheduled jobs | Native | `enclii jobs` |
| Volumes | Native | Longhorn PVCs (cluster-provisioned) |
| One-click rollback | Native | `enclii rollback` |

**Project model mapping:**

| Railway | Enclii |
|---|---|
| Project | Project |
| Service (one per repo or folder) | Service (one `service.yaml` per deployable unit) |
| Environment (production / staging / preview) | Environment (`--env prod` / `--env staging` / `--env dev`) |
| Plugin (Postgres, Redis) | Addon (`enclii addon`) |

## Service-per-repo vs multi-service-per-repo

Railway lets you define multiple services from a monorepo with different root directories. Enclii supports this with one `service.yaml` per service — typically at the repo root for single-service repos, or at `services/<name>/service.yaml` for monorepos.

Monorepo layout:

```
my-monorepo/
├── services/
│   ├── api/
│   │   ├── service.yaml
│   │   └── src/...
│   ├── worker/
│   │   ├── service.yaml
│   │   └── src/...
│   └── web/
│       ├── service.yaml
│       └── src/...
└── package.json
```

Deploy each from its directory:

```bash
cd services/api && enclii deploy --env prod
cd ../worker && enclii deploy --env prod
```

Or from the repo root with `-f`:

```bash
enclii deploy -f services/api/service.yaml --env prod
```

## 10-minute migration

### 1. Pull Railway env vars (1 min)

```bash
railway variables --json > railway-vars.json
```

### 2. `enclii init` in your repo (1 min)

```bash
enclii init
```

Edit `service.yaml` to match your Railway service:

```yaml
spec:
  build:
    type: auto         # or node / go / python
  runtime:
    port: 8080         # match your Railway PORT
    replicas: 2
    healthCheck: /health
```

### 3. Port env vars (2 min)

```bash
# Register the service from ./service.yaml so the variables have a target
# (--project must match metadata.project in service.yaml)
enclii services-sync --project <project>

# `enclii secrets set` takes KEY=VALUE pairs for the service in ./service.yaml.
# Without --env the values apply to all environments (--env prod only works
# once the environment exists; the first `enclii deploy --env prod` creates it).
jq -r 'to_entries[] | "\(.key)=\(.value)"' railway-vars.json \
  | while IFS= read -r kv; do
      enclii secrets set "$kv" --secret
    done
```

### 4. Database migration (3 min)

Railway Postgres → Enclii managed Postgres is a dump + restore:

```bash
# Export from Railway
railway run pg_dump "$DATABASE_URL" > dump.sql

# Create a managed Postgres addon bound to the service; its connection
# string is injected into the service as DATABASE_URL.
enclii addon plans
enclii addon create my-app-db --plan standard-0 --service <service-id>

# Restore. DATABASE_URL is the addon's connection string; run this from a host
# that can reach the addon (the CLI has no port-forward command).
psql "$DATABASE_URL" < dump.sql
```

See [database operations](./database-operations.md) for what the addon CLI covers.

### 5. Deploy (2 min)

```bash
enclii deploy --env prod
```

### 6. Point your domain (1 min)

```bash
enclii domains add myapp.com --env prod
enclii domains verify myapp.com --env prod
```

When healthy, shut the Railway service down.

## What's different at runtime

- **Networking:** Services in the same Enclii project resolve each other via cluster DNS. Set `API_URL=http://api.my-project.svc.cluster.local` for intra-cluster calls — no public hop needed.
- **Volumes:** Longhorn PVCs replace Railway volumes. Declare in `service.yaml`:
  ```yaml
  spec:
    volumes:
      - name: data
        size: 10Gi
        mountPath: /data
  ```
- **Sleep-on-idle:** Railway hobby services sleep. Enclii doesn't sleep by default — set `spec.runtime.replicas: 0` and use Knative autoscale (P3.x) when you need scale-to-zero.

## Next steps

- [Full Railway migration guide](./RAILWAY_MIGRATION_GUIDE.md) — full coverage of plugins, cron, and private networking
- [Database operations](./database-operations.md) — managed Postgres addons
- [Onboarding guide](./ONBOARDING_GUIDE.md) — per-PR preview environments (P1.7)
- [`enclii jobs`](../cli/commands/jobs.md) — cron / scheduled tasks

<!-- TODO(post-first-customer): Document multi-service monorepo deploy with pnpm workspaces real example -->
<!-- TODO(post-first-customer): Add Railway webhook → Enclii event bus migration once a customer needs it -->
