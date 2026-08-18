// Package realtime implements Supabase-style database change subscriptions
// over WebSockets for enclii managed Postgres addons (parity gap C2).
//
// v1 uses Postgres LISTEN/NOTIFY (ADR-002). An opt-in AFTER trigger on a
// watched table calls pg_notify on a shared channel with the changed row as
// JSON; switchyard-api holds one LISTEN connection per addon and fans each
// notification out to the WebSocket subscribers registered for that
// schema.table, applying an optional single-column equality filter per
// subscriber.
//
// The moving parts:
//
//   - Hub          — process-wide registry. Owns one addonConn per addon
//     (lazily opened on first subscriber, closed on last), each backed by a
//     Listener. Fan-out is goroutine-safe.
//   - Listener     — the interface over the raw LISTEN connection (pq.Listener
//     in production, a fake in tests) so the hub is unit-testable without a
//     real Postgres.
//   - Subscription — one WS client's interest in a schema.table (+ filter).
//   - Trigger SQL  — BuildEnableTableSQL / BuildDisableTableSQL generate the
//     idempotent trigger-function + per-table trigger install/uninstall.
//
// The WS handler (handler.go) is modeled on internal/logstream: an
// origin-checked upgrader, a wsConn interface for testability, and read/write
// pumps with heartbeat, idle-timeout, and drop-oldest backpressure.
//
// Deferred to Option B (logical replication / wal2json): table-wide capture
// without triggers, gap-free resume, large-row payloads without truncation,
// and per-subscriber RLS-filtered fan-out. See ADR-002.
package realtime
