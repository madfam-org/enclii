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
- **Daily** (midnight): Roll up hourly → daily_usage
- **Monthly** (1st of month): Generate billing_records

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
