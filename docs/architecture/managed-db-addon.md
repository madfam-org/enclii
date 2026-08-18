# Managed-DB Addon API — Sprint 1 (P3.1)

> **Status:** Sprint 1 in flight (design + plan catalog + audit events + CLI + UI)
> **Remediation plan item:** [P3.1](https://github.com/madfam-org/internal-devops/blob/main/roadmaps/2026-04-enclii-remediation-plan.md)
> **Owner:** ai
> **Related audit:** `audits/2026-04-enclii-platform-audit.md`

## Problem statement

Railway / Heroku / Render / Fly all expose a one-shot addon API:
`<cli> addon create postgres --plan <name>` yields a fresh isolated database
scoped to the caller's service, with `DATABASE_URL` auto-injected as an env var.
Enclii today does not. Customers must either run inside MADFAM's shared
platform Postgres (tenancy-coupled — not sellable) or hand-provision a
`CloudNativePG` cluster and wire credentials by hand. This is the biggest
Vercel / Railway parity gap identified by the 2026-04 audit and gates every
Phase 3 external-customer conversation.

## Scope by sprint

| Sprint | Deliverable | Status |
| --- | --- | --- |
| 1 (this PR) | Design doc, **plan catalog** (schema + seed), **audit events table**, plan plumbed through service+API+CLI, CLI `enclii addon`, UI page | ✅ this PR |
| 2 | **HA variant** (3-replica synchronous quorum), **PITR** via pgBackRest (R2 tie-in from P1.1), backup UI, restore flow | queued |
| 3 | **Billing integration** — Waybill emitter per addon, unit cost catalog, budget throttle hook, Stripe line-items | queued |
| 4+ | Non-Postgres engines (Redis/MySQL are *scaffolded* — not GA), custom extensions, read replicas, per-region placement | demand-gated |

## Pre-Sprint-1 baseline

A surprising amount of the addon stack already existed before Sprint 1 began — enough to pivot Sprint 1 away from "build from scratch" and toward "fill the gaps, formalize the contract."

Pre-existing work (from `001_genesis.up.sql` and earlier unshipped work):

- `database_addons` and `database_addon_bindings` tables with full column set
  (status, config, k8s resource refs, audit, soft delete).
- `addons.AddonProvisioner` interface implemented by `PostgresProvisioner`,
  `RedisProvisioner`, `MySQLProvisioner` — Postgres path uses **CloudNativePG**
  CRDs (cluster-per-addon) which is *strictly stronger isolation* than the
  "DB-per-addon in shared cluster" model originally sketched for Sprint 1.
  This is a better starting point for P3 and we are keeping it.
- `addons.AddonService` with `CreateAddon / ListAddons / DeleteAddon / CreateBinding /
  GetCredentials / RefreshStatus`.
- HTTP handlers under `/v1/projects/:slug/addons` and `/v1/addons/:id/*`
  registered in `api/handlers.go:612-624`, with `RequireRole(Developer|Admin)`
  gating the mutations.
- `DatabaseAddonRepository` with full CRUD + soft delete, sqlmock-backed test
  suite in `db/database_addon_repository_test.go`.

What Sprint 1 **adds** to this baseline:

1. **Plan catalog**: replaces the free-form `DatabaseAddonConfig` as the
   customer-facing knob. Plans are an enum with resource presets attached. This
   is the foundation Sprint 3 billing hangs prices off.
2. **Audit events table** `managed_db_addon_events` — domain-specific append-only
   ledger distinct from the general `audit_logs` surface. Each lifecycle
   transition emits an event; Sprint 3 billing reads `created` / `destroyed`
   events as billable signals.
3. **CLI first-class command**: `enclii addon create|ls|destroy` (pre-existing
   HTTP surface had no CLI ergonomics).
4. **UI page**: `/projects/[slug]/addons` with create modal + destroy flow.
5. **Design and contract clarity** — this document.

## Decisions (locked for Sprint 1)

| # | Decision | Reasoning | Reversible at |
| - | -------- | --------- | ------------- |
| D1 | **Isolation: cluster-per-addon (CloudNativePG)**, not DB-per-addon in a shared cluster. | Pre-existing implementation is already at this level; rolling back to shared-cluster would be a downgrade. Cluster-per-addon matches Heroku Essential-tier isolation and avoids noisy-neighbor / credential-leak classes of bug entirely. | — (choice ratified) |
| D2 | **Plans are enum (not free-form config)**. Sprint 1 ships three: `standard-0` (1 GB / 0.1 CPU / 256 Mi), `standard-1` (10 GB / 0.5 CPU / 1 Gi), `standard-2` (50 GB / 1 CPU / 2 Gi). | Plans are how billing will price in Sprint 3. Making them enum-typed now avoids a retroactive migration. Free-form config remains on the row as `config_override` for internal escape hatches. | Sprint 3 billing cutover |
| D3 | **Credential delivery: K8s Secret ref** (CloudNativePG materializes `<cluster>-app`). API responses carry the Secret name, never the password. Bindings inject `DATABASE_URL` env var by reading the Secret at Deployment-render time. | Matches existing pattern. External Secrets Operator + Vault integration (from P0.2) is a Sprint 2 hardening — for Sprint 1 the Secret lives in the project's namespace directly. | Vault integration (Sprint 2) |
| D4 | **Lifecycle states: `pending / provisioning / ready / pending_deletion / deleting / deleted / failed`**. `pending_deletion` added by the 2026-08-17 audit #10 retention hold (migration 035). | Spec called for `provisioning → ready → decommissioning → destroyed`; the pre-existing state set is functionally equivalent and already enforced by a CHECK constraint. `pending_deletion` is the retention-hold grace state — see "Retention hold on delete" below. | — |
| D5 | **Naming: customer-provided `name`, unique per project**. No forced `<service>-db` suffix. | Pre-existing `unique_addon_name_per_project` constraint. Customers pick; CLI can default if they leave it blank. | — |
| D6 | **Extensions: `uuid-ossp` and `pgcrypto` on by default**. | Configured via CloudNativePG `bootstrap.initdb.postInitSQL` in Sprint 2; Sprint 1 provisions the cluster without them and documents the gap. | Sprint 2 |
| D7 | **Plan enforcement is server-side**. CLI sends `plan` string; API rejects unknown plans with 400. The plan catalog lives in the `managed_db_plans` table (Sprint 1) so Sprint 3 can attach prices and toggle availability without a code deploy. | Decoupling catalog from code lets product adjust pricing and plan availability operationally. | — |
| D8 | **Auto-delete on service destruction is NOT wired**. Addon outlives a service destroy; deletion is explicit only. | Matches Heroku semantics and protects against accidental data loss in multi-service setups. Customer-reported deletes are the only source of truth. | — |

## Data flow (create)

```
  CLI: enclii addon create my-db --plan standard-0 --service api
        │
        ▼
  POST /v1/projects/:slug/addons  (RequireRole Developer)
        │
        ▼
  api.CreateAddon handler
        │ • resolve slug → project
        │ • validate plan via ManagedDBPlans.GetByCode()
        │ • resolve service_id if --service given
        │ • audit: emit 'addon.create.requested' event
        ▼
  addons.AddonService.CreateAddon
        │ • db.WithTransaction:
        │     - INSERT database_addons (status=pending, plan=standard-0)
        │     - INSERT managed_db_addon_events (type=created)
        │ • spawn goroutine → provisionAddon()
        │                        ▼
        │                 PostgresProvisioner.Provision
        │                        │ • ensure namespace project-<8char>
        │                        │ • build CloudNativePG Cluster CR from plan
        │                        │ • dynamicClient.Create
        │                        ▼
        │                 CloudNativePG operator materializes:
        │                    • StatefulSet + PVC
        │                    • Services (rw/ro/r)
        │                    • Secret <cluster>-app (host/port/user/password/dbname/uri)
        │                 update addon: k8s_namespace, k8s_resource_name, connection_secret
        │                 status transition: pending → provisioning
        │                 emit 'addon.provisioning.started'
        ▼
  reconciler loop (addon_reconciler) polls status every 15s:
        │ • phase=Cluster in healthy state & ready≥1 → status=ready, emit 'addon.ready'
        │ • phase=Failed → status=failed, emit 'addon.failed' + error_message
        ▼
  Binding flow (when --service given):
        POST /v1/addons/:id/bindings {service_id, env_var_name=DATABASE_URL}
        → INSERT database_addon_bindings (active)
        → emit 'addon.binding.created'
        → next deploy of service picks up env var via GetEnvVarsForService()
```

## Data flow (destroy)

```
  CLI: enclii addon destroy <id>
        │
        ▼
  DELETE /v1/addons/:id  (RequireRole Admin)
        │
        ▼
  addons.AddonService.DeleteAddonWithOptions
        │
        ├─ default (data-bearing engine, provisioned): RETENTION HOLD
        │     • status: → pending_deletion, deletion_scheduled_at = now + 7d
        │     • emit 'addon.destroy.requested' {retention_hold:true, deletion_scheduled_at}
        │     • CNPG Cluster + PVCs + barman backups LEFT INTACT
        │     • HTTP 202; recoverable until the window elapses
        │           │
        │           ▼  (reconciler tick, once deletion_scheduled_at passes)
        │     addon_reconciler.sweepExpiredRetentionHolds
        │           → AddonService.FinalizeExpiredDeletion → finalizeDeletion (below)
        │
        └─ force=true (platform-admin only) OR Redis OR not-yet-provisioned:
              finalizeDeletion:
                • status: → deleting
                • PostgresProvisioner.Deprovision → dynamicClient.Delete Cluster CR
                  (CNPG cascades StatefulSet, PVC, Services, Secret — but PVCs are
                   on a Retain-reclaim StorageClass so the underlying PV survives)
                • soft-delete row: deleted_at=now(), status=deleted
                • emit 'addon.destroyed' {forced}
        ▼
  Bindings referencing this addon fall through to empty env vars on next deploy.
  Historical data in database_addons row is retained for audit + future billing.
```

## Retention hold on delete (2026-08-17 audit #10)

Before this change a `DELETE /v1/addons/:id` immediately tore down the
CloudNativePG `Cluster`, and CNPG's default reclaim behavior destroyed the
cluster PVCs — a single delete irreversibly destroyed a client's production
database with no grace window and no platform-side recovery path. Two
mitigations now stand between a delete request and data destruction:

1. **Retention window (soft delete).** For a provisioned, platform-owned,
   data-bearing engine (managed Postgres/MySQL) a plain delete marks the addon
   `pending_deletion` and stamps `deletion_scheduled_at = now + 7d`
   (`DefaultDeletionRetention`). The CNPG `Cluster`, its PVCs, and its barman
   backups are **not** torn down. The `addon_reconciler` sweeps for holds whose
   window has elapsed (`DatabaseAddons.ListDeletionDue`) and only then runs the
   destructive teardown. During the window the delete is fully recoverable and a
   departing client has time to pull an export ("dignified exit", relevant to
   ADR-0004). Redis addons and not-yet-provisioned rows skip the hold — there is
   no platform-recoverable data to protect.

2. **PVC reclaim = Retain.** Managed-Postgres PVCs are pinned to the
   `longhorn-replicated` StorageClass (`RetainStorageClass`), whose
   `reclaimPolicy` is `Retain`. So even when the teardown finally deletes the
   PVC, the underlying `PersistentVolume` (and Longhorn volume) survives and the
   data remains recoverable — unlike the default `longhorn` class
   (`reclaimPolicy: Delete`) which destroys the volume with the PVC.

3. **Force delete is platform-admin only.** `DELETE /v1/addons/:id?force=true`
   bypasses the hold and tears down immediately. The route already requires
   `RequireRole(Admin)`; the `force` flag additionally requires the caller to be
   a platform admin (`callerIsPlatformAdmin`), so an ordinary project member
   can only ever schedule the hold, never instantly destroy a production DB.

**Recovering an addon inside the window:** the addon row is still present
(`deleted_at IS NULL`, `status = pending_deletion`). Clearing
`deletion_scheduled_at` and restoring the prior status un-schedules the
teardown; the CNPG `Cluster` was never touched. A first-class
`enclii addon undo-delete <id>` surface is a follow-up (see below).

## Rollback strategy per state

| Transition | Failure mode | Recovery |
| --- | --- | --- |
| `pending → provisioning` | CloudNativePG namespace create fails | Row stuck in `pending`; reconciler retries 3× with 30s backoff, then `failed` with reason. Operator reruns with `enclii addon retry <id>` (Sprint 2). |
| `provisioning → ready` | Cluster stuck in `Setting up primary` > 15min | Reconciler transitions to `failed` with `provision_timeout`. Operator destroys addon, reviews CNPG logs, recreates. |
| `ready → pending_deletion` | `ScheduleDeletion` write fails | Delete request returns an error; nothing torn down (fail-safe). Client retries. |
| `pending_deletion → deleting` | Retention sweep's `Deprovision` errors | Row stays in `pending_deletion` (or `failed` if deprovision half-ran); next reconciler tick retries the finalize. A stuck teardown surfaces via `status_message` + the event ledger. |
| `ready → deleting` (force) | CNPG Cluster delete returns error | Row moves to `failed` with the deprovision error; force-delete is a synchronous operator action, so the operator sees it and retries. |
| `deleting → deleted` | Row soft-delete fails | CNPG already gone, row orphaned in `deleting`. Audit event captures intent; cleanup script in `scripts/addon-gc.sh` (Sprint 2). |

## Security

Tenancy enforced at three layers:

1. **K8s namespace per project** (`project-<8char>`): CNPG Cluster CR, its
   Secret, Services, and PVC all live in the project namespace. Cross-namespace
   reads are denied by RBAC.
2. **Postgres authz**: CNPG provisions a `<db>` database owned by a dedicated
   `<user>` role (not superuser). The role has `LOGIN` and full privilege on
   its database only; no `CREATEDB`, no `CREATEROLE`, no superuser. Application
   traffic uses this role.
3. **API layer**: `RequireRole(Developer)` for create / create-binding;
   `RequireRole(Admin)` for delete. The handler resolves `project_slug` →
   `project_id` against `ProjectAccess` to verify the authenticated user has
   access. The **immediate/unrecoverable** teardown (`?force=true`) additionally
   requires a platform admin — a project-scoped admin's plain delete only
   schedules the retention hold (2026-08-17 audit #10).

Password rotation: not wired in Sprint 1. CNPG supports password rotation via
`kubectl` restart of the cluster Secret; Sprint 2 adds an `enclii addon
rotate-password <id>` flow.

Secret names follow CNPG convention `<cluster-name>-app`. These are never
returned in API responses as plaintext; responses include only the Secret
name as a reference.

## Open questions for Sprint 2+

1. **WAL archiving to R2 per addon** — P1.1 delivered the pattern for the
   shared Postgres. We want to generalize the pgBackRest sidecar pattern into
   CNPG's native `backup` spec so each addon archives to
   `s3://enclii-addon-backups/<project>/<addon>/`. Cost: ~$0.015/GB/mo. Plan
   budget line item Sprint 3.
2. **PITR UX** — Heroku exposes "fork at timestamp." CNPG supports this via
   `Cluster.spec.bootstrap.recovery`. Sprint 2 wires it into
   `enclii addon clone <src> --at '2026-04-15T14:00Z'`.
3. **Connection pooler** — today each addon binds ~10 connections. For higher
   tiers we want pgBouncer sidecar (already have `provisioning/pgbouncer.go`
   for the shared Postgres; reuse pattern).
4. **Plan migrations** — upsizing from `standard-0` to `standard-1` requires
   PVC resize + resource limit update. CNPG supports rolling updates. Sprint 2.
5. **Per-region placement** — Sprint 4+ (requires multi-region bare-metal).
6. **Redis / MySQL GA** — scaffolds exist but need the same treatment (plans
   + events + CLI + UI). Pushed until there's pull.
7. **`enclii addon undo-delete <id>` + UI affordance** — the retention hold
   (audit #10) makes a delete recoverable during the window, but there is no
   first-class un-schedule surface yet; recovery is currently a manual DB
   update (clear `deletion_scheduled_at`, restore prior status). The delete
   response returns `deletion_scheduled_at` so the UI can show a countdown and
   an "undo" affordance. Follow-up.

## Exit criteria for Sprint 1

- [ ] Plan catalog seeded; `enclii addon create --plan standard-0` succeeds
      end-to-end against a CNPG-enabled cluster.
- [ ] Audit events table populated on every lifecycle transition, verified in
      integration test.
- [ ] `enclii addon ls` lists all non-deleted addons for the user's projects.
- [ ] `enclii addon destroy <id>` soft-deletes row, deletes CNPG Cluster CR,
      emits `addon.destroyed` event.
- [ ] UI page `/projects/[slug]/addons` renders + create modal + destroy flow.
- [ ] ≥25 new unit/handler tests, all green.
- [ ] This design doc merged.
