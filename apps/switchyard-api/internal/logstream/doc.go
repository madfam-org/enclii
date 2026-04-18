// Package logstream implements the in-UI streaming log tail for
// app.enclii.dev/project/<p>/service/<s>/logs (P2.1).
//
// Name note: originally `logs` — renamed because the repo's .gitignore
// excludes any directory literally named "logs" (build-artifact pattern).
// Functionality unchanged.
//
// # Design
//
// The CLI (`enclii logs -f`) already talks to pod stdout via kubectl-style
// streaming (see internal/k8s). That path is fine for a developer with a
// shell, but it doesn't give us:
//
//   - historical queries (kubectl logs is pod-lifetime bound)
//   - label/grep/level filters computed by the log store
//   - cross-pod aggregation without fanout in Go
//   - rate limiting across many tenants
//
// So this package wraps Loki — already deployed on the cluster via
// Fluent Bit → Loki (see infra/k8s). Two Loki endpoints are used:
//
//   - /loki/api/v1/query_range for windowed queries (REST /v1/services/:id/logs)
//   - /loki/api/v1/tail        for live tailing    (WS   /v1/services/:id/logs/tail)
//
// LogQL is constructed server-side so the client never sees raw Loki URLs
// (a thin indirection that lets us swap stores later without breaking the
// UI contract).
//
// # RBAC
//
// Reuses switchyard-api's existing project-membership + service-admin
// checks via the AuthzChecker interface. Non-members get 403; members
// can read logs for any service in their projects.
//
// # Rate limiting
//
// Token-bucket per caller (user_id) at 32 queries/min with bursts of 8.
// The WS tail is budget-neutral — one WS uses one token on connect, then
// drains from Loki's own tail endpoint. Abusive clients hit 429 with a
// "Retry-After" header.
//
// # Backpressure
//
// Slow WS clients have their send queue bounded to 256 entries. When
// full, we drop the oldest and emit a {"type":"dropped","count":N}
// frame so the UI can render a "messages skipped" pill.
//
// # Degradation
//
// If Loki is unreachable the REST endpoint returns 503 with a JSON
// {"error":"log_store_unavailable","detail":"..."} envelope and the
// WS endpoint closes with code 1011 and a JSON error frame. The UI
// surfaces both as a clean "logs temporarily unavailable" state — never
// a blank page or infinite spinner.
package logstream
