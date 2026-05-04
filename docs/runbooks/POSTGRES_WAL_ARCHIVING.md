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

## 9. Lessons from the 2026-04-19 → 2026-05-04 silent-outage

The pgBackRest pipeline was **broken for ~14 days without alerting**
because every failure mode below silently chained — fix one, hit the
next. Document here so future operators recognise the chain quickly.
Each fix landed as a separate enclii PR (193, 195, 196, 197, 198, 199,
200, 201, 202, 203, 204, 205, 206, 207, 208, 214 — see PR list).

### 9.1 The chain

| # | Symptom | Root cause | Fix |
|---|---------|------------|-----|
| 1 | "[041] unable to open file '/etc/pgbackrest/pgbackrest.conf'" | woblerr/pgbackrest:alpine bakes `/etc/pgbackrest/` mode 750 owned by image-local pgbackrest user; sidecar runs as uid 999 (a different user inside the container) and can't traverse the dir | Mount config at `/etc/pgbackrest.conf` (file in `/etc/`, not subdir). pgbackrest checks both paths by default. |
| 2 | "[037] check command requires option: pg1-path" | pgbackrest 2.49+ rejects stanza-only options (`pg1-*`) under `[global]` | Move `pg1-path` / `pg1-port` / `pg1-user` / `pg1-socket-path` under `[main]` in the configmap |
| 3 | "[027] no database found" (sidecar) | Postgres unix socket lives in postgres container's filesystem; sidecar can't see it | Add a shared `postgres-socket` emptyDir mounted at `/var/run/postgresql` in BOTH containers |
| 4 | "FileMissingError: pg_control" | `pg1-path=/var/lib/postgresql/data/postgres` — phantom nested path; the subPath mount already presents postgres/ AT /var/lib/postgresql/data | Fix `pg1-path=/var/lib/postgresql/data` |
| 5 | "[087] archive_mode must be enabled" | Postgres `args:[]` was emptied earlier to avoid an `include_dir` crash; this disabled archive_mode | Re-enable via individual `-c` flags: `wal_level=replica`, `archive_mode=on`, `archive_command=...`, etc. |
| 6 | "no valid backups", stanza-create hangs forever | pgbackrest-r2-credentials secret never provisioned (manifest had `optional: true`) | Create secret with `R2_ACCOUNT_ID`, `PGBACKREST_REPO1_S3_KEY`, `PGBACKREST_REPO1_S3_KEY_SECRET`, `PGBACKREST_REPO1_S3_ENDPOINT`, `PGBACKREST_REPO1_CIPHER_PASS` |
| 7 | TCP timeout on stanza-create + check (after secret applied) | postgres-egress NetworkPolicy was DNS-only (port 53/UDP); blocked HTTPS to R2 | Add port 443/TCP to postgres-egress (and 80/TCP for the apt path in the init container) |
| 8 | "exit 127: cannot execute: required file not found" (archive_command) | woblerr image's pgbackrest binary is musl-linked; postgres container is glibc → can't run the binary | Init container uses `postgres:15-bookworm` + apt + ldd-bundles libs into a shared volume. Wrapper script sets `LD_LIBRARY_PATH` |
| 9 | "libssh2.so.1: cannot open shared object file" | binary works but its dynamic deps aren't in the postgres container's `/usr/lib` | Same init container also copies every `ldd /usr/bin/pgbackrest` dep into `/opt/pgbackrest/lib/` |
| 10 | "unable to verify certificate presented by R2" | Postgres minimal image has no `/etc/ssl/certs`; pgbackrest can't verify R2 TLS | Init container also copies `ca-certificates.crt`; wrapper sets `SSL_CERT_FILE` |
| 11 | "'zstd' is not allowed for 'compress-type'" | pgbackrest 2.58 (postgres-side, glibc) only accepts `zst`; 2.55 (sidecar, musl) accepts both | Use `zst` (3-letter form) — both versions accept it |

### 9.2 Why this didn't alert

- `pgbackrest-check` CronJob fired every 15 min, exited non-zero, but
  the `pgbackrest_check_healthy` Prometheus metric was never wired to
  alertmanager via a `record:` rule that fires on `==0` for >1h. **TODO**:
  add an alert on the textfile metric so future regressions surface
  inside an hour.
- WAL accumulation in spool was below the alerting threshold (2 GiB
  queue max); lots of headroom masked the failure-by-volume.
- The `postgres-restore-drill` CronJob targets the legacy `pg_dump`
  path, not pgBackRest — even when it ran, it didn't exercise the
  broken pipeline.

### 9.3 What's protective going forward

- **Monthly DR drill** (`postgres-pgbackrest-restore-drill` CronJob in
  `infra/k8s/platform-infra/`) — runs a side-channel pgbackrest restore
  to a 4 GiB Longhorn PVC, validates `pg_control`, emits
  `pgbackrest_restore_drill_success` metric. Triggered by `kubectl
  create job --from=cronjob/pgbackrest-restore-drill <suffix>`
  on demand, otherwise 1st of each month at 06:00 UTC.
- **Postgres init container** is now `postgres:15-bookworm` with apt-
  installed pgbackrest + bundled libs + CA — keeps the binary glibc-
  matched against the running postgres image even after upstream tag
  bumps.
- **Single-source config** — every option pgbackrest needs is either in
  the configmap (under `[global]` or `[main]` per stanza-only rules),
  or in `pgbackrest-r2-credentials` Secret as `PGBACKREST_<OPTION>` env
  var. Don't try to `${VAR}`-substitute in the configmap (pgbackrest
  doesn't do shell expansion).

---

_Amendments_

- 2026-04-17 / ai (P1.1 build) / initial runbook.
- 2026-05-04 / ai (post-mortem) / added §9 documenting the 11-step
  failure chain caught in the silent-outage window plus the protective
  changes (DR drill, init container glibc bundle, alerting gap noted).
