# ADR-002: Realtime Database Change Subscriptions over WebSockets

> **Status**: Accepted (v1 — LISTEN/NOTIFY)
> **Date**: 2026-08-17
> **Authors**: Backend / control-plane
> **Supersedes**: None
> **Parity gap**: C2 — the Supabase Realtime equivalent for enclii managed Postgres
> **Related**: `docs/architecture/managed-db-addon.md` (the addon stack this rides on)

---

## Summary

enclii managed Postgres (CloudNativePG, one `Cluster` per addon, namespace
`project-<uuid8>`) has no way for an application to *subscribe* to row-level
changes. Supabase, Firebase, and Convex all ship this as a headline feature:
a client opens a socket, says "tell me about writes to `public.orders`", and
receives `{INSERT|UPDATE|DELETE, table, record}` frames as they happen. This
ADR records the decision to build that on enclii and the topology chosen for
the first cut.

**Decision: v1 uses Postgres `LISTEN/NOTIFY` with per-table triggers, fanned
out to WebSocket subscribers by a hub inside `switchyard-api`. Full logical-
replication CDC (`wal2json`/`pgoutput`) is documented as the deferred Option B
upgrade.**

---

## Context

The Supabase model is a pipeline:

```
Postgres logical replication (wal_level=logical)
  → a decoder (wal2json / pgoutput)
  → a server that fans row changes out to subscribed WS clients
  → filtered by table/row, gated by RLS
```

That is the *destination*. It is also a meaningful infrastructure lift for a
multi-tenant control plane: it requires flipping `wal_level` to `logical` on
every addon cluster (a CNPG config change **plus a Postgres restart**),
provisioning and lifecycle-managing a replication slot per subscribed database
(a slot that, if a consumer dies, **pins WAL and can fill the disk**), and a
decoder plugin in the image. None of that is wrong; it is simply not the
cheapest path to the first working subscription.

enclii already ships the two things a LISTEN/NOTIFY design needs:

1. A **WebSocket pattern** — `internal/logstream` serves `GET
   /v1/services/:id/logs/tail` as a WS backed by Loki: origin-checked
   upgrader, a `wsConn` interface so the pump is unit-testable without a
   socket, and read/write pumps with heartbeat, idle-timeout, and
   drop-oldest backpressure. The realtime WS handler is modeled directly on
   it.
2. A **managed-DB addon stack** — `addons.AddonService` already resolves an
   addon to a live connection URI (`GetCredentials → ConnectionURI`) and
   enforces project access via `loadAddonWithAccess`. The realtime hub dials
   that same URI.

## Options considered

### Option A — Postgres `LISTEN/NOTIFY` (chosen for v1)

A SQL trigger on each **opt-in** table calls `pg_notify(channel, row_json)`.
`switchyard-api` holds **one** `LISTEN` connection per addon (via `lib/pq`'s
`pq.Listener`, already a dependency) and fans each notification out to the
WS subscribers registered for that `schema.table`.

- **Pros**: No `wal_level` change. Works on the standard CNPG cluster with no
  restart, no replication slot, no WAL-pinning failure mode. Uses deps already
  in `go.mod` (`lib/pq`, `gorilla/websocket`, `golang.org/x/time/rate`). The
  trigger is *explicit opt-in* per table, which doubles as the v1 authorization
  boundary (see below). One listener connection per addon, not per subscriber.
- **Cons / limits**:
  - **8000-byte NOTIFY payload cap.** A wide row's full JSON can exceed it.
    Postgres raises an error *at write time on the app's own transaction* if
    the payload is too large — an unacceptable blast radius. v1 handles this in
    the trigger: it measures the JSON and, when it would exceed a safe ceiling
    (7500 bytes, leaving headroom for the envelope), sends a **truncated frame**
    carrying the operation, the table, and the **primary-key columns only**,
    with `"truncated": true`. The client treats a truncated frame as a
    change signal and re-`SELECT`s the row itself. The app's write never fails.
  - **Triggers must be installed per table.** This is a feature in v1
    (opt-in = authz boundary) but means no "subscribe to the whole database"
    until Option B.
  - NOTIFY has **at-most-once, no-replay** semantics: a change emitted while no
    listener is connected is lost. Acceptable for a live-subscription feature
    (it is not an event bus); documented for clients so they cold-start with a
    `SELECT`.

### Option B — Logical replication / `wal2json` (deferred)

CNPG cluster set to `wal_level=logical`, a replication slot per subscribed
database, a decoder (`wal2json` or native `pgoutput`) streamed by the hub.

- **Pros**: True CDC. No per-table triggers — captures every table
  automatically. No 8 KB payload cap. Ordered, and *replayable* from a
  confirmed LSN, so a reconnecting client can resume without a gap.
- **Cons**: `wal_level=logical` needs a CNPG spec change **and a restart** of
  the addon cluster. A replication slot **pins WAL until consumed** — a dead or
  slow consumer can fill the volume and take the customer's database down; this
  needs slot-lifecycle management, a max-slot-lag guard, and disk alerting
  before it is safe to expose to tenants. Larger image (decoder plugin). This
  is the right destination but is explicitly **out of scope for v1**.

Note: enclii already uses `wal_level=logical` **only** for one-shot migration
cutover today; it is not exposed to running apps, and the HA config sets
`wal_level=replica` for archiving. Option B would make logical WAL a
steady-state, tenant-facing dependency — a bigger commitment than the
migration use.

## Decision

Ship **Option A** now. It delivers the Supabase-parity developer experience
(open socket → get row changes) on the existing cluster with no new infra
failure modes, and its opt-in trigger model gives a clean, auditable
authorization boundary for a v1. Track **Option B** as the CDC upgrade for when
a customer needs table-wide capture, large rows without re-fetch, or gap-free
resume.

## Architecture (Option A)

### Topology

```
WS client ──(GET /v1/projects/:slug/addons/:id/realtime)──► switchyard-api
                                                              │
                                              realtime.Hub (one per process)
                                                              │
                        ┌─────────────────────────────────────┼───────────────────────┐
                        │ addonConn(addon A)                   │ addonConn(addon B)     │
                        │  LISTEN enclii_realtime  ◄───────────┼── pq.Listener          │
                        │  subscribers: {sub1(orders),         │   ...                  │
                        │                sub2(orders WHERE …)} │                        │
                        └──────────────▲───────────────────────┴────────────────────────┘
                                       │ pg_notify('enclii_realtime', json)
                        ┌──────────────┴───────────────┐
                        │  addon Postgres (CNPG)         │
                        │  trigger on public.orders      │
                        └────────────────────────────────┘
```

- **One `LISTEN` connection per addon**, lazily opened on the first subscriber
  and closed when the last subscriber for that addon disconnects. All tables in
  an addon share **one** NOTIFY channel (`enclii_realtime`); the payload's
  `table`/`schema` fields let the hub route to the right subscribers. One
  channel keeps the listener count at one-per-addon regardless of how many
  tables are watched.
- **The hub is process-local.** With N replicas of switchyard-api, each replica
  holds its own listener per addon and each NOTIFY is delivered to every
  replica — so a subscriber connected to any replica sees every change. This is
  correct (NOTIFY broadcasts to all listeners) at the cost of N listener
  connections per hot addon. Bounded and acceptable at current replica counts;
  revisit with Option B or a shared relay if it grows.

### Wire protocol

Client → server (subscribe), sent as the first WS text frame, or via query
params on the upgrade for a single-table subscription:

```json
{ "type": "subscribe", "schema": "public", "table": "orders",
  "filter": { "column": "status", "value": "paid" } }
```

Server → client:

```json
{ "type": "change", "event": "INSERT", "schema": "public", "table": "orders",
  "record": { "id": 42, "status": "paid", ... }, "commit_ts": "2026-08-17T…Z" }
{ "type": "change", "event": "UPDATE", "schema": "public", "table": "orders",
  "record": {...}, "old_record": { "id": 42 }, "truncated": true }
{ "type": "subscribed", "schema": "public", "table": "orders" }
{ "type": "error", "error": "table_not_enabled" }
{ "type": "bye" }
```

- `event` ∈ `INSERT | UPDATE | DELETE`.
- `record` is the new row (the old row for DELETE). `old_record` carries the
  changed row's key on UPDATE/DELETE.
- `truncated: true` means the payload exceeded the NOTIFY ceiling and only the
  key columns are present — re-`SELECT` to hydrate.
- Filtering is a single `column = value` equality in v1, evaluated **server-side
  in the hub** against the decoded record (not in SQL), so multiple subscribers
  with different filters share one trigger and one listener.

### Trigger install mechanism

Realtime on a table is **explicit opt-in**. A tenant enables it per table:

```
enclii addon realtime enable  <addon> --table public.orders
enclii addon realtime list    <addon>
enclii addon realtime disable <addon> --table public.orders
```

which drive:

```
POST   /v1/addons/:id/realtime/tables   { "schema": "public", "table": "orders" }
GET    /v1/addons/:id/realtime/tables
DELETE /v1/addons/:id/realtime/tables/:schema/:table
```

Enabling installs a shared `enclii_realtime_notify()` trigger function (created
once, idempotently) and an `AFTER INSERT OR UPDATE OR DELETE` row trigger named
`enclii_realtime_<table>` on the target table. The function builds the JSON
envelope, applies the size ceiling, and `pg_notify`s the shared channel.
Disabling drops the trigger. The SQL is generated by
`realtime.BuildEnableTableSQL` / `BuildDisableTableSQL`, both identifier-quoted
against injection and unit-tested.

### Authorization & RLS

Two layers gate a subscription:

1. **Endpoint layer** — the WS route sits behind `RequireProjectAccessBySlug`
   and re-checks the addon's project via `loadAddonWithAccess`, exactly like
   every other `/v1/addons/:id/*` route. A caller who is not a member of the
   addon's project cannot open the socket. This is the same boundary that
   guards the credentials endpoint.
2. **Table opt-in layer** — a table emits changes **only** if a project member
   explicitly enabled realtime on it. There is no way to subscribe to a table
   whose owner has not turned it on. This makes "what can leak over realtime" an
   auditable, deliberate allowlist rather than the whole schema.

**RLS depth is deliberately limited in v1 and documented as such.** Postgres
row-level security is enforced on `SELECT`/`DML` for the *connecting role*, but
a `LISTEN/NOTIFY` payload is produced by the trigger under the **writer's**
transaction and broadcast verbatim; it is not re-filtered per-subscriber by the
subscriber's RLS policy. So v1's guarantee is **table-granular, not
row-granular**: every project member who can open the socket and who subscribes
to an enabled table sees every change to that table. Per-subscriber
row-filtering that honors each subscriber's RLS visibility is a follow-up that
lands naturally with Option B (where the fan-out server holds a per-subscriber
authenticated connection and can re-check visibility), and is called out in the
"Deferred" section. Tenants who need row-level isolation over realtime in the
interim should model it with a filter column and enable realtime only on tables
whose full contents are safe to share within the project.

## Consequences

- **Positive**: Supabase-parity subscriptions on the existing cluster; no
  `wal_level` change, no replication slots, no new disk-fill failure mode; opt-in
  triggers double as an auditable authz allowlist; reuses the proven logstream
  WS machinery and existing deps.
- **Negative / accepted**: table-granular (not row-granular) authz in v1; no
  replay of changes emitted while disconnected; wide rows arrive truncated and
  require a client re-fetch; N listener connections per hot addon at N replicas.
- **Follow-up (Option B)**: logical replication for table-wide capture,
  gap-free resume, large-row payloads, and the substrate for per-subscriber
  RLS-filtered fan-out.

## Verification

- `go test ./internal/realtime/...` covers hub fan-out (subscribe / publish /
  unsubscribe, concurrent subscribers), per-subscriber table+filter routing,
  the enable/disable trigger SQL generation (including identifier quoting), and
  the WS auth/subscribe handshake against a fake listener + fake socket.
- The WS endpoint self-disables cleanly (503) when the realtime hub is not
  wired, mirroring how `/v1/services/:id/logs` 503s without Loki.
