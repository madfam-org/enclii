# Managed Postgres Addon Health Alerts

> **Boundary checkpoint (2026-10-26, platform on-call):** Public-safe runbook.
> No secrets or node identity; namespace and cluster names below are
> placeholders. Policy: `docs/PUBLIC_REPO_BOUNDARY.md`.

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: the remedies here go through Enclii
> (`enclii ops …` and the `/v1/ops` API). Raw `kubectl` is bootstrap or
> break-glass only; record any step that still needs it in
> [`ADAPTER_GAPS.md`](../ADAPTER_GAPS.md).

**Scope:** the CloudNativePG `Cluster`s Enclii provisions for managed-Postgres
addons in `project-*` namespaces. The platform `postgres-ha` cluster has its
own rules (`infra/k8s/production/postgres-ha/podmonitor.yaml`).

## Why these alerts exist

Until 2026-10-26 the only CNPG health rules watched `postgres-ha`. On
2026-10-24 two project-database incidents paged nobody:

1. Every cluster in one project sat in «Instance Status Extraction Error»: the
   operator could not reach the instances' status port (TCP 8000) through the
   namespace's default-deny policy. PostgreSQL answered the whole time, so the
   instance metrics (`cnpg_collector_up`) read healthy.
2. After a node went NotReady and its storage replicas were rebuilt, one
   instance kept a broken mount (`global/pg_filenode.map: Input/output
   error`). It was fixed by a break-glass pod delete.

The phase in case 1 exists only on the `Cluster` object, so kube-state-metrics
publishes it (`infra/k8s/production/monitoring/kube-state-metrics.yaml`,
`--custom-resource-state-config`) as:

| Series | Labels | Value |
|--------|--------|-------|
| `kube_cnpg_cluster_info` | `namespace`, `cluster`, `phase`, `hibernation` (annotation), `hibernated` (CNPG's `cnpg.io/hibernation` condition) | 1 |
| `kube_cnpg_cluster_spec_instances` | `namespace`, `cluster` | `spec.instances` |
| `kube_cnpg_cluster_status_ready_instances` | `namespace`, `cluster` | `status.readyInstances`, 0 when unset |

kube-state-metrics lists every Cluster, including addons whose instance pods
have no PodMonitor.

## The alerts

Rules: `infra/k8s/production/monitoring/addon-db-rules.yaml`, group
`enclii-addon-db.cnpg-health`, evaluated by the `rules-eval` Prometheus.
Critical alerts route to `critical-receiver` (Courier and email). Courier's
identity for them is the namespace, so two clusters failing in one project in
the same episode page once, with both in the payload. Tests:
`tests/scripts/test_cnpg_project_rules.py`.

| Alert | Severity | Fires when | First move |
|-------|----------|-----------|------------|
| `CNPGInstanceStatusUnreachable` | critical | phase not «Cluster in healthy state» for 10 min, cluster not hibernated | [Phase not healthy](#phase-not-healthy) |
| `CNPGReadyInstancesBelowSpec` | critical | `readyInstances` below `spec.instances` for 10 min, not hibernated | [Instance not ready](#instance-not-ready) |
| `CNPGInstancePostgresDown` | critical | the instance's own `cnpg_collector_up` is 0 for 5 min | [Instance not ready](#instance-not-ready) |
| `CNPGClusterStateMetricsAbsent` | warning | no `kube_cnpg_cluster_info` at all for 30 min | [Blind](#blind) |

**Hibernated clusters** run 0 instances on purpose. They are excluded only once
CNPG confirms the hibernation (condition `cnpg.io/hibernation` True). A
cluster annotated for hibernation that CNPG never acted on still pages; that
was case 1.

**`CNPGInstanceStatusUnreachable` is a contract name.** Selva's infra
self-healing intake (selva-office RFC 0047) keys on it with the `namespace`
and `cluster` labels. Do not rename it or drop those labels.

One outage can raise more than one of these at once (an instance that is down
is usually also not ready). Each names a different fact; treat them as one
incident.

## Phase not healthy

1. Read the phase: it is in the alert's `phase` annotation and summary.
2. «Instance Status Extraction Error»: the operator cannot reach port 8000.
   Read the namespace's policies (`POST /v1/ops/inventory/network-policies`)
   and look for `<cluster>-operator-ingress`. The addon reconciler converges it
   on start and every 10 minutes (enclii #854). **Do not recreate the pod**:
   CNPG recreates it into the same state.
3. Any other phase: read the instance pod with
   `enclii ops pods diagnose <cluster>-1 -n <namespace>` and the operator's view
   in its events and log.

## Instance not ready

1. `enclii ops pods diagnose <pod> -n <namespace>`: is the pod Running but not
   Ready, and does the postgres log show an `Input/output error` on a data
   file?
2. If the volume mount broke (case 2), recreate the pod through Enclii. CNPG
   brings it back on the same PVCs:

   ```bash
   enclii ops pods restart pod/<pod> -n <namespace>       # dry run: read the warnings and the UID
   enclii ops pods restart pod/<pod> -n <namespace> --expect-uid <reviewed-uid> \
     --apply --reason "<what the log showed>"
   ```

   Run the dry run and the apply as two separate steps, with the review in
   between. The guards are in
   [`ops.md`](../cli/commands/ops.md#cnpg-instance-pods-in-project-namespaces).
3. If the volume itself is faulted, it is a storage incident: start with
   `enclii ops storage longhorn` and `enclii ops storage repair-plan`.

## Blind

kube-state-metrics publishes no Cluster state, so the first two alerts
evaluate over nothing. Read the kube-state-metrics pod log for
custom-resource-state errors (`enclii ops pods logs`), and check that its
ClusterRole still grants `list`/`watch` on `postgresql.cnpg.io` clusters and on
`customresourcedefinitions`.

## Verify after a merge

A merged rule is not necessarily loaded. After Argo syncs `monitoring`:

1. kube-state-metrics rolled out (its args changed) and serves the series:
   query `count by (namespace) (kube_cnpg_cluster_info)` on `rules-eval`.
2. `rules-eval` loaded the group: `/api/v1/rules` lists
   `enclii-addon-db.cnpg-health` with its four rules `inactive` while every
   project cluster is healthy.

## Selva proposing the remedy

RFC 0047 v1 diagnoses and proposes, and a human applies. Its toolbox already
allows `pods/restart` as a remedy dry run (`enclii_remedy_dry_run`, always
sent with `dry_run: true`). Before 2026-10-26 that dry run was refused in every
project namespace; it now returns the plan for a CNPG instance pod: the
warnings, `pvcsKept` and the UID, with nothing changed. For case 2 the report
can therefore cite a `ready_to_apply` plan, and the human runs the printed
`--expect-uid … --apply --reason` command. For case 1 the dry run would also
succeed, and it is the wrong remedy (see [Phase not healthy](#phase-not-healthy)).
Selva's `cnpg-operator-ingress-blocked` signature proposes the NetworkPolicy
instead. Stateful systems stay out of pre-approved runbooks (RFC 0047 §9, v3)
unless their own RFC says otherwise.
