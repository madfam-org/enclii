# Changelog

All notable changes to `@madfam/enclii-sdk`.

## Unreleased

Aligns the SDK with the Switchyard API (`apps/switchyard-api`) wherever the two
disagreed. Not yet published; the package version is unchanged.

### Breaking

- `logs.history()` returns `LogHistory` (`{ service_id, service_name,
  environment, namespace, logs, lines }`, where `logs` is raw text) instead of
  `Page<LogEntry>`. `LogHistoryOptions` is now `{ env?, lines?, since? }`;
  `limit`, `level`, `until` and `cursor` are removed (the API never read them).
  `since` (an RFC3339 timestamp or a positive Go duration) is honored by
  servers that include enclii #622; a malformed `since` throws before sending.
- `logs.iter()` is removed: the history endpoint does not page.
- `logs.tail()` and `nodeLogsTail()` yield `LogStreamMessage` frames
  (`{ type, pod?, container?, timestamp, message }`, including the `connected`
  status frame). `LogTailOptions` is now `{ env?, lines?, timestamps?, since?, signal? }`;
  `level`, `pod` and `container` are removed (the API never read them).
  `LogEntry` is a deprecated alias of `LogStreamMessage`.
- `nodeLogsTail()` throws when the server rejects the upgrade with a 4xx status
  instead of retrying and then completing without output.
- `secrets.bulkSet()` resolves to `{ message, count }` (was `EnvVar[]`), and its
  entries are `BulkEnvVar` (no per-variable `environment_id`).
- `webhooks.eventTypes()` calls `GET /lifecycle-webhooks/event-types` and
  resolves to `OutboundWebhookEventTypeInfo[]` (`{ type, description }`)
  instead of `OutboundWebhookEventType[]`.
- `rollback.manifest()` takes only the deployment ID (a second argument
  throws) and resolves to `ManifestRollbackResponse` instead of `void`.
  `RollbackRequest` is removed.
- `AuditEvent` matches the API's rows: `created_at` and `service_id` are
  removed; `timestamp`, `actor_role`, `resource_name`, `environment_id`,
  `ip_address`, `user_agent`, `outcome` and `context` are added.
- `EnvVar.value` is always present (secrets are masked as `••••••••`).

### Fixed

- `secrets.list()` reads `environment_variables` (always returned `[]`).
- `secrets.bulkSet()` sends `{ variables }` (failed with 400).
- `jobs.listOneOff()` reads `one_off_jobs` (always returned `[]`).
- `jobs.createCron()` / `jobs.createOneOff()` unwrap `cron_job` / `one_off_job`
  (the returned `id` was `undefined`).
- `deployments.latest()` unwraps `{ deployment, release }` (fields were
  `undefined`).
- `logs.tail()` sends the token as the `token` query parameter the server
  accepts; `nodeLogsTail()` can send an `Origin` header (`options.origin`).
- `audit.iter()` walks every page (it stopped after the first 50 rows);
  `audit.list()` and `webhooks.deliveries()` map `cursor`/`nextCursor` onto the
  API's `limit`/`offset` paging.
- `services.restart()` / `services.scale()` send `environment` as `env`, the
  field the API reads.

### Added

- `secrets.list()` option and `SetEnvVarRequest` field `environment_id`;
  `secrets.bulkSet()` third argument `{ environment_id }`.
- `DeployRequest.change_ticket_url`; `environment_name` is optional (the API
  defaults to `development`).
- `CreateProjectRequest.description`; `ci_runner_mode` is deprecated (the
  create endpoint ignores it).
- `services.restart()` option `reason`.
- `OneOffJob.failure_reason`.
- `EncliiClient.resolveToken()`.
- `logs.tail()` and `nodeLogsTail()` option `since` (an RFC3339 timestamp or a
  positive Go duration, as `parseLogsSince` in
  `apps/switchyard-api/internal/api/logs_since.go` reads it): sent as the
  stream's `since` query parameter to limit the backlog to newer lines, and
  resent on every `nodeLogsTail()` reconnect. A malformed value throws before
  connecting, since a browser cannot read the server's 400. Servers before
  enclii #625 ignore it.
- `audit.list()`, `webhooks.deliveries()` and `logs.history()` reject an
  out-of-range `limit`/`lines` or a malformed cursor before sending, where the
  API would silently substitute its default.

### Deprecated

- `limit`/`cursor` on the list methods of endpoints that return every row in
  one response (`projects`, `services`, `deployments.list`/`listReleases`,
  `webhooks.list`, `secrets.list`, `jobs.*`), and `pageSize` on their `iter()`.
  They are no longer sent; `nextCursor` is always `null` there.

## 0.1.0 - 2026-04-17

Initial release (P2.4a of the Enclii remediation plan).

### Added

- `EncliiClient` with bearer auth (static token or async provider)
- Exponential-backoff retry on 429/5xx, honors `Retry-After`
- Cursor pagination helpers: `.list()` returns `{ data, nextCursor }`, `.iter()`
  yields an AsyncIterable
- Typed error hierarchy: `EncliiError`, `AuthenticationError`,
  `AuthorizationError`, `NotFoundError`, `ConflictError`, `ValidationError`,
  `RateLimitError`, `ServerError`, `NetworkError`
- Resources:
  - `projects` — CRUD for projects
  - `services` — CRUD, restart, scale
  - `deployments` — CRUD, deploy, build, releases, v-number resolution, wait
  - `rollback` — P0.5 instant (selector flip) + manifest-commit variants
  - `canary` — P2.7 lifecycle: start, get, promote, rollback, wait
  - `logs` — history (paginated) + live tail (WebSocket, browser + Node ≥22)
  - `audit` — activity/audit event querying with filters
  - `webhooks` — P2.3 outbound lifecycle webhook subscription CRUD,
    deliveries, test.ping, secret rotation
  - `secrets` — service env-vars + reveal (RFC 0005 bridge)
  - `jobs` — cron + one-off scheduled jobs (Timetable)
- `verifyWebhookSignature()` — Stripe-compatible `t=<ts>,v1=<hex>` HMAC-SHA256
  validation with replay-window check
- `@madfam/enclii-sdk/node` subpath with `nodeLogsTail()` for reconnect-aware
  streaming in Node
- Dual ESM + CJS output via `tsup`, strict TypeScript, full tree-shaking
