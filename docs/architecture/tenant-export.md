# Tenant Data Export

Status: Sprint (P3.6)
Owner: Platform
Audience: customers, auditors, SOC 2 assessors, internal ops

## 1. Why this exists

Enclii promises portability: "you can take your data and leave at any time"
(trust-center, P0.3). That promise needs tooling — not a prose paragraph.
P3.6 ships the customer-facing surface behind it and, by rotation, also
becomes the tooling P1.4's Q1-2027 DR rehearsal scenario depends on.

This is a sales asset (no lock-in, verifiable), a compliance asset
(SOC 2 CC6 / portability, GDPR Art. 20 data portability), and an internal
resilience asset (we can self-serve a full-project snapshot for support).

## 2. Scope

A single tarball per project, produced on demand, containing everything
Enclii holds about the project that the customer could re-assemble into a
stock Kubernetes cluster:

- K8s manifests (project, services, deployments, cron jobs, env-vars with
  redacted secret references) as authored YAML
- `pg_dump` of each managed or shared-tenant database addon bound to the
  project
- R2 blob inventory (keys, sizes, sha256) for any project-owned buckets
- Secret references (names, types, rotation timestamps) — **not values**
- Audit timeline scoped to the project (Selva RFC 0005/0006/0007/0008)
- CLI / API metadata (release versions, deployment history)
- A README explaining the layout and a restore recipe

### Explicitly out of scope

- Secret **values**. If a customer wants the plaintext material they must
  rotate post-leave via Vault (P0.2 + RFC 0005 Sprint 3). Exporting values
  would turn one stolen session into a credential-harvest. A cleartext
  export would also defeat the per-secret audit trail.
- R2 blob **contents**. The tarball ships an inventory; blobs are pulled
  directly from R2 with the customer's own credentials, because re-packing
  multi-TB of blobs would gate every export on the slowest bucket. The
  inventory is a sha256-verifiable manifest.
- Per-row application data outside managed DBs. If a tenant runs their own
  schema on their own cluster that Enclii can't reach (BYO DB), we don't
  have it and can't ship it.
- Infrastructure state owned by the platform (node pools, shared
  ingresses, Cloudflare tunnel config). That's Enclii-tenant, not
  customer-tenant.

## 3. Format

Single tarball named `enclii-export-<project_slug>-<iso_ts>.tar.gz`,
compressed with gzip -9. Structure:

```
manifests/
  project.yaml                         # Enclii project spec
  services/<service-name>.yaml         # Service specs (Enclii format)
  deployments/<service-name>.yaml      # Active K8s Deployment + Service
                                       # (namespace scrubbed to "<placeholder>")
  cron_jobs/<cron-name>.yaml           # K8s CronJob manifests
  envvars/<service-name>.json          # Env var keys (+types, metadata); values redacted
databases/
  <addon-name>/pg_dump.sql.gz          # Custom-format pg_dump per addon
  <addon-name>/schema.sql              # Plain SQL schema for grep-ability
  <addon-name>/addon.json              # Addon metadata (version, size, etc.)
blobs/
  <bucket-name>/manifest.json          # {key, size, sha256, last_modified}[]
secrets/
  references.json                      # {name, type, created_at, last_rotated_at}[]
                                       # NO VALUES. Values are rotated post-leave.
audit/
  timeline.ndjson                      # Project-scoped Selva RFC 0005/0006/0007/0008 events
  deployments.ndjson                   # Deployment history from Switchyard
README.md                              # What's inside + restore instructions
MANIFEST.json                          # Top-level: file list, sha256s, counts, sizes
```

### Size caps and splitting

Hard cap: 5 GB compressed per part. Projects whose export exceeds 5 GB get
split:

```
enclii-export-<slug>-<ts>-part001.tar.gz
enclii-export-<slug>-<ts>-part002.tar.gz
enclii-export-<slug>-<ts>-index.json   # which part each path lives in + sha256
```

The `part_count` column on `tenant_exports` tracks this. The index file is
cheap to fetch independently and lets the customer pick up interrupted
multi-part downloads.

Realistic ceiling: the largest production project today is about 2.3 GB
(dhanam with two bound databases and a moderate audit history). Nothing
today triggers the split path, but it exists so a future DB-heavy tenant
doesn't force a special case.

## 4. Authorization

**Role gate**: project-admin. Developer and viewer roles get 403. Admin on
the platform can trigger any project's export (consistent with how other
admin operations behave).

**HITL approval gate** (production only):

The production gate exists because a credential-steal attacker who
captures a customer session token could otherwise grab *everything* Enclii
holds about the project in one silent call. Binary guarantee: in
production we never process a tenant export without an out-of-band
approval.

Flow:

1. `POST /v1/projects/:slug/exports` in production inserts the row with
   `status=pending` and emits an approval request. The request is a
   specialization of the existing approval surface used for prod instant
   rollbacks — same email/Slack routing through
   `h.notificationService`.
2. A separate project admin (distinct from requester) approves via a
   time-limited link. Approval writes `approved_by` and `approved_at`,
   flips status to `running`, and kicks off the pipeline.
3. If no approval lands in 72h the row auto-expires (status=`failed`,
   error=`approval_timeout`).

In dev and staging the HITL step is skipped — rows insert directly with
`status=running`. The gate is an environment-awareness check on
`cfg.Environment == "production"`, not a feature flag.

## 5. SLA

- **Initiation** (row in DB + pipeline kicked off): within 1 hour.
  Production's HITL can stretch this in practice, which is why the SLA
  clock starts post-approval.
- **Completion** (tarball ready, email sent): within 24 hours for typical
  projects (<5 GB). Multi-part exports over 5 GB get a best-effort SLA.
- **Delivery**: email-driven. Customer receives a deep link into the
  Switchyard UI, not a direct URL. Each download is a fresh 15-minute
  pre-signed R2 URL.

Where the 24h claim can break: (a) customer has a DB addon larger than
~8 GB — pg_dump dominates and a long-running Job might exceed the
allowance; (b) audit timeline is multi-year and requires a full
nexus-api backfill; (c) shared-DB tenants whose `pg_dump -t` on specific
schemas has to wait behind other tenants' write traffic.

## 6. Retention

- Tarball lives in R2 for 14 days after `status=ready`.
- `tenant_exports` row retains for 90 days (R2 key is nulled after 14).
- Cleanup via `tenant-export-cleanup` CronJob, hourly. Staleness guard:
  if the job doesn't run for 48h, alerting fires.

## 7. R2 path convention

```
tenant-exports/
  <project_slug>/
    <export_id>/
      part001.tar.gz
      part002.tar.gz
      index.json
```

Deliberately not under the `backups/` prefix — these are customer-
initiated and have different retention from the platform backup stream.

## 8. Pipeline

The export runs as a Kubernetes Job in the `data` namespace, not as a
goroutine in the API pod. A tenant export can be multi-GB and shouldn't
compete with API pod CPU or memory. The Job:

1. Marks `status=running`, writes start timestamp.
2. Gathers K8s manifests for the project's namespace via client-go
   (`Deployment`, `Service`, `CronJob`, `ConfigMap`, `Ingress`). Scrubs
   namespace names and Secret values — only references.
3. For each database addon: runs `pg_dump` (custom format + plain schema),
   streams gzipped output into the staging volume.
4. For each R2 bucket prefix owned by the project: enumerates and builds
   a sha256 manifest. No blob content is copied.
5. Pulls Selva audit events (RFC 0005/0006/0007/0008) scoped to
   `project_id` from nexus-api.
6. Writes README + MANIFEST.json.
7. Tars, gzips, splits into parts if >5 GB.
8. Computes tarball sha256(s), uploads to R2.
9. Updates row: `status=ready`, `tarball_r2_key`, `tarball_size_bytes`,
   `sha256`, `part_count`, `expires_at = now + 14d`.
10. Emails customer: "Your Enclii export is ready" with deep link.
11. On any error: `status=failed`, `error_message=<truncated>`. Defer
    cleanup of partial R2 objects; never leave half-tarballs stranded.

Pre-signed URLs are **never** persisted in audit details or logs — only
the redacted form (`r2://<bucket>/<project>/<export_id>/...`) is logged.

## 9. API surface

| Method | Path | Auth | Behavior |
|--------|------|------|----------|
| `POST` | `/v1/projects/:slug/exports` | project-admin | prod: `pending` + HITL; non-prod: `running` immediately. 202. |
| `GET` | `/v1/projects/:slug/exports` | project-member | List project's exports, 14-day window. |
| `GET` | `/v1/exports/:export_id` | project-admin | Status; if `ready`, returns a fresh 15-min pre-signed URL per call. |
| `DELETE` | `/v1/exports/:export_id` | project-admin | Soft delete, purge R2 early. |

No re-use of pre-signed URLs across GETs. Each call regenerates — makes
URL leakage non-catastrophic.

## 10. Restore guide (separate follow-up)

This PR ships the export path only. A companion doc
`docs/runbooks/RESTORE_FROM_TENANT_EXPORT.md` will walk a customer through
unpacking the tarball into a stock K8s cluster (kubectl apply + pg_restore
+ R2 rsync). That doc depends on this PR landing so its screenshots and
hashes are stable.

## 11. Threat model

- **Stolen session token** → prod HITL gate means attacker needs a second
  admin session in the 72h window. Escalation: email alert to all project
  admins on pending-export creation.
- **URL harvesting** → pre-signed URLs are 15-minute, regenerated per
  request. Audit logs carry only redacted forms.
- **Tarball tampering** → sha256 in the row (and in MANIFEST.json inside
  the tarball). Customer restore docs instruct verification.
- **Cross-project leak** → every gather step is scoped to `project_id`.
  Integration tests explicitly assert project-B data doesn't appear in
  project-A exports.

## 12. Observability

Prometheus metrics:

- `enclii_tenant_export_duration_seconds{status}` (histogram)
- `enclii_tenant_export_size_bytes{status}` (histogram)
- `enclii_tenant_exports_total{status}` (counter)
- `enclii_tenant_export_pending_approval{project}` (gauge, alert if >1h)

Log lines structured JSON. No URLs, no secret material, no DB rows.
