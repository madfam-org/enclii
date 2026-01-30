---
title: Migration FAQ
description: Moving from other platforms to Enclii
sidebar_position: 5
tags: [faq, migration, railway, vercel, heroku]
---

# Migration FAQ

Questions about migrating to Enclii from other platforms.

## General Migration

### How long does migration take?

| Complexity | Timeline | Description |
|------------|----------|-------------|
| Simple | 1-2 hours | Single service, no database |
| Medium | 1-2 days | Multiple services, database |
| Complex | 1-2 weeks | Large app, multiple databases, custom domains |

Most migrations can be done incrementally with zero downtime.

### Can I migrate gradually?

Yes! Recommended approach:

1. **Deploy to Enclii** alongside existing platform
2. **Test** in staging environment
3. **Switch DNS** to Enclii
4. **Monitor** for issues
5. **Decommission** old platform

### Do I need to change my code?

Usually minimal changes:
- Update environment variable names (if different)
- Ensure health endpoint exists
- Use environment-based configuration

Enclii is designed to be compatible with most existing apps.

### What about my database?

Options for database migration:
1. **Export/Import**: pg_dump → pg_restore
2. **Replication**: Set up follower, then promote
3. **New database**: Start fresh if possible

See [Database Operations](/docs/guides/database-operations) for detailed steps.

## Railway Migration

### How do I migrate from Railway?

See our detailed [Railway Migration Guide](/docs/guides/RAILWAY_MIGRATION_GUIDE).

**Quick steps**:
1. Export Railway environment variables
2. Create Enclii project and service
3. Import environment variables
4. Connect GitHub repository
5. Deploy and test
6. Switch DNS

### Are Railway features supported?

| Railway Feature | Enclii Equivalent |
|-----------------|-------------------|
| Services | Services |
| Environments | Environments |
| Variables | Environment Variables / Secrets |
| Domains | Custom Domains |
| Builds | Buildpacks / Dockerfile |
| Databases | Database Addons |
| Private Networks | Kubernetes Services |
| Webhooks | GitHub Webhooks |

### What about Railway's private networking?

Enclii uses Kubernetes service discovery:

```
# Railway
redis.railway.internal:6379

# Enclii
redis.<namespace>.svc.cluster.local:6379
```

Update your connection strings accordingly.

## Vercel Migration

### How do I migrate from Vercel?

See our detailed [Vercel Migration Guide](/docs/guides/VERCEL_MIGRATION_GUIDE).

**Key differences**:
- Vercel is edge-first, Enclii is container-first
- Serverless functions → Container services
- Edge middleware → Regular middleware

### Can I migrate Vercel serverless functions?

Yes, but they become regular services:

```javascript
// Vercel (serverless)
export default function handler(req, res) {
  res.json({ hello: 'world' })
}

// Enclii (Express server)
import express from 'express'
const app = express()
app.get('/api/hello', (req, res) => {
  res.json({ hello: 'world' })
})
app.listen(process.env.PORT || 3000)
```

### What about Next.js?

Next.js works great on Enclii:

```dockerfile
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./
EXPOSE 3000
CMD ["npm", "start"]
```

API routes, SSR, and ISR all work. Edge runtime requires adaptation.

## Heroku Migration

### How do I migrate from Heroku?

**Environment translation**:

| Heroku | Enclii |
|--------|--------|
| Dyno | Pod |
| Procfile | Dockerfile CMD |
| Config Vars | Environment Variables |
| Add-ons | Database Addons |
| Buildpacks | Buildpacks (same!) |
| Review Apps | Preview Environments |

**Procfile example**:
```procfile
# Heroku
web: npm start
worker: npm run worker
```

Becomes two Enclii services, each with its own command.

### What about Heroku Postgres?

Export and import:

```bash
# Export from Heroku
heroku pg:backups:download -a your-app

# Restore to Enclii Postgres
pg_restore --verbose --clean --no-acl --no-owner \
  -h <enclii-db-host> -U <user> -d <database> latest.dump
```

### Are Heroku buildpacks compatible?

Enclii uses Cloud Native Buildpacks, which are compatible with most Heroku buildpacks. Your app should build without changes.

## Database Migration

### How do I migrate PostgreSQL?

**Option 1: pg_dump/pg_restore**
```bash
# Export from source
pg_dump -Fc -h <source> -U <user> <database> > backup.dump

# Import to Enclii
pg_restore -h <enclii-db> -U <user> -d <database> backup.dump
```

**Option 2: Logical replication**
1. Set up source as publisher
2. Set up Enclii DB as subscriber
3. Let data sync
4. Promote Enclii DB

### How do I migrate Redis?

**Option 1: RDB dump**
```bash
# Get RDB from source
redis-cli -h <source> --rdb dump.rdb

# Restore on Enclii
redis-cli -h <enclii-redis> --pipe < dump.rdb
```

**Option 2: Key migration** (for small datasets)
```bash
redis-cli -h <source> --scan | xargs redis-cli -h <source> DUMP | \
  xargs -I {} redis-cli -h <target> RESTORE {} 0
```

### What about managed databases?

You can continue using external databases:
- Supabase
- PlanetScale
- Neon
- AWS RDS

Just update connection strings in Enclii environment variables.

## Domain Migration

### How do I migrate my domain?

1. **Add domain to Enclii**:
   ```bash
   enclii domains add --service <id> --domain example.com
   ```

2. **Update DNS** (at your registrar):
   ```
   Type: CNAME
   Name: www
   Value: <tunnel-id>.cfargotunnel.com
   ```

3. **SSL is automatic** via Cloudflare

### Can I do a zero-downtime DNS switch?

Yes, with low TTL:

1. Lower TTL to 60 seconds (24h before)
2. Deploy and verify on Enclii
3. Switch DNS record
4. Monitor for 1 hour
5. Restore normal TTL

### What about email (MX records)?

Enclii handles web traffic only. Keep your MX records unchanged:
- Only update A/CNAME for web
- MX, SPF, DKIM stay at current provider

## Environment Variables

### How do I export from another platform?

**Railway**:
```bash
railway variables --json > env.json
```

**Vercel**:
```bash
vercel env pull .env
```

**Heroku**:
```bash
heroku config -a your-app --json > env.json
```

### How do I import to Enclii?

```bash
# From .env file
enclii services env import --service <id> --file .env

# Individual variables
enclii services env set --service <id> KEY=value

# Secrets (encrypted)
enclii secrets set --service <id> SECRET_KEY=sensitive
```

### Are variable names the same?

Usually, but check for platform-specific variables:

| Platform Variable | Enclii Equivalent |
|------------------|-------------------|
| `RAILWAY_*` | Remove (not needed) |
| `VERCEL_*` | Remove (not needed) |
| `HEROKU_*` | Remove (not needed) |
| `DATABASE_URL` | `DATABASE_URL` (same) |
| `PORT` | `PORT` (same) |

## CI/CD Migration

### How do I migrate GitHub Actions?

Enclii handles builds automatically. You can simplify your workflow:

```yaml
# Before (Railway/Vercel)
name: Deploy
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: railway up  # or vercel deploy

# After (Enclii) - No workflow needed!
# Just push to GitHub, Enclii handles the rest
```

### What if I need custom CI?

Use the Enclii API:

```yaml
name: Custom CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: npm test
      - run: |
          curl -X POST https://api.enclii.dev/v1/services/$SERVICE_ID/deploy \
            -H "Authorization: Bearer $ENCLII_TOKEN"
```

## Related Documentation

- **Railway Guide**: [Railway Migration Guide](/docs/guides/RAILWAY_MIGRATION_GUIDE)
- **Vercel Guide**: [Vercel Migration Guide](/docs/guides/VERCEL_MIGRATION_GUIDE)
- **Heroku Guide**: [Heroku Migration Guide](/docs/guides/HEROKU_MIGRATION_GUIDE)
- **Database Operations**: [Database Operations Guide](/docs/guides/database-operations)
- **Getting Started**: [Quickstart](/docs/getting-started/QUICKSTART)
