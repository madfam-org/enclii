# Managed Postgres Addon Backups

> **Boundary checkpoint (2026-10-26, platform on-call):** adds an intake target
> id, Vault property NAMES, Kubernetes object names, a bucket name and the
> drill's plan-hash procedure. No credential value, account identifier beyond
> what `postgres-ha/cluster.yaml` already carries, hostname or tenant name
> appears here.

> **Boundary checkpoint (2026-08-17, platform on-call):** Public-safe runbook —
> no secrets or production topology beyond this repo's own IaC; the R2 bucket
> names and env keys here are configuration contracts, not values. Private
> operational detail and the incident sink live in `internal-devops` (2026-08-17
> enclii platform security audit). Policy: `docs/PUBLIC_REPO_BOUNDARY.md`
> (repo-boundary contract).


> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: raw `kubectl` below is bootstrap /
> break-glass only. The recurring path is switchyard's provisioner and the
> addon reconciler's backup sweep, which wire backups automatically once the
> operator prerequisites here exist.

**Scope:** the per-tenant Postgres created by `enclii` addons
(CloudNativePG `Cluster`s in `project-*` namespaces) — NOT the platform's own
`postgres-ha` (see [BACKUP_COVERAGE.md](BACKUP_COVERAGE.md) for that).

## The gap this closes

Until 2026-08-17, the addon provisioner emitted a `Cluster` with **no backup
stanza**. That change taught the provisioner to emit
`spec.backup.barmanObjectStore` and a daily `ScheduledBackup` when the object
store is configured, but on 2026-10-26 `AddonBackupNeverCompleted` was still
firing for every live addon cluster, and no addon had ever been backed up.
Four links were missing, and each was enough on its own:

1. switchyard-api ran without `ENCLII_ADDON_BACKUP_DESTINATION_BASE`, so the
   provisioner left `spec.backup` out;
2. the source credential `enclii/enclii-db-backup-credentials` had never been
   created, and nothing in git wrote it;
3. the `switchyard-api` ClusterRole had no rule for `scheduledbackups`, so the
   ScheduledBackup create (and Deprovision's delete) would have been refused;
4. the addon namespace's `default-deny` admits DNS egress only, and the CNPG
   control-plane policy opens 443 to `10.0.0.0/8` only, so an instance could
   not have reached the object store.

Nothing would have healed a cluster that already existed either: the wiring
ran only inside `Provision`.

Two signals looked healthy while all of this held. Every addon cluster reports
`ContinuousArchiving=True`: with no backup stanza CNPG's archive command
succeeds without archiving anything, so the condition says nothing. And the
platform's own `postgres-ha` (and `angelia-pg`) back up daily to the platform
bucket, so "Postgres backups work" was true of the platform and false of every
tenant.

## How backups are wired now

switchyard-api converges backups onto every live, awake managed-Postgres
addon cluster: `Provision` for a new cluster, and the addon reconciler's
**backup sweep** (on start, then every 10 minutes, beside the NetworkPolicy
sweep) for every existing one. Per cluster, in this order:

1. copy `enclii/enclii-db-backup-credentials` into the addon namespace, owned
   by the Cluster and immutable;
2. create `<cluster>-backup-egress`: TCP 443 to non-private addresses for this
   cluster's pods only (R2 has no stable narrow CIDR and NetworkPolicy has no
   FQDN peer; this is the same shape service namespaces already get);
3. set `spec.backup` (merge patch of that field alone): `barmanObjectStore` at
   `<base>/<namespace>/<cluster>`, the copied credential, `retentionPolicy: 30d`.
   CNPG archives WAL to the same destination, so this is base backups **and**
   continuous WAL archiving;
4. create `<cluster>-daily` (`0 0 4 * * *`, `immediate: true`).

Setting `spec.backup` switches WAL archiving to the object store. If an
instance could not authenticate or connect, `archive_command` would fail and
WAL would pile up in `pg_wal` until the volume filled. So steps 1 and 2 come
first, and the whole convergence **refuses** (writes nothing) while the
credential is absent or lacks a key. `Provision` makes the same decision
before the Cluster exists and strips the stanza rather than start a database
that cannot archive.

Never wired: a **hibernated** cluster (`cnpg.io/hibernation=on`; it is wired
when it wakes). Its Enclii ScheduledBackup, if it has one, is suspended
(`spec.suspend: true`, annotated `enclii.dev/suspended-for-hibernation`) so it
does not produce a failed Backup every night, and resumed when the cluster
wakes; a schedule an operator suspended is never resumed. Never touched: a
cluster Enclii does not manage (labels
`managed-by=enclii` and `enclii.dev/addon-id` must match the addon), the
Karafiel/Janua staging profiles (backup-free by design), and a cluster whose
existing `spec.backup` differs from the one Enclii writes (logged at ERROR as
drift, left for an operator).

Fail visible: while the store or its credential is missing, every sweep logs
one ERROR line with the unprotected count, and `AddonBackupNeverCompleted`
keeps paging.

## Operator prerequisites (one-time, owner)

Merged and synced, the code and manifests are inert until the credential
exists. In this order:

1. **Create the R2 bucket** `enclii-addon-db-backups` in the platform
   Cloudflare account. It is separate from the platform `enclii-backups`
   bucket so tenant retention and the tenant-facing credential stay apart.
   Create it first: submitting the credential is what arms WAL archiving on
   every live cluster within one sweep, and archiving into a missing bucket
   fails.
2. **Mint an R2 API token**: permission Object Read & Write, scoped to the
   `enclii-addon-db-backups` bucket **only**.

   > [!WARNING]
   > **Least-privilege the R2 token.** The copy's lifetime and tamper surface
   > are bounded in code, but the *credential itself* is only as scoped as the
   > R2 token you mint. The copy lands in every addon namespace, so a
   > bucket-wide or account-wide token there would expose other backups.
   > Mint a token scoped to the addon-backups bucket ONLY, and prefer a
   > per-prefix token as the object store gains per-tenant paths. Remaining
   > leg of audit finding #1.
3. **Submit it through intake** (masked prompt; never paste it in chat or git):

   ```bash
   enclii secrets intake submit enclii/addon-db-backups \
     --reason "addon backups: R2 token for enclii-addon-db-backups"
   enclii secrets intake status int_<id>
   ```

   The prompt asks for `addon_backup_access_key_id` and
   `addon_backup_secret_access_key` (the token's S3 access key id and secret).
   Intake merges them into `secret/enclii`. The `enclii-db-backup-credentials`
   ExternalSecret
   (`infra/k8s/production/enclii-db-backup-credentials.externalsecret.yaml`)
   picks them up on its 15-minute refresh and writes exactly
   `ACCESS_KEY_ID` / `SECRET_ACCESS_KEY` (a template, so no other
   `secret/enclii` property reaches a Secret that is copied into every addon
   namespace). Intake does not force-sync it; to hurry it:

   ```bash
   enclii ops secrets sync-sweep                       # dry run: review the plan
   enclii ops secrets sync-sweep --apply --reason "addon backups credential"
   ```

switchyard-api already carries, in git
(`infra/k8s/production/environment-patch.yaml`):

- `ENCLII_ADDON_BACKUP_DESTINATION_BASE` = `s3://enclii-addon-db-backups`
- `ENCLII_ADDON_BACKUP_ENDPOINT_URL` = the account's R2 S3 endpoint (the same
  one `postgres-ha` uses)

Empty destination base = backups disabled (ERROR on every provision and every
sweep). Rotation: submit the target again; ESO updates the source Secret
within its refresh interval and the next sweep replaces every copy.

## Verify

Within about 25 minutes of the submit (one ExternalSecret refresh plus one
sweep), then again after the first
immediate backup completes. Read-only; raw `kubectl` here is break-glass
**read** because no Enclii read returns CNPG backup objects yet:

```bash
enclii ops secrets external -n enclii            # enclii-db-backup-credentials: SecretSynced
kubectl get scheduledbackups.postgresql.cnpg.io -A
kubectl get backups.postgresql.cnpg.io -n project-<id>     # phase completed
kubectl get clusters.postgresql.cnpg.io -n project-<id> <cluster> \
  -o jsonpath='{.status.lastSuccessfulBackup} {.status.firstRecoverabilityPoint}{"\n"}'
```

Check every **live, awake** addon, including any provisioned before the
credential existed (those were created without a stanza and are wired by the
sweep, not by `Provision`). Retired addons that are hibernated show no
schedule or a suspended one; that is by design.

`AddonBackupNeverCompleted` clears for a cluster once CNPG reports its first
available backup (the alert has `for: 2h`, so allow for it). Hibernated
clusters carry no schedule until they wake; that is by design.

`last_backup_at` on the `database_addons` row is the reporting surface (wiring
that column from the CNPG Backup status is a tracked follow-up).

## What this does NOT yet provide (tracked)

- **A tenant-facing restore path** — restore is currently an operator action
  via CNPG `Backup`/`Recovery` CRs; the client-self-service restore is part of
  the dignified-exit surface, not shipped.
- **A scheduled, Enclii-native restore drill** — the drill below is an
  operator script (break-glass `kubectl`, plan-bound). The adapter and the
  schedule are tracked in `ROADMAP.md` (engineering backlog) and
  `docs/ADAPTER_GAPS.md`.
- **A tested point-in-time recovery** — WAL is archived, but no drill has yet
  recovered to a timestamp.

## Restore drill for one addon

`scripts/restore-addon.sh` restores a single addon from its own barman backup
into a scratch CloudNativePG cluster and compares row counts against the
source. It needs a completed backup: before the prerequisites above it exits
`20` in the dry run, which is the finding, not a script failure.

It is a dry run, a review, then an apply bound to the reviewed plan. Never
paste the two runs as one block.

**1. Dry run** (writes nothing; prints the plan on stderr and its fingerprint):

```bash
scripts/restore-addon.sh \
  --namespace project-<id> \
  --cluster pg-<addon>-<id8> \
  --addon <addon-name> \
  --operator "$USER" \
  --dry-run
# ...
# PLAN-HASH <sha256>
```

The fingerprint covers the source Cluster's UID, the selected Backup (name,
UID, start time), the object-store path and endpoint, the image, the scratch
namespace and restored cluster name, the count scope and the flags.

**2. Review** the plan: the right cluster, the backup you mean to prove, a
scratch namespace (`enclii-restore-<cluster>` by default).

**3. Apply**, same arguments, without `--dry-run`, with the reviewed hash:

```bash
scripts/restore-addon.sh \
  --namespace project-<id> \
  --cluster pg-<addon>-<id8> \
  --addon <addon-name> \
  --operator "$USER" \
  --expect-plan <sha256>
```

It re-plans and refuses (exit `10`, nothing created) unless the live plan
hashes to the reviewed one: a newer backup, a recreated cluster or a different
target means a new dry run. A run without `--expect-plan` is refused.

It restores into `enclii-restore-<cluster>` and tears that namespace down
afterwards (`--keep` to inspect it). The credential copy in the scratch
namespace is rebuilt from name, type and data only: the addon's copy is owned
by the source Cluster, and carrying that owner reference into another
namespace would let the garbage collector delete the copy mid-restore.

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
lose column values. A mismatch, however, proves inequality. Rows written after
the backup and before the drill make the counts differ legitimately on a busy
database; drill a quiet addon, or pass `--table` for a table that does not
move.

Exit codes: `0` verified, `10` preflight, refused target or plan mismatch,
`20` no usable backup, `30` restore failed, `40` verification failed.

**Capacity.** The recovery Cluster asks for a 10Gi volume from the default
StorageClass in the scratch namespace. Check free Longhorn capacity first; a
recovery that cannot bind its volume fails with exit `30` and says nothing
about the backup.

The platform's own Postgres has separate drills: `enclii ops jobs trigger
postgres-restore-drill -n data --apply --reason "..."` (see
`scripts/wave1-ga-ops.sh --backup-drill`) and the monthly pgBackRest drill in
`infra/k8s/platform-infra/postgres-pgbackrest-restore-drill.yaml`. Before
reading a failure of the pgBackRest drill as a backup failure, check its
scratch volume's health: on 2026-10-26 its PVC was found running on one
replica although the StorageClass asks for two (a degraded volume, tracked by
the storage lane), so a drill failure there may be storage, not backup.
