---
title: Database Operations
description: Manage databases including provisioning, migrations, backups, and connections
sidebar_position: 20
tags: [guides, database, postgresql, redis, mysql, migrations, backups]
---

# Database Operations Guide

This guide covers all aspects of database management on Enclii, including provisioning, migrations, backups, and troubleshooting.

## Prerequisites

- [CLI installed](/cli/)
- Project created in Enclii

## Related Documentation

- **Troubleshooting**: [API Errors](/troubleshooting/api-errors)
- **Migration Guide**: [Migration FAQ](/faq/migration)
- **Service Spec**: [Service Specification](/reference/service-spec)

## Database Types

Enclii supports the following database addons:

| Type | Use Case | Features |
|------|----------|----------|
| **PostgreSQL** | Relational data, ACID transactions | Full SQL, extensions, JSON |
| **Redis** | Caching, sessions, queues | Key-value, pub/sub, streams |
| **MySQL** | Legacy apps, WordPress | Wide compatibility |

## Provisioning Databases

### Create a Database Addon

```bash
# PostgreSQL (recommended for most apps)
enclii addons create postgres --name my-db --project <project-id>

# Redis
enclii addons create redis --name my-cache --project <project-id>

# MySQL
enclii addons create mysql --name my-mysql --project <project-id>
```

### Configuration Options

```bash
# PostgreSQL with specific version and size
enclii addons create postgres \
  --name production-db \
  --project <project-id> \
  --version 16 \
  --size small \
  --storage 20Gi

# Redis with persistence
enclii addons create redis \
  --name session-cache \
  --project <project-id> \
  --persistence enabled
```

**Size options**:

| Size | CPU | Memory | Storage | Use Case |
|------|-----|--------|---------|----------|
| `micro` | 0.1 | 256Mi | 1Gi | Development |
| `small` | 0.5 | 1Gi | 10Gi | Staging, small prod |
| `medium` | 1 | 2Gi | 50Gi | Production |
| `large` | 2 | 4Gi | 100Gi | High traffic |

### Connect Service to Database

```bash
# Link database to service (auto-creates DATABASE_URL)
enclii addons link my-db --service <service-id>

# Verify connection string is set
enclii services env list --service <service-id>
```

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

**External** (for admin tools, local development):
```bash
# Forward port to localhost
enclii addons forward my-db --port 5432

# Then connect to localhost:5432
psql postgres://localhost:5432/mydb
```

### Connection Pooling

For high-traffic applications, enable PgBouncer:

```yaml
# In addon configuration
addons:
  - name: production-db
    type: postgres
    pooler:
      enabled: true
      maxConnections: 100
      poolMode: transaction
```

Use the pooler connection string:
```
postgres://user:pass@my-db-pooler:5432/mydb
```

## Running Migrations

### With Popular Migration Tools

**Node.js (Prisma)**:
```bash
# Run migrations via exec
enclii exec --service <id> -- npx prisma migrate deploy
```

**Node.js (Knex)**:
```bash
enclii exec --service <id> -- npx knex migrate:latest
```

**Go (golang-migrate)**:
```bash
enclii exec --service <id> -- migrate -path ./migrations -database $DATABASE_URL up
```

**Python (Alembic)**:
```bash
enclii exec --service <id> -- alembic upgrade head
```

**Ruby (Rails)**:
```bash
enclii exec --service <id> -- rails db:migrate
```

### Migration Best Practices

1. **Run migrations before deploying new code**:
   ```bash
   # Build → Migrate → Deploy pattern
   enclii deploy --service <id> --pre-deploy "npm run migrate"
   ```

2. **Make migrations backward-compatible**:
   - Add new columns as nullable first
   - Migrate data in separate step
   - Remove old columns after all pods updated

3. **Test migrations in staging first**:
   ```bash
   enclii deploy --service <id> --env staging
   enclii exec --service <id> --env staging -- npm run migrate
   ```

### Rollback Migrations

```bash
# Rollback last migration
enclii exec --service <id> -- npx prisma migrate reset --skip-generate
# or
enclii exec --service <id> -- npx knex migrate:rollback
```

## Backup and Restore

### Automated Backups

Enclii performs daily automated backups:

```bash
# List available backups
enclii addons backups list --addon my-db

# View backup details
enclii addons backups get <backup-id>
```

**Backup schedule**:
- Daily at 2:00 AM UTC
- Retained for 30 days
- Stored in Cloudflare R2

### Manual Backups

```bash
# Create on-demand backup
enclii addons backup create --addon my-db --name "pre-migration-backup"

# With custom retention
enclii addons backup create --addon my-db --retention 90d
```

### Restore from Backup

```bash
# Restore to same addon (destructive!)
enclii addons restore --addon my-db --backup <backup-id>

# Restore to new addon (safer)
enclii addons restore --backup <backup-id> --target new-db
```

### Export/Import (Manual)

**PostgreSQL export**:
```bash
# Forward port
enclii addons forward my-db --port 5432 &

# Export with pg_dump
pg_dump -h localhost -U user -d mydb -Fc > backup.dump
```

**PostgreSQL import**:
```bash
# Forward port
enclii addons forward my-db --port 5432 &

# Import with pg_restore
pg_restore -h localhost -U user -d mydb backup.dump
```

**Redis export**:
```bash
# Forward port
enclii addons forward my-cache --port 6379 &

# Export RDB
redis-cli -h localhost --rdb dump.rdb
```

## Database-Specific Operations

### PostgreSQL

**Create extension**:
```bash
enclii exec --addon my-db -- psql -c "CREATE EXTENSION IF NOT EXISTS pg_trgm;"
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

**Query performance**:
```bash
# Enter psql shell
enclii addons shell my-db

# Analyze slow queries
EXPLAIN ANALYZE SELECT * FROM users WHERE email = 'test@example.com';
```

**View connections**:
```sql
SELECT * FROM pg_stat_activity WHERE datname = 'mydb';
```

### Redis

**Monitor in real-time**:
```bash
enclii addons shell my-cache

# Inside redis-cli
MONITOR
```

**Memory usage**:
```bash
enclii addons shell my-cache

INFO memory
MEMORY STATS
```

**Flush cache**:
```bash
# Flush specific database
enclii exec --addon my-cache -- redis-cli -n 0 FLUSHDB

# Flush all (careful!)
enclii exec --addon my-cache -- redis-cli FLUSHALL
```

### MySQL

**Show databases**:
```bash
enclii addons shell my-mysql
SHOW DATABASES;
```

**Create user**:
```sql
CREATE USER 'readonly'@'%' IDENTIFIED BY 'password';
GRANT SELECT ON mydb.* TO 'readonly'@'%';
FLUSH PRIVILEGES;
```

## Monitoring

### Database Metrics

```bash
# View addon metrics
enclii addons metrics my-db

# Key metrics to watch:
# - Connection count
# - Query latency
# - Storage usage
# - Replication lag (if applicable)
```

### Alerts

Configure alerts for database health:

```yaml
# In addon configuration
alerts:
  - name: high-connections
    metric: connection_count
    threshold: 80
    operator: gt

  - name: storage-warning
    metric: storage_percent
    threshold: 85
    operator: gt
```

## Security

### Connection Security

- All connections require SSL (`sslmode=require`)
- Credentials are stored encrypted
- Network policies restrict access to namespace

### Credential Rotation

```bash
# Rotate database credentials
enclii addons rotate-credentials --addon my-db

# This will:
# 1. Generate new credentials
# 2. Update linked services
# 3. Restart affected pods
```

### Access Control

```bash
# Create read-only user
enclii addons users create --addon my-db --role readonly --name analyst

# List addon users
enclii addons users list --addon my-db

# Revoke access
enclii addons users delete --addon my-db --user analyst
```

## Troubleshooting

### Connection Issues

| Symptom | Cause | Solution |
|---------|-------|----------|
| "Connection refused" | Pod not running | Check `enclii addons status` |
| "Too many connections" | Pool exhausted | Enable connection pooling |
| "Authentication failed" | Wrong credentials | Rotate credentials |
| Timeout | Network policy | Check namespace isolation |

### Performance Issues

```bash
# Check slow queries (PostgreSQL)
enclii addons shell my-db
SELECT * FROM pg_stat_statements ORDER BY total_time DESC LIMIT 10;

# Check index usage
SELECT * FROM pg_stat_user_indexes WHERE idx_scan = 0;
```

### Storage Issues

```bash
# Check storage usage
enclii addons metrics my-db --metric storage

# If running low:
# 1. Clean up old data
# 2. Vacuum database (PostgreSQL)
# 3. Resize addon
enclii addons resize my-db --storage 50Gi
```

## Advanced Topics

### Read Replicas

```bash
# Create read replica
enclii addons replicas create --addon my-db --name my-db-replica

# Use replica for read queries
# Primary: my-db.namespace.svc.cluster.local
# Replica: my-db-replica.namespace.svc.cluster.local
```

### Point-in-Time Recovery

```bash
# Restore to specific point in time
enclii addons restore --addon my-db --point-in-time "2026-01-24T10:30:00Z"
```

### Multi-Region (Enterprise)

For enterprise customers, databases can be replicated across regions:

```yaml
addons:
  - name: global-db
    type: postgres
    regions:
      - eu-central (primary)
      - us-east (replica)
```

## Related Commands

```bash
# Full addon management reference
enclii addons --help

# Common operations
enclii addons list                    # List all addons
enclii addons status <addon>          # Check addon health
enclii addons logs <addon>            # View database logs
enclii addons shell <addon>           # Interactive shell
enclii addons forward <addon>         # Port forward for local access
```

## Related Documentation

- **Troubleshooting**: [Deployment Issues](/troubleshooting/deployment-issues)
- **Migration FAQ**: [Migrating Databases](/faq/migration#database-migration)
- **Service Config**: [Service Specification](/reference/service-spec)
