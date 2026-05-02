# Master-admin tenant switching — design

Goal: as `admin@madfam.io` (master-admin), see and enter every tenant ("white-glove client") account exactly as that client would see it, without sharing credentials, with a complete audit trail.

This is a design doc. No code changes attached. Review and revise before implementing.

## Today's state (relevant slice of the model)

- `users` row (`internal/db/migrations/001_genesis.up.sql:193`) — id, email, role (varchar; values `admin`, `developer`, `viewer`).
- `teams` row (line 1179) — id, name, slug, owner_id, billing_email, settings (JSONB).
- `team_members` join table (line 1168) — `(team_id, user_id, role)`, role values `owner|admin|member|viewer`.
- `projects.team_id` is **nullable** (line 996); a NULL value means "personal".
- Auth middleware (`apps/switchyard-api/internal/middleware/auth.go:270-285`) — SEC-007: if JWT carries no `roles` claim, but email is in `ENCLII_ADMIN_EMAILS` and issuer matches the configured OIDC, grant `["admin","developer"]`.
- Frontend scope state (`apps/switchyard-ui/contexts/ScopeContext.tsx`) — synthesizes a "Personal Account" virtual scope from `user.id`/`user.email`; fetches `/v1/teams` and converts each row to a "team scope". Active scope persists in `localStorage` only.
- The Janua SDK ships an `OrganizationSwitcher` component but it is *not* used anywhere in switchyard-ui.

## Reality of "white-glove clients"

User confirmed: partly orgs / partly bespoke. Today, all 25 visible projects (Dhanam, Karafiel, factlas, …) have `team_id = NULL`. They appear under the master admin's "personal" virtual scope because the API does not filter by team for admins, and because no team rows exist for these client tenants.

So the data model already has a bucket (`teams`) but it isn't being used. Step zero is to actually create one team row per white-glove client and re-parent each `projects` row to it.

## Concept

A **tenant** in this system is just a `team`. We will not introduce a new entity. What's missing is:

1. A canonical team per white-glove client.
2. A way for master admin to *act as* a given team for a session.
3. A way for the API to honor that scope.
4. An audit row recording the act.

## Server changes

### 1. Re-parent existing projects
- Migration: for each known white-glove client, insert a `teams` row (slug, name) and update `projects` where `slug` matches the client name to set `team_id`. One-shot, idempotent. Backfill `team_members` so `admin@madfam.io` has `role='owner'` (or a new role `master_admin`) on every team.

### 2. New endpoint group: `/v1/admin/tenants`
- `GET /v1/admin/tenants` — admin-only. Lists every team plus aggregate counts (projects, services, members, last_deploy_at). Powers the switcher.
- `POST /v1/admin/tenants/:slug/enter` — admin-only. Creates an *acting-as session*. Returns `{ session_id, expires_at }` and sets a server-side cookie `ax_acting_as=<slug>`.
- `POST /v1/admin/tenants/exit` — clears the cookie and the session row.

### 3. New table: `admin_acting_sessions`
- Columns: `id` (UUID), `admin_user_id` (FK users), `tenant_team_id` (FK teams), `started_at`, `expires_at`, `ended_at`, `reason` (free-text), `client_ip` (`inet` — and don't pass empty strings, see audit AU-1).
- Indexed by `(admin_user_id, ended_at IS NULL)` so the lookup on every request is cheap.

### 4. Middleware change
- After auth resolves `user_id` + `user_roles`, if `admin` is present and the request carries the `ax_acting_as` cookie, look up the active session.
- If valid, set `ctx.acting_team_id = team_id` and `ctx.is_acting_as = true`. Downstream handlers query `WHERE team_id = ?` instead of "all projects" when `is_acting_as` is true. Master admin gets exactly the tenant's view.
- If the cookie names a tenant the admin isn't authorized for or the session expired, treat the request as if no cookie were present and clear the cookie on the response.

### 5. Audit trail
- Every authenticated request logs the existing fields plus `acting_on_behalf_of_team_id` (nullable). Tied to `admin_acting_sessions.id` so we can reconstruct sessions later.
- The `audit` page surfaces this column with a clear "as <tenant>" badge for those rows.

## Frontend changes

### 1. Replace the synthetic personal scope for admins
`ScopeContext.tsx` — when `user.roles.includes('admin')`:
- Replace the `Personal Account` virtual scope with `All tenants` (read-only umbrella scope; can't act on it, only used for the dashboard's cross-tenant view).
- Fetch tenants from `GET /v1/admin/tenants` (not `/v1/teams`).
- Each tenant becomes a scope with `type: 'tenant'`.
- Drop the `plan: 'Hobby'` field for admins; admins are not subject to plans.

### 2. Switcher UX
- The same chip in the header. For admins, the menu has three sections:
  - **All tenants** (default selection on first load) — cross-tenant dashboard view.
  - **Tenants** (scrollable list) — every team, with project count and a "last deploy" timestamp.
  - **Tools** — `Create Team`, `End acting-as session` (only when active).
- Selecting a tenant fires `POST /v1/admin/tenants/:slug/enter`, sets the `ax_acting_as` cookie via the response, and reloads the route. From then on, every page is filtered to that tenant. The chip shows `Acting as <tenant>` with a distinct color.
- "All tenants" fires `POST /v1/admin/tenants/exit` and reloads.

### 3. Read-mode banner
When `is_acting_as` is true, render a thin banner across the top: `You are acting as <tenant>. End session.` This is the single most important UX guard against accidental data leak / mis-attributed mutation.

### 4. Settings + Audit clarity
- `/settings` — show `Role: Master admin` (XC-1 / ST-4 fix).
- `/audit` — render `as <tenant>` next to the actor email when `acting_on_behalf_of_team_id` is non-null.

## Non-master users

For non-admin users, no contract changes:
- They keep their `Personal Account` synthetic scope **only when they own at least one project with `team_id IS NULL`**.
- They keep their list of teams from `/v1/teams`.
- They never see `/v1/admin/tenants` (403).

This means the existing free-tier UX is preserved. The change is purely additive for admins.

## Phasing

1. ✅ **Round 7 (`<this commit>`)**: re-parenting migration `024_reparent_projects_to_teams` — the seed data the entire XC-2 stack was waiting on. Creates 20 `teams` rows (19 white-glove client teams + `madfam-platform` for internal infra), parents the 25 audited projects to their respective tenants, backfills `team_members` so `admin@madfam.io` is `owner` on every team. Fully idempotent (`ON CONFLICT (slug) DO NOTHING` for inserts; `WHERE team_id IS NULL` guard on every UPDATE). Symmetric down migration. 6 regression tests in `migration_024_test.go` covering idempotency, NOT EXISTS guard, email-based admin resolution, up/down slug symmetry, and a no-bare-transaction-marker check. **The XC-2 acting-as filter that's been live since Round 4 finally has tenant data to filter against.**
2. ✅ **Round 0 (`cc60e2e9`)**: design doc + frontend label fix — `Master Admin` chip with shield avatar replaces the synthetic `Personal Account (Hobby)`. No backend changes.
2. ✅ **Round 1 (`bc4c69b9`)**: migration `023_admin_acting_sessions` (table + index + `audit_logs.acting_on_behalf_of_team_id` column), `AdminActingSessionRepository`, four endpoints (`GET /v1/admin/tenants{,active}`, `POST .../:slug/enter`, `POST .../exit`), gated under the existing admin role middleware.
3. ✅ **Round 2 (`eecf5728`)**: scope switcher rewritten — admins fetch `/v1/admin/tenants`, picking a tenant fires `POST .../:slug/enter` and reloads, `AdminActingBanner` shows tenant + countdown + End-session.
4. ✅ **Round 3 (`<this commit>`)**: handler-level tests for the four endpoints (sqlmock); `enclii admin tenants list/active/enter/exit` CLI commands; doc updates.
5. ✅ **Round 4 (`4a180eb6`)**: tenant-filter middleware. `middleware.ActingAsMiddleware` reads `ax_acting_as`, validates the open session via `AdminActingSessionRepository.GetActive`, stashes `acting_team_id` in the gin context. `GET /v1/projects` consults `middleware.ActingTeamID` and filters via the new `ProjectRepository.ListByTeam` + `ProjectService.ListProjectsScoped`.

### Round 5 — handler coverage (`<this commit>`)

`/v1/projects` was the seed; Round 5 rolls the same dispatch out to every other tenant-bound list/detail surface. Each scoped handler reads `middleware.ActingTeamID(c)` and either (a) calls a `*ByTeam` repo method when acting-as is active, or (b) falls back to the unscoped path for non-admin / non-acting callers.

**Scoped list endpoints** (acting-as → `*ByTeam` repo dispatch):
- `GET /v1/deployments` — `DeploymentRepository.ListAllEnrichedByTeam` (joins releases+services+projects)
- `GET /v1/activity` — `AuditLogRepository.QueryByTeam` (matches both `project_id → projects.team_id` and `acting_on_behalf_of_team_id`)
- `GET /v1/databases` (and the `/v1/addons` alias) — `DatabaseAddonRepository.ListByTeam` (joins projects)
- `GET /v1/domains` — `CustomDomainRepository.ListAllByTeam` (joins services+projects)

**Per-resource detail endpoints** (acting-as → 404 on cross-tenant id, via shared helper `Handler.enforceActingTeamForProject`):
- `GET /v1/services/:id`
- `GET /v1/services/:id/deployments`
- `GET /v1/services/:id/deployments/latest`
- `GET /v1/services/:id/versions/:version`
- `GET /v1/deployments/:id`
- `GET /v1/services/:id/domains`, `GET /v1/services/:id/domains/:domain_id`
- `GET /v1/addons/:id`

The 404-rather-than-403 choice keeps the impersonation surface opaque: a master admin scoped into "tenant A" must not be able to fingerprint "tenant B" resources by guessing UUIDs.

**Repo additions**: `ProjectRepository.GetTeamID` (one-shot lookup for the 403 helper), `ServiceRepository.ListByTeam`, `DeploymentRepository.ListAllEnrichedByTeam`, `DatabaseAddonRepository.ListByTeam`, `CustomDomainRepository.ListAllByTeam`, `AuditLogRepository.QueryByTeam`. All exclude rows whose project's `team_id IS NULL` — same convention as Round 4.

**Tests**: 27 new repo subtests (`team match` / `team mismatch` / `no rows` / `db error` × 7 methods including `GetTeamID`) + 9 handler subtests (acting-as / no-acting-as for each scoped list endpoint, plus four cases for the `enforceActingTeamForProject` helper). Full suite green.

#### Round 5 — partial coverage (resolved in Round 6)

`GET /v1/audit` (consolidated audit surface, `internal/audit/handler.go`) was **not scoped** in this round. Rationale: the consolidated handler aggregates across six upstreams (Janua sessions, Switchyard `audit_logs`+`deployment_lifecycle_events`, four nexus-api Selva ledgers) via the `Aggregator`/`Source` interface. Each source has its own filtering vocabulary (Janua filters on user sub, Selva on resource path, etc.) — none accepts `team_id` today. Threading a `TeamID` through `audit.Query` and into every per-source `Fetch()` is a non-trivial cross-cutting change that's out of scope for "small repetitive per-handler updates".

The legacy single-source view (`GET /v1/activity`, backed by `AuditLogRepository`) **is scoped** and gives the operator an honest tenant-bound view of switchyard mutations. ✅ Round 6 (below) lands the deferred consolidated-aggregator scoping using both push-down (switchyard source) and post-filter (Janua, Nexus) strategies.

### Round 6 — audit aggregator scoping (`<this commit>`)

Threads `acting_team_id` through the consolidated `/v1/audit` aggregator that Round 5 explicitly punted on. Hybrid approach:

- **`audit.Query` gains a `TeamID *uuid.UUID` field** (`internal/audit/event.go`). Nil = unscoped. The handler reads `middleware.ActingTeamID(c)` via the new `ActingTeamReader` shim (`audit.GinActingTeamReader`) and stamps it on the query for both `List` and `Export`.
- **Switchyard source push-down** (`internal/audit/switchyard_source.go`): `audit_logs` query gains the same `project_id IN (… team_id = $N) OR acting_on_behalf_of_team_id = $N` clause that `AuditLogRepository.QueryByTeam` uses; `deployment_lifecycle_events` gains the same `project_id IN (…)` clause. `acting_on_behalf_of_team_id` is now read on every row and surfaced via `AuditEvent.ActingTeamID` for the "as &lt;tenant&gt;" badge enrichment.
- **Janua + Selva post-filter**: neither upstream accepts a `team_id` parameter today, so the aggregator post-filters via a new `TeamResolver` interface (satisfied by `db.ProjectRepository.GetTeamID`) wrapped in a per-request memoising cache (`audit.cachingTeamResolver`). A row survives if its `ActingTeamID` matches OR its `projectID` resolves to the team. Conservative default: rows with no project linkage and no acting-team are dropped under team scoping (this is the documented Janua-login behaviour — those events have no tenant proof, so they don't appear in tenant-scoped reads).
- **CSV export** (`/v1/audit/export`) gains a trailing `acting_team_id` column and applies the same scoping. Existing column-position consumers keep working.
- **Tests**: 14 new subtests in `internal/audit/team_scope_test.go` covering switchyard SQL push-down (TeamID nil / match / mismatch), Janua post-filter fallback, Nexus post-filter fallback, aggregator-level keep/drop logic across `ActingTeamID` and `projectID` paths, no-resolver-wired safety net, handler dispatch (acting-as / no-session / query-threading white-box), and the caching resolver dedup.

Wiring lives in `cmd/api/main.go`: `audit.NewAggregator(...).WithTeamResolver(repos.Projects)` plus `auditH.SetActingReader(audit.GinActingTeamReader{})`.

6. ✅ **Round 6**: tenant-filter on consolidated `/v1/audit` — landed.
7. ⏳ **Next**: backfill migration that creates `teams` rows for each white-glove client and re-parents existing `projects` (currently `team_id IS NULL`) onto the right tenant. Idempotent; one-shot.
8. ⏳ **Cleanup**: remove the "personal account" synthetic scope from the admin codepath entirely once tenant-filter middleware is live; reconfirm non-admin behavior in tests.

## Implementation references

- Migration: `apps/switchyard-api/internal/db/migrations/023_admin_acting_sessions.up.sql`
- Repository: `apps/switchyard-api/internal/db/admin_acting_session_repository.go`
- Handlers: `apps/switchyard-api/internal/api/admin_tenants_handlers.go`
- Handler tests: `apps/switchyard-api/internal/api/admin_tenants_handlers_test.go`
- Routes: `apps/switchyard-api/internal/api/register_admin_routes.go`
- Frontend context: `apps/switchyard-ui/contexts/ScopeContext.tsx`
- Frontend banner: `apps/switchyard-ui/components/AdminActingBanner.tsx`
- Frontend lib + tests: `apps/switchyard-ui/lib/admin-tenants.ts` + `lib/admin-tenants.test.ts`
- CLI: `packages/cli/internal/cmd/admin_tenants.go` + tests in `admin_test.go`

## Open questions

- **Tenant naming**: "white-glove client" is informal. Pick: "tenant" (technical), "client" (business), "workspace" (consumer-y). Recommend **tenant** in code, **client** in user-facing copy. Source of truth: `teams.name` and `teams.slug`.
- **Master role on teams**: do we add a new `team_members.role = 'master_admin'`, or rely on the global `users.role = 'admin'` JWT claim? Recommend the latter — keeps `team_members.role` semantics clean and lets us revoke admin globally if needed.
- **Cookie domain**: scope to `app.enclii.dev` only, never `.enclii.dev` — to keep the acting session out of admin.enclii.dev / status.enclii.dev sessions.
- **Session length**: default 4h, max 24h; enforce server-side.
- **Hard separation for forj/fortuna/symbiosis-hcm/etc.**: do we want to mark "internal MADFAM" tenants distinctly from external white-glove ones? Optional `teams.tier` enum.
