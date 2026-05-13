# Database Recovery Runbook

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Last Updated:** 2026-01-13
**Owner:** Platform Team

---

## Overview

This runbook documents procedures for PostgreSQL backup and recovery in the Enclii platform.

### Backup Architecture

```
PostgreSQL → CronJob (daily 2AM UTC) → pg_dump → gzip → Cloudflare R2
                                                           ↓
                                                    enclii-backups/
                                                    └── postgres/
                                                        ├── latest.sql.gz
                                                        ├── 20260113_020000.sql.gz
                                                        └── ...
```

### Retention Policy
- **Daily backups**: Retained for 30 days
- **Latest backup**: Always available at `postgres/latest.sql.gz`

---

## Quick Commands

```bash
# Check backup status
./scripts/backup-database.sh status

# Trigger immediate backup
./scripts/backup-database.sh backup

# List available backups
./scripts/backup-database.sh list

# Restore from latest
./scripts/backup-database.sh restore

# Restore from specific backup
./scripts/backup-database.sh restore postgres/20260113_020000.sql.gz
```

---

## Recovery Procedures

### Scenario 1: Data Corruption

**Symptoms:**
- Application errors indicating invalid data
- Database constraint violations
- Missing or incorrect records

**Steps:**

1. **Assess impact**
   ```bash
   # Check application logs
   kubectl logs -n enclii -l app=switchyard-api --tail=100
   ```

2. **Identify last known good backup**
   ```bash
   ./scripts/backup-database.sh list
   ```

3. **Scale down applications**
   ```bash
   kubectl scale deployment switchyard-api --replicas=0 -n enclii
   kubectl scale deployment switchyard-ui --replicas=0 -n enclii
   ```

4. **Restore from backup**
   ```bash
   ./scripts/backup-database.sh restore postgres/20260112_020000.sql.gz
   ```

5. **Verify restoration**
   ```bash
   kubectl exec -n data postgres-0 -- psql -U enclii -d enclii -c "SELECT COUNT(*) FROM projects;"
   ```

6. **Scale up applications**
   ```bash
   kubectl scale deployment switchyard-api --replicas=2 -n enclii
   kubectl scale deployment switchyard-ui --replicas=2 -n enclii
   ```

7. **Verify application health**
   ```bash
   curl https://api.enclii.dev/health
   ```

---

### Scenario 2: Accidental Deletion

**Symptoms:**
- User reports missing data
- API returns 404 for existing resources

**Steps:**

1. **Do NOT panic** - Backups are available

2. **Create immediate backup of current state**
   ```bash
   ./scripts/backup-database.sh backup
   ```

3. **Identify when deletion occurred**
   - Check audit logs
   - Review user activity

4. **Determine recovery approach**
   - If deletion was recent: restore specific tables
   - If deletion was widespread: full restore

5. **For partial restore** (advanced):
   ```bash
   # Download backup locally
   kubectl run restore-pod --rm -it \
     --image=postgres:15 \
     -n enclii \
     -- bash

   # Inside pod, download and extract specific tables
   ```

6. **For full restore:**
   ```bash
   ./scripts/backup-database.sh restore postgres/YYYYMMDD_HHMMSS.sql.gz
   ```

---

### Scenario 3: Complete Cluster Failure

**Symptoms:**
- Kubernetes cluster unreachable
- All services down

**Steps:**

1. **Provision new cluster**
   ```bash
   ./scripts/deploy-production.sh destroy  # If needed
   ./scripts/deploy-production.sh apply
   ./scripts/deploy-production.sh kubeconfig
   ./scripts/deploy-production.sh post-deploy
   ```

2. **Deploy database (data namespace)**
   ```bash
   # Production postgres runs in the data namespace, not enclii namespace
   kubectl apply -k infra/k8s/production/data/
   kubectl wait --for=condition=ready pod -l app=postgres -n data --timeout=300s
   ```

3. **Configure R2 credentials**
   ```bash
   cp infra/k8s/production/backup/backup-secrets.yaml.template \
      infra/k8s/production/backup/backup-secrets.yaml
   # Edit with your R2 credentials
   kubectl apply -f infra/k8s/production/backup/backup-secrets.yaml
   ```

4. **Restore from R2**
   ```bash
   # Apply backup scripts
   kubectl apply -f infra/k8s/production/backup/postgres-backup.yaml

   # Restore latest backup
   ./scripts/backup-database.sh restore
   ```

5. **Deploy remaining services**
   ```bash
   kubectl apply -f infra/k8s/base/redis.yaml
   kubectl apply -f infra/k8s/base/switchyard-api.yaml
   # etc.
   ```

6. **Verify recovery**
   ```bash
   ./scripts/deploy-production.sh status
   ```

---

### Scenario 4: Point-in-Time Recovery (PITR)

**Note:** Current setup provides daily snapshots. For true PITR, enable WAL archiving.

**Current capability:**
- Restore to any daily backup checkpoint
- Maximum data loss: ~24 hours

**To implement PITR:**

1. Enable WAL archiving to R2
2. Configure `archive_command` in PostgreSQL
3. Store WAL files alongside daily backups

---

## Verification Checklist

After any recovery:

- [ ] Database connectivity verified
- [ ] All tables present and accessible
- [ ] API health check passes
- [ ] UI loads correctly
- [ ] Authentication works
- [ ] Recent data visible in UI
- [ ] Monitoring alerts cleared

---

## Preventive Measures

### Daily Operations
- Monitor backup CronJob status
- Review backup job logs weekly
- Verify backup file sizes are reasonable

### Monthly Tasks
- Test restore procedure in staging
- Verify R2 bucket policies
- Review retention policy effectiveness

### Quarterly Tasks
- Full disaster recovery drill
- Update this runbook
- Review backup encryption needs

---

## Contacts

| Role | Contact |
|------|---------|
| Platform Lead | @platform-team |
| On-Call | PagerDuty |
| Database SME | @backend-team |

---

## Appendix: Manual R2 Access

If scripts are unavailable:

```bash
# Configure AWS CLI for R2
export AWS_ACCESS_KEY_ID="your-r2-access-key"
export AWS_SECRET_ACCESS_KEY="your-r2-secret-key"
export R2_ENDPOINT="https://YOUR_ACCOUNT_ID.r2.cloudflarestorage.com"

# List backups
aws s3 ls s3://enclii-backups/postgres/ --endpoint-url $R2_ENDPOINT

# Download backup
aws s3 cp s3://enclii-backups/postgres/latest.sql.gz ./backup.sql.gz --endpoint-url $R2_ENDPOINT

# Restore manually
gunzip -c backup.sql.gz | psql -h localhost -U postgres -d enclii
```
