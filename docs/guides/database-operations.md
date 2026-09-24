---
title: Database Operations
description: Manage databases including provisioning, migrations, backups, and connections
sidebar_position: 20
tags: [guides, database, postgresql, redis, mysql, migrations, backups]
---

# Database Operations Guide

This guide covers all aspects of database management on Enclii, including provisioning, migrations, backups, and troubleshooting.

Managed databases are provisioned with [`enclii addon`](../cli/commands/addon.md) (alias `enclii addons`). The CLI surface is `plans`, `create`, `ls`, `destroy`, `api`, and `realtime`. Anything this guide describes that has no CLI subcommand is called out as such.

## Prerequisites

- [CLI installed](/cli/)
- Project created in Enclii

## Related Documentation

- **Troubleshooting**: [API Errors](/troubleshooting/api-errors)
- **Migration Guide**: [Migration FAQ](/faq/migration)
- **Service Spec**: [Service Specification](/reference/service-spec)
- **Addon design**: [Managed DB addon](../architecture/managed-db-addon.md)

## Database Types

Enclii supports the following database addons:

| Type | Use Case | Features |
|------|----------|----------|
| **PostgreSQL** | Relational data, ACID transactions | Full SQL, extensions, JSON |
| **Redis** | Caching, sessions, queues | Key-value, pub/sub, streams |
| **MySQL** | Legacy apps, WordPress | Wide compatibility |

PostgreSQL (CloudNativePG) is the managed engine. `enclii addon create` accepts `--engine redis` and `--engine mysql`, but those engines are scaffolded rather than generally available; `enclii addon plans --engine <engine>` shows whether any plan is offered for them.

## Provisioning Databases

### Create a Database Addon

```bash
# See the available plans
enclii addon plans

# PostgreSQL (recommended for most apps)
enclii addon create my-db --plan standard-0 --project <project-slug>

# Redis / MySQL (only if `enclii addon plans --engine <engine>` lists a plan)
enclii addon create my-cache --engine redis --plan <plan> --project <project-slug>
enclii addon create my-mysql --engine mysql --plan <plan> --project <project-slug>
```

The addon starts in a provisioning state; poll it with `enclii addon ls --project <project-slug>`.

### Configuration Options

Sizing is chosen by plan. There are no `--version`, `--size`, `--storage`, or `--persistence` flags. The plans defined in the [addon design](../architecture/managed-db-addon.md) are:

| Plan | CPU | Memory | Storage |
|------|-----|--------|---------|
| `standard-0` | 0.1 | 256Mi | 1 GB |
| `standard-1` | 0.5 | 1Gi | 10 GB |
| `standard-2` | 1 | 2Gi | 50 GB |

`enclii addon plans` is the authoritative, live catalog.

### Connect Service to Database

A service is bound when the addon is created. The connection string is injected into the service as `DATABASE_URL` (`REDIS_URL` / `MYSQL_URL` for those engines), and the bound service rolls automatically once the credentials secret exists:

```bash
# Create and bind in one step (--service takes the service ID)
enclii addon create my-db --plan standard-0 --project <project-slug> --service <service-id>

# Use a different env var name
enclii addon create reporting-db --plan standard-0 --project <project-slug> \
  --service <service-id> --env-var REPORTING_DATABASE_URL
```

There is no separate `link` command; binding an existing addon to another service is not available in the CLI. `enclii projects services <project-slug>` lists service IDs.

## Connection Strings

### Format by Database Type

**PostgreSQL**:
```
postgres://user:password@host:5432/database?sslmode=require
```

**Redis**:
```
redis://user:password@host:6379/0
```

**MySQL**:
```
mysql://user:password@host:3306/database
```

### Internal vs External Access

**Internal** (from within cluster):
```
postgresql://user:pass@my-db.enclii-workloads.svc.cluster.local:5432/mydb
```

**External** (for admin tools, local development): the CLI has no port-forward command. Options:

- Run one-off SQL inside the cluster with `enclii jobs run-once` (see [One-off SQL](#one-off-sql)).
- Expose the database over HTTPS as a REST API with `enclii addon api enable <addon_id>` (PostgREST; authorization via row-level security). See [`enclii addon api`](../cli/commands/addon.md#api).
- Operators can use `kubectl port-forward` into the project namespace as a break-glass path.

### Connection Pooling

A managed connection pooler (PgBouncer) for addons is not available yet; it is an open item in the [addon design](../architecture/managed-db-addon.md). Size your application's pool to the plan (each addon supports a small number of connections).

## Running Migrations

Run migrations as a one-off job with [`enclii jobs run-once`](../cli/commands/jobs.md). The job runs in the service's currently deployed image with the service's environment and secrets (so it sees `DATABASE_URL`), and the command is executed with `/bin/sh -c`. There is no `enclii exec`.

### With Popular Migration Tools

**Node.js (Prisma)**:
```bash
enclii jobs run-once --name prisma-migrate --command "npx prisma migrate deploy" \
  --service-id <id> --project <project-slug>
```

**Node.js (Knex)**:
```bash
enclii jobs run-once --name knex-migrate --command "npx knex migrate:latest" \
  --service-id <id> --project <project-slug>
```

**Go (golang-migrate)**:
```bash
# Assumes the migrate binary and ./migrations are in the service image
enclii jobs run-once --name migrate-up \
  --command 'migrate -path ./migrations -database "$DATABASE_URL" up' \
  --service-id <id> --project <project-slug>
```

**Python (Alembic)**:
```bash
enclii jobs run-once --name alembic-upgrade --command "alembic upgrade head" \
  --service-id <id> --project <project-slug>
```

**Ruby (Rails)**:
```bash
enclii jobs run-once --name db-migrate --command "rails db:migrate" \
  --service-id <id> --project <project-slug>
```

Check the result with `enclii jobs get <job-id>` and `enclii jobs logs <job-id>`.

### Migration Best Practices

1. **Order migrations and deploys deliberately**: `enclii deploy` has no pre-deploy hook flag. A one-off job uses the service's *current* deployment image unless you pass `--image`, so a migration shipped with new code runs after the deploy that ships it:
   ```bash
   # Deploy the new code, then migrate
   enclii deploy --env prod --wait
   enclii jobs run-once --name db-migrate --command "npm run migrate" \
     --service-id <id> --project <project-slug>
   ```

2. **Make migrations backward-compatible**:
   - Add new columns as nullable first
   - Migrate data in separate step
   - Remove old columns after all pods updated

3. **Test migrations in staging first**. `jobs run-once` has no `--env` flag; it targets the service ID you pass, so use the staging service's ID:
   ```bash
   enclii deploy --env staging --wait
   enclii jobs run-once --name db-migrate --command "npm run migrate" \
     --service-id <staging-service-id> --project <project-slug>
   ```

### Rollback Migrations

```bash
# Rollback last migration
enclii jobs run-once --name knex-rollback --command "npx knex migrate:rollback" \
  --service-id <id> --project <project-slug>
```

(`npx prisma migrate reset` drops and recreates the database; it is not a single-step rollback.)

## Backup and Restore

### Automated Backups

Per-addon backups, backup listing, and restore are not exposed in the CLI. The addon design tracks per-addon WAL archiving and point-in-time recovery as follow-up work.

`enclii db wal-status` reports WAL-archive and backup freshness for the **platform** Postgres instance (operator, read-only); it does not cover addons.

### Manual Backups

A tenant export includes a `pg_dump` of each bound database addon:

```bash
# Start an export and download it when ready
enclii export --project <project-slug> --wait --out ./backup.tar.gz

# Or check on it later
enclii export list --project <project-slug>
enclii export status <export_id>
enclii export download <export_id> --out ./backup.tar.gz
```

Production exports require a second project admin's approval (`enclii export approve`). See [`enclii export`](../cli/commands/export.md).

### Restore from Backup

There is no `restore` command. To restore, create an addon (or use an existing one) and load the dump with `pg_restore` from somewhere that can reach it, for example a one-off job:

```bash
enclii jobs run-once --name restore --image postgres:16-alpine \
  --command 'pg_restore --no-owner --no-acl -d "$DATABASE_URL" /path/to/backup.dump' \
  --service-id <id> --project <project-slug>
```

The dump must be reachable from inside the job (for example downloaded from object storage as part of the command).

### Export/Import (Manual)

**PostgreSQL export**:
```bash
# From a host that can reach the database
pg_dump -d "$DATABASE_URL" -Fc > backup.dump
```

**PostgreSQL import**:
```bash
pg_restore -d "$DATABASE_URL" --no-owner --no-acl backup.dump
```

**Redis export**:
```bash
redis-cli -u "$REDIS_URL" --rdb dump.rdb
```

## Database-Specific Operations

### One-off SQL

There is no `enclii addon shell` and no interactive session. Run statements as one-off jobs; `--image` supplies the client while the service's `DATABASE_URL` is inherited:

```bash
enclii jobs run-once --name sql --image postgres:16-alpine \
  --command 'psql "$DATABASE_URL" -c "SELECT version();"' \
  --service-id <id> --project <project-slug>
enclii jobs logs <job-id>
```

### PostgreSQL

**Create extension** (the addon's database role owns its database but is not a superuser, so only trusted extensions such as `pg_trgm`, `uuid-ossp`, and `pgcrypto` can be created this way):
```bash
enclii jobs run-once --name create-ext --image postgres:16-alpine \
  --command 'psql "$DATABASE_URL" -c "CREATE EXTENSION IF NOT EXISTS pg_trgm;"' \
  --service-id <id> --project <project-slug>
```

**Common extensions**:
```sql
-- Full-text search
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- UUID generation
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Geographic data
CREATE EXTENSION IF NOT EXISTS postgis;

-- JSON path queries
CREATE EXTENSION IF NOT EXISTS jsonb_plperl;
```

**Query performance** (as a one-off job, see [One-off SQL](#one-off-sql)):
```sql
EXPLAIN ANALYZE SELECT * FROM users WHERE email = 'test@example.com';
```

**View connections**:
```sql
SELECT * FROM pg_stat_activity WHERE datname = 'mydb';
```

### Redis

Redis addons are not generally available (see [Database Types](#database-types)). For a Redis bound to a service, run `redis-cli` as a one-off job:

**Memory usage**:
```bash
enclii jobs run-once --name redis-info --image redis:7-alpine \
  --command 'redis-cli -u "$REDIS_URL" INFO memory' \
  --service-id <id> --project <project-slug>
```

**Flush cache**:
```bash
# Flush the database in REDIS_URL (careful!)
enclii jobs run-once --name redis-flush --image redis:7-alpine \
  --command 'redis-cli -u "$REDIS_URL" FLUSHDB' \
  --service-id <id> --project <project-slug>
```

Interactive commands such as `MONITOR` need a live session, which the CLI does not provide.

### MySQL

MySQL addons are not generally available. Statements can be run the same way with a `mysql` client image and `MYSQL_URL`.

**Create user**:
```sql
CREATE USER 'readonly'@'%' IDENTIFIED BY 'password';
GRANT SELECT ON mydb.* TO 'readonly'@'%';
FLUSH PRIVILEGES;
```

## Monitoring

### Database Metrics

There is no addon metrics command. `enclii addon ls` shows each addon's status; [`enclii observe`](../cli/commands/observe.md) reports service-level metrics (CPU, memory, requests, latency) for the service that uses the database.

```bash
enclii addon ls --project <project-slug>
enclii observe metrics --service <service-id>
```

Key metrics to watch from inside the database (via [One-off SQL](#one-off-sql)): connection count (`pg_stat_activity`), slow queries (`pg_stat_statements`), and storage (`pg_database_size`).

### Alerts

Addon-level alert configuration is not available. `enclii observe alerts --service <service-id>` lists active alerts for a service.

## Security

### Connection Security

- All connections require SSL (`sslmode=require`)
- Credentials are stored encrypted
- Network policies restrict access to namespace

### Credential Rotation

Addon credential rotation is not exposed in the CLI yet (it is planned in the [addon design](../architecture/managed-db-addon.md)). Credentials live in a Kubernetes Secret in the project namespace and are never returned in plaintext by the API.

### Access Control

There are no addon user-management commands. The addon's application role has no `CREATEROLE` privilege, so additional database users cannot be created from the application connection. For read access from outside the cluster, prefer the data API (`enclii addon api enable`, with row-level security policies and `enclii addon api token --role <role>`).

## Troubleshooting

### Connection Issues

| Symptom | Cause | Solution |
|---------|-------|----------|
| "Connection refused" | Pod not running | Check the STATUS column of `enclii addon ls` |
| "Too many connections" | Pool exhausted | Reduce the application pool size or move to a larger plan |
| "Authentication failed" | Wrong credentials | Confirm the service was bound at `enclii addon create --service` and uses the injected env var |
| Timeout | Network policy | Check namespace isolation |

### Performance Issues

Run these as [one-off SQL](#one-off-sql):

```sql
-- Check slow queries (PostgreSQL)
SELECT * FROM pg_stat_statements ORDER BY total_time DESC LIMIT 10;

-- Check index usage
SELECT * FROM pg_stat_user_indexes WHERE idx_scan = 0;
```

### Storage Issues

There is no addon resize or storage-metrics command; check size with `SELECT pg_size_pretty(pg_database_size(current_database()));` as [one-off SQL](#one-off-sql).

If running low:
1. Clean up old data
2. Vacuum database (PostgreSQL)
3. Move to a larger plan: create a new addon on a bigger plan, export and restore the data, then destroy the old addon

## Advanced Topics

### Read Replicas

Read replicas are not available for addons (tracked as future work in the [addon design](../architecture/managed-db-addon.md)).

### Point-in-Time Recovery

Point-in-time recovery is not available for addons yet (tracked in the [addon design](../architecture/managed-db-addon.md)).

### Multi-Region

Per-region addon placement is not available.

### Realtime

Stream row changes from a Postgres addon table over a WebSocket:

```bash
enclii addon realtime enable <addon_id> --table public.orders
enclii addon realtime list <addon_id>
```

## Related Commands

```bash
# Full addon management reference
enclii addon --help

# Common operations
enclii addon plans                                    # List managed-database plans
enclii addon create <name> --plan <plan>              # Create (optionally --service <id>)
enclii addon ls --json                                # List addons with full IDs
enclii addon destroy <addon_id>                       # Destroy (asks for confirmation; --yes skips)
enclii addon api enable <addon_id>                    # REST API over the database
enclii jobs run-once --name <n> --command "<cmd>" \
  --service-id <id> --project <slug>                  # One-off command with the service's DATABASE_URL
enclii export --project <slug> --wait                 # Tenant export incl. pg_dump of bound addons
```

## Related Documentation

- **Troubleshooting**: [Deployment Issues](/troubleshooting/deployment-issues)
- **Migration FAQ**: [Migrating Databases](/faq/migration#database-migration)
- **Service Config**: [Service Specification](/reference/service-spec)
