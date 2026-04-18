# Changelog

All notable changes to `@madfam/enclii-sdk`.

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
