# Browser Audit Report: app.enclii.dev Full User Journey

**Date:** 2026-01-29
**Tool:** Playwright MCP (Chromium, headless)
**Auditor:** Claude Code (automated)

---

## Executive Summary

**Result: BLOCKED at login.** SSO authentication with Janua succeeds, but the dashboard immediately rejects the session token (401 on API calls), causing a redirect loop back to `/login`. Phases 2-4 (dashboard verification, sustained interaction, session persistence) could not be executed.

---

## Phase 1: Landing Page → Login

### Step 1 — Landing page (enclii.dev)
| Check | Result |
|-------|--------|
| Page loads | PASS |
| Title | "Enclii - Deploy Without the Bill Shock" |
| Hero renders | PASS — "Production Ready" badge, headline, CTAs |
| "Get Started" link | PASS — points to `https://app.enclii.dev` |
| "Start Deploying" CTA | PASS |
| Footer links (Docs, GitHub, Status) | PASS |
| Feature cards (4) | PASS |
| Pricing comparison section | PASS |

**Issues found:**
| Severity | Issue |
|----------|-------|
| Low | `favicon.ico` returns 404 on enclii.dev |
| Info | Cloudflare beacon script blocked by CSP (`script-src 'self' 'unsafe-eval' 'unsafe-inline'` doesn't include `static.cloudflareinsights.com`) — analytics not collecting |

### Step 2 — Login page (app.enclii.dev/login)
| Check | Result |
|-------|--------|
| Redirect from "Get Started" | PASS — navigates to `/login` |
| Login page renders | PASS |
| "Sign in with Janua SSO" button | PASS |
| Branding (Enclii logo, "Switchyard Platform") | PASS |

**Issues found:**
| Severity | Issue |
|----------|-------|
| Low | `favicon.ico` missing on auth.madfam.io |
| Info | Cloudflare beacon blocked by CSP on app.enclii.dev (same as landing) |
| Info | Silent iframe to `auth.madfam.io` returns 400 + blocked by `X-Frame-Options: deny` — expected if silent auth not configured for iframe flow |

### Step 3 — SSO Login (auth.madfam.io)
| Check | Result |
|-------|--------|
| Redirect to Janua login page | PASS |
| Login form renders | PASS — Email + Password fields |
| Shows "Signing in to **Enclii Platform**" | PASS |
| Credential submission (admin@madfam.io) | PASS |
| OAuth callback redirect | PASS — redirects to `/auth/callback` with tokens |
| "Authentication successful!" message | PASS |

**Token details observed:**
- Access token: RS256 JWT, issuer `enclii-switchyard`, role `developer`, 8-hour expiry
- Refresh token: RS256 JWT, 7-day expiry
- IDP token: RS256 JWT from Janua (`kid: janua-primary-key`), roles `["owner", "admin"]`, `is_admin: true`

**Note:** The Switchyard-issued token has `role: "developer"` while the Janua IDP token has `roles: ["owner", "admin"]`. This role mismatch may be intentional (Switchyard assigns its own roles) but is worth verifying.

### Step 4 — Dashboard redirect (FAIL)
| Check | Result |
|-------|--------|
| Dashboard loads after callback | **FAIL** |
| Token stored in browser | Partial — callback page processes tokens |
| API calls succeed | **FAIL** — `/v1/activity?limit=10` returns **401** |
| Session established | **FAIL** — redirected back to `/login` within ~2 seconds |

---

## Root Cause Analysis

### The login loop sequence (observed twice, fully reproducible):

```
1. User clicks "Sign in with Janua SSO"
2. Redirect → auth.madfam.io/api/v1/auth/login (Janua login page)
3. User submits credentials → Janua authenticates successfully
4. Redirect → app.enclii.dev/auth/callback?access_token=...&refresh_token=...
5. UI shows "Authentication successful! Redirecting to dashboard..."
6. UI navigates to dashboard (/)
7. Dashboard makes API call: GET /v1/activity?limit=10 → 401 Unauthorized
8. Auth error handler triggers → redirect to /login
9. /login page attempts silent auth via iframe → auth.madfam.io returns 400
10. Loop: back at login page
```

### Network evidence

| Request | Status | Notes |
|---------|--------|-------|
| `GET /v1/auth/silent-check` | 200 | Silent check endpoint responds |
| `GET auth.madfam.io/.../authorize?prompt=none` | **400** | Silent re-auth fails (no iframe session) |
| `GET /v1/activity?limit=10` | **401** | Dashboard API call rejected |
| `GET /v1/dashboard/stats` | 200 | Some API calls succeed |
| `GET /v1/teams` | 200 | Some API calls succeed |
| `GET /health` | 200 | Health endpoint works |

**Key observation:** `/v1/dashboard/stats` and `/v1/teams` return 200 but `/v1/activity?limit=10` returns 401. This suggests the activity endpoint has stricter auth requirements, or the token is being validated differently. The 401 from activity triggers the auth error handler which redirects to login, even though other endpoints work.

### Likely root causes (in order of probability):

1. **Activity endpoint auth bug**: The `/v1/activity` endpoint rejects a valid token while other endpoints (`/v1/teams`, `/v1/dashboard/stats`) accept it. This single 401 triggers the UI's global auth error handler, which redirects to login — even though the session is otherwise valid.

2. **JWT signing key mismatch (multi-replica)**: If the API has multiple pods with ephemeral signing keys, the pod that issued the token during OAuth callback may differ from the pod serving `/v1/activity`. The recent fix (commit `45e86cb`) for shared JWT signing keys may not be fully deployed.

3. **Token storage race condition**: The callback page may redirect to the dashboard before tokens are fully persisted to localStorage/cookies, causing the first API call to fire without credentials.

---

## Phases 2-4: Not Executed

Dashboard verification, sustained interaction testing, and session persistence testing could not be performed due to the login loop blocking access to the authenticated app.

---

## All Issues Summary

| # | Severity | Component | Issue |
|---|----------|-----------|-------|
| 1 | **Critical** | Auth/API | Login loop — `/v1/activity` returns 401 after successful SSO, triggering redirect back to `/login` |
| 2 | **Medium** | Auth | Silent auth (iframe-based `prompt=none`) returns 400 — no fallback, contributes to loop |
| 3 | **Low** | Auth | Role mismatch: Switchyard token says `developer`, Janua IDP token says `owner/admin` |
| 4 | **Low** | Landing | `favicon.ico` missing on enclii.dev (404) |
| 5 | **Low** | Auth | `favicon.ico` missing on auth.madfam.io (404) |
| 6 | **Info** | All | Cloudflare Insights beacon blocked by CSP on both enclii.dev and app.enclii.dev |

---

## Recommended Next Steps

1. **Investigate `/v1/activity` endpoint** — Why does it return 401 when `/v1/teams` and `/v1/dashboard/stats` return 200 with the same token? Fix this and the login loop likely resolves.

2. **Make auth error handling non-catastrophic** — A single 401 on a non-critical endpoint (activity feed) should not redirect the entire app to login. Consider:
   - Only redirect on 401 from critical auth endpoints (e.g., `/v1/auth/me`)
   - Show inline error for non-critical 401s

3. **Fix silent auth** — The `prompt=none` OAuth flow returns 400. Either implement proper silent auth support in Janua or remove the iframe-based silent check to avoid console noise.

4. **Add favicons** to enclii.dev and auth.madfam.io.

5. **Update CSP** to allow `static.cloudflareinsights.com` if Cloudflare analytics are desired.

6. **Re-run this audit** after fixing issue #1 to complete Phases 2-4.

---

## Screenshots

| File | Description |
|------|-------------|
| `audit-01-landing.png` | Landing page (enclii.dev) — renders correctly |
| `audit-02-login.png` | Login page (app.enclii.dev/login) — renders correctly |
| `audit-03-login-bounce.png` | Login page after bounce-back from failed dashboard load |
