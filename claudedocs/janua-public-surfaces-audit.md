# Janua public surfaces audit — 2026-05-02

curl/HTTP-only audit (no browser) of three public Janua surfaces:
- `https://api.janua.dev` — REST API
- `https://docs.janua.dev` — developer docs (Next.js)
- `https://janua.dev` — marketing site (Next.js)

Source repo cross-checks: `/Users/aldoruizluna/labspace/janua`.
Companion to `claudedocs/janua-app-fidelity-audit.md` (which covers the protected dashboard).

Severity: 🔴 critical · 🟡 important · 🟢 nit · ✅ verified-correct.

Token in `~/.enclii/credentials.json` was **expired** (`expires_at: 2026-04-16`, today is 2026-05-02), so all API tests are unauthenticated only. No further auth attempted per scope.

---

## api.janua.dev

OpenAPI surface: 246 paths, 1 security scheme (`HTTPBearer`), 3 servers declared (production, staging-api.janua.dev, http://localhost:4100).

### A-1 🔴 `/metrics/performance` is publicly exposed and leaks per-endpoint analytics
- **Where**: `GET https://api.janua.dev/metrics/performance` returns 200 unauthenticated.
- **Evidence**: response includes `endpoint_performance` for 37 endpoints with `count`, `total_time`, `min_time`, `max_time`, `error_count`. Sample:
  ```
  GET:/api/v1/health/ready: count=28369 max_time=5052ms
  GET:/api/v1/auth/me: count=1060 max_time=492ms
  POST:/api/v1/auth/logout: ...
  GET:/api/v1/integrations/github/token: ...
  ```
- **Why bad**: leaks production traffic shape (call counts, latency profile) to anyone on the internet. A typo'd endpoint `GET:/api/v1/auth/login-form'` (with trailing apostrophe) is also visible — that's a 404'd attacker probe being accumulated and exposed.
- **Fix**: gate behind admin auth, or move to a separate Prometheus-only endpoint behind the cluster network.

### A-2 🔴 `/metrics` (Prometheus) is publicly exposed
- **Where**: `GET https://api.janua.dev/metrics` returns 200 unauthenticated.
- **Evidence**: `janua_system_cpu_percent 10.6`, `janua_system_memory_percent 32.6`, `janua_system_disk_free_bytes ...`. Full Prometheus scrape data.
- **Why bad**: exposes host CPU / memory / disk telemetry to the public internet. Standard practice is to scrape from inside the cluster only.
- **Fix**: same as A-1 — restrict to in-cluster ServiceMonitor.

### A-3 🟡 `/api/status` leaks infrastructure choice
- **Where**: `GET https://api.janua.dev/api/status` returns 200 with `{"infrastructure": "Railway PostgreSQL + Redis", ...}`.
- **Why noteworthy**: explicit naming of hosting provider + datastore aids targeted attack reconnaissance. Most public status endpoints return only `status`/`version`. The `features` map (`signups: true, magic_links: true, oauth: true, mfa: true, organizations: true`) is fine; the `infrastructure` string is the leak.
- **Fix**: drop the `infrastructure` field, or keep it admin-only.

### A-4 🟡 OpenAPI declares `Enterprise Features: SAML/SSO` but **zero SAML or `/sso/*` paths exist in the live OpenAPI**
- **Where**: `GET https://api.janua.dev/openapi.json` info.description claims `🎯 Enterprise Features: SAML/SSO, SCIM, Compliance (GDPR, SOC 2), Webhooks`. Source: `apps/api/app/main.py:170`-ish (description block).
- **Reality**: filtering 246 paths — `saml: 0`, `/sso/: 0`. SAML router code exists at `apps/api/app/sso/routers/configuration.py`, `metadata.py`, `oidc.py`, but is **not mounted in production**. `apps/api/app/main.py:187-201` references the imports inside an `enterprise_routers` dict but no `app.include_router(...)` for any of them appears in the live OpenAPI surface.
- **What is mounted**: `mfa: 10 paths`, `webhooks: 10 paths`, `audit-logs: 6 paths`, `scim: 4 paths` (only org-scoped config endpoints). Passkeys: 7 paths. OAuth/OIDC: 32 paths.
- **Note**: `python3-saml>=1.16.0` is in `apps/api/requirements.txt` and the `SAMLProtocol` class at `apps/api/app/sso/domain/protocols/saml.py:1-50` uses `OneLogin_Saml2_Auth` behind a `SAML_AVAILABLE` ImportError guard — so SAML is *coded* but not *exposed*. Marketing/docs claim it as a shipped feature.

### A-5 🟡 `/.well-known/openid-configuration` advertises `token_endpoint_auth_methods_supported: ["client_secret_basic", "client_secret_post", "none"]`
- **Where**: `GET https://api.janua.dev/.well-known/openid-configuration`.
- **Why noteworthy**: `"none"` enables public clients (PKCE flow) which is correct for SPA / mobile usage and matches the existing dispatch / admin portals pattern (Session 98). Just flagging that all three methods are publicly enumerable — this is OIDC-spec-compliant, just want the maintainer to confirm intent.
- **Also**: issuer is `https://auth.madfam.io` — i.e., `api.janua.dev` is a re-skin of the same Janua/madfam auth service. Not wrong, just notable for anyone confused about brand vs implementation.

### A-6 ✅ Unauth admin endpoints return 401 (not 500)
- **Tested**: `GET /api/v1/admin/users` → 401 `{"error":{"code":"HTTP_ERROR","message":"Not authenticated", ...}}`. Includes `request_id` + `timestamp`. Good error envelope.
- **Existing audit XJ-2** found 500/404/network-error on the *authenticated* path of these endpoints; the unauth path is correct.

### A-7 ✅ Validation errors return 422 with structured details (not 500)
- **Tested**: `POST /api/v1/auth/signup` with `{}` → 422 with `validation_errors[].field/message/type`. With invalid JSON `not-json` → 422 `json_invalid`. With type mismatch `"hello"` → 422 `model_attributes_type`. Consistent envelope.
- **Note**: pedantically, schema-validation failures on a public endpoint *could* be 400 instead of 422; FastAPI default is 422 and that's industry-acceptable.

### A-8 ✅ Five representative endpoints behave correctly unauth
| Endpoint | Status | Notes |
|---|---|---|
| `GET /api/v1/users/profile` | 401 | correct |
| `GET /api/v1/organizations` | 307 | redirects (probably to trailing-slash); 401 expected on follow |
| `GET /api/v1/passkeys/` | 401 | correct |
| `GET /api/v1/admin/dashboard` | 404 | endpoint not in OpenAPI, 404 is correct |
| `GET /api/v1/auth/me` | 401 | correct |

### A-9 ✅ Health latency is reasonable
- 5 sequential `GET /health` calls: 0.275s–0.339s (median ~0.288s) from this client. Marketing site claims `<30ms` *edge verification* — see M-1.
- Security headers are strong: HSTS preload, X-Frame-Options DENY, CSP defined, X-Content-Type-Options nosniff, Permissions-Policy locks down sensors + payment + USB. Good baseline.

### A-10 ✅ No Swagger UI / ReDoc exposed
- `/docs`, `/api-docs`, `/redoc` all 404. Only the raw `openapi.json` is reachable. That's a reasonable hardening choice (less interactive surface) — but anyone who knows the conventional path can still pull the full schema from `/openapi.json`. Not a finding, just an observation.

**API findings: 3 🔴 / 2 🟡 / 5 ✅**

---

## docs.janua.dev

Next.js SPA, server-rendered. Top-nav reachable: Getting Started, Guides, API Reference, SDKs, Changelog. All linked pages return 200.

### D-1 🔴 Claims "SAML 2.0 SSO integration" as shipped in `[1.0.0] - 2024-12-01` changelog — not reachable in the live API
- **Where**: `https://docs.janua.dev/changelog` lists under `[1.0.0] - 2024-12-01 Core Authentication: ... SAML 2.0 SSO integration`.
- **Reality**: see A-4. SAML is implemented in source but no `/saml/*` or `/sso/*` paths are mounted in production OpenAPI. A customer reading this changelog would buy expecting SAML to work.
- **Fix**: either mount the SSO router or move SAML 2.0 to "Unreleased" / "In progress".

### D-2 🟡 Pricing card in marketing says **"No SSO/SAML"** for Pro tier and `SSO/SAML` only on Scale ($299) — but docs/changelog claim SAML 2.0 ships in 1.0.0 today
- **Where**: cross-check `docs.janua.dev/changelog` vs `janua.dev/pricing`. Internally inconsistent: pricing says SAML is gated on Scale tier; changelog says it shipped in 1.0.0.
- **Reality**: per A-4, neither is delivered. Pricing tier-gating language is at least closer to the truth than the changelog.

### D-3 🟡 `docs.janua.dev/getting-started/quick-start` headlines "in under 5 minutes" — quickstart is 6 numbered steps with copy-paste config, sign-up at `app.janua.dev` to get credentials, and writing both an API route and a UI component
- **Where**: `https://docs.janua.dev/getting-started/quick-start`.
- **Reality**: the 6 steps are realistically `5–15 minutes` if the developer already has a Next.js app. Step 2 requires obtaining `JANUA_CLIENT_ID` + `JANUA_CLIENT_SECRET` from `app.janua.dev` — which itself is a sign-up + org-create + OAuth-client-create flow not measured in the "5 minute" window. Marketing also says `Time to First Auth: 5 min` (vs Clerk 30, Auth0 45) — same overcount.
- **Fix**: re-time it honestly or scope the claim ("5 minutes once you have credentials").

### D-4 🟢 `docs.janua.dev/sdks` lists 14 SDKs (vs marketing "8 production SDKs" / "6+ SDK languages")
- **Reality**: `packages/` directory shows 17 packages, of which the SDK-shaped ones are: `typescript-sdk`, `react-sdk`, `vue-sdk`, `nextjs-sdk`, `sveltekit-sdk`, `react-native-sdk`, `flutter-sdk`, `go-sdk`, `python-sdk` = **9 SDK packages in source**.
- **Docs SDK page** lists these by maturity: Stable (4 JS/TS), Stable (3 Python), Beta (3 — Go, React Native, Flutter), Alpha (3 — PHP, Rust, Ruby), Beta (Flask). PHP/Rust/Ruby don't exist in `packages/` — they're either external repos or vapor.
- **Inconsistency**: marketing home says `8 production SDKs`, comparison table says `6+ SDK languages`, docs page lists 14, source has 9 directories.
- **Fix**: pick a single SDK count and make all three surfaces agree. Mark Alpha/external SDKs explicitly as "community" (the page does have a "Community SDKs" section, but the Alpha listings are above it under "Other Languages" suggesting first-party).

### D-5 🟢 Docs `[Unreleased]` section in changelog is dead-ended
- "OAuth provider configuration via API", "Custom domain support for organizations", "Bulk user import/export" listed but no date / no version target. Either ship + dated, or make explicit the planned milestone. Companion: `[1.0.0]` is dated `2024-12-01` (1.5 years ago) and there's been no new version since — looks abandoned even though the API is at v0.1.0 internally (per `/health` and `/api/status`).

### D-6 ✅ Passkeys / WebAuthn docs match reality
- Doc claims `WebAuthn/FIDO2` passkey support. OpenAPI confirms `/api/v1/passkeys/register/options`, `/register/verify`, `/authenticate/options`, `/authenticate/verify`, `/passkeys/`, `/{passkey_id}`, `/availability` (7 endpoints). Source backs it (`apps/api/app/routers/v1/passkeys.py`). Real feature, real code.

### D-7 ✅ OIDC support is real
- Doc / marketing claim OIDC. OpenAPI exposes 32 OAuth/OIDC paths including `/.well-known/openid-configuration` and `/.well-known/jwks.json`. JWKS returns a valid RS256 key (`kid: janua-primary-key`). Real.

**Docs findings: 1 🔴 / 2 🟡 / 2 🟢 / 2 ✅**

---

## janua.dev

Marketing Next.js site. Pricing, About, Solutions, Demo all 200. Several primary nav links 404.

### M-1 🔴 Three primary nav links return 404 (broken footer/nav)
- **Where**: links extracted from home page HTML `<a href>`:
  - `GET https://janua.dev/security` → 404 ("This page could not be found.")
  - `GET https://janua.dev/compliance` → 404
  - `GET https://janua.dev/changelog` → 404
  - `GET https://janua.dev/signup` → 404
- **Why bad**: `/security` and `/compliance` are linked from the main page nav and are exactly the URLs an enterprise buyer clicks first when evaluating an *identity* vendor. Returning 404 on those two pages from an identity-platform marketing site is a brand-damaging bug. `/signup` 404 is worse — the conversion flow is broken from the marketing site.
- **Fix**: either implement the pages or remove the links.

### M-2 🔴 Pricing inconsistency between marketing site and dashboard
- **Marketing** (`janua.dev/pricing`): Community $0 / Pro **$69**/mo / Scale **$299**/mo / Enterprise custom. Free tier listed as **"2,000 monthly active users"**.
- **Marketing comparison table on same page**: "Free Tier MAU: **10,000**" (vs Clerk 5K, Auth0 7K). That's the same page contradicting itself by 5×.
- **Dashboard `/settings/billing`** (per `claudedocs/janua-app-fidelity-audit.md` XJ-3): Free / Pro **$49** / Scale **$199** / Enterprise. Free tier listed as **"Up to 1,000 monthly active users"**.
- **Net**: three different prices ($49/$69/-) and three different free-tier quotas (1,000 / 2,000 / 10,000) across surfaces a buyer would see in the same session.
- **Fix**: source-of-truth one place. The dashboard is supposed to be self-hosted-master per existing audit XJ-3, so it shouldn't be showing plan tiers at all.

### M-3 🔴 "Actually Implemented: WebAuthn passkeys, TOTP MFA, SAML SSO, and OAuth 2.0 - all working in production code" — SAML SSO is **not exposed** in production
- **Where**: home page hero subtitle.
- **Reality**: see A-4. SAML code exists, SAML router is not mounted, OpenAPI has zero SAML paths. The phrase "actually implemented" is the marketing site explicitly inviting this scrutiny — and it fails. Given the audience (developers who can curl the API), this is the highest-risk credibility claim on the page.

### M-4 🟡 "<30ms Edge Response*" / "Sub-30ms Edge-Fast Verification" — disclaimer says "Performance based on edge architecture. Real-world benchmarks coming Q1 2025"
- **Where**: hero stats row + Performance feature card.
- **Reality**: today is 2026-05-02. "Coming Q1 2025" disclaimer is **16 months stale**. Measured `https://api.janua.dev/health` from this client: 275–339ms. There may be an edge layer that handles JWT verification separately (the docs claim `/edge-verify` is a deployed Worker), but the marketing page's specific "Real-Time Performance Test" widget is described as currently "Detecting…" and the disclaimer confirms benchmarks aren't published.
- **Fix**: either run the benchmarks the disclaimer promised, or drop the `<30ms` claim until you can substantiate it.

### M-5 🟡 "Security Audit: Q1 2025" — also stale disclaimer
- **Where**: hero subtitle row.
- **Reality**: 16 months past the promised quarter. No audit summary linked. `/security` (where this would live) is 404. Given that the marketing target is enterprises, a stale security-audit promise is worse than no claim.

### M-6 🟡 "Test Coverage 19.6%" displayed as a feature on the marketing home
- **Where**: `Current Development Status` widget.
- **Reality**: 19.6% test coverage is a **detractor**, not a feature. Not a falsity (CLAUDE.md history confirms low Janua coverage was a known gap remediated in S98), but volunteering it on the marketing home page is a self-own. Either bring it up before marketing it, or remove it from the public storefront.

### M-7 🟡 Solutions/Enterprise page claims "SOC 2 Type II Certified secure by independent auditors" + "99.99% uptime SLA"
- **Where**: `https://janua.dev/solutions/enterprise`.
- **Reality**: no audit report is linked anywhere reachable (`/security` and `/compliance` are 404, see M-1). The dashboard `/compliance` route per existing audit suggests internal compliance work but no public attestation. Per the About page, the company is "Currently in Private Alpha" — pre-Alpha companies don't have SOC 2 Type II (which requires 6-12 months of operating evidence).
- **Risk**: claiming SOC 2 Type II without a report is the kind of statement that loses an enterprise deal *and* attracts a complaint. The About page openly says "Private Alpha"; the Enterprise page says SOC 2 Type II Certified. These cannot both be true.
- **Fix**: remove the certification claim until you have the report; replace with "SOC 2 Type II in progress" or similar accurate hedging.

### M-8 🟡 Comparison table claims "Free Tier MAU: 10,000" against Clerk/Auth0/Supabase but pricing card says 2,000
- See M-2 for the conflict. Listing 10,000 in the competitive comparison while charging cap'd at 2,000 in the actual pricing card is misleading. If a buyer signs up expecting 10,000, they'll hit the 2,000 wall.

### M-9 🟢 "Janua by Aureo Labs / A MADFAM Company / Currently in Private Alpha / Backed by founders and angels" — About page
- Honest framing. Good. Tension with M-7 noted above.

### M-10 🟢 Marketing claims "8 Production SDKs" (hero) vs "6+" (comparison) vs docs "14 SDKs listed"
- See D-4. Same inconsistency surfaces on multiple pages.

### M-11 ✅ `/demo` and `/about` reachable; `/pricing`, `/solutions/enterprise` reachable
- Core conversion-funnel pages exist. Just the secondary trust pages (security, compliance, changelog) and the actual signup CTA target (`/signup`) are broken.

**Marketing findings: 3 🔴 / 5 🟡 / 2 🟢 / 1 ✅**

---

## Summary

| Surface | 🔴 | 🟡 | 🟢 | ✅ |
|---|---|---|---|---|
| api.janua.dev | 3 | 2 | 0 | 5 |
| docs.janua.dev | 1 | 2 | 2 | 2 |
| janua.dev | 3 | 5 | 2 | 1 |

**Three highest-priority items** (any one of which independently warrants action):
1. **A-1/A-2** — `/metrics` and `/metrics/performance` are publicly exposed. Lock them down.
2. **M-1/M-7** — `/security` and `/compliance` linked from the marketing home both 404, while the Enterprise solutions page claims SOC 2 Type II certification. Pick one — either ship the pages with real attestation links, or remove the certification claim and hide the dead links.
3. **A-4 / D-1 / M-3** — SAML 2.0 is claimed as shipped on the marketing home, in the docs changelog, and in the OpenAPI description, but no SAML/SSO routes are mounted in the production API. The code exists; the wiring does not. Either mount the SSO router or stop selling SAML as a present-tense feature.

**Pricing** (M-2) is the worst single trust hit — the marketing site contradicts *itself on the same page* (2,000 MAU in the pricing card vs 10,000 MAU in the comparison table) and contradicts the dashboard ($69 vs $49 Pro, $299 vs $199 Scale).

No exposed secrets or PII observed during the audit. No authenticated calls were made (token expired); audit stopped at 401 confirmation per scope.
