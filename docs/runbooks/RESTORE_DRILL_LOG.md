# Restore Drill Log

**Owner:** Platform Team
**Cadence:** Monthly (1st of each month, 5 AM UTC)
**Last Updated:** 2026-03-08

---

## Purpose

Track monthly backup restoration test results to ensure database backups are valid and the restore procedure works end-to-end. A passing drill confirms that:

1. R2 backups are intact and downloadable
2. The SQL dump restores without errors into a temporary database
3. All expected tables are present after restoration
4. The cleanup step drops the temporary database without side effects

Restore drills are **non-destructive** -- they never touch production databases. A temporary database (`enclii_restore_test`) is created, validated, and dropped within the same job run.

---

## Procedure

### Automated (K8s CronJob)

The CronJob `postgres-restore-drill` in the `data` namespace runs automatically on the 1st of each month at 5 AM UTC. Results appear in job logs.

```bash
# View the most recent drill result
kubectl logs -n data job/postgres-restore-drill

# View CronJob status
kubectl get cronjob postgres-restore-drill -n data
```

**Manifest:** [`infra/k8s/production/backup/restore-drill-cronjob.yaml`](../../infra/k8s/production/backup/restore-drill-cronjob.yaml)

### Manual

Use the wrapper script for ad-hoc drills outside the monthly cadence:

```bash
./scripts/backup-restore-drill.sh
```

**Script:** [`scripts/backup-restore-drill.sh`](../../scripts/backup-restore-drill.sh)

Or apply the one-shot Job directly:

```bash
kubectl apply -f infra/k8s/production/backup/postgres-restore-drill.yaml
kubectl logs -n data job/postgres-restore-drill -f
kubectl delete job postgres-restore-drill -n data
```

**Job Manifest:** [`infra/k8s/production/backup/postgres-restore-drill.yaml`](../../infra/k8s/production/backup/postgres-restore-drill.yaml)

---

## Drill Steps (what the job does)

| Step | Action | Validation |
|------|--------|------------|
| 1/5 | Find latest backup in R2 (`s3://enclii-backups/postgres/`) | Backup file exists |
| 2/5 | Download backup to `/tmp/restore-drill.sql.gz` | Download completes, file size > 0 |
| 3/5 | Create temp database `enclii_restore_test`, restore dump | `psql` restore exits 0 |
| 4/5 | Validate: count public-schema tables | Table count >= 1 |
| 5/5 | Drop temp database, delete downloaded file | Cleanup completes |

---

## Results

| Date | Backup Source | Tables Restored | Duration | Pass/Fail | Operator | Notes |
|------|---------------|-----------------|----------|-----------|----------|-------|
| _template_ | `YYYYMMDD_HHMMSS.sql.gz` | _N_ | _Xm Ys_ | PASS/FAIL | _initials_ | _any observations_ |

> **Instructions:** After each drill (automated or manual), add a row to the table above with the results from the job log output. The job prints the backup filename, table count, and timestamps.

---

## Interpreting Results

### PASS

The log ends with:

```
=== RESTORE DRILL PASSED ===
  Backup: YYYYMMDD_HHMMSS.sql.gz
  Tables: N
  Time: YYYY-MM-DDTHH:MM:SSZ
```

Record the backup filename, table count, and elapsed time in the results table.

### FAIL

Common failure modes:

| Failure | Log Message | Action |
|---------|-------------|--------|
| No backups in R2 | `FAIL: No backups found in s3://enclii-backups/postgres/` | Check daily backup CronJob (`postgres-backup`). Verify R2 credentials in `r2-backup-credentials` secret. |
| Download error | AWS CLI errors | Verify `r2-backup-credentials` secret has valid keys. Check R2 bucket exists. |
| Restore error | `psql` errors during restore | Inspect the specific SQL errors. May indicate a corrupt dump. Restore from an older backup. |
| No tables found | `FAIL: No tables found after restore` | The dump may be empty. Check daily backup job logs for dump errors. |

---

## Monthly Cadence Reminder

The automated CronJob handles scheduling, but operators should:

1. **By the 2nd of each month:** Check the CronJob completed successfully
   ```bash
   kubectl get cronjob postgres-restore-drill -n data
   kubectl get jobs -n data -l app=postgres-restore-drill --sort-by=.metadata.creationTimestamp
   ```

2. **Log the result:** Add a row to the Results table above

3. **Escalate failures:** If the drill fails, open an incident and follow the [Database Recovery Runbook](./DATABASE_RECOVERY.md)

---

## Related Documents

- [Database Recovery Runbook](./DATABASE_RECOVERY.md)
- [Disaster Recovery Runbook](./DISASTER_RECOVERY.md)
- Daily backup CronJob: [`infra/k8s/production/backup/postgres-backup.yaml`](../../infra/k8s/production/backup/postgres-backup.yaml)
- Backup secrets template: [`infra/k8s/production/backup/backup-secrets.yaml.template`](../../infra/k8s/production/backup/backup-secrets.yaml.template)
