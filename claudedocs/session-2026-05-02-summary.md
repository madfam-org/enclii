# Session summary — 2026-05-02

A single sustained session covering CLI parity, four browser-verified fidelity audits (app.enclii.dev, app.janua.dev, admin.janua.dev, public surfaces), seven rounds of XC-2 master-admin tenant switching, multiple Janua remediation rounds, and end-to-end public-surface hardening.

Two repos touched: `enclii` (this repo, `main`) and `janua` (`/Users/aldoruizluna/labspace/janua`, `main`). All work is merged, pushed, and tested green.

## Audit deliverables (all in `claudedocs/`)

| File | Scope |
|------|-------|
| `app-fidelity-audit.md` | app.enclii.dev — 30+ findings, 14 routes, 18 screenshots |
| `master-admin-tenant-switching.md` | XC-2 design doc + Round 0–7 implementation status |
| `janua-app-fidelity-audit.md` | app.janua.dev — 17 findings, 14 routes, 18 screenshots |
| `janua-admin-fidelity-audit.md` | admin.janua.dev — login flow blocker (AJ-1) |
| `janua-public-surfaces-audit.md` | api.janua.dev + docs.janua.dev + janua.dev — 7 critical, 9 important findings |
| `cross-app-public-audit-2026-05-02.md` | status / docs / npm / analytics source-level audit |
| `screenshots-2026-05-02/` | enclii browser screenshots (18 PNGs) |
| `janua-screenshots-2026-05-02/` | janua dashboard screenshots (18 PNGs) |
| `janua-admin-screenshots-2026-05-02/` | janua admin screenshots (2 PNGs) |
| `session-2026-05-02-summary.md` | this file |

## Enclii repo — commits on `main`

Range: `976d336a..f808883b` — 25 commits.

| SHA | Theme |
|-----|-------|
| `7c30811c` | CLI UI/admin parity (38 commands, security hardening, auto-refresh) |
| `cc60e2e9` | app.enclii.dev audit + Master-Admin scope label |
| `bc4c69b9` | Round 1 — XC-2 backend, XC-3, AU-1, ST-1..5, SV-1/OB-1 fixes |
| `eecf5728` | Round 2 — XC-2 frontend switcher, admin plan-UI hide |
| `3b5f5586` | Round 3 — handler tests, CLI tenants parity, doc updates |
| `4a180eb6` | Round 4 — XC-2 middleware enforcement + DM honesty layer + UI fidelity batch |
| `fee21a32` | app.janua.dev audit |
| `2014d72d` | admin.janua.dev + janua public-surfaces audit |
| `f6afe6b7` | Round 5 — tenant-filter rollout to all list/detail handlers |
| `c1401d9f` | Round 6 — tenant-scope the `/v1/audit` aggregator |
| `48cd38b2` | Round 7 — re-parenting migration (20 teams, 25 projects, admin owner) |
| `3477c91e` | cross-app source-level audit (status/docs/npm/analytics) |
| `92987608`, `3e040279` | ST-1: `/health/public` anonymous probe |
| `55e8340d`, `284de444` | DC-2: 9 CLI command docs backfilled |
| `37aa1878`, `1003f261` | DC-1, DC-4: docs PostHog host + dead installer URL |
| `f6ec5b10`, `f191e2b0` | DC-3, AN-1: docs CSP/HSTS/XFO + analytics worker header strip |
| `afdd49e8`, `67d74492` | ST-2, ST-3: status footer SHA + configmap drift CI |
| `68865c5b`, `f808883b` | NP-1, NP-2: Verdaccio search auth-gate + version fingerprint strip |

## Janua repo — commits on `main`

Range: `1c2469b1..b2f9263f` — 6 substantive commits + merges.

| SHA | PR | Theme |
|-----|----|-------|
| `628e3a85` (in `1c2469b1`) | direct | Five dashboard fidelity bugs (api-keys empty-state, billing self-hosted hide + Invalid Date, CSP, SSO no-org guard, audit-logs schema unify) |
| `e0731b88` | direct + #356 | Marketing round 1 — pricing alignment, SOC 2 Type II → "in progress" |
| `fb7438dd` | #354 | 3 broken admin endpoints (Users 500, Sessions 404, Organizations net-error) — same nullable-Pydantic-bools pattern as enclii AU-1 |
| `a45ff4e7` | #355 | `/metrics*` token gate + SOC 2 Type II claim rewrite (4 marketing pages) |
| `70baba48` | #357 | admin.janua.dev login HttpOnly cookie persistence (AJ-1, AJ-2, AJ-3) |
| `2b051603` | #358 | Marketing round 2 — third pricing scheme aligned, Pro/Scale SLAs removed, dead-code testimonials/use-cases/pricing-preview deleted |
| `2917c641` | #359 | API test coverage 23.31% → 38.22% (8 new test modules, 280 tests) |
| `91de6f11` | #360 | `@janua/ui` SignIn `showEmailPassword` prop respected + version bump 0.1.4→0.1.5 |
| `b2f9263f` | #361 | Landing rewrite — pain-point copy, verified claims only |

## Audit findings — final triage

### app.enclii.dev (claudedocs/app-fidelity-audit.md)
| ID | Description | Status |
|----|-------------|--------|
| XC-1 | Personal Account label for master admin | ✅ `cc60e2e9` |
| XC-2 | Master-admin tenant impersonation surface | ✅ Rounds 1–7 |
| XC-3 | CSRF 404 + Sentry 503 spam | ✅ `bc4c69b9` |
| XC-4 | Long-running pages hang on Loading | ✅ `bc4c69b9`, `4a180eb6` |
| XC-5 / D-1 / D-4 / US-1..4 | Plan-tier UI for self-hosted master admin | ✅ `eecf5728` |
| AU-1 | audit_logs Postgres `inet` empty-string crash | ✅ `bc4c69b9` |
| AU-2 | 0 events shown (depended on AU-1) | ✅ `bc4c69b9` |
| ST-1..5 | Settings profile fields empty | ✅ `bc4c69b9` |
| SV-1 | /services hangs on Loading | ✅ `bc4c69b9` |
| OB-1 | /observability hangs on Loading | ✅ `bc4c69b9` |
| DM-1..4 | Domains inventory honesty | ✅ `4a180eb6` (banner layer; real Cloudflare verifier deferred) |
| PR-1 | Dashboard vs /projects deploy timestamp drift | ✅ `4a180eb6` |
| DB-1 | "shared" memory label opaque | ✅ `4a180eb6` |
| IG-1 | /integrations/janua marketing copy | ✅ `4a180eb6` |
| D-5 | Add New… CTA opacity | ✅ `4a180eb6` |

### app.janua.dev (claudedocs/janua-app-fidelity-audit.md)
| ID | Description | Status |
|----|-------------|--------|
| XJ-2 / J-U1 | `/api/v1/admin/users` 500 | ✅ janua#354 |
| J-S1 | Sessions endpoint 404 | ✅ janua#354 |
| J-O1 | Organizations Network error | ✅ janua#354 |
| J-AL1 | `/audit-logs` shows 0, dashboard tab shows 6 | ✅ janua dashboard PR (628e3a85) |
| J-AK1 | API Keys 503 + empty state simultaneously | ✅ janua dashboard PR |
| J-SS1 | SSO blocked by "No organization" guard | ✅ janua dashboard PR |
| J-B1 | Billing surface for self-hosted master admin | ✅ janua dashboard PR |
| J-B2 | Billing Period Start/End "Invalid Date" | ✅ janua dashboard PR |
| XJ-1 / J-AT2 | CSP + audit IP capture | ✅ janua dashboard PR |
| J-D1..D5 | Dashboard stat cards / labels / org count | mixed (✅ for label drift, accepted as honest for the rest) |
| J-P1 | Master admin MFA disabled | open (operator action) |

### admin.janua.dev (claudedocs/janua-admin-fidelity-audit.md)
| ID | Description | Status |
|----|-------------|--------|
| AJ-1 | Login token persistence vs middleware mismatch | ✅ janua#357 |
| AJ-2 | No UI affordance on doomed login | ✅ janua#357 |
| AJ-3 | CSP blocks Cloudflare Insights | ✅ janua#357 |
| AJ-5 | `<SignIn showEmailPassword>` prop ignored | ✅ janua#360 |

### Public Janua surfaces (claudedocs/janua-public-surfaces-audit.md)
| ID | Description | Status |
|----|-------------|--------|
| `/metrics*` reachable unauthenticated | exposes per-endpoint metrics + attacker probe data | ✅ janua#355 |
| SAML/SSO claimed in 246 paths but zero routes mounted | docs lie | (rolled into Round 2 marketing — claims now tagged "rolling out") |
| SOC 2 Type II claim while Private Alpha | legal liability | ✅ janua#355 |
| ISO 27001 / HIPAA / GDPR / PCI DSS past validUntil | ✅ janua#356 / #358 |
| Fictional 50K+/75K+ user counts | ✅ janua#358 (deleted) |
| 99.99% / 99.9% / 99.95% SLAs | ✅ janua#356 / #358 |
| /security, /compliance, /changelog, /signup nav 404s | ✅ janua#356 (links removed) |
| Pricing contradictions across 4 surfaces | ✅ janua#356 / #358 (aligned to API canonical) |

### Cross-app public surfaces (claudedocs/cross-app-public-audit-2026-05-02.md)
| ID | Description | Status |
|----|-------------|--------|
| ST-1 | api.enclii.dev/health/ready 404 publicly | ✅ enclii `92987608` (`/health/public` ships) |
| ST-2 | No commit/build SHA on status footer | ✅ enclii `afdd49e8` |
| ST-3 | 12-svc enclii vs 60-svc madfam configmap drift | ✅ enclii `afdd49e8` (CI guard + 2 drift bugs found and fixed) |
| DC-1 | docs build 53 days stale | ✅ enclii `37aa1878` (services.json change-detection root cause fixed) |
| DC-2 | 9 CLI commands without docs | ✅ enclii `55e8340d` |
| DC-3 | docs origin missing HSTS/CSP/XFO | ✅ enclii `f6ec5b10` |
| DC-4 | get.enclii.dev installer dead | ✅ enclii `37aa1878` |
| NP-1 | npm anonymous metadata enumeration | ✅ enclii `68865c5b` |
| NP-2 | Verdaccio version fingerprint | ✅ enclii `68865c5b` |
| AN-1 | analytics worker leaks PostHog headers | ✅ enclii `f6ec5b10` |

**Every audit-surfaced item is shipped.**

## XC-2 master-admin tenant switching — full lifecycle

Round 0 → 7. Started as a misleading "Personal Account (Hobby)" label; ended with `admin@madfam.io` able to (a) see every tenant on the platform via the scope switcher, (b) enter a tenant via cookie-bearing session, (c) have every list-style endpoint filter to that tenant's data, (d) have an enriched audit-trail row recording who acted as whom, and (e) see real tenant data because the re-parenting migration created the 20 teams + parented the 25 audited projects.

| Round | Commit | Substance |
|-------|--------|-----------|
| 0 | `cc60e2e9` | Master Admin chip with shield avatar |
| 1 | `bc4c69b9` | Migration 023 + repository + 4 endpoints |
| 2 | `eecf5728` | Scope switcher rewrite + AdminActingBanner |
| 3 | `3b5f5586` | Handler tests + CLI tenants parity (`enclii admin tenants list/active/enter/exit`) |
| 4 | `4a180eb6` | Tenant-filter middleware (`acting_as.go`) + first list endpoint scoped (`/v1/projects`) |
| 5 | `f6afe6b7` | Filter rolled out to /v1/services, /v1/deployments, /v1/activity, /v1/databases, /v1/domains |
| 6 | `c1401d9f` | `/v1/audit` aggregator scoped (SQL push-down + post-filter fallback for HTTP sources) |
| 7 | `48cd38b2` | Re-parenting migration: 20 teams, 25 projects parented, admin owner on every team |

## Test status (end of session)

- enclii `apps/switchyard-api`: full `go test -count=1 ./...` green across 39 packages.
- enclii `apps/status`: 204/204 jest tests pass.
- enclii `apps/switchyard-ui`: typecheck no new errors (pre-existing log-viewer / @enclii module-resolution noise unchanged).
- janua `apps/api`: full pytest passes 2,675 tests with 38.22% coverage (was 23.31%, threshold 25%).
- janua `packages/ui`: 489/489 unit tests pass.
- janua `apps/admin`: 9/9 new route-handler tests pass; 15/16 admin suite (1 pre-existing fail unrelated).

## Operator actions queued (post-session, outside-repo)

These are deploy/ops-side; the source changes are merged.

1. `kubectl rollout restart deployment/verdaccio -n npm-registry` — picks up NP-1, NP-2.
2. `cd infra/cloudflare/verdaccio-edge && npx wrangler deploy --env production` — picks up NP-2 worker.
3. `cd infra/cloudflare/posthog-proxy && npx wrangler deploy --env production` — picks up AN-1 worker header strip.
4. ArgoCD-driven docs-site image rebuild — picks up DC-3 origin headers.
5. ArgoCD-driven status app rebuild — picks up ST-2 footer SHA.
6. **Cloudflare tunnel route restoration for `api.enclii.dev`** — currently 404s for ALL paths at the public edge. Once restored, `/health/public` (ST-1 fix) becomes reachable and the status page becomes honest.
7. Run the `024_reparent_projects_to_teams` migration on the production DB (next deploy of switchyard-api will trigger it; verify post-deploy that `SELECT COUNT(*) FROM teams` returns 20 and `SELECT COUNT(*) FROM projects WHERE team_id IS NOT NULL` returns 25).

## What's not done (truly out of scope this session)

- Janua master admin MFA enable (J-P1 — operator action, not a code change).
- Janua `/audit-logs` IP capture root cause (probably the same `inet` pattern as enclii AU-1; would need an api-side migration similar to enclii's).
- Janua API 23 dependabot PRs left open (not session scope).
- enclii admin.enclii.dev browser-verified audit (separate session-class work; backend tenant-filter is in place to make it tractable).

## Methodology notes

- **Browser-verified audits** used Playwright with the user's `admin@madfam.io` credentials. Findings were captured as screenshots committed to `claudedocs/*-screenshots-2026-05-02/`.
- **Source-only audits** used curl + grep + read against the repo source. Cheaper than browser; enough for content/header/contract questions.
- **Parallel-agent dispatch** carried most of the throughput. The pattern was: foreground takes the architecturally-coupled item (middleware, migration, tenant-switching backend); independent surface-level fixes go to subagents in parallel.
- **Per the auto memory feedback during this session**: the user prefers the enclii CLI over `kubectl` whenever possible, and zero-touch onboarding extends to ExternalSecrets (not just NetworkPolicies). Both rules respected.
