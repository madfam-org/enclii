# Redis Sentinel Failover Drill Log

> **Boundary checkpoint (2026-09-19, platform-infra):** public-safe. This log
> names pod ordinals, timings and Sentinel event names only — no pod IPs, node
> identities, secret values or tunnel ids. Operator detail per drill lives in
> `madfam-org/internal-devops` (`runbooks/redis-failover-log.md`). Policy:
> [`PUBLIC_REPO_BOUNDARY.md`](../PUBLIC_REPO_BOUNDARY.md).

> Last Updated: 2026-09-19
> Owner: Platform Infra

Log of every Redis Sentinel failover — both planned (chaos drills) and
unplanned (production incidents).

## Schema

Each row is one failover event.

| Field | Type | Description |
|---|---|---|
| `date` | ISO 8601 UTC | When the failover started (from `SENTINEL +switch-master` event). |
| `kind` | `drill` \| `incident` | Planned drill via `scripts/redis-failover-chaos.sh`, or real prod failure. |
| `old_master` | pod name | `redis-ha-<n>` that was master before. |
| `new_master` | pod name | `redis-ha-<n>` that was elected master. |
| `failover_s` | seconds | Wall-clock from pod delete to new master accepting writes. Target: < 20s. |
| `client_errors` | int | Count of client-side errors observed in consumer logs during the window. |
| `result` | `pass` \| `fail` | `pass` if failover_s < 20 AND no data loss; `fail` otherwise. |
| `notes` | string | Root cause / observations / actions. |

## Drill cadence

- **Monthly**: first Tuesday at 10:00 UTC, run `./scripts/redis-failover-chaos.sh` against production.
- **After any upgrade**: Kubernetes version bump, Redis image bump, Longhorn upgrade.

## Log

| date | kind | old_master | new_master | failover_s | client_errors | result | notes |
|---|---|---|---|---|---|---|---|
| 2026-09-19T06:29:11Z | drill | redis-ha-0 | redis-ha-2 | 6 | 0 | pass | Roll-induced (enclii#571 merged with the owner watching; the StatefulSet roll restarted the master): `+odown #quorum 2/2` → `+switch-master` in ≈6 s; both proxy pods `UP` on the new master at ≈7.5 s; `redis-ha-1` partial resync (no data loss); `redis-ha-0` booted as a second master for ~15 s until `+convert-to-slave`. Zero consumers, so client_errors is n/a. First automatic failover ever — the Sentinels had no quorum before #571 (`announce-ip` unrendered). Observed from logs, not via the chaos script. |
| 2026-09-19T08:06:36Z | drill | redis-ha-2 | redis-ha-0 | 64 | 0 | fail | Roll-induced (enclii#575 restarted all three pods, owner watching). Sentinel promoted `redis-ha-0` at +17 s (election took 10 s), but a stale slave entry by old IP from the first drill made it send the promoted master `REPLICAOF` itself one second later; the roll then restarted `redis-ha-1` and `redis-ha-0`; a second failover attempt aborted (no good slave); the final master came from config-init's bootstrap fallback because every new pod's Sentinel lookup was refused during the new-pod admission lag. Proxy had no backend for 64 s. Converged healthy; every pod now announced by hostname. Follow-ups: `SENTINEL RESET` on all three, init lookup retry, proxy `init-state down`. |
| 2026-09-19T17:10:58Z | drill | redis-ha-0 | redis-ha-2 | 9 | 0 | pass | Roll-induced (enclii#577 restarted all three pods, owner watching, master last). Every config-init took the Sentinel path, no bootstrap fallback: the two replicas re-joined the master at once; the restarted master waited out its grace and joined the promoted pod. `+odown` at +5 s, leader elected in 0.1 s, `+switch-master` at ≈7 s, both proxy pods `UP` on `redis-ha-2` at 8.5 s (HAProxy 3.2, `init-state down`, `connected_slaves` check); partial resync (201 B); no second master; all pods by hostname. Stale Sentinel runids remain until a `SENTINEL RESET` (persisting Sentinel state would stop that). |
| 2026-09-19T20:37:53Z | drill | redis-ha-0 | redis-ha-0 | 23 | 0 | fail | Roll-induced (enclii#582, persist-Sentinel-state, owner watching). Two failovers in one roll: killing the master `redis-ha-2` first with healthy peers re-routed the proxy in ~3 s (clean); killing the new master `redis-ha-0` LAST, while both peers were still mid-resync from their own restarts, left no good slave, so Sentinel declined to promote and waited for `redis-ha-0` to reboot and reclaim (`+reboot master`), ~23 s backendless. No data loss. A whole-StatefulSet-roll artifact, not a failover-mechanism defect; single-node failover is still ~8.5 s (row above). Follow-up: gate the redis readiness probe on `master_link_status:up` (enclii#580). Persist-state worked (every pod first-booted rendering from the template); one final `SENTINEL RESET` clears the stale runids from this roll, none after future rolls. |
