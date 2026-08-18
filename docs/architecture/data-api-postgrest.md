# Data API — auto-generated REST over managed Postgres (PostgREST)

> **Status:** Sprint 1 in flight (design + provisioning reconciler + service + CLI + tests)
> **Parity gap:** C1 — "auto REST API over managed Postgres" (the Supabase PostgREST equivalent). Biggest single Railway/Supabase parity gap after managed DB itself.
> **Builds on:** [`managed-db-addon.md`](./managed-db-addon.md) (cluster-per-addon CloudNativePG).
> **Owner:** backend
> **Related:** `docs/production/GAP_ANALYSIS.md` (native DB is "bring your own connection strings" — this closes the read/write-over-HTTP half of that gap).

## Problem statement

A managed Postgres addon today exposes exactly one thing: a connection string
(`DATABASE_URL`) reachable only from inside the cluster on port 5432. Supabase,
Railway (via templates), and Neon's data-API all give the tenant a second front
door: a **REST API auto-generated from the database schema**, reachable over
HTTPS, with row-level-security-enforced authz. `GET /rest/v1/todos?select=id,title&done=eq.false`
returns JSON with no server code written by the tenant.

Enclii has managed Postgres (cluster-per-addon via CloudNativePG) and an
HTTP exposure path (ingress-nginx behind the Cloudflare tunnel), but nothing
turns a tenant's schema into an API. This document specifies that feature.

## Architecture decision: provision PostgREST (Option A), do not reimplement it

Two options were on the table:

| | Option A — provision PostgREST | Option B — Go request→SQL translator |
| - | ------------------------------ | ------------------------------------ |
| What | Deploy [PostgREST](https://postgrest.org) as a managed Deployment+Service per addon; expose it through the existing ingress at `<addon>.<project>.data.enclii.dev`. | Build a schema-introspection + PostgREST-dialect request parser in `switchyard-api` that translates `?select=…&col=eq.…` into SQL. |
| SQL coverage | Full PostgREST surface: filtering, ordering, embedding (resource embedding via FKs), full-text search, RPC (`/rpc/fn`), bulk insert/upsert, `Prefer` headers, OpenAPI spec, aggregates. Battle-tested. | Whatever we implement. Realistically a fraction, with a long tail of edge cases (embedding, RPC, upsert conflict targets) that each become a support ticket. |
| Authz model | RLS in the tenant DB is the boundary. PostgREST's role-switching (`authenticator` → `anon`/`authenticated` via JWT `role` claim + `SET LOCAL ROLE`) is exactly Supabase's model. | We would have to reimplement role-switching and per-request `SET ROLE` correctly, or push authz into the API layer (weaker — no defense-in-depth in the DB). |
| Ops surface | One more container image to track (`postgrest/postgrest`), pin, and scan. Runs as an ordinary Deployment we already know how to reconcile. | No new image, but a large new code surface in the control plane that owns tenant-data read/write paths — a much larger blast radius for a bug. |
| Supabase parity | **PostgREST _is_ what Supabase uses.** Choosing it means our data-API contract is Supabase's contract; tenants' existing `@supabase/postgrest-js` / PostgREST clients work unchanged. | We would be Supabase-_flavored_ at best, with subtle incompatibilities. |
| Effort | Reconcile a Deployment+Service+ConfigMap+Secret+NetworkPolicy+Ingress (we have a template for every one of these). Generate a bootstrap SQL for roles. | Large. Introspection cache, dialect parser, role-switching, connection pooling, and an endless compatibility backlog. |

**Decision: Option A.** We provision PostgREST. The only real cost is one more
image in the supply chain; the alternative is owning a tenant-data SQL translator
forever and still being incompatible. This mirrors the `managed-db-addon.md` D1
reasoning ("the pre-existing, stronger-isolation implementation is a better
starting point than what we'd build") — here the "pre-existing" thing is an
open-source project that already solved the problem.

**Blocking-reason check (per the task):** none found. PostgREST is a single
static binary in a ~20 MB image, stateless, horizontally scalable, and speaks
only to Postgres over the connection string we already materialize. It fits the
"provision a Deployment+Service+route for a feature" shape that
`reconciler/function_controller.go` already implements.

## Topology

**One PostgREST Deployment per addon** (not shared, not per-project). Rationale:

- The connection string, exposed schema set, anon role, and JWT secret are all
  **per-addon** — a shared PostgREST would need per-request tenant routing and a
  connection string it doesn't have. Cluster-per-addon isolation (D1 of the
  managed-DB design) would be undermined by a shared data-API pod that holds
  every tenant's credentials.
- It lives in the **addon's namespace** (`project-<uuid8>`), next to the
  CloudNativePG cluster it fronts, so the existing per-project NetworkPolicy and
  resource-guard machinery covers it for free.
- Deferred: a **shared-pool topology** for very-low-traffic addons (many idle
  PostgREST pods is wasteful). Noted in "Deferred" below; the per-addon model is
  correct for Sprint 1 and the isolation baseline.

```
Tenant's HTTPS client
      │  GET https://<addon>.<project>.data.enclii.dev/todos?select=*  (+ Bearer JWT)
      ▼
Cloudflare tunnel  ──►  ingress-nginx  ──►  Ingress (host-routed, cert-manager TLS)
                                              │  (namespace project-<uuid8>)
                                              ▼
                                   Service  data-<addon>   (ClusterIP :80 → :3000)
                                              ▼
                                   Deployment  data-<addon>  (postgrest/postgrest)
                                              │  PGRST_DB_URI (authenticator role)
                                              ▼
                                   CloudNativePG cluster  pg-<addon>-<id8>  :5432
                                              │  RLS policies (tenant-owned) enforce authz
                                              ▼
                                        tenant tables
```

Ingress ⇄ Service ⇄ Deployment is exactly the function/service exposure path;
the NetworkPolicy that admits ingress-nginx to the pod is the same one
`reconciler/networking.go` builds for services.

## Auth & role model (the security core)

PostgREST's authorization model is **role-switching driven by a JWT**, and RLS in
the tenant database is the real boundary. This is identical to Supabase and is
the single most important thing to get right.

Three Postgres roles, created by an idempotent **bootstrap SQL** the provisioner
runs against the addon (as the addon owner) when the data-API is enabled:

| Role | Purpose | Privileges |
| ---- | ------- | ---------- |
| `authenticator` | The role PostgREST logs in as. **`NOINHERIT`, `LOGIN`.** Can `SET ROLE` to `anon` / `authenticated` and nothing else. Holds no table privileges itself. | `LOGIN NOINHERIT`; `GRANT anon, authenticated TO authenticator`. |
| `anon` | Unauthenticated requests (no/invalid JWT). Whatever the tenant chooses to expose publicly. | `NOLOGIN`. By default granted **only** `USAGE` on the exposed schema — **no table grants**, so an addon with no RLS/grants is closed by default. |
| `authenticated` | Requests bearing a valid JWT (signed with the addon's secret). | `NOLOGIN`. Same default: `USAGE` on schema, nothing else until the tenant grants + writes RLS. |

Request flow:

1. Client sends `Authorization: Bearer <jwt>`. PostgREST verifies the signature
   against `PGRST_JWT_SECRET` (the per-addon signing secret we generate).
2. The JWT's `role` claim selects the DB role. No/invalid JWT → `PGRST_DB_ANON_ROLE`
   (`anon`). Valid JWT with `role=authenticated` → PostgREST issues
   `SET LOCAL ROLE authenticated` inside the request transaction.
3. Every query then runs **as that role**, so **RLS policies and GRANTs the
   tenant defined are the authorization boundary.** PostgREST enforces nothing
   itself beyond signature + role selection.

**The tenant is responsible for RLS — exactly like Supabase.** We create the
roles, wire the JWT secret, and expose the schema; a table with no RLS policy
and no GRANT is unreachable (closed by default). We ship a documented default
bootstrap that is *deny-by-default* and a `docs/` snippet showing the tenant how
to `ENABLE ROW LEVEL SECURITY` + `CREATE POLICY` + `GRANT`. We do **not**
silently grant broad access — the failure mode of "enabled the data API and
every row leaked" is the one thing this design refuses to allow.

### JWT signing secret

- Generated by enclii (32 random bytes, base64) when the data-API is enabled,
  stored in a per-addon Kubernetes Secret `data-<addon>-jwt` and referenced by
  the deployment as `PGRST_JWT_SECRET`. Never returned in list/info responses.
- `enclii addon api token <addon> --role authenticated [--claim k=v] [--ttl 1h]`
  mints a short-lived HS256 JWT signed with that secret, for the tenant to hand
  to their app / test with. The signing happens server-side in switchyard-api
  (the secret never leaves the cluster); the CLI receives only the finished
  token.
- Rotation: re-enabling regenerates on request; deferred as a first-class verb
  to a follow-up.

### Limited-role posture

PostgREST connects as `authenticator`, which is `NOINHERIT` and has **no table
privileges of its own** — it can only become `anon`/`authenticated`. It is
therefore not a superuser, cannot `CREATE`/`DROP`, and cannot read a table the
tenant has not explicitly exposed. This satisfies "PostgREST runs with a limited
role."

## Network isolation

The data-API pod is reachable from exactly two directions, both constrained:

1. **Ingress → PostgREST (:3000):** a NetworkPolicy admits traffic to the
   `data-<addon>` pods only from the `ingress-nginx` namespace (the Cloudflare
   tunnel entry point), mirroring `reconciler/networking.go`'s service ingress
   rule. Nothing else in the cluster can reach the PostgREST port.
2. **PostgREST → Postgres (:5432):** the addon's existing
   `EnsureProjectScopedIngressPolicy` already admits same-project pods to the
   CNPG cluster on 5432. The PostgREST pod carries this project's label (it's in
   the project namespace), so it is admitted; a **different** tenant's pod is
   not — the 2026-08-17 tenant-isolation guarantee is preserved, and the data
   API does not widen it.

Per-project isolation is thus reused, not reinvented: the data-API pod inherits
the addon namespace's project label and the CNPG cluster's project-scoped
ingress policy.

## Persistence

A new table `managed_db_data_apis` (migration 035) holds one row per addon that
has the data-API enabled:

| column | meaning |
| ------ | ------- |
| `addon_id` (PK, FK→database_addons) | the addon this data-API fronts |
| `project_id` (FK→projects) | denormalized for scoping/forensics |
| `status` | `pending / provisioning / ready / disabling / disabled / failed` |
| `status_message` | human-readable last transition detail |
| `schemas` | comma-sep exposed schema list (default `public`) |
| `anon_role` | anon role name (default `anon`) |
| `db_pool` | PostgREST connection pool size (from plan; default 10) |
| `jwt_secret_name` | K8s Secret holding the signing secret |
| `host` | public host `<addon>.<project>.data.enclii.dev` |
| `k8s_resource_name` | the Deployment/Service name `data-<addon>` |
| timestamps | created/updated/enabled/disabled |

Lifecycle events reuse the existing `managed_db_addon_events` ledger with two new
event types (`addon.data_api.enabled`, `addon.data_api.disabled`) so the audit
trail stays in one place.

## Lifecycle

```
enclii addon api enable <addon> [--schemas public] [--anon-role anon]
      │
      ▼
POST /v1/addons/:id/data-api  (RequireRole Developer, project-access checked)
      │  • addon must be ready (has a connection secret)
      │  • generate 32-byte JWT secret → Secret data-<addon>-jwt
      │  • INSERT managed_db_data_apis (status=pending)
      │  • emit addon.data_api.enabled
      ▼
DataAPIReconciler (30s tick, mirrors AddonReconciler):
      │  status=pending → provision:
      │     • run bootstrap SQL against the addon (create roles, grant, deny-by-default)
      │     • ConfigMap  data-<addon>-config  (db-schemas, db-anon-role, server-port)
      │     • Deployment data-<addon>         (postgrest image; envFrom cfgmap + jwt secret;
      │                                         PGRST_DB_URI from the CNPG -app secret's `uri`)
      │     • Service    data-<addon>         (:80 → :3000)
      │     • NetworkPolicy (ingress-nginx → :3000)
      │     • Ingress     data-<addon>        (host <addon>.<project>.data.enclii.dev, TLS)
      │  status → provisioning
      │  Deployment ready → status=ready
      ▼
enclii addon api disable <addon>
      │
      ▼
DELETE /v1/addons/:id/data-api  (RequireRole Admin)
      │  • status → disabling; reconciler deletes Ingress, NetworkPolicy, Service,
      │    Deployment, ConfigMap, jwt Secret (best-effort, idempotent)
      │  • bootstrap roles are LEFT in the DB (dropping roles that may own objects
      │    is unsafe; documented). Re-enable reuses them.
      │  • row → disabled; emit addon.data_api.disabled
```

## Security summary (call-outs)

- **RLS is the tenant's responsibility and the real authz boundary.** Enclii
  creates deny-by-default roles; a table is unreachable until the tenant grants +
  writes policies. We refuse to auto-grant broad access.
- **PostgREST runs as a limited, `NOINHERIT` `authenticator` role** with no table
  privileges of its own — never a superuser.
- **Exposure is per-project isolated**, reusing the addon namespace's project
  label and the CNPG cluster's `EnsureProjectScopedIngressPolicy`. The PostgREST
  port is reachable only from ingress-nginx.
- **The JWT signing secret never leaves the cluster**; token minting happens in
  switchyard-api and only the finished token is returned.
- **Closed by default:** enabling the data-API on a schema with no grants/RLS
  yields an API that returns nothing — the safe failure mode.

## Deferred (explicitly out of Sprint-1 scope)

1. **GraphQL** (`pg_graphql` / PostGraphile). PostgREST is REST-only. A GraphQL
   surface is a separate image and a separate design.
2. **Shared-pool PostgREST topology** for idle addons (cost optimization). The
   per-addon model is the correct isolation baseline; sharing is an optimization
   with its own routing/credential-isolation design.
3. **First-class JWT rotation verb** (`enclii addon api rotate-jwt`). The secret
   and mint path exist; the rotate ergonomics are a follow-up.
4. **Realtime / websockets** (Supabase Realtime). Out of scope; different system.
5. **Autoscaling / scale-to-zero** for the PostgREST pod (KEDA HTTPScaledObject,
   like functions). Sprint 1 runs a single fixed replica; scale-to-zero is a
   clean follow-up because the exposure path is identical to functions.
6. **Per-addon `db-max-rows` / statement-timeout tuning UI.** Sprint 1 ships
   conservative defaults baked into the ConfigMap.
7. **Redis/MySQL data-APIs.** PostgREST is Postgres-only; this feature is
   Postgres-addon-only by construction.

## Exit criteria (Sprint 1)

- [x] This design doc.
- [x] `managed_db_data_apis` migration (035) + repository.
- [x] `DataAPIProvisioner`: builds ConfigMap/Deployment/Service/NetworkPolicy/
      Ingress + bootstrap SQL; reconciled by `DataAPIReconciler`.
- [x] `DataAPIService`: enable/disable/info + JWT secret generation + token mint.
- [x] HTTP handlers: `POST/DELETE/GET /v1/addons/:id/data-api`, `POST …/data-api/token`.
- [x] CLI: `enclii addon api enable|disable|info|token <addon>`.
- [x] Tests: reconciler creates the right objects; enable/disable lifecycle;
      config generation; role/JWT wiring. `go build ./...` + `go test` green.
