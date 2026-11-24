# Native MCP (cost ledger reads)

> Last Updated: 2026-11-06

`POST /mcp` on Enclii's API host (`api.enclii.dev`, switchyard-api) is a
native [Model Context Protocol](https://modelcontextprotocol.io) endpoint.
Agents and agent fleets use it to read what the cost ledger needs from
Enclii: Waybill's daily resource ledger. It serves **usage and requests
only, never money**. There is no node cost yet.

It is **off by default**. With `ENCLII_MCP_ENABLED` unset or `false`, the
route is not registered and answers 404. The owner turns it on (see
[Enabling it](#enabling-it-owner-steps)); agents never do.

This is Enclii's part of wave A2 of RFC 0051 (native MCP per platform, the
COGS slice of P1, private `internal-devops` repo).

Code:

| Path | What |
| --- | --- |
| `apps/switchyard-api/internal/mcp` | Protocol, gates, audit line, rate limit |
| `apps/switchyard-api/internal/waybillmcp` | The tools, the coverage contract, the drift guard |
| `apps/switchyard-api/internal/api/mcp_routes.go` | Mounting and the switch |
| `apps/switchyard-api/internal/auth/jwt_machine.go` | Janua machine-token claims, verified like the HTTP API's tokens |
| `apps/waybill/http-routes.json` | Waybill's published route list, which the drift guard reads |

## Why the endpoint is on switchyard-api

RFC 0051 puts `/mcp` on each platform's own API host. Waybill has no public
route of its own: its Service is reachable only in-cluster, from
switchyard-api, Prometheus, Roundhouse and Weighbridge (NetworkPolicy
`waybill-ingress`). It also verifies no Janua token. Its `/api/v1` group is
open in-cluster until enclii#907 adds the service key. Enclii's API host is
switchyard-api, which already reaches Waybill for the billing proxy
(`/v1/projects/:slug/billing/*`).

So the MCP lives on switchyard-api. Each tool calls the Waybill route it
covers, with the billing proxy's base URL (`ENCLII_WAYBILL_BASE_URL`) and
service key (`ENCLII_WAYBILL_INTERNAL_API_KEY`, sent as `X-API-Key` when
set). Waybill's own handler parses and validates every argument. There is
no second implementation and no back door. No new network path exists:
`switchyard-api-waybill-egress` and `waybill-ingress` already allow it.

## Transport

- Streamable HTTP, JSON-RPC 2.0. MCP revisions `2025-11-25` (preferred),
  `2025-06-18` and `2025-03-26`.
- Stateless: no `Mcp-Session-Id`, no server-sent streams. Every response is
  one `application/json` body. `GET` and `DELETE /mcp` answer 405.
- Methods: `initialize`, `ping`, `tools/list`, `tools/call`. Notifications
  answer 202. Batches are refused (400).
- Request bodies are `application/json` (415 otherwise), at most 64 KiB (413).

## Who may call it

Only **Janua machine tokens** (`client_credentials`), in the
`Authorization: Bearer` header. The token is verified exactly like the HTTP
API's Janua tokens: the JWKS signature (`ENCLII_EXTERNAL_JWKS_URL`), expiry,
issuer (`ENCLII_EXTERNAL_ISSUER`) and the API's existing audience
(`ENCLII_JANUA_AUDIENCE`). The MCP refuses to mount without an issuer and an
audience.

The gates, in order:

| Condition | Answer |
| --- | --- |
| `Origin` header present (browser) | 403 `origin_not_allowed` |
| No bearer; invalid, expired, foreign-issuer or wrong-audience token; a switchyard session token; an `enclii_` API token | 401, `WWW-Authenticate: Bearer realm="enclii-mcp"` |
| A person's Janua token | 403 `machine_token_required` (people come in Phase B) |
| A machine client bound to no Janua organization, or to one not in `ENCLII_MCP_ORG_IDS` | 403 `org_not_allowed` |
| A machine token without exactly `enclii-cogs:read` | 403 `insufficient_scope` |
| Over the client's rate | 429 with `Retry-After` |

**Scope:** `enclii-cogs:read` (RFC 0051 M2, `<platform>-cogs:read` with
platform `enclii`). It opens the MCP and nothing else. No other scope opens
the MCP.

**Organization:** the ledger is platform-wide (every namespace of the
cluster), so a client must be bound to the platform operator's Janua
organization. Its id goes in `ENCLII_MCP_ORG_IDS`. Janua puts it in the
token's `tenant_id` and `org_id` claims only when the client is
organization-bound.

**Audience caveat:** in production `ENCLII_JANUA_AUDIENCE` is Janua's global
default audience, not an Enclii-only one. Other Janua clients can also get
tokens with that audience, so the audience check does not keep them out.
The organization allow-list and the reserved, exact scope do. A dedicated
Enclii API audience would also move the CLI and session tokens, so it is a
separate follow-up.

## Tools

Both tools are read-only (`readOnlyHint: true`). There are no write tools.
Tool names follow Anthropic's `^[a-zA-Z0-9_-]{1,64}$`, which has no dots.
The RFC 0051 catalog names lead each tool's `title`.

| Tool (title) | Arguments | Returns | Same read as |
| --- | --- | --- | --- |
| `usage_resource_daily` (`usage.resource_daily`) | `from`, `to` (civil dates `YYYY-MM-DD`, both inclusive, required, at most 62 days), `namespace?`, `cluster?` | `{from, to, timezone, captures, uncaptured_dates, rows}`: per day, node, namespace, workload and container, CPU core-hours and memory GiB-hours used, requested and limit, p95/max, pod and sample counts, coverage | `GET /api/v1/resource-usage/daily` (Waybill) |
| `usage_node_daily` (`usage.node_daily`) | `from`, `to` (as above), `cluster?` | `{from, to, timezone, captures, uncaptured_dates, nodes}`: per day and node, allocatable, requested and used CPU and memory, p95/max, pod and sample counts, coverage | `GET /api/v1/resource-usage/nodes/daily` (Waybill) |

- Days in `uncaptured_dates` have no data yet. They are not zeros.
  `captures` says how much of each captured day Prometheus held.
- The HTTP routes default `from`/`to` to the week ending yesterday. The
  tools require both, so every answer names its period.
- The output is a projection of Waybill's JSON. The tool returns only the
  fields its output schema lists. If Waybill adds a field, MCP callers do
  not get it until the drift guard fails and the schema lists the field.
- An argument the caller can fix comes back as a tool result with
  `isError: true`. That covers an unknown argument, a malformed date, and
  every 400 from Waybill's handler (from after to, more than 62 days, a bad
  namespace or cluster). An answer larger than 4 MiB is refused the same
  way, with a request to narrow the range or pass `namespace`.
- An unknown tool is JSON-RPC `-32602`. A Waybill failure (unreachable, 5xx,
  key refused) is HTTP 500 with `-32603` and no detail.

## Coverage contract and drift guard

`apps/switchyard-api/internal/waybillmcp/coverage.go` classifies each
COGS-relevant route as COVERED by a tool or EXEMPTED with a reason:

- **Waybill:** every route except `/health` and `/ready`. The two
  resource-usage reads are COVERED. Event ingest, per-project metering,
  list-priced spend, estimates, invoices, plans and budgets are EXEMPTED.
- **switchyard-api:** every route with a `usage`, `billing`, `cost(s)`,
  `invoices`, `budgets`, `resource-usage` or `convergence` path segment.
  `/v1/usage*`, the billing proxy, the Dhanam checkout and
  `/v1/admin/costs*` are all EXEMPTED. They serve people under session
  RBAC, move money, or are hand-entered allocations.

The guard (`coverage_test.go`) fails CI when:

- a Waybill route, or a switchyard usage/billing/cost route, is added
  without an entry, or an entry matches no route;
- a tool covers no route, or a COVERED route is not a GET;
- a tool's arguments differ from the query parameters Waybill publishes for
  its route;
- a tool's output fields differ from the response fields Waybill publishes.

Switchyard cannot import the Waybill module, so Waybill publishes
`apps/waybill/http-routes.json`. Waybill's own test
(`internal/api/http_routes_manifest_test.go`) keeps that file equal to its
router, its handlers' query parameters and its response types. CI tests
every Go module on every PR, so a Waybill change runs both checks.

## Audit and limits

- One structured log line per `POST /mcp` request, message `mcp.call`.
  Fields: `mcp_client_id` and `org_id` (verified machine clients only),
  `rpc_method`, `tool`, `outcome`, `latency_ms`, `http_status` and
  `args_sha256`, the SHA-256 of the arguments' canonical JSON. The
  arguments and the token are never logged.
- Metrics: `enclii_mcp_calls_total{method,tool,outcome}` and
  `enclii_mcp_call_duration_seconds{method,tool}`, on the existing
  in-cluster `/metrics`.
- Rate limit per Janua client (token bucket): `ENCLII_MCP_RATE_PER_MINUTE`
  (default 60) and `ENCLII_MCP_BURST` (default 10). The API's global
  per-address limiter still applies.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `ENCLII_MCP_ENABLED` | `false` | Mounts `POST /mcp`. Off: 404. |
| `ENCLII_MCP_ORG_IDS` | empty | Comma-separated Janua organization ids a client may be bound to. Empty while enabled: not mounted (logged). |
| `ENCLII_MCP_RATE_PER_MINUTE` | `60` | Sustained calls per minute per client. |
| `ENCLII_MCP_BURST` | `10` | Calls a client may make at once. |

The MCP also stays unmounted, with an error in the log, without the
external Janua JWKS (`ENCLII_EXTERNAL_JWKS_URL`, `ENCLII_EXTERNAL_ISSUER`)
or without `ENCLII_WAYBILL_BASE_URL`.

## Enabling it (owner steps)

These steps belong to the owner or the platform. No agent runs them (RFC 0051
M6).

1. **Reserve the scope in Janua.** Make `enclii-cogs:read` a scope that only a
   platform admin can grant.
2. **Provision one client per consumer** (RFC 0051 M3: Selva's agent fleet,
   the ops agents). Declare each in `config/ecosystem-oidc-provision.yaml`
   and its embedded copy, like the existing machine edges:
   `client_credentials` only, confidential, `audience` equal to production's
   `ENCLII_JANUA_AUDIENCE`, `allowed_scopes: [enclii-cogs:read]`, bound to
   the platform operator's Janua organization. `enclii secrets provision
   oidc` creates each client, and `enclii secrets intake` delivers it to the
   consumer's namespace.
3. **Turn the switch on.** In switchyard-api's production environment
   (`infra/k8s/production/environment-patch.yaml`), set
   `ENCLII_MCP_ORG_IDS` to that organization's id and `ENCLII_MCP_ENABLED`
   to `true`. Merging that change is a deploy. `ENCLII_WAYBILL_BASE_URL` is
   already set in the base manifest. Once enclii#906 and enclii#907 land,
   the Waybill service key reaches switchyard-api as
   `ENCLII_WAYBILL_INTERNAL_API_KEY` and the tools send it.
4. **Smoke test with an agent.** Call `initialize`, then `tools/list`
   (two tools), then `usage_node_daily` for yesterday. Check the `mcp.call`
   lines and `enclii_mcp_calls_total`.

To turn it off, unset the variable or set it to `false`. The route answers
404 again.

## Follow-ups

- The consumer clients for Selva's fleet and the ops agents
  (`config/ecosystem-oidc-provision.yaml`), as in step 2.
- Node cost: `cost_line` from the provider invoice, its daily accrual and
  its allocation to workloads (RFC 0049 Phase 2, RFC 0050 digital COGS).
  When that ships, a cost tool covers it, and `/v1/admin/costs*` is
  reconsidered.
- Phase B: an RFC 8707 protected resource `https://api.enclii.dev/mcp` with
  RFC 9728 metadata and scope `enclii.cogs:read`, for people through Claude
  connectors. It waits for Janua's production promotion hold to lift.
- A dedicated Enclii API audience (see the audience caveat).
- The ecosystem registry's `mcp` field for Enclii (RFC 0051 M5).
