---
title: Migrating from Heroku
description: 10-minute path from a Heroku app to Enclii
sidebar_position: 3
tags: [migration, heroku, guides]
---

# Migrating from Heroku to Enclii

Enclii's build system is Paketo — the same buildpack family Heroku helped pioneer. Most Heroku apps deploy to Enclii with zero code changes. For advanced topics (Heroku add-ons, dynos, release phase), see the [full Heroku migration guide](./HEROKU_MIGRATION_GUIDE.md).

## Feature parity at a glance

| Capability | Heroku | Enclii |
|---|---|---|
| Buildpacks | Classic Heroku buildpacks | Paketo buildpacks (Heroku-compatible) |
| Procfile | Native | Native (mapped to services) |
| Git-push deploys | Native | Native (GitHub App) |
| Review Apps | Native | Preview environments per PR (P1.7) |
| Custom domains + TLS | Native | Native (Cloudflare for SaaS) |
| Config vars | Native | `enclii secrets set` |
| Heroku Postgres | Native | P3.1 (managed-DB addon) |
| Heroku Redis | Native | P3.1 (managed-cache addon) |
| Scheduler | Native | `enclii jobs` |
| Rollback | One click | `enclii rollback` |
| Release phase | Native | `spec.release.command` |

## Procfile → `service.yaml`

A Heroku `Procfile` maps to one Enclii service per process type. For a typical Rails app:

```
# Procfile
web: bundle exec puma -C config/puma.rb
worker: bundle exec sidekiq
release: bundle exec rails db:migrate
```

Becomes two services plus a release command:

```yaml
# services/web/service.yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web
spec:
  build:
    type: auto
    command: bundle exec puma -C config/puma.rb
  release:
    command: bundle exec rails db:migrate
  runtime:
    port: 3000
    replicas: 2
    healthCheck: /health
```

```yaml
# services/worker/service.yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: worker
spec:
  build:
    type: auto
    command: bundle exec sidekiq
  runtime:
    type: worker     # no HTTP port
    replicas: 1
```

## 10-minute migration

### 1. Pull Heroku config vars (1 min)

```bash
heroku config --json --app my-heroku-app > heroku-config.json
```

### 2. `enclii init` (1 min)

```bash
enclii init
```

Edit `service.yaml` — Paketo auto-detects your Gemfile / package.json / requirements.txt / go.mod.

### 3. Port config vars to secrets (2 min)

```bash
jq -r 'to_entries[] | "\(.key)=\(.value)"' heroku-config.json \
  | while IFS='=' read -r key value; do
      enclii secrets set "$key" "$value" --env prod
    done
```

### 4. Migrate Heroku Postgres (3 min)

```bash
# Capture and download the latest Heroku backup
heroku pg:backups:capture --app my-heroku-app
heroku pg:backups:download --app my-heroku-app

# Until P3.1 (managed-DB addon), coordinate Postgres provisioning with your operator.
# With P3.1:
#   enclii addon create postgres --name my-app-db --env prod
#   enclii addon attach my-app-db --service web --env prod
# Restore:
pg_restore --verbose --no-owner --no-acl -d "$DATABASE_URL" latest.dump
```

### 5. Deploy (2 min)

```bash
enclii deploy --env prod
```

The Paketo buildpack produces an OCI image. Release phase (`spec.release.command`) runs against the new image before any pods start serving traffic.

### 6. Point your domain (1 min)

```bash
enclii domains add myapp.com --env prod
enclii domains verify myapp.com
```

Flip DNS when the new deployment is healthy. Shut the Heroku app down once traffic stabilizes.

## Review Apps → Preview environments

Heroku Review Apps spin up a fresh environment per PR. Enclii preview environments (P1.7) do the same: open a PR, Enclii builds the branch, deploys to `pr-<n>.preview.enclii.dev`, comments the URL on the PR, and cleans up on merge/close. Enable in the dashboard or via `spec.previewEnvironments.enabled: true`.

## What's different at runtime

- **No dyno types.** `web` vs `worker` is expressed via `spec.runtime.type: http | worker | job`. Resources are set per service, not per dyno hour.
- **No free tier sleep.** Services stay up. To scale to zero, set `replicas: 0` and use Knative autoscale (P3.x).
- **Logs:** `heroku logs --tail` → `enclii logs <service> -f`. Logs are structured JSON by default.
- **Release phase:** Heroku's release phase is `spec.release.command`. It runs once per deploy, against the new image, before the new pods receive traffic. If it fails, the deploy rolls back automatically.

## Next steps

- [Full Heroku migration guide](./HEROKU_MIGRATION_GUIDE.md) — add-ons, dyno sizing, release phase details
- [`enclii jobs`](../cli/commands/jobs.md) — replaces Heroku Scheduler
- [Templates](../templates/templates.md) — Rails and Django starters (coming soon)
- [Service spec reference](../reference/service-spec.md) — all `service.yaml` fields

<!-- TODO(post-first-customer): Document Heroku Redis → Enclii managed-cache addon once P3.1 lands and a customer runs it -->
<!-- TODO(post-first-customer): Add a Rails-on-Heroku → Enclii concrete walkthrough after first customer ships -->
