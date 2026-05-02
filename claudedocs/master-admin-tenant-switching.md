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

1. **Now (this session)**: design doc (this file) + small frontend label fix — drop `Personal Account (Hobby)` for admins, show `Master Admin` instead. No backend changes; no behavior change beyond the label.
2. **Next PR**: create the `admin_acting_sessions` table, write the three new endpoints, write middleware. Server-only, no UI binding yet.
3. **Next PR**: backfill migration for white-glove tenants. Add `team_members` rows for admin@madfam.io across all of them.
4. **Next PR**: scope switcher rewrite (uses `/v1/admin/tenants`), banner, audit-row enrichment.
5. **Cleanup PR**: delete the "personal account" synthetic scope concept on the admin codepath entirely; reconfirm non-admin behavior in tests.

## Open questions

- **Tenant naming**: "white-glove client" is informal. Pick: "tenant" (technical), "client" (business), "workspace" (consumer-y). Recommend **tenant** in code, **client** in user-facing copy. Source of truth: `teams.name` and `teams.slug`.
- **Master role on teams**: do we add a new `team_members.role = 'master_admin'`, or rely on the global `users.role = 'admin'` JWT claim? Recommend the latter — keeps `team_members.role` semantics clean and lets us revoke admin globally if needed.
- **Cookie domain**: scope to `app.enclii.dev` only, never `.enclii.dev` — to keep the acting session out of admin.enclii.dev / status.enclii.dev sessions.
- **Session length**: default 4h, max 24h; enforce server-side.
- **Hard separation for forj/fortuna/symbiosis-hcm/etc.**: do we want to mark "internal MADFAM" tenants distinctly from external white-glove ones? Optional `teams.tier` enum.
