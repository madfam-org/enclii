# app.enclii.dev fidelity audit — 2026-05-02

Authenticated session: `admin@madfam.io` (master-admin via SEC-007 email allowlist).
Browser: Playwright. Screenshots in `claudedocs/screenshots-2026-05-02/`.

Severity: 🔴 critical · 🟡 important · 🟢 nit · ✅ verified-correct.

---

## Cross-cutting

### XC-1 🔴 "Personal Account" label for master admin
- **Source**: `apps/switchyard-ui/contexts/ScopeContext.tsx:104-112` — `createPersonalScope` hardcodes `name: 'Personal Account'` and `plan: 'Hobby'`.
- **Observed**: top-left scope chip on every protected route, on the dropdown header, and on the menu item itself ("Hobby" badge).
- **Truth**: `admin@madfam.io` is master-admin (SEC-007 path, `apps/switchyard-api/internal/middleware/auth.go:270-285`). It is *not* a hobby-tier user. The 25 projects shown belong to distinct tenants (Dhanam, factlas, karafiel, pravara-mes, …), all currently with `team_id = NULL` so they appear under the "personal" virtual scope.

### XC-2 🔴 No master-admin tenant impersonation surface
- Confirmed via codebase search: no `/v1/admin/clients`, no `/v1/admin/impersonate`, no `as_user` JWT claim, no `acting_as` audit field. The scope switcher shows `Personal Account (Hobby)` and `Create Team` — **zero tenants visible**, no way to enter a client account.
- Today admin sees the union of all data because the API does not filter projects by team for admins; everything just appears under "Personal Account" as a fallback.
- Required: server-side `Acting-As: <tenant_slug>` middleware + audit row + UI tenant switcher fed by an admin endpoint.

### XC-3 🔴 21 console errors on first dashboard paint
- `GET /v1/csrf` → 404 (not implemented or not exposed).
- `GET /v1/observability/sentry?service=<id>` → 503 once per visible project (10+ times).
- Both noisy enough to drown out real errors.

### XC-4 🟡 Long-running pages hang on `Loading…` with no timeout / retry / visible network failure
- `/services` — "Loading services…" indefinitely. Network requests show `dashboard/stats`, `activity?limit=10`, `health`, `teams` all 200, but **no `/v1/services` listing call is ever made**. Page is wired wrong.
- `/observability` — "Loading observability data…" indefinitely.
- Dashboard's "System Health" sidebar — "Loading…" indefinitely.
- Several project cards (routecraft, karafiel, pravara-mes) — "Loading health" indefinitely.

### XC-5 🟡 Plan-tier UI applied to a self-hosted master admin
- `/usage`: "Compute 597.4 / 500.0 GB-hours +$4.87 overage charges", "Storage 46.5 / 10.0 GB +$9.12 overage charges", total cost $13.99. The whole plan/quota/overage frame is wrong for a self-hosted owner.
- Source likely `/v1/usage` returning a synthetic `included` allocation. For master-admin, included should be `Unlimited` (or the limit-based UI hidden entirely).

---

## Per-route findings

### `/` Dashboard

#### D-1 🔴 Usage widget: 100% Compute / 100% Storage / 8% Bandwidth / 61% Build minutes
Confirmed implausible (the cluster is operational; storage is not full). Same root cause as XC-5 — plan limits being applied where they don't belong. Likely `included` < `used` so percentage clamps to 100%.

#### D-2 🟡 System Health card stuck "Loading…"
Per network capture, no health endpoint is even being called from this widget on the dashboard.

#### D-3 ✅ Per-project status pills math (running/total + "+N more services") checks out for the projects spot-checked (Dhanam 3/3, factlas 3/4, routecraft 8/9, karafiel 5/7, pravara-mes 7/13, madfam-site 2/3, symbiosis-hcm 2/3, accionables-madlab 2/2, forj 2/3).

#### D-4 🔴 Alerts sidebar shows fabricated alerts
All 7 entries timestamped "just now":
- High Error Rate
- Service Deployment Failed (×2)
- Compute Over Plan Limit
- Build Minutes Over Plan Limit
- Storage Over Plan Limit
- Bandwidth Over Plan Limit

Plan-limit alerts are fabricated from XC-5/D-1. Same `just now` on all suggests static seed data or generated-on-render fallback.

#### D-5 🟢 "+ Add New…" CTA is opaque
No menu/affordance on hover; doesn't say what gets added.

---

### `/projects`
- ✅ 25 projects rendered: Dhanam, factlas, routecraft, nuit-one, karafiel, pravara-mes, madfam-site, accionables-madlab, symbiosis-hcm, forj, coforma-studio, blueprint-harvester, bloom-scroll, ceq, digifab-quoting, primavera3d, fortuna, avala, forgesight, NPM Registry, Platform Infrastructure, Yantra4D, tezca, Enclii, Janua.
- 🟡 **PR-1**: Most cards show "No recent deployments" even when the dashboard reports a deploy timestamp for the same project (e.g., factlas: dashboard "1d ago", projects "No recent deployments"). Inconsistency in `latest_deployment` resolution.
- 🟢 **PR-2**: List view is much less informative than dashboard cards (no service rollups). Deliberate or oversight?

---

### `/services`
- 🔴 **SV-1**: Page hangs at "Loading services…" indefinitely. Network shows no GET `/v1/services` call. The component never fires its data fetch.
- 0 console errors on this page (silent failure).

---

### `/deployments`
- ✅ Renders an "Active Deployments" + "Deployment History" list with many entries and a mix of green/red/yellow status badges. Visually consistent.
- 🟡 **DP-1**: Some entries show no project/service name pairing — verify against `/v1/deployments` shape and confirm 1:1 with active K8s deployments.

---

### `/observability`
- 🔴 **OB-1**: Page hangs at "Loading observability data…" indefinitely. Top-level component never resolves.

---

### `/audit`
- 🔴 **AU-1**: Banner: `Partial results: some upstream audit sources are unavailable — switchyard (switchyard: fetch audit_logs: pq: invalid input syntax for type inet: "")`. Real Postgres type error: column expects `inet`, gets empty string. Backend bug.
- 🔴 **AU-2**: 0 events shown in the default 7-day window even though we just logged in, navigated >10 routes, and the page even loads on this very session. Audit log is either not capturing user actions or `AU-1` is suppressing the result.
- ✅ Filters layout looks correct (Since/Until/Category/Source/Actor/Target).

---

### `/activity`
- ✅ Renders many events. Shape looks right.
- 🟢 **AC-1**: Alert/Activity feed rendering compresses timestamps and truncates resource names — minor.

---

### `/databases`
- ✅ Renders 23 database addons across many projects, all "ready". Each card has Project/Memory/Credentials/Delete.
- 🟡 **DB-1**: "Memory: shared" shown for many addons — meaning unclear; user-facing label should be either `256Mi` (concrete) or `Shared (cluster pool)` (explicit), not the bare word "shared".

---

### `/teams`
- 🔴 **TM-1**: "No teams yet" empty state for master admin. Confirms XC-2: there are no teams in the DB; all 25 projects have `team_id = NULL` and appear under the synthetic "personal" scope.
- ✅ Empty-state copy is honest.

---

### `/settings`
Profile tab observed; other tabs (Notifications, Security, API Tokens) not exercised live.
- 🔴 **ST-1**: Avatar shows literal letter `U`. Should be `A` (admin's first initial) or the actual avatar.
- 🔴 **ST-2**: Full Name field is **empty**. (User has a name in Janua.)
- 🔴 **ST-3**: Email field is **empty** while caption reads "Email cannot be changed". Show the email value, then mark immutable.
- 🔴 **ST-4**: Role pill is **blank** (just `Role:` with no value). Should read `admin` (or `Master Admin`).
- 🔴 **ST-5**: "Member since **Unknown**". `users.created_at` exists in the DB; the API/UI just isn't wiring it.

---

### `/usage`
- 🔴 **US-1**: "Live Resource Usage" shows "Metrics collection unavailable. Install metrics-server in your cluster". The cluster *does* have metrics-server installed. Either the API isn't using it, the env var isn't wired, or the UI is fall-through-only.
- 🔴 **US-2**: "Compute 597.4 / 500.0 GB-hours" with "+$4.87 overage charges" — fabricated allocation for a self-hosted master admin (XC-5).
- 🔴 **US-3**: "Storage 46.5 / 10.0 GB" with "+$9.12 overage" — same.
- 🟡 **US-4**: "Custom Domains: 8.0 / ∞ domains" — but the platform clearly hosts more than 8 domains (`hcm.madfam.io`, `app.dhan.am`, `kf-admin.madfam.io`, …). Inventory under-reports (also see DM-1).
- ✅ Period range "2026-05-01 — 2026-05-31" correct.
- ✅ "Customer billing is handled by Dhanam." footer is honest.

---

### `/domains`
- 🔴 **DM-1**: "Total domains: 8" with "0% healthy". Seven of eight rows show `Last verified: never`. The ecosystem actually has more than 8 domains; the inventory is incomplete.
- 🔴 **DM-2**: "% healthy: 0%" with all rows status `Unknown` and Cert expiry `unknown`. We know `dhan.am`, `app.dhan.am`, `hcm.madfam.io` are serving traffic right now. Health probe / Cloudflare poll is broken.
- 🔴 **DM-3**: Missing rows for `enclii.dev`, `app.enclii.dev`, `admin.enclii.dev`, `docs.enclii.dev`, `status.enclii.dev`, `npm.madfam.io`, `analytics.madfam.io`, all admin portals.
- 🟡 **DM-4**: "synced just now" + per-row "Last verified: never" is a contradiction; the sync is happening but its result isn't being persisted/read.

---

### `/integrations/janua`
- 🟡 **IG-1**: Page is essentially marketing copy ("Pricing $0/month / Pro Features +$49/month") inside the *protected* app surface. For master-admin / self-hosted, this is misplaced. Should be a status/health view (Janua reachable? OAuth client active? user count?), not a sales page.

---

### `/templates`
- ✅ Honest empty state ("No templates found"). 0 console errors.

---

### Auth flows (`/login`, `/signup`, `/auth/callback`, `/onboarding`, `/signup/verify`)
Not browser-walked in this pass beyond `/login` (verified PKCE redirect to Janua → callback → dashboard works).

---

## Triage / remediation backlog

| ID | Sev | Where to fix | Est |
|----|-----|--------------|-----|
| XC-1 | 🔴 | `apps/switchyard-ui/contexts/ScopeContext.tsx` (frontend-only, conditional label by role) | S |
| XC-2 | 🔴 | `apps/switchyard-api` new admin endpoint + middleware + UI switcher | L |
| XC-3 | 🔴 | `/v1/csrf` impl or remove caller; Sentry handler return 200+empty when ungated | M |
| XC-4 | 🟡 | Per-page: surface the network error, add retry, add 30s soft timeout fallback copy | M |
| XC-5 | 🟡 | `/v1/usage` server-side: skip plan limits for admin; UI: hide overage UI when limits null | M |
| D-1, D-4 | 🔴 | Same as XC-5 (Usage widget + Alerts derive from it) | (folded) |
| D-2 | 🟡 | Wire dashboard System Health to `/v1/observability/health` | S |
| SV-1 | 🔴 | Trace `/services` page bootstrap, fire its missing GET, add error boundary | M |
| OB-1 | 🔴 | Same shape as SV-1 for observability landing | M |
| AU-1 | 🔴 | Fix Postgres `inet` cast — accept null/empty, not empty string | S |
| AU-2 | 🔴 | Verify audit pipeline ingests user actions (depends on AU-1) | M |
| ST-1..5 | 🔴 | Wire `/v1/users/me` → settings form (name, email, role, created_at) + avatar fallback | S |
| US-1 | 🔴 | Backend: have usage API consult metrics-server | M |
| US-2..4 | 🔴 | Same as XC-5 | (folded) |
| DM-1..4 | 🔴 | Domain inventory: complete sync from Cloudflare, fix verify pipeline | M |
| PR-1 | 🟡 | Reconcile "latest deployment" between `/v1/projects` and `/v1/projects/:slug/services` | S |
| DB-1 | 🟡 | Replace bare "shared" with explicit label/tooltip | XS |
| IG-1 | 🟡 | Replace marketing copy with operational dashboard for self-hosted | S |
| D-5 | 🟢 | Disambiguate "+ Add New…" copy / split-button | XS |

Folded means the underlying issue is the same as a parent finding.

## Remediation status (updated 2026-05-02)

After the audit landed, two follow-on rounds shipped.

**Round 1 — `bc4c69b9`**
- ✅ XC-1 — scope-switcher label / Hobby badge for master-admin (`apps/switchyard-ui/contexts/ScopeContext.tsx`, `components/navigation/scope-switcher.tsx`)
- ✅ XC-2 *backend* — migration `023_admin_acting_sessions`, repository, four endpoints (`GET /v1/admin/tenants{,active}`, `POST .../:slug/enter`, `POST .../exit`), `audit_logs.acting_on_behalf_of_team_id` column. Frontend wiring landed in round 2.
- ✅ XC-3 — `/v1/csrf` handler + Sentry handler returns 200+`{ enabled, errors, stats, reason }` instead of 503; 21 console errors per dashboard load eliminated.
- ✅ AU-1 — `normalizeInet` boundary helper for the `inet` Postgres type; reader/writer/source SELECT all updated.
- ✅ ST-1..5 — settings profile renders avatar initial, name, email, role pill ("Master Admin"), member-since placeholder.
- ✅ SV-1 — `/services` rewritten to fan-out projects→services with `Promise.allSettled` + per-project Retry banner.
- ✅ OB-1 — `/observability` per-panel state; one panel's 503 no longer freezes the page.

**Round 2 — `eecf5728`**
- ✅ XC-2 *frontend* — `ScopeContext.fetchScopes` admin path, switcher routes admin team-clicks through `enterTenant`, `AdminActingBanner` mounts above the topbar with countdown.
- ✅ D-1 — Usage widget switches to absolute units for admin scope.
- ✅ D-4 — Plan-overage alerts filtered out of the alerts sidebar for admin scope.
- ✅ US-1..4 — `/usage` cost pill / donut / bar chart / cost table all hidden for admin scope; "Cluster utilization" header.

**Round 3 — `<this commit>`**
- ✅ XC-2 *handler tests* — 9 new sqlmock-backed tests for `admin_tenants_handlers.go` covering nil-repos guard, ListTenants happy path with project counts, EnterTenant slug requirement + cookie shape + duration cap, ExitTenant cookie clearing, ActiveTenant no-session / active-session / dangling-session paths.
- ✅ XC-2 *CLI parity* — `enclii admin tenants list/active/enter/exit` so operators can act-as from a script with the same surface as the web app.
- ✅ Doc updates — `docs/cli/commands/admin.md` documents the new tenants subtree; this audit file and `master-admin-tenant-switching.md` updated with shipped status.

## Still open (queued, not shipped)

- 🔴 **DM-1..4** — `/domains` inventory + Cloudflare-driven verification pipeline. Needs real backend integration.
- 🔴 **Tenant-filter middleware** — the `ax_acting_as` cookie is set, the session is recorded, the banner renders, but the existing `/v1/projects` and friends do **not yet** filter by `acting_team_id`. Next architectural step: middleware that reads the cookie, validates the session, sets `c.acting_team_id`, and updates each list-style handler to consult it.
- 🟡 **PR-1** — Projects vs Dashboard "latest deployment" inconsistency.
- 🟡 **DB-1** — Replace bare "shared" memory label on /databases.
- 🟡 **IG-1** — Replace marketing copy on `/integrations/janua` with operational status.
- 🟢 **D-5** — Disambiguate "+ Add New…" CTA copy.
- Cross-app audits: **admin.enclii.dev**, **status / docs / landing / npm / analytics**.
