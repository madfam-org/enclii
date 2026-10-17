# switchyard-api leader election (design)

Status: **proposed**, not implemented. The tracking PR is a draft.

## Problem

switchyard-api runs at least 2 replicas (HPA `minReplicas: 2`, `maxReplicas: 5`). Every replica starts every background loop in `cmd/api/main.go`, and nothing elects a single writer. So at steady state every periodic job runs 2–5 times:

| Loop | Started in | What doubles |
|---|---|---|
| Deploy controller `workScheduler` (pending poll, 30s) | `reconciler.Controller.Start` | Both replicas enqueue every `pending` deployment each cycle at `Attempt: 1` |
| Deploy controller `k8sSyncScheduler` (60s) | `reconciler.Controller.Start` | K8s→DB health and record sync |
| Addon reconciler (30s) | `main.go` | CNPG status reads and writes |
| Data-API reconciler (30s) | `main.go` | PostgREST provisioning |
| Function reconciler (30s) | `main.go` | KEDA objects |
| Namespace discoverer (5m) | `main.go` | Orphan/zombie scans |
| Timetable reconciler (30s) | `main.go` | CronJob/Job applies |
| Pgbouncer userlist drift checker (5m) | `main.go` | `pg_authid` reads and metrics (two series, one per pod) |
| Admin reconciler (60s) | `main.go` | Cluster, fleet, Argo drift and cost provider calls |
| Domain background sync (5m) | `main.go` | Cloudflare/registrar calls |
| ArgoCD Application poller (when enabled) | `argocd_poller_wiring.go` | Application lists, record writes |

The per-service workload lock (`pg_advisory_xact_lock` via `workloadtargets`) and `RefuseBound` prevent a double apply at the Kubernetes layer. So today this costs wasted work, doubled provider API calls and duplicated metrics, not corrupted state. It grows linearly with the replica count.

Out of scope, because they are already multi-replica safe:

- the outbound webhook worker (`FOR UPDATE SKIP LOCKED` claims);
- the deploy controller's **workers** for work enqueued locally by an API request (`ScheduleReconciliation`, 3 call sites). They must keep running on every replica, or a deploy accepted by a non-leader would sit in that replica's channel.

## Design

1. **Lease.** A `coordination.k8s.io/v1` Lease named `switchyard-api-leader` in the pod's namespace (`enclii`), using client-go `tools/leaderelection` with `resourcelock.LeasesResourceLock`.
   - Identity: `POD_NAME`, from the Downward API.
   - Timings: `LeaseDuration` 30s, `RenewDeadline` 20s, `RetryPeriod` 5s.
   - `ReleaseOnCancel: true`, so a rolling update hands over within a few seconds instead of waiting for the lease to expire.
2. **New package `internal/leader`.** `Run(ctx, Config, onLead func(termCtx))` campaigns in a loop. When it wins it calls `onLead` with a term context, which client-go cancels on loss. When `onLead` returns or the lease is lost, it campaigns again. It exports `enclii_switchyard_leader{pod}` (0/1) and logs each transition.
3. **Singleton set.** Move the starts of the loops above out of `main.go` into `cmd/api/singleton_loops.go`, as `startSingletonLoops(termCtx, deps)`. This also takes `main.go` (795 lines) away from the 800-line gate. Every loop listed already exits on `ctx.Done()` and can be restarted with a new context, with two exceptions:
   - `reconciler.Controller` has a `started` guard and one `stopCh`. Split it: `StartWorkers(ctx)` runs on every replica (workers, result processor, retry drain); `StartSchedulers(termCtx)` runs on the leader only (`workScheduler`, `k8sSyncScheduler`).
   - `services.DomainSyncService.StartBackgroundSync` takes no context. Add `RunBackgroundSync(ctx, interval)`.
4. **HTTP is never gated.** The HTTP server starts unconditionally, as today. A replica that never gets, or loses, the lease serves every route. The only effect is that it does not run the singleton loops. Readiness does **not** depend on leadership.
5. **Fail-safe when there is no Kubernetes client** (local dev, `k8sClient == nil`) or `ENCLII_LEADER_ELECTION=false`: start the singleton set directly, which is today's behaviour, and log it at WARN.
6. **Pending-deploy latency.** A deploy enqueued on a non-leader is processed by that replica's own workers immediately, as today. The leader's 30s poll remains the backstop. Nothing waits on the lease.

## Manifests (same PR as the code)

- `infra/k8s/base/rbac.yaml`: a namespaced `Role` and `RoleBinding` in `enclii` for SA `switchyard-api`, granting `coordination.k8s.io` `leases` `get, update` on `resourceNames: [switchyard-api-leader]` plus `create`. Kubernetes cannot restrict `create` by name.
- `infra/k8s/base/switchyard-api.yaml`: env `POD_NAME` and `POD_NAMESPACE` from `fieldRef`.
- Regenerate the affected goldens under `tests/golden/k8s/` in the same PR.

## Rollout and operator view

- Merging rebuilds and pins switchyard-api. During the rollout, old pods run every loop and new pods elect one leader, so the doubling ends when the last old pod exits.
- Observable: `enclii_switchyard_leader` reports exactly one pod at 1. The singleton loops log only on that pod. If the leader dies, a standby takes over within about 30s (the lease duration).
- Rollback: `ENCLII_LEADER_ELECTION=false` restores the current behaviour without a code change.

## Tests

- `internal/leader`: with the client-go fake clientset, two candidates elect exactly one leader; cancelling the leader's context hands the lease to the other within `RetryPeriod`; a candidate that loses keeps running and campaigns again.
- `cmd/api`: with election disabled or no Kubernetes client, the singleton set starts directly.
- `reconciler`: `StartWorkers` without `StartSchedulers` processes locally scheduled work and never polls `pending`.

## Size

About 500–600 changed lines, including the `main.go` extraction, the controller split, RBAC, env and goldens. It is above the size where one PR is easy to review, so it lands as code in a follow-up of this draft.
