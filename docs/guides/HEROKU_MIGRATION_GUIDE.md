# Heroku to Enclii Migration Guide

**Version**: 1.0
**Last Updated**: 2026-01-30
**Estimated Migration Time**: 2-4 hours per app

---

## Table of Contents

1. [Pre-Migration Checklist](#pre-migration-checklist)
2. [Concept Mapping](#concept-mapping)
3. [Procfile Conversion](#procfile-conversion)
4. [Database Migration](#database-migration)
5. [Add-on Equivalents](#add-on-equivalents)
6. [Environment Variables & Secrets](#environment-variables--secrets)
7. [Buildpacks Compatibility](#buildpacks-compatibility)
8. [Custom Domains & Routing](#custom-domains--routing)
9. [CLI Command Equivalents](#cli-command-equivalents)
10. [Deployment & Verification](#deployment--verification)
11. [Migration Examples](#migration-examples)
12. [Common Issues](#common-issues)

---

## Pre-Migration Checklist

### 1. Audit Your Heroku Setup

```bash
# List all apps
heroku apps

# Check app info
heroku info -a your-app

# List add-ons
heroku addons -a your-app

# Export environment variables
heroku config -a your-app --json > heroku-env.json

# Check dyno formation
heroku ps -a your-app

# Check Procfile
cat Procfile

# List custom domains
heroku domains -a your-app
```

**Document:**
- [ ] App names and types (web, worker)
- [ ] Dyno sizes and counts
- [ ] Add-ons (Postgres, Redis, etc.)
- [ ] Environment variables (especially secrets)
- [ ] Custom domains
- [ ] Procfile commands
- [ ] Buildpack(s) in use
- [ ] Pipeline configuration (staging/production)

### 2. Prepare Enclii Environment

```bash
# Install Enclii CLI
curl -fsSL https://install.enclii.dev | sh

# Login
enclii login

# Create project
enclii project create my-app

# Create environments
enclii env create production
enclii env create staging
```

---

## Concept Mapping

| Heroku | Enclii | Notes |
|--------|--------|-------|
| App | Project | Container for services and environments |
| Dyno | Pod | Container instance running your code |
| Procfile | Dockerfile CMD / enclii.yaml command | Service start command |
| Config Vars | Environment Variables / Secrets | `enclii secret create` for sensitive values |
| Add-ons | In-cluster services or external providers | PostgreSQL and Redis run in-cluster |
| Buildpacks | Cloud Native Buildpacks | Compatible — most apps build without changes |
| Review Apps | Preview Environments | Auto-created for PRs |
| Pipeline | Environments (staging → production) | Promotion via `enclii deploy --env` |
| Heroku Scheduler | Timetable (cron jobs) | `enclii.yaml` cron spec |
| Heroku Redis | In-cluster Redis | Self-hosted, $0 cost |
| Heroku Postgres | In-cluster PostgreSQL | Self-hosted with PVC storage, daily backups to R2 |
| Private Spaces | Kubernetes namespaces + NetworkPolicy | Network isolation per environment |
| Heroku CI | GitHub webhook CI/CD | Automatic builds on push |

---

## Procfile Conversion

Heroku uses a `Procfile` to declare process types. In Enclii, each process type becomes a separate service.

### Single Web Process

```procfile
# Heroku Procfile
web: npm start
```

Becomes:

```yaml
# enclii.yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web
spec:
  build:
    buildpack: auto
  runtime:
    port: 3000
    command: ["npm", "start"]
    replicas: 2
    healthCheck:
      type: http
      path: /health
      port: 3000
```

### Web + Worker

```procfile
# Heroku Procfile
web: npm start
worker: npm run worker
```

Becomes two Enclii services:

```yaml
# web.yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web
spec:
  build:
    buildpack: auto
  runtime:
    port: 3000
    command: ["npm", "start"]
    replicas: 2
    healthCheck:
      path: /health
```

```yaml
# worker.yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: worker
spec:
  build:
    buildpack: auto
  runtime:
    command: ["npm", "run", "worker"]
    replicas: 1
```

### Release Phase

```procfile
# Heroku Procfile
release: npm run migrate
web: npm start
```

In Enclii, use an init container or a pre-deploy job:

```yaml
# enclii.yaml
spec:
  jobs:
    - name: migrate
      command: ["npm", "run", "migrate"]
      phase: pre-deploy
  runtime:
    command: ["npm", "start"]
```

---

## Database Migration

### Heroku Postgres → Enclii PostgreSQL

Enclii runs PostgreSQL in-cluster with PVC storage and daily backups to Cloudflare R2.

**1. Export from Heroku**

```bash
# Create a backup
heroku pg:backups:capture -a your-app

# Download the backup
heroku pg:backups:download -a your-app
# Creates: latest.dump
```

**2. Import to Enclii PostgreSQL**

```bash
# Get Enclii database connection info
kubectl get secret db-credentials -n enclii -o jsonpath='{.data.password}' | base64 -d

# Port-forward to access the database
kubectl port-forward svc/postgresql -n enclii 5433:5432

# Restore the dump
pg_restore --verbose --clean --no-acl --no-owner \
  -h localhost -p 5433 -U postgres -d myapp latest.dump
```

**3. Verify Data**

```bash
psql -h localhost -p 5433 -U postgres -d myapp \
  -c "SELECT schemaname, tablename, n_live_tup FROM pg_stat_user_tables ORDER BY n_live_tup DESC;"
```

### Heroku Redis → Enclii Redis

Enclii runs Redis in-cluster. For most use cases (caching, sessions), no data migration is needed — the cache rebuilds naturally.

**If you need to migrate Redis data:**

```bash
# Export from Heroku Redis
heroku redis:cli -a your-app
> BGSAVE
> exit

# For key-by-key migration
heroku redis:cli -a your-app --command "KEYS *" | while read key; do
  heroku redis:cli -a your-app --command "DUMP $key" | \
    redis-cli -h <enclii-redis> RESTORE "$key" 0
done
```

---

## Add-on Equivalents

| Heroku Add-on | Enclii Equivalent | Notes |
|---------------|-------------------|-------|
| **Heroku Postgres** | In-cluster PostgreSQL | Self-hosted, PVC storage, R2 backups |
| **Heroku Redis** | In-cluster Redis | Self-hosted, Sentinel-ready |
| **Heroku Scheduler** | Timetable (cron jobs) | Native K8s CronJobs |
| **Papertrail** | Signal (observability stack) | Logs, metrics, traces |
| **New Relic / Scout** | Prometheus + Grafana | In-cluster monitoring |
| **SendGrid / Mailgun** | External service | Use same provider, update env vars |
| **Cloudinary** | Cloudflare R2 + Images | Zero-egress object storage |
| **Memcachier** | In-cluster Redis | Use Redis as cache |
| **RabbitMQ (CloudAMQP)** | External or in-cluster | Deploy via Helm chart |
| **Elasticsearch (Bonsai)** | External or in-cluster | Deploy via Helm chart |

For add-ons that are external SaaS services (SendGrid, Stripe, etc.), no migration is needed — just update environment variables.

---

## Environment Variables & Secrets

### Export from Heroku

```bash
# Export as JSON
heroku config -a your-app --json > heroku-env.json

# Export as shell format
heroku config -a your-app -s > heroku.env
```

### Import to Enclii

**Option 1: Enclii Lockbox (Recommended)**

```bash
# Import secrets one by one
enclii secret create DATABASE_URL "postgresql://..." --env production
enclii secret create REDIS_URL "redis://redis.enclii.svc.cluster.local:6379" --env production
enclii secret create SECRET_KEY_BASE "$(openssl rand -hex 64)" --env production
```

**Option 2: Bulk Import**

```bash
#!/bin/bash
# import-heroku-env.sh

while IFS='=' read -r key value; do
  [[ $key =~ ^#.*$ ]] && continue
  [[ -z $key ]] && continue

  # Skip Heroku-specific variables
  [[ $key =~ ^HEROKU_ ]] && continue
  [[ $key == "DYNO" ]] && continue

  value=$(echo "$value" | sed "s/^'//; s/'$//")

  echo "Importing $key..."
  enclii secret create "$key" "$value" --env production
done < heroku.env

echo "Import complete!"
```

### Variable Name Mapping

| Heroku Variable | Enclii Equivalent | Action |
|-----------------|-------------------|--------|
| `DATABASE_URL` | `DATABASE_URL` | Update connection string to Enclii PostgreSQL |
| `REDIS_URL` | `REDIS_URL` | Update to `redis://redis.<ns>.svc.cluster.local:6379` |
| `PORT` | `PORT` | Same — set in `enclii.yaml` runtime.port |
| `HEROKU_*` | N/A | Remove — not needed |
| `DYNO` | N/A | Remove — use `HOSTNAME` env in K8s |
| `SECRET_KEY_BASE` | `SECRET_KEY_BASE` | Same — keep existing value |
| `WEB_CONCURRENCY` | Resource limits | Configure in `enclii.yaml` resources section |

---

## Buildpacks Compatibility

Enclii uses **Cloud Native Buildpacks (CNB)**, which are compatible with most Heroku buildpacks. Your app should build without changes in most cases.

### Automatic Detection

```bash
# Enclii auto-detects your app type
enclii init --buildpack auto
```

Supported runtimes (auto-detected):
- **Node.js** — `package.json`
- **Python** — `requirements.txt` or `Pipfile`
- **Ruby** — `Gemfile`
- **Go** — `go.mod`
- **Java** — `pom.xml` or `build.gradle`
- **PHP** — `composer.json`

### Using a Dockerfile Instead

If you need more control, create a `Dockerfile`:

```dockerfile
FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --production
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
```

```yaml
# enclii.yaml
spec:
  build:
    dockerfile: Dockerfile
```

### Multi-Buildpack Apps

Heroku multi-buildpack setups (e.g., Node.js + Python) work best with a Dockerfile on Enclii:

```dockerfile
FROM python:3.12-slim
RUN apt-get update && apt-get install -y nodejs npm
WORKDIR /app
COPY requirements.txt ./
RUN pip install -r requirements.txt
COPY package*.json ./
RUN npm ci
COPY . .
CMD ["python", "app.py"]
```

---

## Custom Domains & Routing

### Export from Heroku

```bash
heroku domains -a your-app
```

### Configure in Enclii

Enclii uses Cloudflare Tunnel for ingress — no exposed node ports.

```
Internet → Cloudflare Edge → cloudflared → K8s Service:80 → Container:port
           (TLS, DDoS)       (tunnel)      (ClusterIP)
```

**1. Add Domain**

```bash
enclii domain add myapp.com \
  --service web \
  --env production \
  --tls-enabled
```

**2. Update DNS**

Point your domain to the Cloudflare Tunnel:

```dns
myapp.com.      300  IN  CNAME  <tunnel-id>.cfargotunnel.com
www.myapp.com.  300  IN  CNAME  <tunnel-id>.cfargotunnel.com
```

SSL is automatic via Cloudflare — no cert-manager configuration needed.

**3. Verify**

```bash
curl https://myapp.com/health
```

---

## CLI Command Equivalents

| Heroku CLI | Enclii CLI | Description |
|------------|------------|-------------|
| `heroku create` | `enclii project create` | Create a new project |
| `heroku apps` | `enclii project list` | List projects |
| `heroku ps` | `enclii ps` | Show running processes |
| `heroku logs -t` | `enclii logs <service> -f` | Tail logs |
| `heroku config` | `enclii secret list` | List environment variables |
| `heroku config:set K=V` | `enclii secret create K V` | Set environment variable |
| `heroku run bash` | `enclii exec <service> -- bash` | Open shell in container |
| `heroku pg:psql` | `kubectl exec ... -- psql` | Connect to database |
| `heroku domains` | `enclii domain list` | List custom domains |
| `heroku domains:add` | `enclii domain add` | Add custom domain |
| `heroku releases` | `enclii releases list` | List releases |
| `heroku rollback` | `enclii rollback <service>` | Rollback to previous release |
| `heroku maintenance:on` | `enclii maintenance enable` | Enable maintenance mode |
| `heroku pg:backups` | `enclii backup list` | List database backups |
| `heroku addons` | N/A (in-cluster services) | Add-ons are built-in |
| `git push heroku main` | `git push origin main` | Deploy (auto via webhook) |

---

## Deployment & Verification

### Deploy to Staging

```bash
# Deploy
enclii deploy --env staging

# Monitor
enclii status --env staging --follow

# Check logs
enclii logs web --env staging --follow

# Smoke test
curl https://staging.myapp.com/health
```

### Deploy to Production

```bash
# Deploy with canary strategy
enclii deploy --env production --strategy canary --canary-percent 10

# Monitor error rates
enclii logs web --env production --follow | grep ERROR

# Full rollout
enclii deploy --env production

# Or rollback if issues
enclii rollback web --env production
```

### Verification Checklist

- [ ] All services healthy: `enclii status --env production`
- [ ] HTTPS works: `curl https://myapp.com`
- [ ] Database connectivity: test critical endpoints
- [ ] Background workers running: `enclii ps --env production`
- [ ] Cron jobs scheduled: `enclii jobs list --env production`
- [ ] Logs flowing: `enclii logs --env production`
- [ ] Custom domains resolve: `nslookup myapp.com`
- [ ] Health checks passing: `curl https://myapp.com/health`

---

## Migration Examples

### Example 1: Rails App with Postgres

**Heroku Setup:**
- Rails 7 app with Postgres and Redis
- Web + Sidekiq worker
- Custom domain: `myapp.com`

**Migration:**

```bash
# 1. Export Heroku database
heroku pg:backups:download -a myapp

# 2. Export environment
heroku config -a myapp -s > heroku.env

# 3. Create Enclii project
enclii project create myapp
enclii env create production

# 4. Import database
kubectl port-forward svc/postgresql -n enclii 5433:5432
pg_restore --verbose --clean --no-acl --no-owner \
  -h localhost -p 5433 -U postgres -d myapp latest.dump

# 5. Create web service
cat > web.yaml <<EOF
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web
spec:
  build:
    buildpack: auto
  runtime:
    port: 3000
    command: ["bundle", "exec", "puma", "-C", "config/puma.rb"]
    replicas: 2
    healthCheck:
      path: /health
  secrets:
    - DATABASE_URL
    - REDIS_URL
    - SECRET_KEY_BASE
    - RAILS_MASTER_KEY
  routes:
    - domain: myapp.com
      tlsEnabled: true
EOF

# 6. Create worker service
cat > worker.yaml <<EOF
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: worker
spec:
  build:
    buildpack: auto
  runtime:
    command: ["bundle", "exec", "sidekiq"]
    replicas: 1
  secrets:
    - DATABASE_URL
    - REDIS_URL
    - SECRET_KEY_BASE
EOF

# 7. Import secrets
enclii secret create DATABASE_URL "postgresql://postgres:pass@postgresql.enclii:5432/myapp" --env production
enclii secret create REDIS_URL "redis://redis.enclii.svc.cluster.local:6379" --env production
enclii secret create SECRET_KEY_BASE "$(heroku config:get SECRET_KEY_BASE -a myapp)" --env production
enclii secret create RAILS_MASTER_KEY "$(heroku config:get RAILS_MASTER_KEY -a myapp)" --env production

# 8. Deploy
enclii service create -f web.yaml --env production
enclii service create -f worker.yaml --env production
enclii deploy --env production

# 9. Run migrations
enclii exec web --env production -- bundle exec rails db:migrate

# 10. Configure domain and switch DNS
enclii domain add myapp.com --service web --env production
```

### Example 2: Node.js API with Redis Caching

**Heroku Setup:**
- Express API
- Heroku Redis for caching
- Heroku Scheduler for cleanup jobs

**Migration:**

```bash
# 1. Create Enclii service
cat > enclii.yaml <<EOF
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: api
spec:
  build:
    buildpack: auto
  runtime:
    port: 3000
    command: ["npm", "start"]
    replicas: 2
    healthCheck:
      path: /health
  env:
    - name: NODE_ENV
      value: production
  secrets:
    - DATABASE_URL
    - REDIS_URL
    - API_SECRET
  jobs:
    - name: cleanup
      command: ["npm", "run", "cleanup"]
      schedule: "0 2 * * *"
  routes:
    - domain: api.myapp.com
      tlsEnabled: true
EOF

# 2. Import secrets
enclii secret create DATABASE_URL "postgresql://..." --env production
enclii secret create REDIS_URL "redis://redis.enclii.svc.cluster.local:6379" --env production
enclii secret create API_SECRET "$(heroku config:get API_SECRET -a myapp)" --env production

# 3. Deploy
enclii service create -f enclii.yaml --env production
enclii deploy --env production

# 4. Test
curl https://api.myapp.com/health
```

---

## Common Issues

### Issue 1: Buildpack Detection Fails

**Symptom:** Build fails with "unable to detect application type"

**Fix:** Ensure your project has the correct marker file:
- Node.js: `package.json` in root
- Python: `requirements.txt` or `Pipfile`
- Ruby: `Gemfile`

Or switch to a Dockerfile:
```yaml
spec:
  build:
    dockerfile: Dockerfile
```

### Issue 2: PORT Mismatch

**Symptom:** Service starts but health checks fail

**Diagnosis:** Heroku dynamically assigns `$PORT`. Enclii uses a fixed port.

**Fix:** Ensure your app reads `PORT` from environment:
```javascript
const port = process.env.PORT || 3000
app.listen(port)
```

And `enclii.yaml` matches:
```yaml
runtime:
  port: 3000
```

### Issue 3: Missing Heroku-Specific Environment Variables

**Symptom:** App crashes referencing `HEROKU_*` variables

**Fix:** Remove or replace Heroku-specific variables:
```bash
# Replace HEROKU_APP_NAME
enclii secret create APP_NAME "myapp" --env production

# Replace HEROKU_SLUG_COMMIT
# Git SHA is available via build metadata
```

### Issue 4: Worker Not Processing Jobs

**Symptom:** Sidekiq/Celery/Bull worker not picking up jobs

**Fix:** Ensure the worker service can reach Redis:
```bash
# Verify Redis connectivity
enclii exec worker --env production -- redis-cli -h redis.enclii.svc.cluster.local ping
```

Update `REDIS_URL` to use Kubernetes service discovery:
```
redis://redis.<namespace>.svc.cluster.local:6379
```

---

## Decommission Heroku

After 2-4 weeks of stable Enclii operation:

```bash
# 1. Verify zero traffic on Heroku
heroku logs -a your-app --tail  # Should show no requests

# 2. Final backup
heroku pg:backups:capture -a your-app
heroku pg:backups:download -a your-app -o final-backup.dump

# 3. Scale down Heroku dynos
heroku ps:scale web=0 worker=0 -a your-app

# 4. Delete Heroku app (after confirmation period)
heroku apps:destroy your-app --confirm your-app
```

---

## Support & Resources

### Documentation
- Enclii Docs: https://docs.enclii.dev
- Heroku Migration FAQ: https://docs.enclii.dev/faq/migration#heroku-migration
- Enclii CLI Reference: https://docs.enclii.dev/cli

### Community
- Discord: https://discord.gg/enclii
- GitHub Issues: https://github.com/madfam-org/enclii/issues

### Professional Services
- Email: support@enclii.dev
- Migration Consulting: https://enclii.dev/services/migration

---

## Cost Comparison

| Component | Heroku | Enclii |
|-----------|--------|--------|
| 2 Standard Dynos | $50/month | Included |
| Heroku Postgres (Standard) | $50/month | $0 (in-cluster) |
| Heroku Redis (Premium) | $15/month | $0 (in-cluster) |
| Custom Domain SSL | Free | Free (Cloudflare) |
| CI/CD (Heroku CI) | $10/month | $0 (GitHub webhooks) |
| **Total** | **$125+/month** | **~$55/month (shared infrastructure)** |

Enclii's infrastructure cost is shared across all your services, so additional apps add zero marginal infrastructure cost.

---

**Next Steps:** After completing this migration, see the [Railway Migration Guide](./RAILWAY_MIGRATION_GUIDE.md) or [Vercel Migration Guide](./VERCEL_MIGRATION_GUIDE.md) if you have services on other platforms.
