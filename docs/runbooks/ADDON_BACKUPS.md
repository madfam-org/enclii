# Managed Postgres Addon Backups

> **Boundary checkpoint (2026-10-26, provisioning):** the credential is
> provisioned by `ops.addons.backup-credential`; this page names Cloudflare
> permission-group names and object names only, never a token or key.
>
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
4. annotate the Cluster `enclii.dev/backup-credential-version` with a short
   SHA-256 prefix of the copied credential (it names a version, never a value).
   CNPG's instance manager does not watch Secrets: it reloads the credential
   only when its Cluster changes, so without this step a replaced copy leaves
   running instances on the old one;
5. create `<cluster>-daily` (`0 0 4 * * *`, `immediate: true`).

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

## Provisioning the credential (one-time; no human holds it)

Owner rule (2026-10-26): a person handles only the Vault admin token; every
other credential is provisioned by Enclii. The addon backup credential is no
exception. Nobody creates the bucket, mints a token or types a key. Switchyard
does all three through `ops.addons.backup-credential`:

1. ensure the bucket that `ENCLII_ADDON_BACKUP_DESTINATION_BASE` names
   (`enclii-addon-db-backups`), adopting it if it exists;
2. mint a Cloudflare API token scoped to **that bucket only**, permission
   group "Workers R2 Storage Bucket Item Write" (Object Read & Write), through
   the same `R2Provisioner` that project buckets use;
3. merge the S3 pair into `secret/enclii` (`addon_backup_access_key_id` = the
   token id, `addon_backup_secret_access_key` = sha256 of the token value), the
   path and keys of the intake target `enclii/addon-db-backups`;
4. force-sync the `enclii-db-backup-credentials` ExternalSecret
   (`infra/k8s/production/enclii-db-backup-credentials.externalsecret.yaml`),
   which templates exactly `ACCESS_KEY_ID` / `SECRET_ACCESS_KEY`.

The secret is never returned, printed, logged or written to an audit row. The
token id (the access key id) is reported: it is not secret, and it is the
handle for revoking the token.

It is a dry run, a review, then an apply bound to the reviewed plan. Never
paste the two as one block.

```bash
# 1. Dry run: writes nothing. Shows bucket create|adopt, token mint|keep,
#    the Vault path and keys, and plan_hash.
enclii ops addons backup-credential

# 2. Review the plan.

# 3. Apply, bound to it.
enclii ops addons backup-credential --apply \
  --expect-plan <plan_hash> \
  --idempotency-key addon-backups-credential-1 \
  --reason "addon backups: provision the bucket-scoped R2 credential"
```

Properties:

- **Platform rank** only. Apply writes an audit row before any Cloudflare or
  Vault mutation and refuses to start without one; a second row records the
  result (token name and id, bucket created or adopted, Vault version).
- **Idempotent.** When Vault already holds both keys the plan is `keep`, the
  dry run reports `succeeded`, and an apply writes nothing. Replaying a spent
  plan never mints a second token.
- **Rotation only when asked:** pass `--rotate` to both the dry run and the
  apply. The previous token is reported, not revoked: running clusters keep
  archiving with their copies until the sweep replaces them and annotates each
  Cluster with the new credential version, which makes its instances reload.
  Revoke it in
  Cloudflare once `AddonWALArchiveFailing` has stayed quiet for a day.
- **A refused mint names the missing permission.** If Cloudflare refuses, the
  result is `blocked` (HTTP 424) and names the exact permission the
  switchyard-api `CLOUDFLARE_API_TOKEN` lacks on the account: "API Tokens
  Write" plus "Workers R2 Storage Bucket Item Write" to mint, or "Workers R2
  Storage Write" to create or read the bucket. Nothing is written to Vault.
  The dry run cannot prove the mint permission; only the apply exercises it.
- **A failed Vault write revokes the new token,** so no unrecorded credential
  is left behind.
- **Endpoint guard.** The plan is refused if `ENCLII_ADDON_BACKUP_ENDPOINT_URL`
  is not the account switchyard-api provisions in.

Within one ExternalSecret sync (forced by the apply) plus one backup sweep
(about 10 minutes), every live, awake addon cluster is wired.

switchyard-api already carries, in git
(`infra/k8s/production/environment-patch.yaml`):

- `ENCLII_ADDON_BACKUP_DESTINATION_BASE` = `s3://enclii-addon-db-backups`
- `ENCLII_ADDON_BACKUP_ENDPOINT_URL` = the account's R2 S3 endpoint (the same
  one `postgres-ha` uses)

Empty destination base = backups disabled (ERROR on every provision and every
sweep).

**Break-glass only:** `enclii secrets intake submit enclii/addon-db-backups`
still writes the same two keys, but it needs a token minted outside Enclii.
It is not the procedure.

## Verify

Within about 10 minutes of the apply (the forced ExternalSecret sync plus one
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

If an awake instance still logs `Credential access key has length 10` (or
any `InvalidAccessKeyId`) after the sweep, check that its Cluster carries the
current `enclii.dev/backup-credential-version`; the sweep logs «Addon backup
credential version annotated» when it writes one.

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
