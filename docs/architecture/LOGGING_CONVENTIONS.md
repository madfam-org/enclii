# Logging Conventions

Reference for logging standards across Enclii Go services.

---

## Current State

Enclii has a split logging approach resulting from the order services were built:

| Service | Library | Format | Import Count |
|---------|---------|--------|--------------|
| switchyard-api | `github.com/sirupsen/logrus` | Text (key=value) | 94 files |
| CLI (conductor) | `github.com/sirupsen/logrus` | Text | 2 files |
| roundhouse | `go.uber.org/zap` | Structured JSON | 16 files |
| waybill | `go.uber.org/zap` | Structured JSON | 11 files |

**Why the split exists:** switchyard-api was the first service written and adopted logrus, which was the de facto Go logger at the time. Roundhouse and waybill were built later and adopted zap for its structured JSON output and superior performance. There is zero cross-contamination -- no service mixes the two libraries.

---

## Standard for New Services

All new Go services MUST use `go.uber.org/zap` with `zap.NewProduction()`.

**Rationale:**
- Structured JSON output is machine-parseable by Loki, Fluent Bit, and Grafana without regex parsing.
- Strongly typed fields prevent log injection and inconsistent field names.
- zap's zero-allocation design avoids GC pressure in hot paths.
- Aligns with OpenTelemetry structured logging conventions.

**Initialization pattern** (from roundhouse/waybill):

```go
logger, err := zap.NewProduction()
if err != nil {
    log.Fatalf("failed to initialize logger: %v", err)
}
defer logger.Sync()
```

Do NOT use `zap.NewDevelopment()` in production images. Use build tags or environment detection if you need human-readable output locally.

---

## Field Conventions

Every log line MUST include these fields where applicable:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `service` | string | Always | Service name: `switchyard-api`, `roundhouse`, `waybill` |
| `level` | string | Always | Provided by the logger automatically |
| `ts` | float64/ISO 8601 | Always | Provided by the logger automatically |
| `msg` | string | Always | Human-readable event description |
| `request_id` | string | HTTP handlers | Correlation ID from `X-Request-ID` header or generated UUID |
| `user_id` | string | Authenticated requests | From JWT claims when available |
| `error` | string | Error paths | Use `zap.Error(err)` or `logrus.WithError(err)` |

**Context-specific fields** (add when relevant):

| Field | Context |
|-------|---------|
| `project_id` | Project-scoped operations |
| `service_id` | Service-scoped operations |
| `build_id` | Build pipeline operations |
| `deployment_id` | Deployment operations |
| `duration_ms` | Timed operations |

**Naming rules:**
- Use `snake_case` for all field names.
- Do not abbreviate (`request_id`, not `req_id`).
- Do not nest fields -- keep them flat for Loki label extraction.

---

## Log Levels

| Level | When to use | Production default |
|-------|-------------|--------------------|
| `debug` | Detailed debugging: SQL queries, full request bodies, internal state | Disabled |
| `info` | Normal operational events: startup, request served, deployment triggered, build completed | Enabled |
| `warn` | Recoverable issues: retry attempts, degraded dependency, deprecated usage, slow query | Enabled |
| `error` | Failed operations requiring attention: DB connection lost, API call failed, build error | Enabled |
| `fatal` | Process cannot continue. **Only use in `main.go` or init paths.** | Enabled |

**Do not use `fatal` or `panic` inside goroutines.** A fatal log in a goroutine kills the entire process without cleanup. Instead, return errors through channels or use `error`-level logging with graceful degradation.

```go
// Wrong -- kills the process from a goroutine
go func() {
    logger.Fatal("something failed", zap.Error(err))
}()

// Right -- propagate the error
go func() {
    if err := doWork(); err != nil {
        logger.Error("work failed", zap.Error(err))
        errCh <- err
    }
}()
```

---

## Sensitive Data

Never log:
- Passwords, tokens, API keys, or secrets (even partially).
- Full request/response bodies that may contain PII.
- Database connection strings with credentials.

When logging identifiers that could be sensitive, truncate:

```go
logger.Info("webhook verified",
    zap.String("signature", signature[:20]+"..."),
)
```

This pattern is already used in `roundhouse/internal/webhook/github.go`.

---

## Fluent Bit Parser Configuration

When Fluent Bit is deployed for log aggregation, two parsers are needed to handle both formats.

### zap JSON format (roundhouse, waybill, new services)

```ini
[PARSER]
    Name        zap
    Format      json
    Time_Key    ts
    Time_Format %Y-%m-%dT%H:%M:%S.%LZ
```

No regex required -- zap outputs valid JSON natively.

### logrus text format (switchyard-api, CLI)

```ini
[PARSER]
    Name        logrus
    Format      regex
    Regex       ^time="(?<time>[^"]+)" level=(?<level>\w+) msg="(?<message>[^"]*)"(?<fields>.*)$
    Time_Key    time
    Time_Format %Y-%m-%dT%H:%M:%S%z
```

**Note:** logrus text format requires regex parsing, which is slower and more fragile than JSON parsing. This is one more reason new services should use zap.

### Input configuration

Use the Kubernetes filter to attach pod metadata automatically:

```ini
[INPUT]
    Name              tail
    Path              /var/log/containers/switchyard-api*.log
    Parser            logrus
    Tag               switchyard.*

[INPUT]
    Name              tail
    Path              /var/log/containers/roundhouse*.log
    Parser            zap
    Tag               roundhouse.*

[INPUT]
    Name              tail
    Path              /var/log/containers/waybill*.log
    Parser            zap
    Tag               waybill.*
```

---

## Migration Plan: logrus to zap in switchyard-api

**Status: Deferred. Do not start this migration now.**

**Why not:**
- 94 files import logrus. The blast radius is the entire control plane.
- switchyard-api is the highest-churn service. A logging migration creates merge conflicts with every in-flight feature branch.
- The operational cost (dual-parser Fluent Bit config) is low compared to the risk of a cross-cutting refactor.
- logrus works. It is maintained. It is not a security or correctness issue.

**When to migrate:**
- When switchyard-api undergoes a major internal restructuring (e.g., module decomposition).
- When logrus is formally deprecated or has an unpatched vulnerability.
- When the Fluent Bit regex parser becomes a measurable bottleneck.

**How to migrate (when the time comes):**
1. Add a `pkg/log` wrapper in switchyard-api that exposes the zap interface.
2. Migrate one package at a time, starting with the most frequently changed packages (check `git log --name-only` for churn data).
3. Each package migration is a single PR. Do not batch packages.
4. Update Fluent Bit parsers only after the last logrus import is removed.
5. Delete the `pkg/log` wrapper once migration is complete -- use zap directly.

---

## Quick Reference

```go
// New service initialization
logger, _ := zap.NewProduction()
defer logger.Sync()

// Request handler logging
logger.Info("deployment created",
    zap.String("service", "roundhouse"),
    zap.String("request_id", reqID),
    zap.String("project_id", projectID),
    zap.String("deployment_id", deployID),
)

// Error logging
logger.Error("build failed",
    zap.String("service", "roundhouse"),
    zap.String("build_id", buildID),
    zap.Error(err),
    zap.Duration("duration", elapsed),
)

// Existing switchyard-api pattern (do not change)
logrus.WithFields(logrus.Fields{
    "service":    "switchyard-api",
    "request_id": reqID,
    "project_id": projectID,
}).Info("deployment created")
```
