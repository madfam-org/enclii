# Redis Sentinel Failover Drill Log

> Last Updated: 2026-04-17
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
| TBD (first drill) | drill | redis-ha-0 | — | — | — | — | Schedule immediately after ArgoCD reports Sentinel app Synced + Healthy. |
