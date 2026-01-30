# Browser Audit: Full User Journey & 5-Minute Stability Reaudit

**Date:** January 30, 2026 (Late-evening reaudit)
**Auditor:** Claude Code (Playwright MCP)
**Target:** app.enclii.dev (production)
**User:** admin@madfam.io (OIDC via Janua SSO)
**Previous Audit:** 2026-01-30-browser-journey-audit.md

## Executive Summary

**Result: FAIL** — The JWT ephemeral key bug remains the dominant issue. This reaudit shows a **regression** compared to the earlier audit today: navigation-triggered 401s now cause login redirects on **89% of page transitions** (8/9 in soak test), up from ~30% reported earlier. The likely explanation is that the API replica count or load balancer behavior has shifted, reducing the probability of hitting the same replica that issued the token.

**Root Cause (Unchanged):** Each API replica generates its own ephemeral RSA signing key. Tokens signed by replica A are rejected by replicas B-E. The fix is infrastructure-only: deploy a shared `jwt-secrets` Kubernetes secret.

**Key Finding:** The auth-expired cascade fix (commit `e762345`) still works — background `NotificationBell` 401s do NOT force logout. However, when a navigation-triggered API call (e.g., `/v1/projects`) returns 401, the UI attempts silent refresh via iframe, which fails (`X-Frame-Options: deny` on auth.madfam.io), and the app redirects to `/login`.

---

## Phase 1: Landing -> Login (SSO Flow)

| Step | Result | Notes |
|------|--------|-------|
| Navigate to enclii.dev | **PASS** | Title: "Deploy Without the Bill Shock" |
| Click "Get Started" | **PASS** | Redirects to app.enclii.dev/login |
| Click "Sign in with Janua SSO" | **PASS** | Redirects to auth.madfam.io login form |
| Fill credentials & submit | **PASS** | admin@madfam.io authenticated successfully |
| OIDC callback → Dashboard | **PASS** | Tokens delivered via URL fragment, dashboard loaded |
| Dashboard stat cards | **PASS** | 24 Healthy Services, 5 Active Projects, 0 Deployments Today |

**SSO Flow: 6/6 PASS**

---

## Phase 2: Navigation Audit (All 10 Routes)

Navigation used client-side clicks (`document.querySelector('a[href="..."]').click()`) to preserve auth cookies/tokens.

| Route | Result | Notes |
|-------|--------|-------|
| `/` (Dashboard) | **PASS** | Loaded with stat cards and services table (24 healthy, some unknown) |
| `/projects` | **FAIL** | 401 on `/v1/projects` → login redirect |
| `/services` | **FAIL** | 401 → login redirect (after re-auth from /projects failure) |
| `/deployments` | **PASS** | Loaded after re-auth |
| `/observability` | **PASS** | Loaded |
| `/templates` | **PASS** | Loaded |
| `/databases` | **PASS** | Loaded |
| `/domains` | **PASS** | Loaded |
| `/activity` | **PASS** | Loaded (despite background 401 on `/v1/activity?limit=10`) |
| `/settings` | **PASS** | Loaded |

**Navigation: 8/10 PASS, 2/10 FAIL** (both due to JWT ephemeral key mismatch)

Note: Routes that don't make API calls on load (or whose API calls are treated as background polls) survive. Routes that make navigation-critical API calls (`/projects`, `/services`) fail when hitting a non-signing replica.

---

## Phase 3: 5-Minute Stability Soak

**Duration:** 303 seconds (~5 minutes)
**Route cycle:** `/` → `/projects` → `/services` → `/deployments` → `/observability` → repeat
**Cycle interval:** ~30 seconds

### Results

| Metric | Value |
|--------|-------|
| Total navigation cycles | 9 |
| Login redirects | **8** (89%) |
| Successful navigations | **1** (11%) — only the initial `/` load |
| API 401 responses | 9 |
| Re-authentications required | 8 |
| Screenshots captured | 4 |

### Soak Log

| Time (s) | Route | Result |
|----------|-------|--------|
| 3 | `/` | OK |
| 33 | `/projects` | LOGIN REDIRECT |
| 70 | `/services` | LOGIN REDIRECT |
| 108 | `/deployments` | LOGIN REDIRECT |
| 145 | `/observability` | LOGIN REDIRECT |
| 182 | `/` | LOGIN REDIRECT |
| 220 | `/projects` | LOGIN REDIRECT |
| 259 | `/services` | LOGIN REDIRECT |
| 296 | `/deployments` | LOGIN REDIRECT |

### Error Categories

| Error Type | Count | Severity |
|------------|-------|----------|
| 401 on API calls (`/v1/projects`, `/v1/activity`) | 9+ | **CRITICAL** — JWT key mismatch |
| CSP blocks Cloudflare beacon | Every page load | Cosmetic — no functional impact |
| X-Frame-Options: deny (silent refresh iframe) | Every 401 recovery attempt | **HIGH** — prevents silent token refresh |
| favicon.ico 404 (enclii.dev, auth.madfam.io) | 2 | Cosmetic |
| No hydration errors | 0 | N/A |
| No CORS errors | 0 | N/A |

---

## Phase 4: Logout

Logout was not tested due to the session being in a login-redirect state at soak test conclusion. Previous audit confirmed logout works correctly (RP-Initiated Logout terminates Janua session).

---

## Success Criteria Scorecard

| Criterion | Result |
|-----------|--------|
| Landing page loads at enclii.dev | **PASS** |
| "Get Started" navigates to app.enclii.dev | **PASS** |
| SSO login completes without errors | **PASS** |
| Dashboard loads with stat cards populated | **PASS** |
| All 10 navigation targets load without /login redirect | **FAIL** (2/10 failed) |
| No CORS errors in console | **PASS** |
| No 401 errors in console | **FAIL** (9+ 401s) |
| No hydration errors in console | **PASS** |
| 5-minute soak: zero login redirects | **FAIL** (8 redirects) |
| 5-minute soak: zero console errors | **FAIL** (continuous 401s, CSP blocks) |
| Logout completes cleanly | **NOT TESTED** |

**Overall: 5/11 PASS, 5/11 FAIL, 1/11 NOT TESTED**

---

## Comparison with Previous Audit (Earlier Today)

| Metric | Previous Audit | This Reaudit | Delta |
|--------|---------------|--------------|-------|
| Navigation success rate | ~70% (7/10) | 80% (8/10) | Similar |
| Soak login redirects | 0 in 3 min | 8 in 5 min | **Regression** |
| API 401s during soak | ~3 (background only) | 9+ | **Regression** |
| Auth cascade logout | Fixed | Fixed | Stable |
| Hydration errors | 0 | 0 | Stable |
| CORS errors | 0 | 0 | Stable |

The regression in soak test results suggests the token-to-replica affinity has worsened, possibly due to replica scaling or load balancer changes.

---

## Root Cause Analysis

### Primary: JWT Ephemeral Key (CRITICAL)

The `switchyard-api` deployment runs multiple replicas. Each replica generates its own RSA key pair on startup. When the OIDC callback hits replica A (which signs the JWT), subsequent API calls may route to replica B-E, which reject the token with 401.

**Fix:** Deploy a shared signing key:
```bash
openssl genrsa -out jwt-private.pem 2048
openssl rsa -in jwt-private.pem -pubout -out jwt-public.pem
kubectl create secret generic jwt-secrets \
  --from-file=private-key=jwt-private.pem \
  --from-file=public-key=jwt-public.pem \
  -n enclii
kubectl rollout restart deployment/switchyard-api -n enclii
```

### Secondary: Silent Refresh Blocked (HIGH)

`auth.madfam.io` sets `X-Frame-Options: deny`, which prevents the iframe-based silent token refresh. Every 401 recovery attempt fails, forcing a full-page redirect to `/login`.

**Fix:** Set `X-Frame-Options: SAMEORIGIN` or use `frame-ancestors 'self' https://app.enclii.dev` in CSP on auth.madfam.io.

### Cosmetic: CSP Blocks Cloudflare Beacon

Cloudflare injects `beacon.min.js` which violates the app's CSP `script-src` directive. No functional impact.

**Fix:** Add `https://static.cloudflareinsights.com` to the CSP `script-src` directive, or accept the cosmetic error.

---

## Recommendations (Priority Order)

1. **Deploy shared JWT secret** — Fixes 90%+ of all failures. Infrastructure-only change, no code needed.
2. **Fix X-Frame-Options on auth.madfam.io** — Enables silent token refresh as fallback.
3. **Add missing favicons** — enclii.dev and auth.madfam.io both return 404 for favicon.ico.
4. **Update CSP for Cloudflare beacon** — Eliminates cosmetic console errors.

---

*Generated by Claude Code using Playwright MCP browser automation. Test duration: ~10 minutes (phases 1-3) + 5-minute soak.*
