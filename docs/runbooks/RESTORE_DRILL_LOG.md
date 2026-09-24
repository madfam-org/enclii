# Restore Drill Log

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Owner:** Platform Team
**Cadence:** Monthly (1st of each month, 5 AM UTC)
**Last Updated:** 2026-09-24

> **Boundary checkpoint (2026-09-24, platform on-call):** Public-safe runbook:
> drill procedure and pass/fail semantics only, no node identity, secrets or
> tenant data. Private operational detail stays in `internal-devops`; see
> [`docs/PUBLIC_REPO_BOUNDARY.md`](../PUBLIC_REPO_BOUNDARY.md).

---

## Purpose

Track monthly backup restoration test results to ensure database backups are valid and the restore procedure works end-to-end. A passing drill confirms that:

1. R2 backups are intact and downloadable
2. The SQL dump restores without errors into a temporary database
3. All expected tables are present after restoration
4. The cleanup step drops the temporary database without side effects

Restore drills are **non-destructive**: they never connect to the production server. Each drill restores into an ephemeral cluster (a throwaway `initdb` for the logical drill, a side-channel PVC for the pgBackRest drill) that is discarded when the Job ends.

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

Start a one-off Job from the CronJob (there is exactly one drill definition),
wait for it and print its logs:

```bash
./scripts/backup-restore-drill.sh
```

**Script:** [`scripts/backup-restore-drill.sh`](../../scripts/backup-restore-drill.sh)

The one-shot Job manifests (`postgres-restore-drill.yaml`) were removed on
2026-09-24: they had drifted from the CronJob and could not fail.

---

## Drill Steps (what the job does)

The drill restores into an ephemeral Postgres inside its own pod and never
connects to the production server. Every validation below is a hard
failure: a drill that restores nothing, or restores a stale dump, exits
non-zero.

| Step | Action | Validation (fails the drill) |
|------|--------|------------------------------|
| 1/5 | Find the latest backup in R2 (`s3://enclii-backups/postgres/`) | A backup exists and is at most `MAX_DUMP_AGE_HOURS` (48) old |
| 2/5 | Download it to the pod | Download completes |
| 3/5 | `initdb` an ephemeral cluster and restore the pg_dumpall | The dump declares at least `MIN_DATABASES` (5) databases; no SQL `ERROR` outside `RESTORE_ERROR_ALLOWLIST` |
| 4/5 | Validate | Every declared database exists; at least `MIN_USER_TABLES` (500) user tables in any schema |
| 5/5 | Stop the ephemeral cluster | Always runs (trap) |

Its signal is the CronJob's last successful run
(`kube_cronjob_status_last_successful_time`), alerted by
`RestoreDrillNeverSucceeded` and `RestoreDrillOverdue` in
`infra/k8s/production/monitoring/prometheus.yaml`.

The pgBackRest point-in-time drill
([`postgres-pgbackrest-restore-drill.yaml`](../../infra/k8s/platform-infra/postgres-pgbackrest-restore-drill.yaml))
is stricter still: it restores the latest backup plus all archived WAL,
starts the restored cluster with archiving off, and fails unless it holds at
least 5 databases and 500 user tables and its last replayed transaction is at
most 6 hours older than the drill start.

---

## Results

| Date | Backup Source | Tables Restored | Duration | Pass/Fail | Operator | Notes |
|------|---------------|-----------------|----------|-----------|----------|-------|
| 2026-05-30 | R2 latest pg_dumpall (`backup-verify-ga-0529-2046`) | 1135 | ~64s | **PASS** | platform-ops | Ephemeral postgres restore; 59 projects in `enclii` DB. `postgres-restore-drill` CronJob fixed (initContainer + ephemeral cluster; prior single-DB restore incompatible with pg_dumpall). |
| _template_ | `YYYYMMDD_HHMMSS.sql.gz` | _N_ | _Xm Ys_ | PASS/FAIL | _initials_ | _any observations_ |

> **Instructions:** After each drill (automated or manual), add a row to the table above with the results from the job log output. The job prints the backup filename, table count, and timestamps.

---

## Interpreting Results

### PASS

The log ends with:

```
=== RESTORE DRILL PASSED ===
  Backup: YYYYMMDD_HHMMSS.sql.gz
  Databases: N
  Tables: N
  Duration: Ns
```

Record the backup filename, table count, and elapsed time in the results table.

### FAIL

Common failure modes:

| Failure | Log Message | Action |
|---------|-------------|--------|
| No backups in R2 | `FAIL: No backups found in s3://enclii-backups/postgres/` | Check daily backup CronJob (`postgres-backup`). Verify R2 credentials in `r2-backup-credentials` secret. |
| Download error | AWS CLI errors | Verify `r2-backup-credentials` secret has valid keys. Check R2 bucket exists. |
| Stale dump | `FAIL: newest dump is Nh old` | The daily `postgres-backup` CronJob has stopped producing dumps. Fix that first. |
| Restore error | `FAIL: restore raised SQL errors outside the allowlist` | The log lists the first 40 errors. A corrupt or partial dump fails here. Widen `RESTORE_ERROR_ALLOWLIST` only with evidence that an error is benign. |
| Missing databases or tables | `FAIL: N declared databases are missing` / `FAIL: only N user tables restored` | The dump is incomplete. Check the daily backup job logs for dump errors. |
| Pod killed before a verdict | Job `DeadlineExceeded`, or pod evicted for ephemeral storage | The instance outgrew the drill. Raise `activeDeadlineSeconds` or the `temp-data` sizeLimit, using the sizes the last passing run printed. |

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
