# Managed Postgres Addon Backups

> **Boundary checkpoint (2026-08-17, platform on-call):** Public-safe runbook —
> no secrets or production topology beyond this repo's own IaC; the R2 bucket
> names and env keys here are configuration contracts, not values. Private
> operational detail and the incident sink live in `internal-devops` (2026-08-17
> enclii platform security audit). Policy: `docs/PUBLIC_REPO_BOUNDARY.md`
> (repo-boundary contract).


> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: raw `kubectl` below is bootstrap /
> break-glass only. The recurring path is switchyard's provisioner, which
> wires backups automatically once the operator prerequisites here exist.

**Scope:** the per-tenant Postgres created by `enclii` addons
(CloudNativePG `Cluster`s in `project-*` namespaces) — NOT the platform's own
`postgres-ha` (see [BACKUP_COVERAGE.md](BACKUP_COVERAGE.md) for that).

## The gap this closes

Until 2026-08-17, the addon provisioner emitted a `Cluster` with **no backup
stanza**: 28 addons provisioned, `last_backup_at NEVER` on every one — the
platform's flagship DB was protected while every tenant's was not, silently.
The provisioner now emits a `spec.backup.barmanObjectStore` and a paired
daily `ScheduledBackup` (`immediate: true`) whenever the object store is
configured, and logs at ERROR on every provision when it is not.

## Operator prerequisites (one-time)

The provisioner wires backups **only when these exist**. Until then it
provisions clusters WITHOUT backups and says so loudly in the logs.

1. **R2 bucket** for addon backups, e.g. `enclii-addon-db-backups`, separate
   from the platform `enclii-backups` bucket so tenant retention/lifecycle is
   independent.
2. **Credentials Secret** `enclii-db-backup-credentials` in namespace
   `enclii`, keys `ACCESS_KEY_ID` and `SECRET_ACCESS_KEY`. The provisioner
   replicates this into each addon namespace at provision time (CNPG resolves
   `s3Credentials` locally); the copy is **owned by the CNPG Cluster**
   (garbage-collected on delete) and **Immutable** (a tenant workload cannot
   swap it) — 2026-08-17 security audit.

   > [!WARNING]
   > **Least-privilege the R2 token.** The copy's lifetime and tamper surface
   > are bounded in code, but the *credential itself* is only as scoped as the
   > R2 token you mint. The destination path is a per-cluster PREFIX under one
   > bucket, so a bucket-wide token grants any namespace that can read the
   > copied Secret visibility into every tenant's backups. Mint a token scoped
   > to the addon-backups bucket ONLY, and prefer a per-prefix token as the
   > object store gains per-tenant paths. Remaining leg of audit finding #1.
3. **switchyard-api env**:
   - `ENCLII_ADDON_BACKUP_DESTINATION_BASE` = `s3://enclii-addon-db-backups`
   - `ENCLII_ADDON_BACKUP_ENDPOINT_URL` = the R2 S3 endpoint
   Empty destination base = backups disabled (loud ERROR per provision).

## Backfill the addons provisioned before this landed

The 6 currently-ready addons have no backup config. For each, patch the
`Cluster` with a backup stanza and create the `ScheduledBackup` — or, simpler
and self-healing: **re-run provisioning** (the provisioner is idempotent on
the netpol/backup wiring). Verify:

```bash
# Which addon clusters lack a ScheduledBackup?
for ns in $(kubectl get clusters.postgresql.cnpg.io -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\n"}{end}' | sort -u); do
  cnt=$(kubectl get scheduledbackups.postgresql.cnpg.io -n "$ns" --no-headers 2>/dev/null | wc -l)
  echo "$ns scheduledbackups=$cnt"
done
```

## Verify a new addon is protected

```bash
# After provisioning, within a few minutes:
kubectl get scheduledbackups.postgresql.cnpg.io -n project-<id>
kubectl get backups.postgresql.cnpg.io -n project-<id>   # immediate:true → first backup exists
```

`last_backup_at` on the `database_addons` row is the reporting surface (wiring
that column from the CNPG Backup status is a tracked follow-up).

## What this does NOT yet provide (tracked)

- **Point-in-time recovery / WAL archiving** for addons — the stanza does base
  backups + retention; continuous WAL to the object store is the next
  increment (the platform `postgres-ha` already has it).
- **A tenant-facing restore path** — restore is currently an operator action
  via CNPG `Backup`/`Recovery` CRs; the client-self-service restore is part of
  the dignified-exit surface, not shipped.
- **Automated restore drill for addons** — a drill now EXISTS as an operator
  command (`scripts/restore-addon.sh`, below); scheduling it is the remaining
  step.

## Restore drill for one addon

`scripts/restore-addon.sh` restores a single addon from its own barman backup
into a scratch CloudNativePG cluster and compares row counts against the
source.

```bash
scripts/restore-addon.sh \
  --namespace project-<id> \
  --cluster pg-<addon>-<id8> \
  --addon <addon-name> \
  --operator "$USER"
```

It restores into `enclii-restore-<cluster>` and tears that namespace down
afterwards (`--keep` to inspect it). Preview everything it would do without
touching anything with `--dry-run`.

**Safety.** Any target that is not a scratch namespace is refused unless
`--i-understand-production` is passed deliberately; restoring into the addon's
own namespace is refused unconditionally, flag or not. Row counts run inside
the database pods over the local socket via `kubectl exec`, so no password,
DSN or connection URI is ever assembled on the operator's machine.

**The output of record** is the single line beginning `DR-LOG-ROW`. Paste it
verbatim into the private drill log:

```
DR-LOG-ROW | run_ts=… | addon=… | namespace=… | cluster=… | backup=… | backup_started=… | scope=… | rows_source=… | rows_restored=… | verdict=match | elapsed_s=… | operator=…
```

**What a passing drill proves, and what it does not.** It proves that the named
backup of the named addon can be recovered into a fresh cluster and that the
restored database holds the same number of rows the source held when the drill
started. It is not a recovery-point or recovery-time figure, not an
availability claim, and not evidence that point-in-time recovery works —
`elapsed_s` is the wall clock of one drill on one cluster under one load. A
row-count match is also not a content match: a restore can preserve counts and
lose column values. A mismatch, however, proves inequality.

Exit codes: `0` verified, `10` preflight or refused target, `20` no usable
backup, `30` restore failed, `40` verification failed.
