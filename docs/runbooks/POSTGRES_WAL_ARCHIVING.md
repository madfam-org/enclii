# Postgres WAL Archiving Runbook (pgBackRest → R2)

> **Last Updated:** 2026-04-17
> **Owner:** Platform oncall
> **Scope:** In-cluster Postgres (`data/postgres`), single-node today
> **P1.1 deliverable** — moves Postgres RPO from 24h (daily `pg_dump`) to ~1min.
> Companion doc in `internal-devops`: [`runbooks/postgres-wal-archiving.md`](../../../internal-devops/runbooks/postgres-wal-archiving.md)

---

## 1. How it works (one screen)

```
  Postgres (data/postgres, primary)
     |
     |  archive_command = pgbackrest --stanza=main archive-push %p
     v
  Shared spool (pgbackrest-spool-pvc, 20Gi Longhorn)
     |
     |  pgbackrest sidecar drains async queue
     v
  Cloudflare R2 (s3://enclii-backups/pgbackrest/enclii-main/)
     |
     +-- archive/           (WAL segments, continuous)
     +-- backup/            (full + diff, daily + weekly)

  Scheduled (CronJobs in data namespace):
    pgbackrest-check       */15 * * * *  (every 15 min)
    pgbackrest-backup-diff 0 2 * * 1-6   (Mon-Sat 02:00 UTC)
    pgbackrest-backup-full 0 2 * * 0     (Sunday 02:00 UTC)

  Alerts (monitoring/postgres-wal-archive PrometheusRule):
    PostgresWALArchiveBehind   (critical, paging)
    PostgresWALLagHigh         (warning)
    PostgresBackupCheckFailed  (warning)
    PostgresWALSpoolDiskHigh   (warning)
    PostgresBackupJobsStale    (warning)
```

**RPO**: continuous WAL with `archive_timeout=60` → **≤ 1 minute** in the steady state.
**RTO (DB-level restore)**: TBD until first WAL drill — budget is ≤ 30 min.

---

## 2. One-time operator bootstrap

These steps MUST be done in order. They are **not** automated by ArgoCD by
design — the first one touches production DB config and requires a
maintenance window.

### Step 1 — Apply R2 credentials secret

```bash
# Gather from Cloudflare R2 dashboard: Account ID + an API token with
# Object Read & Write scope on the enclii-backups bucket.
# (Optional) Generate a cipher passphrase: openssl rand -base64 48

kubectl create secret generic pgbackrest-r2-credentials \
  --namespace=data \
  --from-literal=R2_ACCOUNT_ID='<cf-account-id>' \
  --from-literal=PGBACKREST_REPO1_S3_KEY='<r2-access-key-id>' \
  --from-literal=PGBACKREST_REPO1_S3_KEY_SECRET='<r2-secret-access-key>' \
  --from-literal=PGBACKREST_REPO1_CIPHER_PASS='<48-char-passphrase-or-empty>'
```

Template: `infra/k8s/platform-infra/pgbackrest-r2-credentials.yaml.template`

**If you choose `cipher-type=none`** (lower CPU, relies on R2 server-side
encryption only): omit `PGBACKREST_REPO1_CIPHER_PASS` AND edit
`infra/k8s/platform-infra/pgbackrest-config.yaml` to set
`repo1-cipher-type=none` before ArgoCD syncs.

### Step 2 — Schedule the Postgres restart window

Turning on `archive_mode=on` requires a Postgres **restart** (not a reload).
Pick a low-traffic window (~5 min). During the restart:
- API 500s will spike for the connection-pool duration
- ArgoCD sync must be allowed (auto-sync is fine; do NOT pause)

Pre-checks the operator runs before flipping:

```bash
# 1. Confirm manifests are in Git but not yet synced.
argocd app get platform-infra-services --show-params

# 2. Confirm R2 secret exists.
kubectl -n data get secret pgbackrest-r2-credentials

# 3. Dry-run the new pod spec.
kubectl -n data rollout status deploy/postgres        # baseline: Ready
```

Cutover:

```bash
# ArgoCD will sync automatically once the PR merges; if auto-sync is
# paused, force:
argocd app sync platform-infra-services

# Observe new pod come up with sidecar:
kubectl -n data get pods -l app=postgres -w
# Expect:  1 initContainer Completed → 2 containers (postgres + pgbackrest) Ready
```

Verify Postgres picked up the new config:

```bash
kubectl -n data exec deploy/postgres -c postgres -- psql -U postgres -c "SHOW archive_mode;"
# → on
kubectl -n data exec deploy/postgres -c postgres -- psql -U postgres -c "SHOW archive_command;"
# → /opt/pgbackrest/bin/pgbackrest --stanza=main archive-push %p
```

### Step 3 — Initialize the stanza (ONE TIME)

```bash
kubectl -n data exec deploy/postgres -c pgbackrest -- \
  pgbackrest --stanza=main stanza-create
```

Expected output ends with `stanza-create command end: completed successfully`.

**Do not run this twice.** stanza-create is idempotent but if the repo path
or cipher changes, the stanza must be *rebuilt* with `stanza-upgrade` or
a fresh stanza — see `pgbackrest --help stanza-upgrade`.

### Step 4 — Force a first full backup

Don't wait for Sunday 02:00 UTC:

```bash
kubectl -n data exec deploy/postgres -c pgbackrest -- \
  pgbackrest --stanza=main --type=full --log-level-console=info backup
```

Should finish in 1-5 min for a 10GiB PVC. Confirm:

```bash
kubectl -n data exec deploy/postgres -c pgbackrest -- pgbackrest info
# Look for:
#   stanza: main
#     status: ok
#     ...
#     full backup: YYYYMMDD-HHMMSSF
```

### Step 5 — Register the DR drill cadence

Run `enclii/scripts/dr-drill.sh` within 24h of Step 4 to validate the
end-to-end restore path. See §5 below.

---

## 3. Daily operations (no downtime)

### Verify archive health

```bash
# Via the CLI (preferred):
enclii db wal-status

# Or directly:
kubectl -n data exec deploy/postgres -c pgbackrest -- pgbackrest info
```

### Trigger an on-demand backup

```bash
# Full (rarely needed — Sunday CronJob covers it):
kubectl -n data exec deploy/postgres -c pgbackrest -- \
  pgbackrest --stanza=main --type=full backup

# Differential (before a risky deploy, for example):
kubectl -n data exec deploy/postgres -c pgbackrest -- \
  pgbackrest --stanza=main --type=diff backup
```

### Point-in-time restore (PITR)

**This restores into an ephemeral namespace. NEVER restore in-place in
production without a signed-off incident.**

```bash
# 1. Spin up an ephemeral Postgres (uses the DR test manifest):
kubectl create namespace pitr-restore
kubectl -n pitr-restore apply -f infra/k8s/dr-test/ephemeral-postgres.yaml

# 2. Copy the R2 credentials + pgbackrest config:
kubectl get secret pgbackrest-r2-credentials -n data -o yaml \
  | sed 's/namespace: data/namespace: pitr-restore/' \
  | kubectl apply -f -

# 3. Run pgbackrest restore targeting the PITR point:
kubectl -n pitr-restore exec deploy/dr-postgres -- \
  pgbackrest --stanza=main \
    --type=time \
    --target="2026-04-17 14:30:00+00" \
    restore

# 4. Start Postgres (or let the startup probe drive it) and verify:
kubectl -n pitr-restore exec deploy/dr-postgres -- \
  psql -U postgres -c "SELECT NOW(), COUNT(*) FROM public.projects;"

# 5. Export what you need (pg_dump | psql to prod, or direct COPY).
# 6. Tear down:
kubectl delete namespace pitr-restore
```

---

## 4. Failure modes

### 4.1 R2 outage

Symptom: `archive_command` starts failing, `pg_stat_archiver.failed_count`
increments, `PostgresWALLagHigh` fires.

What Postgres does automatically:
- `archive-async=y` means the archiver retries in the background.
- WAL segments pile up in `/var/lib/pgbackrest/spool` on the spool PVC.
- Postgres **does not block** until the spool fills.

What you do:
1. Confirm R2 is actually down (CF status, dashboard).
2. Watch spool usage:
   ```bash
   kubectl -n data exec deploy/postgres -c pgbackrest -- \
     df -h /var/lib/pgbackrest
   ```
3. If spool crosses 80%: `PostgresWALSpoolDiskHigh` fires. Evaluate:
   - Extend the PVC (Longhorn supports online expand):
     ```bash
     kubectl -n data patch pvc pgbackrest-spool-pvc \
       -p '{"spec":{"resources":{"requests":{"storage":"40Gi"}}}}'
     ```
   - Or increase `archive-push-queue-max` in the config (ceiling where
     Postgres starts erroring `archive_command`).
4. Once R2 is back, the archiver drains automatically; confirm with:
   ```bash
   kubectl -n data exec deploy/postgres -c pgbackrest -- pgbackrest info
   ```

### 4.2 Archive lag (archiver slow, not failing)

Symptom: `PostgresWALLagHigh` (warning) but `failed_count` flat.

Causes:
- High write volume > single-process archive throughput.
- R2 latency spike.

Fix: bump `process-max` in `pgbackrest-config.yaml` (currently 4). Requires
a sidecar restart (no Postgres restart).

### 4.3 `pgbackrest check` failures

Symptom: `PostgresBackupCheckFailed`.

Diagnose in order:

```bash
# 1. The most recent CronJob Job shows the actual error:
kubectl -n data get jobs -l app=pgbackrest-check --sort-by=.metadata.creationTimestamp | tail -5
kubectl -n data logs job/<failed-job-name>

# 2. Common causes & fixes:
#   - "unable to get address" → R2 secret wrong or missing; redo Step 1.
#   - "cipher mismatch"       → cipher-pass changed; restore old pass OR
#                               do a fresh stanza (`stanza-delete` + create).
#   - "WAL segment not found" → archiver is behind; check pgbackrest logs.
#   - "stanza not created"    → never ran Step 3 (see §2).
```

### 4.4 Stanza corruption

Rare. Only happens if R2 objects are deleted out of band.

```bash
# Validate:
kubectl -n data exec deploy/postgres -c pgbackrest -- \
  pgbackrest --stanza=main verify

# If unrecoverable (truly lost):
kubectl -n data exec deploy/postgres -c pgbackrest -- \
  pgbackrest --stanza=main --force stanza-delete
kubectl -n data exec deploy/postgres -c pgbackrest -- \
  pgbackrest --stanza=main stanza-create
# Run a fresh full backup immediately:
kubectl -n data exec deploy/postgres -c pgbackrest -- \
  pgbackrest --stanza=main --type=full backup
```

Heads up: `stanza-delete` wipes all archives — you lose PITR history. The
daily logical dump CronJob (still running during the 30-day dual-write soak)
is your fallback.

---

## 5. DR drill integration

`enclii/scripts/dr-drill.sh` has a P1.1 phase that validates:

- Most recent WAL archive segment is < 5 min old.
- `pgbackrest info` returns healthy status.
- PITR restore of 5-minutes-ago into the `dr-test` namespace works.

The drill appends two rows to `internal-devops/runbooks/dr-log.md` per run:
one for the legacy logical-backup restore, one for the WAL/PITR restore.
This lets us compare RPO directly.

Drill passing for 3 consecutive runs is the acceptance criterion for
retiring the daily logical dump CronJob.

---

## 6. Rollback (undo WAL archiving)

If we need to remove WAL archiving entirely (unlikely but documented):

```bash
# 1. Edit postgres-wal-config.yaml ConfigMap: set archive_mode=off.
# 2. Let ArgoCD sync.
# 3. Restart Postgres (archive_mode change requires restart):
kubectl -n data rollout restart deploy/postgres

# 4. Pause the pgbackrest CronJobs:
kubectl -n data patch cronjob pgbackrest-check       -p '{"spec":{"suspend":true}}'
kubectl -n data patch cronjob pgbackrest-backup-diff -p '{"spec":{"suspend":true}}'
kubectl -n data patch cronjob pgbackrest-backup-full -p '{"spec":{"suspend":true}}'

# 5. The daily logical pg_dump CronJob continues working independently.
# 6. Optional: delete R2 archives when you're sure you don't need them:
aws s3 rm s3://enclii-backups/pgbackrest/enclii-main/ --recursive \
  --endpoint-url https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com
```

---

## 7. Escape hatches & links

- **When WAL+PITR fails and you need to restore yesterday's dump**:
  see `docs/runbooks/DATABASE_RECOVERY.md` — the daily logical backup
  CronJob is still live during the soak period.
- **When the cluster is totally gone**:
  see `../../../internal-devops/runbooks/disaster-recovery.md` §7.
- **When the R2 bucket is gone** (multi-region DR is Phase 4):
  escalate; secondary R2 region is not yet configured.

---

## 8. Capacity + retention budget

| Resource | Policy | Why |
|----------|--------|-----|
| Full backups | 7 kept (count) | ~7 weeks @ weekly |
| Diff backups | 3 kept | Rolling window; older diffs pruned naturally |
| WAL segments | Auto (tied to oldest surviving backup) | pgBackRest manages |
| Compression | zstd, level 6 | Good compression, low CPU vs gzip-9 |
| Encryption (opt) | AES-256-CBC | Client-side, before R2 upload |

Steady-state R2 object count for a 10GiB DB with 1min WAL cadence:
- ~10,000 WAL segments / 7-week window (16MiB each after compression → ~40GB)
- 7 fulls × ~3GB = ~21GB
- 3 diffs × ~500MB = ~1.5GB
- **Budget ~60GB in R2.** Well under the 10GB R2 free tier? No — but
  egress is free from R2 and storage is ~$0.015/GB/month ≈ $1/month.

---

_Amendments_

- 2026-04-17 / ai (P1.1 build) / initial runbook.
