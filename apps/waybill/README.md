# Waybill - Usage Tracking & Billing Service

Waybill is Enclii's usage tracking and billing service that handles metering, cost calculation, and Stripe integration for customer billing.

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Switchyard    │────▶│  Waybill API    │────▶│   PostgreSQL    │
│   (deploys)     │     │   (Go/Gin)      │     │  usage_events   │
└─────────────────┘     └─────────────────┘     └────────┬────────┘
                                                         │
┌─────────────────┐                                      │
│   Roundhouse    │────▶ POST /internal/events           │
│   (builds)      │                                      │
└─────────────────┘                                      │
                                                         ▼
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Dashboard     │◀────│  Aggregator     │◀────│  hourly_usage   │
│   (UI)          │     │  (cron job)     │     │  billing_records│
└─────────────────┘     └─────────────────┘     └─────────────────┘
                               │
                               ▼
                        ┌─────────────────┐
                        │     Stripe      │
                        │   (payments)    │
                        └─────────────────┘
```

## Components

### API Server (`cmd/api`)
- Records usage events from platform services
- Provides usage/billing APIs for dashboard
- Handles Stripe webhooks

### Aggregator (`cmd/aggregator`)
- Runs hourly aggregation of raw events
- Calculates daily/monthly usage summaries
- Generates billing records
- Captures each finished day's per-workload CPU and memory usage and requests
  from Prometheus into the [resource usage ledger](#resource-usage-ledger)

## Quick Start

```bash
# Start with Docker Compose
docker-compose up -d

# Or run locally
export DATABASE_URL=postgresql://localhost/enclii_dev

# Start API
go run ./cmd/api

# Start Aggregator (separate terminal)
go run ./cmd/aggregator
```

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `API_PORT` | API server port | `8082` |
| `DATABASE_URL` | PostgreSQL connection URL | required |
| `INTERNAL_API_KEY` | API key for internal services | - |
| `STRIPE_SECRET_KEY` | Stripe secret key | - |
| `STRIPE_WEBHOOK_SECRET` | Stripe webhook secret | - |
| `PRICE_COMPUTE_GB_HOUR` | Compute cost per GB-hour | `0.000463` |
| `PRICE_BUILD_MINUTE` | Build cost per minute | `0.01` |
| `PRICE_STORAGE_GB_MONTH` | Storage cost per GB-month | `0.25` |
| `PRICE_BANDWIDTH_GB` | Bandwidth cost per GB | `0.10` |
| `WAYBILL_METERING_STALE_AFTER` | API: how old the aggregator heartbeat may be before cost reads report `metering.status: stale`. Keep it above the aggregator's `WAYBILL_ALERT_INTERVAL` | `1h` |
| `WAYBILL_RESOURCE_PROMETHEUS_URL` | Aggregator: Prometheus HTTP API base for the [resource usage ledger](#resource-usage-ledger). Unset = capture off | unset (production: `http://prometheus.monitoring.svc.cluster.local:9090`) |
| `WAYBILL_RESOURCE_CLUSTER` | Aggregator: cluster name written on every row (Prometheus' external labels are not applied to query results) | `enclii-production` |
| `WAYBILL_RESOURCE_TIMEZONE` | Aggregator and API: the civil-day zone of `date` | `America/Mexico_City` |
| `WAYBILL_RESOURCE_LOOKBACK_DAYS` | Aggregator: complete days, ending yesterday, each run checks for gaps. Match Prometheus retention | `9` |
| `WAYBILL_RESOURCE_SETTLE` | Aggregator: how long after local midnight a day counts as complete | `15m` |
| `WAYBILL_RESOURCE_QUERY_TIMEOUT` | Aggregator: timeout of one Prometheus call | `2m` |
| `WAYBILL_RESOURCE_CHUNK_HOURS` | Aggregator: range queries read a day in windows of this many hours | `6` |
| `WAYBILL_RESOURCE_RUNNER_NAMESPACES` | Aggregator: comma-separated CI runner namespaces, aggregated per namespace instead of per workload (`-` = none) | `arc-runners` |
| `WAYBILL_RESOURCE_CADVISOR_JOB` / `WAYBILL_RESOURCE_KSM_JOB` | Aggregator: the scrape jobs the PromQL pins, so a second scrape of an exporter cannot double a row | `kubernetes-cadvisor` / `kube-state-metrics` |

An invalid `WAYBILL_RESOURCE_*` value stops the process at startup; it never
falls back to a default.

## API Endpoints

### Internal (Switchyard/Roundhouse)
```
POST /internal/events         # Record single event
POST /internal/events/batch   # Record batch of events
```

### Public API
```
# Usage
GET  /api/v1/projects/:id/usage/current   # Current period usage
GET  /api/v1/projects/:id/usage/history   # Historical usage
POST /api/v1/estimate                     # Cost estimate

# Billing
GET  /api/v1/projects/:id/invoices        # List invoices

# Plans
GET  /api/v1/plans                        # Available plans

# Resource usage ledger (usage and requests per civil day, no money)
GET  /api/v1/resource-usage/daily?from=&to=&namespace=&cluster=
GET  /api/v1/resource-usage/nodes/daily?from=&to=&cluster=
```

### Health
```
GET /health   # Health check
GET /ready    # Readiness check
```

## Event Types

### Deployment Events
```json
{
  "event_type": "deployment.started",
  "project_id": "uuid",
  "resource_type": "deployment",
  "resource_id": "uuid",
  "metrics": {
    "replicas": 2,
    "cpu_millicores": 500,
    "memory_mb": 512
  }
}
```

### Build Events
```json
{
  "event_type": "build.completed",
  "project_id": "uuid",
  "resource_type": "release",
  "resource_id": "uuid",
  "metrics": {
    "duration_seconds": 45.2,
    "image_size_mb": 250
  }
}
```

### Storage Events
```json
{
  "event_type": "volume.created",
  "project_id": "uuid",
  "resource_type": "volume",
  "resource_id": "uuid",
  "metrics": {
    "size_gb": 10
  }
}
```

## Pricing Model

### Plans

| Plan | Monthly | Includes |
|------|---------|----------|
| Hobby | $5 | 500 GB-hrs, 500 build min, 1 GB storage, 100 GB bandwidth |
| Pro | $20 | 2000 GB-hrs, 2000 build min, 10 GB storage, 500 GB bandwidth |
| Team | $50 | 5000 GB-hrs, 5000 build min, 50 GB storage, 1 TB bandwidth |

### Overage Rates
- Compute: $0.000463/GB-hour
- Build: $0.01/minute
- Storage: $0.25/GB-month (varies by plan)
- Bandwidth: $0.10/GB (varies by plan)

### GB-Hour Calculation
```
GB-equivalent = max(memory_GB, cpu_cores) × replicas
GB-hours = GB-equivalent × hours_running
```

## Database Schema

### Main Tables
- `usage_events` - Raw events (append-only)
- `hourly_usage` - Hourly aggregated metrics
- `daily_usage` - Daily aggregated metrics
- `pricing_plans` - Available subscription plans
- `subscriptions` - Project subscriptions
- `billing_records` - Monthly invoices
- `credits` - Promotional credits

- `resource_daily`, `node_daily`, `resource_daily_captures` - Daily resource
  usage ledger ([below](#resource-usage-ledger))

All of them are created by switchyard-api's migrations
(`apps/switchyard-api/internal/db/migrations/`); Waybill shares that database
and owns no migrations.

### Views
- `project_usage_summary` - Current period usage per project

## Metering status (read proof)

Spend is a sum over `hourly_usage`, which only the aggregator writes. An empty
table therefore reads as `$0.00` whether a project spent nothing or nothing was
metered. `GET /api/v1/projects/:id/cost` says which, in `spend_known` and a
`metering` block:

| `metering.status` | Meaning | Signal |
|---|---|---|
| `ok` | The total is a measurement (a zero is a real zero) | Aggregator heartbeat within `WAYBILL_METERING_STALE_AFTER`, and no overdue metered event without its `hourly_usage` hour |
| `not_running` | The aggregator has never run against this database | No `budget_evaluator_runs` row (the evaluator runs inside the aggregator process) |
| `stale` | The aggregator stopped reporting, or usage events were never rolled up; a non-zero total is a floor | Heartbeat older than the window, or `unaggregated_events > 0` |
| `unknown` | The status read failed | Query error (logged) |

The budget evaluator applies the same check before judging a budget: a budget
whose spend is not `ok` is skipped and counted in the run row's `errors`, with
`notes = metering_unverified=N`. Missing data is never evaluated as "under
budget". `enclii billing show` prints such spend as unknown instead of `$0.00`.

## Aggregation

The aggregator runs on a cron schedule:
- **Hourly** (5 min past): Aggregate raw events → hourly_usage
- **Daily** (00:20 UTC): Roll up yesterday's hourly → daily_usage (SUM per
  project and metric; a rerun replaces the day)
- **Hourly** (35 min past, and at startup): capture any missing complete CDMX
  day into the [resource usage ledger](#resource-usage-ledger)
- **Monthly** (1st of month): not implemented; nothing writes billing_records

Deployed as `waybill-aggregator` (one replica, `infra/k8s/base/waybill-aggregator.yaml`,
image built from this Dockerfile's `aggregator` stage). Never run two: the
hourly upsert is idempotent, but two budget evaluators would each judge and
alert.

## Resource usage ledger

Per-workload compute history for RFC 0049 Phase 2 (capacity plane) and,
later, RFC 0050 digital COGS. Prometheus keeps 7 to 9 days; this ledger keeps
every day after that. Code: `internal/resourceusage`. Tables: switchyard-api
migration `047_resource_daily`.

**What is captured.** For each civil day in `America/Mexico_City` (Mexico has
had no DST since October 2022, so a day is 06:00Z to 06:00Z), at 5-minute
resolution, read over Prometheus range queries in 6-hour windows:

| Table | Grain | Values |
|---|---|---|
| `resource_daily` | date, cluster, node, namespace, workload kind + name, container | `pod_count`; CPU core-hours used, requested and limit; highest per-pod p95 and max of the 5-minute CPU rate; memory GiB-hours used (working set), requested and limit; highest per-pod p95 and max working set in bytes; `sample_count`, `coverage`, `source`, `captured_at` |
| `node_daily` | date, cluster, node | allocatable, requested and used CPU core-hours and memory GiB-hours, CPU p95 and max, memory max; `pod_count`, `sample_count`, `coverage` |
| `resource_daily_captures` | date, cluster | the day was captured and committed; `usage_coverage` (cadvisor) and `requests_coverage` (kube-state-metrics), row counts |

Sources: `container_cpu_usage_seconds_total` (5-minute `rate`) and
`container_memory_working_set_bytes` from the `kubernetes-cadvisor` job
(container series only; the node row reads the root cgroup `id="/"`, so it
includes system and k3s); `kube_pod_container_resource_requests` / `_limits`
and `kube_node_status_allocatable` from `kube-state-metrics`. Requests count
only while the pod is Pending (bound) or Running: kube-state-metrics keeps
reporting a finished Job pod's requests until the pod is deleted, which would
bill a CronJob's history as requested. Owners resolve through `kube_pod_owner`,
`kube_replicaset_owner` (ReplicaSet → Deployment) and `kube_job_owner`
(Job → CronJob); other owners (StatefulSet, DaemonSet, a CNPG `Cluster`) are the
workload; a pod without an owner is `workload_kind = Pod`. Init containers are
not counted.

**Honesty rules.**

- Missing is missing: a value nobody measured is NULL, never 0. `coverage` is
  the fraction of the day's 5-minute slots in which the row had a usage sample
  (a CronJob that ran one hour has `coverage ≈ 0.04`, which is true, not a
  gap). Whether the *source* had data is the day's `usage_coverage` /
  `requests_coverage`.
- A day with no samples at all is not recorded as captured; it is checked
  again on the next run until it leaves the lookback window.
- A re-capture never degrades a day: a row is replaced only when the new read
  saw at least as many samples. Captured days are not re-read.
- CI runner pods (`WAYBILL_RESOURCE_RUNNER_NAMESPACES`, and any pod owned by an
  ARC `EphemeralRunner`) get one `workload_kind = Namespace` row per node and
  container instead of one per runner, so `node_daily` requested equals the sum
  of that node's `resource_daily` rows.
- A day is one transaction: rows and its capture record land together.

**Schedule.** One path for startup and schedule: `RunMissing` captures every
complete day (ended at least `WAYBILL_RESOURCE_SETTLE` ago) in the last
`WAYBILL_RESOURCE_LOOKBACK_DAYS` that has no capture record, oldest first. It
runs once at aggregator start (the backfill of everything Prometheus still
holds) and hourly at :35, so yesterday lands at 00:35 CDMX and a failed day,
an outage or a not-yet-applied migration is retried within the hour. With
nothing missing a run is one database read. Runs never overlap.

**Read API.** `GET /api/v1/resource-usage/daily` (rows) and
`GET /api/v1/resource-usage/nodes/daily` (nodes) take `from` / `to`
(`YYYY-MM-DD`, default the 7 days ending yesterday, at most 62 days),
`cluster`, and for rows `namespace`. Both return `captures` and
`uncaptured_dates` beside the rows, so a missing day is never read as a zero.
They sit under `/api/v1`, so they carry that group's auth. No money fields.

**Agents read them over MCP.** Waybill has no public route, so Enclii's
native MCP lives on switchyard-api (`POST https://api.enclii.dev/mcp`, off
unless `ENCLII_MCP_ENABLED`). Its tools `usage_resource_daily` and
`usage_node_daily` call these two routes in-cluster with the billing proxy's
base URL and service key, so this handler is the only implementation. Janua
machine tokens with scope `enclii-cogs:read` only:
[`docs/operations/NATIVE_MCP.md`](../../docs/operations/NATIVE_MCP.md).

**Published route list.** [`http-routes.json`](http-routes.json) lists every
route this API registers, with the query parameters and response fields of
the two ledger reads. `internal/api/http_routes_manifest_test.go` fails when
the file and the router or handlers differ. Switchyard's MCP drift guard
(`apps/switchyard-api/internal/waybillmcp`) reads the file: a new Waybill
route must be COVERED by an MCP tool or EXEMPTED there with a reason. Change
both in the same PR.

**Not here yet (follow-ups, RFC 0049 Phase 2 / RFC 0050).** Node cost
(`cost_line` from the Hetzner invoice and its daily accrual), the namespace →
product → engagement map, the allocation of node cost to workloads, the
nightly rightsizing recommendations, and `GET /v1/convergence/resource-usage`
for converge-dash `/recursos` are separate changes that read these tables.

## Stripe Integration

Waybill integrates with Stripe for:
- Customer management
- Subscription handling
- Usage-based invoicing
- Payment processing
- Webhook handling (payment succeeded, failed, etc.)

## Metrics Tracked

| Metric | Unit | Description |
|--------|------|-------------|
| `compute_gb_hours` | GB-hours | Container runtime |
| `build_minutes` | minutes | Build time |
| `storage_gb_hours` | GB-hours | Persistent storage |
| `bandwidth_gb` | GB | Network egress |
| `custom_domains` | count | Custom domain count |

## Integration with Platform

1. **Switchyard** calls `/internal/events` on deployment lifecycle
2. **Roundhouse** calls `/internal/events` on build completion
3. **K8s Reconciler** can emit periodic compute snapshots
4. **Dashboard** queries usage APIs for display
5. **Stripe** handles actual payment collection
