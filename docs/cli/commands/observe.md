# enclii observe

Read service observability data (metrics, health, errors, alerts).

## Synopsis

```bash
enclii observe <subcommand> [flags]
```

**Aliases:** `metrics`

## Description

The `observe` command reads service-level observability signals exposed under `/v1/observability/*`. Data sources include the platform metrics pipeline (Prometheus), the health reconciler, and Sentry for the admin-gated error subset. The `/observability` page in the consumer web UI consumes the same endpoints.

Most subcommands require `--service <id>` to scope the query. All subcommands are read-only and accept `--json`.

## Subcommands

### `metrics`

Show a current metrics snapshot for a service: cpu, memory, requests-per-second, latency p50/p95/p99.

```bash
enclii observe metrics --service <id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service ID (required) |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `history`

Show time-series metrics history for a service over a window.

```bash
enclii observe history --service <id> [--window <window>] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service ID (required) |
| `--window` | string | `1h` | Time window: `1h`\|`24h`\|`7d` |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `health`

Show service health status. Omit `--service` for cluster-wide health.

```bash
enclii observe health [--service <id>] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service ID (optional — omit for cluster-wide health) |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `errors`

List recent error events for a service. Backed by Sentry on the server side.

```bash
enclii observe errors [--service <id>] [--limit <n>] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service ID (optional) |
| `--limit` | int | `50` | Maximum number of error events |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `alerts`

List active alerts for a service.

```bash
enclii observe alerts [--service <id>] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service ID (optional) |
| `--json` | bool | `false` | Emit machine-readable JSON |

## Examples

### Current metrics snapshot

```bash
enclii observe metrics --service svc_storefront
```

**Output:**
```
METRIC        VALUE
service       svc_storefront
captured_at   2026-05-02 17:32
cpu           34.20%
memory        58.10%
rps           142.40
latency_p50   18.40ms
latency_p95   62.10ms
latency_p99   189.30ms
```

### 24-hour history

```bash
enclii observe history --service svc_storefront --window 24h
```

**Output:**
```
TIMESTAMP         CPU      MEM      RPS     P95
2026-05-01 18:00  31.20%   55.40%   118.30  58.10ms
2026-05-01 19:00  29.80%   55.90%   124.10  60.40ms
... (truncated) ...
```

### Cluster-wide health (JSON)

```bash
enclii observe health --json
```

### Recent error events from Sentry

```bash
enclii observe errors --service svc_storefront --limit 20
```

**Output:**
```
TIMESTAMP         LEVEL    COUNT  SERVICE         MESSAGE
2026-05-02 17:18  error    14     svc_storefront  TypeError: cannot read property 'id'
2026-05-02 17:02  warning  3      svc_storefront  Rate limit exceeded for /v1/checkout
```

### Active alerts

```bash
enclii observe alerts --service svc_storefront
```

## Notes

- `--window` only supports the documented values; arbitrary durations are rejected.
- The `errors` subcommand requires Sentry to be configured for the target service. Services without a configured DSN return an empty list.
- Cluster-wide queries (`health` without `--service`) may return aggregated data that is more expensive to compute; prefer scoping when possible.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing `--service`) |
| `50` | Authentication error |

## See Also

- [`enclii ps`](./ps.md) - Process and replica status
- [`enclii logs`](./logs.md) - Stream service logs
- [`enclii deployments`](./deployments.md) - Deployment runs and health
- [`enclii activity`](./activity.md) - Lifecycle event stream
