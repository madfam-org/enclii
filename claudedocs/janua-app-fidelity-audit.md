# app.janua.dev fidelity audit — 2026-05-02

Authenticated session: `admin@madfam.io` (master Janua admin).
Browser: Playwright. Screenshots in `claudedocs/janua-screenshots-2026-05-02/`.
Source repo: `/Users/aldoruizluna/labspace/janua/apps/dashboard` (Next.js 14 App Router).

Severity: 🔴 critical · 🟡 important · 🟢 nit · ✅ verified-correct.

Routes browser-walked:
- `/login`, `/` (Overview, Users, Sessions, Organizations, Webhooks, Audit tabs)
- `/profile`, `/audit-logs`, `/users`, `/organizations`
- `/settings`, `/settings/api-keys`, `/settings/sso`, `/settings/oauth-clients`, `/settings/system`, `/settings/billing`
- `/compliance`

---

## Cross-cutting

### XJ-1 🟡 CSP blocks Google Fonts + Cloudflare Insights on every page
- **Where**: every protected route + `/login`. **2 console errors per pageload.**
- **Evidence**: `Loading the stylesheet 'https://fonts.googleapis.com/css2?family=Inter…' violates style-src 'self' 'unsafe-inline'` and a similar message for `static.cloudflareinsights.com/beacon.min.js`.
- **Why wrong**: CSP `style-src 'self' 'unsafe-inline'` lacks `style-src-elem` (so `style-src` falls back) and lacks the Google Fonts + Cloudflare hosts.
- **Fix options**: extend CSP allowlist to include those origins; OR self-host Inter and drop Google Fonts; OR drop Cloudflare Insights if unused.
- **Audit precedent**: enclii Session 98 fixed the equivalent issue on the admin portals — the dashboard app didn't get the same treatment.

### XJ-2 🔴 Several `/api/v1/admin/*` endpoints fail with 500/404/network-error
Affecting Users, Sessions, Organizations, dedicated `/users` route, dedicated `/organizations` route. Reproducible by clicking each tab. Without these, the dashboard's stat cards (Total Identities, Active Sessions, Organizations) cannot be cross-validated, and the operator can't actually manage anything beyond OAuth Clients and Webhooks.
- `GET /api/v1/admin/users` → 500
- Sessions endpoint → 404
- Organizations endpoint → Network request failed

### XJ-3 🟡 Plan-tier UI applied to a self-hosted master admin
Mirrors the **XC-5** finding from app.enclii.dev. `/settings/billing` shows "Current Plan: Free" with `Up to 1,000 monthly active users / 1 organization`, plus four upgrade tiers (Community / Pro $49 / Scale $199 / Enterprise). The master admin of a self-hosted Janua isn't on a plan and shouldn't see upgrade CTAs. Recommendation: hide the plan/billing surface for the master admin scope.

### XJ-4 🟡 IP-address capture broken across audit + dashboard surfaces
Mirrors **AU-1** from app.enclii.dev. Audit tab shows `Unique IPs: 0`; per-row IP column is `–` for every entry. Likely the same `inet` empty-string normalization issue (audit row writer is passing `""` to a Postgres `inet` column or similar).

---

## Per-route findings

### `/login` ✅ + 🟡 XJ-1
Form works, SSO redirect works, JIT session persists. Two CSP errors per load.

### `/` Dashboard / Overview tab

#### J-D1 🟡 "+0% from last month" on every stat card
All four cards display `+0% from last month` regardless of count. The change calculation is hardcoded or the API doesn't return prior-period values; UI defaults to 0%. Either compute real deltas server-side or hide the line until comparison data exists.

#### J-D2 🟡 Active Sessions: 25 vs Total Identities: 1 vs Max Concurrent Sessions: 5
With one identity and `Max Concurrent Sessions: 5` configured in System Settings, an Active Sessions count of 25 means either (a) the limit isn't being enforced, or (b) the count includes expired-but-not-cleaned-up rows. Either way, the number is misleading.

#### J-D3 ✅ Recent Activity feed accurate
10 entries, all `admin@madfam.io`, spanning "Just now → 4/17/2026". Consistent with the single-identity state.

#### J-D4 🟢 Organizations: 0 — accurate but confusing
The Janua model doesn't auto-create "organizations" for OAuth clients. 0 is honest. Recommendation: empty-state copy "No organizations created yet. OAuth clients are managed separately under [Settings → OAuth Clients]."

#### J-D5 ✅ Header surfaces real identity
"Welcome back, admin@madfam.io". Unlike app.enclii.dev's "Personal Account (Hobby)" issue, this is honest.

### `/` Users tab + `/users` (dedicated)

#### J-U1 🔴 Both fail with `Request failed with status code 500`
Reproducible: `GET https://api.janua.dev/api/v1/admin/users?page=1&per_page=20` → 500. The API endpoint is broken. Operator cannot list, search, suspend, or reset any user beyond themselves.

### `/` Sessions tab

#### J-S1 🔴 `Failed to Load Sessions — Request failed with status code 404`
Endpoint missing entirely. Yet the dashboard claims 25 active sessions exist. The card and the list view disagree on whether the data even exists.

### `/` Organizations tab + `/organizations` (dedicated)

#### J-O1 🔴 Both fail: `Failed to Load Organizations — Network request failed`
Network-level error rather than API status code. Could be CORS (the System Settings page only allowlists tezca.mx domains — see J-SY1) or upstream connection refused.

### `/` Webhooks tab ✅
Honest empty state: "0 Total Endpoints, 0 Active, 26 Event Types." All 26 event types listed (user/session/organization/security/oauth/admin/system). Add Webhook button works (not exercised). ✓

### `/` Audit tab

#### J-AT1 🟡 Stat counters vs row count contradict
Header says `Total Events: 2 / Action Types: 1` but the table shows **6 rows** spanning Feb–Apr 2026 with mixed actions (`oauth_client_created`, `oauth_client_secret_rotated`, `oauth_client_updated`). "Top Actions (Last 30 Days)" lists only `oauth_client_created (2)` — that's the source of "Total Events: 2" (windowed to 30 days). Conflating "events in last 30 days" with "Total Events" is misleading.

#### J-AT2 🟡 Unique IPs: 0 — IP capture broken
Every row shows `–` for IP. Likely the same `inet` empty-string normalization bug pattern as enclii AU-1.

### `/audit-logs` (dedicated)

#### J-AL1 🔴 Shows "0 events found" while Audit tab shows 6
Two endpoints, contradictory data. The dedicated `/audit-logs` page calls a different endpoint than the dashboard tab and returns nothing. Plus the Recent Activity card on `/` shows ~10 sign-in events that don't appear on `/audit-logs` either. The page's "Live Feed" toggle and "Export" button are present but the data isn't.

### `/settings`
#### J-ST1 ✅ Settings hub renders 14 cards with clear descriptions.

### `/settings/api-keys`

#### J-AK1 🔴 503 banner + "No API keys yet" empty state shown simultaneously
The page shows an error banner ("Request failed with status code 503") AND the empty-state illustration ("No API keys yet — Create your first API key to get started"). If the request failed, the empty state is misleading — there could be real keys hidden behind the failure. Either render the error state OR the empty state, never both.

### `/settings/oauth-clients` ✅ — best route on the app

Lists **27 OAuth clients** across the ecosystem, all `Active`, properly grouped by Confidential/Public, with masked secrets, scopes (`authorization_code`, `refresh_token`, `openid`, `profile`, `email`, `offline_access`), and creation timestamps spanning 3/7/2026 — 4/28/2026. This page works correctly and shows real data.

Notable list (page 1 of 2): Pravara MES, Symbiosis HCM, Avala API, Selva Office (AutoSwarm), Rondelio, Coforma Studio, Janua Dashboard, Tezca Web, Forgesight App, Yantra4D Studio, Fortuna Web, Digifab Quoting, Forgesight Admin, Phynd CRM API, Pravara Dashboard, PhyndCRM, Deal Sniper, PravaraMES Admin, AutoSwarm Admin, Karafiel Admin (+6 more on page 2).

### `/settings/sso`

#### J-SS1 🔴 "No organization found. Please join or create an organization first."
Master admin is blocked from SSO config because Janua reports 0 organizations. But 27 OAuth clients exist (J-OC1) and `Organizations: 0` is the literal stat (J-D4). Either the SSO page should treat the master admin as having an implicit "platform" org, OR Janua should auto-create an org during admin onboarding. Today the SSO surface is fully gated behind an empty-state guard the master admin can't escape from this UI.

### `/settings/system` ✅ + 🟡 J-SY1
Page renders all four panels (CORS Origins, Session Settings, Password Policy, Rate Limiting) with editable forms.

#### J-SY1 🟡 CORS Origins shows only 2 entries
Allowlist contains only `https://admin.tezca.mx` and `https://tezca.mx`. But 27 OAuth clients exist and they map to many other origins (kf-admin.madfam.io, mes-admin.madfam.io, agents-admin.madfam.io, 4d-admin.madfam.io, dhan.am, etc. per memory). Other admin portals likely fail CORS-protected requests. Either backfill the CORS list from the OAuth client redirect_uris, or document that CORS is configured at the gateway and this UI is lying.

#### J-SY2 🟡 Max Concurrent Sessions: 5 vs Overview's 25 active
The configured limit is 5 per user; the dashboard claims 25 active for a single user. The limit isn't being enforced, OR the Overview count includes orphaned/expired rows. Reconcile.

### `/settings/billing`

#### J-B1 🔴 Billing surface for a self-hosted master admin
Same XC-5 pattern as enclii. Shows "Current Plan: Free" with the four-tier upgrade ladder (Community/Pro $49/Scale $199/Enterprise) and "Auto-Renewal: Enabled" despite no payment method on file.

#### J-B2 🔴 Billing Period Start: Invalid Date / Billing Period End: Invalid Date
Date parsing bug. Either the API returns null and the UI doesn't handle it, or the format string is wrong.

### `/profile`

#### J-P1 🟡 MFA: Disabled on the master admin
The single highest-privilege account on the platform has 2FA off. That's a security concern by itself, and arguably a fidelity issue: any "Security posture" rollup elsewhere would be inflated if it doesn't account for this.

#### J-P2 ✅ Profile fields all populated correctly
Avatar shows "AM" initials, name "Admin MADFAM", email verified, status active, created 2026-02-06, updated 2026-04-17. Unlike app.enclii.dev's settings page (ST-1..5), this page works.

### `/compliance` ✅
Privacy & Compliance tab renders correctly with toggles for Analytics, Activity Tracking, Marketing Emails, Email Notifications, Third-Party Sharing, plus Profile Visibility radio. GDPR Compliant badge present. Honest UX.

---

## Triage / remediation backlog

| ID | Sev | Where | Est |
|----|-----|-------|-----|
| XJ-1 | 🟡 | `apps/dashboard/middleware.ts` or next.config CSP | XS |
| XJ-2 / J-U1 | 🔴 | `apps/api/internal/handlers/admin_users.go` (or equivalent) — investigate 500 | M |
| J-S1 | 🔴 | Sessions admin endpoint missing — implement or remove the tab | M |
| J-O1 | 🔴 | Organizations admin endpoint network error — investigate | M |
| XJ-3 / J-B1..2 | 🟡 | Hide billing tab + plan UI for master-admin scope; fix Invalid Date | S |
| J-AL1 | 🔴 | `/audit-logs` page consults a different endpoint than the dashboard tab — unify | M |
| J-AT1 | 🟡 | Audit tab counters: rename "Total Events" → "Last 30 Days" | XS |
| XJ-4 / J-AT2 | 🟡 | IP-address capture (likely `inet` empty-string normalization) | S |
| J-AK1 | 🔴 | API Keys page: don't show empty state when request failed | XS |
| J-SS1 | 🔴 | SSO page: don't gate master-admin behind "no org" empty state | S |
| J-SY1 | 🟡 | CORS Origins inventory: backfill from OAuth client list | S |
| J-SY2 | 🟡 | Reconcile Active Sessions count with Max Concurrent Sessions limit | S |
| J-D1 | 🟡 | Stat-card deltas: hide or compute server-side | XS |
| J-D2 | 🟡 | Active Sessions count: filter expired rows | S |
| J-P1 | 🟡 | Master admin MFA: enable for security posture | XS (operator action) |

## Patterns vs the enclii audit

The Janua audit reproduces several patterns called out in `claudedocs/app-fidelity-audit.md`:

| Enclii finding | Janua equivalent |
|----------------|------------------|
| XC-5 plan UI for self-hosted admin | XJ-3 / J-B1 |
| AU-1 inet empty-string crash | XJ-4 / J-AT2 |
| ST-1..5 empty profile | None — Janua's profile page works ✓ |
| OB-1 / SV-1 hung "Loading…" | J-S1 / J-O1 (different — these surface explicit error UIs, which is better) |
| AU-2 audit shows 0 events | J-AL1 (Janua has it too, dedicated route only) |

## Still open (queued, not shipped)

- **admin.janua.dev** — same browser-verified audit pattern. Different app under `/Users/aldoruizluna/labspace/janua/apps/admin`.
- **api.janua.dev** — health, OpenAPI surface, response shapes vs documented contract.
- **docs.janua.dev** — content accuracy vs current code.
- **janua.dev** (marketing) — claim-vs-reality.
- **edge-verify** — if exposed to a public surface.
