---
title: ops
description: Audited MADFAM operator workflows for Kubernetes, Argo, jobs, storage, policy, and runners
---

# `enclii ops`

`enclii ops` is the contract-first replacement layer for routine direct
`kubectl`, Argo, Longhorn, ExternalSecrets, Kyverno, CronJob, and ARC operations.

Mutating commands are dry-run by default. Use `--apply --reason "..."` only
when the corresponding server-side adapter is wired and the audit reason is
clear.

Read-only commands call live Switchyard adapters when configured. Current first
coverage includes Argo/Application status and drift summaries, pod
diagnosis/logs, CronJob inventory and manual triggers, PVC/PV inventory,
Longhorn CRDs, ExternalSecrets, Vault pod readiness, Kyverno
reports/exceptions, and ARC runner-scale-set inventory. If an
adapter is missing, the API returns `adapter_unconfigured` instead of
encouraging direct `kubectl`.

## Commands

| Command | Purpose |
|---------|---------|
| `enclii ops capabilities` | List server-supported operator capabilities |
| `enclii ops apps status|sync|sync-sweep|diff|retire|rollback` | Argo app inspection and remediation |
| `enclii ops apps tracking` | Declare Argo CD's annotation tracking default once in `argocd-cm` ([below](#argo-tracking-declaration)) |
| `enclii ops apps staging-intent [read OPERATION_UUID]` | Plan, retain or read an inert Janua staging intent; never creates a service or activation authority ([below](#held-janua-staging-intent)) |
| `enclii ops pods diagnose|logs` | Pod diagnosis and logs |
| `enclii ops pods restart` | Audited rollout restart of a Deployment, StatefulSet or DaemonSet, or recreation of one controller-owned pod; in project namespaces, of one CloudNativePG instance pod ([below](#ops-pods-restart)) |
| `enclii ops jobs list|trigger|suspend` | CronJob inspection, audited triggers and durable service-job holds; independent verified-target Jobs use the [closed API request](../../runbooks/ENCLII_VERIFIED_TARGET_JOBS.md) |
| `enclii ops storage volumes|pvc|longhorn|repair-plan|settings-apply|prune-detached|storageclass-apply|r2-audit` | PVC/PV/Longhorn inspection, repair planning, CPU settings (O-5), orphan prune (O-4), StorageClass reconcile, R2 credential audit (incomplete, shared, or mismatched buckets) |
| `enclii ops secrets external|vault|refresh|sync|sync-sweep|rotate|vault-backfill` | ExternalSecrets and Vault readiness workflows |
| `enclii ops addons backup-credential` | Server-side provisioning of the addon backup bucket and its bucket-scoped R2 credential into Vault; no human holds it ([below](#ops-addons-backup-credential)) |
| `enclii ops secrets provision-kalya-feed` | Server-side kalya standing-feed credential provisioning ([below](#ops-secrets-provision-kalya-feed)) |
| `enclii ops domains reconcile` | Declared-vs-live hostname reconcile for a service ([below](#ops-domains-reconcile)) |
| `enclii secrets intake` | Chat-safe operator credential handoff into Vault ([secrets.md](./secrets.md#enclii-secrets-intake)) |
| `enclii ops policy violations|exceptions|waiver-plan|cosign-enable` | Kyverno policy visibility, waivers, cosign namespace enforce (O-11) |
| `enclii ops runners arc|drain` | ARC runner-set inspection and drain planning |

## Examples

```bash
enclii ops capabilities
enclii ops apps status digifab-quoting-services --json
enclii ops apps diff monitoring -n argocd --json
enclii ops apps retire legacy-app -n argocd --apply --reason "retire reviewed legacy Argo application"
enclii ops jobs list -n forgesight --json
enclii ops jobs trigger forgesight-mexico-wave-seed -n forgesight --apply --reason "populate verified market data"
enclii ops storage settings-apply
enclii ops storage settings-apply --apply --reason "GA O-5 Longhorn CPU"
enclii ops storage prune-detached
enclii ops storage prune-detached --expect-plan <plan_hash> --apply --reason "GA O-4 orphan cleanup"
enclii ops storage storageclass-apply
enclii ops storage storageclass-apply --apply --reason "GA Longhorn StorageClass reconcile"
enclii ops policy cosign-enable
enclii ops policy cosign-enable --apply --reason "GA O-11 Cosign enforce"
./scripts/wave0-ga-ops.sh
./scripts/wave0-ga-ops.sh --apply --disk-prune --reason "GA Wave 0"
enclii ops pods diagnose switchyard-api -n enclii --json
enclii ops pods logs forgesight-pipeline-manual-abc123 -n forgesight --tail 500 --limit-bytes 524288 --json
enclii ops pods restart pod/loki-0 -n logging
enclii ops pods restart pod/loki-0 -n logging --expect-uid <reviewed-uid> --apply --reason "recreate loki-0 on the fixed revision"
enclii ops apps sync monitoring --apply --reason "clear Argo drift after reviewed manifest patch"
enclii ops apps sync-sweep -n argocd
enclii ops apps sync-sweep -n argocd --apply --reason "GA O-8 Argo sweep"
enclii ops secrets sync janua-secrets -n janua
enclii ops secrets sync janua-secrets -n janua --apply --reason "re-read Vault after a live ExternalSecret patch"
enclii ops secrets sync-sweep
enclii ops secrets sync-sweep --apply --reason "GA O-10 ESO reconcile"
enclii secrets vault-backfill enclii-secrets --namespace enclii --vault-path secret/enclii --external-secret enclii-internal-api-key --apply --reason "GA O-10 Vault backfill"
./scripts/post-deploy-ga-adapters.sh
./scripts/wave1-ga-ops.sh --apply --backup-drill --reason "GA Wave 1"
```

## Argo tracking declaration

Explicit workload targets (`ops apps bind-target`, and the scale path that
depends on them) refuse ownership with `argo_tracking_unverified` until
`argocd/argocd-cm` declares `application.resourceTrackingMethod`. An absent key
is not evidence of the installation's default, so Enclii does not infer it.
Declared `annotation`, `annotation+label` and `label` tracking are each accepted
by the target verifier ([explicit targets](../../runbooks/ENCLII_EXPLICIT_DEPLOYMENT_TARGETS.md)).

`ops apps tracking` requires the server-resolved platform-admin rank. Its plan
reports the ConfigMap UID/resourceVersion, the current key, whether an
`installationID` is declared and the running application controller release.
It declares only `annotation`, only when the key is absent and only while every
application controller runs the same Argo CD 3.x release, whose documented
default is annotation tracking. The declaration restates current behaviour;
it does not migrate tracking. A present `label`, `annotation+label` or other
value is refused as a migration this command does not perform.

```bash
enclii ops apps tracking --json
enclii ops apps tracking --expect-uid <reviewed-uid> \
  --expect-resource-version <reviewed-version> \
  --apply --reason "declare the reviewed annotation tracking default" --json
```

The apply writes one data key with the reviewed resourceVersion as an
optimistic-concurrency precondition; labels, annotations and other data are
unchanged. A concurrent change returns `refused` with nothing written. Any
other write error returns `unknown`: run the plan again before another attempt.
An exact repeat after success returns `already-declared` without writing.
The `infra/argocd/resource-customizations.yaml` bootstrap patch predates this
command and does not carry the key; do not re-apply it as a substitute.

## Held Janua staging intent

`ops apps staging-intent` requires an existing actual platform-operator login and
the fixed `--project janua`. After the reviewed API/CLI and migration `045` pass
their native qualification, plan with a caller-chosen canonical operation UUID:

```bash
enclii ops apps staging-intent --project janua --operation-id <operation-uuid> --json
```

Review the exact fingerprint and all unresolved gates. To retain only that
immutable metadata intent, use the same UUID and reviewed fingerprint:

```bash
enclii ops apps staging-intent --project janua --operation-id <operation-uuid> \
  --record --expect-plan <reviewed-sha256> --reason "retain reviewed held intent" --json
```

This command uses `--record`, not the generic `--apply` switch. The default plan
and the `read` subcommand never write. After an uncertain POST outcome, inspect
the same UUID instead of automatically repeating the write:

```bash
enclii ops apps staging-intent read <operation-uuid> --project janua --json
```

The receipt always retains `REPOSITORY_OWNERSHIP_ONLY`, activation held and
`SOURCE_STAGE_REGISTRATION_NOT_PROVEN`. It creates no service, build, workload,
source registration, native binding, secret, DNS or authority grant. Private
actor/reason and upstream response bodies are not printed. See the
[held preregistration contract](../../runbooks/JANUA_STAGING_PREREGISTRATION_DESIGN.md)
for immutable replay, custody and remaining dependency gates.

## Service-job suspension

`ops jobs suspend` requires the server-resolved platform-admin rank, an exact
namespace, a project slug and a service UUID. It accepts only CronJobs carrying
matching Switchyard ownership labels. It does not suspend Timetable records.

First review a read-only plan:

```bash
enclii ops jobs suspend <cronjob> --namespace <namespace> \
  --project <project-slug> --service <service-uuid> --json
```

After reviewing its binding, active-job count, UID and resourceVersion, apply:

```bash
enclii ops jobs suspend <cronjob> --namespace <namespace> \
  --project <project-slug> --service <service-uuid> \
  --expect-uid <reviewed-uid> --expect-resource-version <reviewed-version> \
  --apply --reason "reviewed operational containment" --json
```

Migration **042_service_job_holds** must be applied before the new API and service
reconcilers start. Missing hold storage fails closed. The hold records its service,
project, environment, actor, reason and reviewed Kubernetes identity in PostgreSQL.
Transaction-scoped locks serialize hold writes, reconciliation and manual triggers;
they also support transaction-pooled database connections. The hold commits before
Kubernetes is updated. A failed Kubernetes step returns `hold_pending`: inspect the
CronJob and obtain a fresh plan before retrying. A confirmed exact retry does not
write another hold.

Reconciliation enforces the hold after workload recreation and never transfers it
to another environment or service. Manual triggers are refused while held. Existing
Jobs continue: suspension prevents future scheduling and is not proof that all
writers have stopped. There is deliberately no resume command or automatic expiry.
Removing a hold requires a separate reviewed implementation; migration rollback
refuses to drop a nonempty hold table.

All Switchyard replicas must run the hold-aware reconciler before an operator
applies the first hold; an old replica does not participate in this protocol.
Do not roll back to an unaware build while holds exist. GitOps-managed workloads
are outside this adapter's scope; change their desired manifests instead.

## Pod Log Controls

`enclii ops pods logs` is the default production log-inspection path. It reads
through Switchyard's audited Kubernetes adapter and supports bounded retrieval
without direct `kubectl` access:

| Flag | Description |
|------|-------------|
| `--tail` | Recent lines to request; default `400`; use `0` for all lines within `--limit-bytes` |
| `--limit-bytes` | Maximum bytes to return; default `262144`; server-capped at 2 MiB |
| `--container` | Optional container name for multi-container pods |

## `ops pods restart`

`enclii ops pods restart <kind>/<name> -n <namespace>` replaces two break-glass
commands:

| Target | Effect | Replaces |
|--------|--------|----------|
| `deployment/<name>`, `statefulset/<name>`, `daemonset/<name>` (or `deploy`, `sts`, `ds`) | Rollout restart: one strategic-merge patch of the pod-template annotation `kubectl.kubernetes.io/restartedAt`, the same body `kubectl rollout restart` sends. Nothing else in the object changes. | `kubectl rollout restart` |
| `pod/<name>` | Deletes that one pod so its ReplicaSet, StatefulSet or DaemonSet recreates it. The delete carries a UID precondition and uses the pod's own grace period. It is a delete, not an eviction, so PodDisruptionBudgets are not consulted. | `kubectl delete pod` |

Use `pod/<name>` for a StatefulSet pod that crash-loops on an old revision.
Under the default `OrderedReady` policy the controller never replaces it, so a
merged manifest fix does not take effect, and a rollout restart stalls behind
it. The StatefulSet dry run lists the pods it selects, marks such a pod
`stuck` (not Ready, revision differs from the update revision) and prints the
`pod/<name>` command for it.

Guards, all enforced by the API:

- **Platform rank.** The route requires the server-resolved `platform_admin`
  rank (ADR-003), for the dry run too. `ENCLII_TENANT_SCOPE_ENFORCE` does not
  relax it, so a tenant administrator cannot restart any tenant's workloads.
- **Namespace allowlist.** The namespace must be listed in the API's
  `ENCLII_OPS_RESTART_NAMESPACES` (comma-separated exact names, set on the
  `switchyard-api` Deployment in `infra/k8s/base/switchyard-api.yaml`). An
  empty list refuses every namespace. `kube-*` namespaces are refused even
  when listed. A refused namespace is answered with HTTP 409 `refused` before
  anything is read from the cluster. Add a namespace by PR. The one
  exception is a CloudNativePG instance pod in an Enclii project namespace
  ([below](#cnpg-instance-pods-in-project-namespaces)).
- **Controller-owned pods only.** `pod/<name>` refuses a pod without a
  ReplicaSet, StatefulSet or DaemonSet controller (bare pods, Job pods,
  operator-managed pods), a pod already terminating, and a paused Deployment.
- **Reviewed object.** The dry run prints the object's UID. Pass it back with
  `--expect-uid`; the apply is refused (HTTP 409) when the object was replaced
  since. Without `--expect-uid` a pod delete still binds to the UID it reads
  just before deleting. **CLI version:** the flag arrived with #754
  (2026-10-14), after the last tagged release `v1.0.0-alpha.14` (2026-10-01);
  a CLI built before #754 rejects it as an unknown flag. Check with
  `enclii ops pods restart --help`; with an older CLI, build from `main` or run
  the apply without the flag.
- **Audit.** `--apply` requires `--reason`. Before the mutation the API writes
  an `ops.pods.restart` audit row with outcome `requested`: actor, time,
  namespace, kind, name, UID, reason and the patch. If that row cannot be
  written nothing is changed (HTTP 503). A second row records `success` or
  `failure` and points at the first; the response's `audit_id` is the first.

```bash
# Rollout restart, e.g. so a Deployment reads a changed Secret
enclii ops pods restart deployment/<name> -n <namespace>
enclii ops pods restart deployment/<name> -n <namespace> --expect-uid <reviewed-uid> \
  --apply --reason "load the rotated environment Secret"

# Recreate a crash-looping StatefulSet pod that OrderedReady will not replace
enclii ops pods restart statefulset/loki -n logging   # lists pods; loki-0 shows stuck
enclii ops pods restart pod/loki-0 -n logging --expect-uid <reviewed-uid> \
  --apply --reason "recreate loki-0 on the fixed revision"
```

A successful apply returns HTTP 202 `submitted`: the patch or delete was
accepted, not that the new pods are Ready. Follow it with
`enclii ops pods diagnose`. An HTTP 502 naming RBAC means the
`switchyard-api` ClusterRole rules for this command (`infra/k8s/base/rbac.yaml`)
are not live yet. For a registered Enclii service the service restart API
(`POST /v1/services/{id}/restart`) remains available and is target-bound to
the service's verified Deployment.

### CNPG instance pods in project namespaces

Project namespaces (`project-<first 8 of the project id>`) are created per
project and are never in `ENCLII_OPS_RESTART_NAMESPACES`. In them,
`pod/<name>` may still recreate **one CloudNativePG instance pod** of an
Enclii managed-Postgres addon. It replaces the break-glass `kubectl delete pod`
used on 2026-10-24, when an instance kept a broken volume mount after a node
fault (`global/pg_filenode.map: Input/output error`) and only a new pod
mounted the volume again. Everything else in a project namespace stays
refused: workload rollouts (refused before anything is read), application
pods, poolers and Job pods.

The API checks, in order, and refuses (HTTP 409, nothing changed) unless:

- the namespace is named `project-<8>`, carries `managed-by=enclii` and an
  `enclii.dev/project` UUID whose first 8 characters are that suffix;
- the pod's controller is a `postgresql.cnpg.io` `Cluster`, and its labels say
  the same thing: `cnpg.io/cluster` = that Cluster, `cnpg.io/podRole=instance`
  and `cnpg.io/instanceName` = the pod;
- that Cluster is the pod's live owner (same UID), is not being deleted, is an
  Enclii addon of the same project (`managed-by=enclii`,
  `enclii.dev/project-id`), and is not hibernating (`cnpg.io/hibernation=on`;
  use [`ops.addons.hibernate`](#opsaddonshibernate-api-only) for those);
- the pod is not already terminating.

The dry run reports mode `cnpg-instance-recreate`, the Cluster's phase,
`instances`, `readyInstances`, the pod's `instanceRole` and `pvcsKept` (the
PVCs CNPG mounts in the replacement; the delete never touches them). It warns
when the pod is still Ready (a database that answers would be interrupted),
when the Cluster runs one instance (the database is down until the new pod is
Ready) and when the pod is the primary of a multi-instance cluster (CNPG fails
over). **The apply requires `--expect-uid`** from the dry run, as well as
`--reason`; the delete carries that UID as a precondition and uses the pod's
own grace period. The audit rows add the Cluster UID, phase, instance role and
project.

```bash
enclii ops pods restart pod/<cluster>-1 -n project-<id>
enclii ops pods restart pod/<cluster>-1 -n project-<id> --expect-uid <reviewed-uid> \
  --apply --reason "remount the data volume after the node fault"
```

Do not use it for «Instance Status Extraction Error»: there the operator cannot
reach the instance's status port, and CNPG recreates the pod into the same
state. That case is the `<cluster>-operator-ingress` NetworkPolicy (see
`ops.addons.hibernate` below). The alerts that point here are in
[`ADDON_CNPG_HEALTH.md`](../../runbooks/ADDON_CNPG_HEALTH.md).

## `ops.addons.hibernate` (API only)

Stops a retired managed-Postgres addon's compute and keeps its data. It replaces
the break-glass `kubectl annotate clusters.postgresql.cnpg.io … cnpg.io/hibernation=on`.
There is no CLI verb yet; call the API.

```
POST /v1/ops/addons/hibernate
{"dry_run":true,"scope":{"project":"<slug>"},"args":{"target":"<addon uuid>"}}
→ status ready_to_apply, data.plan_hash
  (or succeeded: CNPG already confirms it; or 409 blocked: see data.blocked)

{"dry_run":false,"reason":"…","idempotency_key":"…","scope":{"project":"<slug>"},
 "args":{"target":"<addon uuid>","expected_plan":"<plan_hash>"}}
```

- **Who:** platform rank only.
- **Refused** unless the addon is managed Postgres, `ready`, belongs to the
  project, and every binding is `deleted`. An addon a service still reads is
  never stopped.
- **Blocked** (HTTP 409, `status: blocked`, nothing changed) while the Cluster's
  phase is not `Cluster in healthy state` and CNPG has not hibernated it. The
  common cause is «Instance Status Extraction Error»: the operator cannot reach
  the instance's status port 8000, so it would never act on the annotation
  (deleting the pod does not help; CNPG recreates it). The
  `<cluster>-operator-ingress` NetworkPolicy fixes that; the addon reconciler
  converges it for every live cluster on start and every 10 minutes.
- **Apply:** annotates the Cluster (an Update guarded by the reviewed
  resourceVersion), then waits up to 75 s for CNPG's `cnpg.io/hibernation`
  condition to be True with no instance pod left, and only then scales
  `<cluster>-exporter` to 0. `succeeded` means CNPG confirmed. If it has not
  confirmed in time the answer is HTTP 202 `submitted` with the annotation on
  and the exporter still running; take a new dry-run and apply again to finish.
  A live state that no longer hashes to `expected_plan` is a 409.
- **Plan fields:** `phase`, `blocked`, `already_hibernated` (annotation on) and
  `hibernated` (CNPG's condition True); the result adds
  `hibernation_confirmed`.
- **Audit:** a row before any mutation (no row, no mutation) and a result row.
- **Never deletes** a Cluster, PVC, Secret or pod, and never changes the
  addon's status.
- **Reverse:** remove the annotation; CNPG recreates the instance from its PVC.

## `ops addons backup-credential`

Provisions the object-store credential every managed-Postgres addon backs up
with, so no human ever holds it (`POST /v1/ops/addons/backup-credential`,
platform rank). Switchyard ensures the bucket named by
`ENCLII_ADDON_BACKUP_DESTINATION_BASE`, mints a Cloudflare token scoped to that
bucket only ("Workers R2 Storage Bucket Item Write"), merges the S3 pair into
the Vault path and keys of the intake target `enclii/addon-db-backups`, and
force-syncs the `enclii-db-backup-credentials` ExternalSecret. The value is
never returned or logged.

```bash
enclii ops addons backup-credential                       # dry run: plan + plan_hash
enclii ops addons backup-credential --apply --expect-plan <plan_hash> \
  --idempotency-key <key> --reason "..."
enclii ops addons backup-credential --rotate              # plan a rotation
```

- A plan of `keep` (Vault already holds both keys) writes nothing on apply; a
  new token is minted only with `--rotate` on both the dry run and the apply.
- Apply requires `--expect-plan`, `--idempotency-key` and `--reason`; it writes
  an audit row before any mutation and a result row after.
- A Cloudflare refusal is `blocked` (HTTP 424) and names the permission the
  switchyard-api token lacks; nothing is written to Vault.
- A failed Vault write revokes the newly minted token.

Runbook: [ADDON_BACKUPS.md](../../runbooks/ADDON_BACKUPS.md#provisioning-the-credential-one-time-no-human-holds-it).

## Required Mutation Flags

| Flag | Description |
|------|-------------|
| `--apply` | Execute instead of returning a dry-run plan |
| `--reason` | Audit reason; required with `--apply` |
| `--idempotency-key` | Optional retry key for safely repeating an operation |
| `--namespace`, `--project`, `--service` | Scope selectors passed to the operation contract |

## `ops domains reconcile`

Provisions every hostname a service declares in its `enclii.yaml`: the proxied
DNS record, the junction, the tunnel ingress rule, and TLS. Idempotent — an
already-provisioned hostname is re-asserted, never duplicated.

Every credential is held server-side. You supply a service name and a reason;
you never supply a Cloudflare token, a zone id, or a tunnel id, and none is
returned.

```bash
# Plan: what is declared but not provisioned
enclii ops domains reconcile nauta-web

# Provision everything the manifest declares
enclii ops domains reconcile nauta-web \
  --apply --reason "<client>-erp.madfam.io declared but never provisioned"

# Provision ONE hostname, leaving every other declared hostname untouched
enclii ops domains reconcile nauta-web --domain <client>-erp.madfam.io \
  --apply --reason "route the declared ERP host"

# Read enclii.yaml at a specific commit instead of the default branch head
enclii ops domains reconcile nauta-web --ref 1a2b3c4 --apply --reason "..."
```

Besides the shared contract flags (`--apply`, `--reason`, `--idempotency-key`, `--json`, `--namespace`, `--project`, `--service`), it accepts `--domain` (reconcile only this hostname) and `--ref` (git ref to read `enclii.yaml` from; default: the repository's default branch head).

**Service names are unique per project, not platform-wide** (#793,
2026-10-18). A bare service name registered in more than one project is
refused as ambiguous; pass `--project <slug>` (or `scope.project`) to choose.

`--domain` is the safe form when only one hostname needs attention. Unlike
`providers cloudflare tunnels-apply --project X`, which reconciles every
junction in the project and can rewrite unrelated backends, a scoped reconcile
touches exactly the hostname named. A hostname the manifest does not declare
plans nothing rather than falling back to all of them.

`enclii domains reconcile` is an alias for the same operation.

## `ops junctions rebind`

Binds a hostname's junction to the service **and environment** that serve it.
The junction is what `providers cloudflare tunnels-apply` plans from, so a
junction bound to the wrong service or environment makes every later
`tunnels-apply` propose a repoint (which its guard refuses).

Data only: the binding is rewritten and the tunnel route is not touched. Dry
run by default, idempotent (a junction already bound as asked is reported and
left alone), and it refuses unknown hostnames, services and environments,
listing the ones that exist.

```bash
# Preview (dry run): the row shows the backend tunnels-apply will want after
# the rebind, and whether the live route already serves it (live_matches)
enclii ops junctions rebind api.example.com --project example \
  --to-service example-api --environment production

# Afterwards, the route plan for that hostname (dry run) should be SKIP
enclii providers cloudflare tunnels-apply api.example.com --project example
```

Flags: `--to-service` and `--environment` (required), `--path` (only the
junction on that path; default every junction for the hostname), plus the
shared contract flags. API: `POST /v1/ops/junctions/rebind` with
`scope.project`, `args.target`, `args.to_service`, `args.environment`.
Available in CLI `v1.0.0-alpha.14` and later. Where a rebind fits in a route
change (dry run, review, rebind, then a reviewed `tunnels-apply` bound with
`--expect-plan`) is the
[tunnels-apply operator procedure](providers.md#cloudflare-tunnels-apply-operator-procedure).

## `ops secrets provision-kalya-feed`

Mints a kalya standing-feed token and files it into its consumers' Vault paths,
entirely server-side.

```bash
enclii ops secrets provision-kalya-feed --tenant <slug> --consumers <client-portal>,nauta
enclii ops secrets provision-kalya-feed --tenant <slug> --consumers <client-portal>,nauta \
  --apply --reason "wire the tenant's standing feed"
```

Switchyard reads kalya's internal API key from `secret/kalya`, asks kalya to
mint the token, and writes (consumer ids and Vault paths are registered in
`kalyaFeedConsumers`, `apps/switchyard-api/internal/api/kalya_feed_provisioner.go`):

| Consumer | Vault path | Properties |
|----------|-----------|------------|
| `<client-portal>` | `secret/<client-portal>` | `kalya_occupancy_feed_url`, `kalya_capacity_feed_url` |
| `nauta` | `secret/nauta` | `kalya_feed_tokens` (merged, so other tenants survive) |

The token is never returned by the API, never logged, and never reaches the
machine running the CLI. Idempotent: consumers already carrying this tenant's
properties are skipped and nothing is minted. `--rotate` is the explicit opt-in
to replace a live token.

| Flag | Description |
|------|-------------|
| `--tenant` | kalya tenant slug; required |
| `--consumers` | Consumers to provision: a registered client portal, `nauta` (comma-separated) |
| `--rotate` | Mint a replacement token even when the consumers are already provisioned |
| `--kalya-origin` | kalya origin (default: kalya's verified service domain, else `https://kalya.app`) |

It also takes the shared contract flags listed under [Required Mutation Flags](#required-mutation-flags).

`enclii secrets provision kalya-feed` is an alias for the same operation.

## `ops secrets sync` and `refresh`

`enclii ops secrets sync <externalsecret> -n <namespace>` (also `enclii secrets
sync`) and `enclii ops secrets refresh` run the same Switchyard adapter. With
`--apply --reason "..."` it merge-patches the ExternalSecret's annotations —
`force-sync=<unix time>` plus the `enclii.dev/last-ops-*` and
`enclii.dev/refresh-requested-at` audit keys — so External Secrets re-reads
Vault on its next reconcile.

It never reads, prints or writes a secret value, and it does not change the
ExternalSecret `spec`. In particular it does **not** apply a merged change to a
manifest under `infra/k8s/base/external-secrets/vault-secrets/`: those files
are not synced by ArgoCD, so a new key needs a live patch first. See
[Procedure: changing a git-only ExternalSecret](../../infrastructure/EXTERNAL_SECRETS.md#procedure-changing-a-git-only-externalsecret-until-539).
Pods that read the Secret through env vars keep their old values until they
restart.

## Remaining Adapter Work

- `apps rollback`, `storage repair-plan`, `policy waiver-plan`, and
  `runners drain` are contract-only until guarded apply adapters are wired.
  (`secrets refresh` and its alias `secrets sync` are wired — see
  [below](#ops-secrets-sync-and-refresh); `pods restart` is wired — see
  [above](#ops-pods-restart).)
- An API build older than the `pods restart` adapter answers the dry run with
  a generic `planned` response (`adapter execution is not wired in this build`)
  and `--apply` with HTTP 501 `adapter_required`. A wired build answers with
  the live object, its UID and the patch.
- `apps retire` deletes only the Argo Application by default and uses orphan
  propagation so live resources are not pruned during routine legacy-app
  retirement. Destructive cascade retirement must be explicitly requested by
  API callers.
- `jobs trigger` creates a Kubernetes Job from the existing CronJob
  `jobTemplate`; it does not edit the CronJob schedule, image, command, env, or
  secret references.
- `apps diff` now reads Argo Application sync/resource/condition drift; full
  desired-vs-live patch hunks remain a follow-up.
- Pod logs currently require a pod target; workload/deployment selector
  resolution is a follow-up, but tail depth and bounded byte retrieval are
  wired.
- Missing CRDs currently return failed read responses rather than soft-empty
  inventories.
