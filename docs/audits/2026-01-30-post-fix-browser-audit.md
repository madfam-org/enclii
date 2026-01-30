# Browser Audit: Post Login-Loop Fix (e762345)

**Date:** 2026-01-30
**Commit Under Test:** `e762345` — fix(ui): eliminate login loop by removing auth-expired event cascade
**Auditor:** Automated Playwright MCP
**Verdict:** FAIL — Login loop still occurs on first client-side navigation

---

## Executive Summary

The login loop fix partially works: SSO login completes and the dashboard renders with live data. However, **the first client-side navigation** (e.g., clicking "Projects") triggers a 401 from the API, which causes an immediate redirect back to `/login`. The auth token is not being sent on subsequent client-side API requests.

---

## Phase 1: Landing Page

| Check | Result |
|-------|--------|
| `enclii.dev` loads | PASS |
| Page title | "Enclii - Deploy Without the Bill Shock" |
| "Get Started" navigates to `app.enclii.dev` | PASS |
| Redirects to `/login` (unauthenticated) | PASS |
| Errors | Missing favicon (404), Cloudflare beacon CSP block |

## Phase 2: SSO Authentication

| Check | Result |
|-------|--------|
| "Sign in with Janua SSO" redirects to `auth.madfam.io` | PASS |
| Janua login form renders | PASS |
| Credentials accepted | PASS |
| Redirect back to `app.enclii.dev/auth/callback` | PASS |
| Callback shows "Authentication successful!" | PASS |
| Dashboard loads after redirect | PASS |

**Dashboard state after login:**
- 24 Healthy Services
- 5 Active Projects
- 0 Deployments Today
- Notification bell: 2 unread
- System status: Operational
- Services table: 24 healthy + ~14 unknown (metrics/webhook services with 0/0 replicas)

## Phase 3: Stability Check — FAILED

| Check | Result |
|-------|--------|
| Dashboard stays on `/` | PASS (while idle) |
| Click "Projects" nav link | **FAIL** — redirected to `/login` |
| `/v1/projects` API response | **401 Unauthorized** |
| Session survived navigation | **NO** |
| `auth-expired` events | Not observed (fix removed the event) |
| Silent SSO check | **400** — repeated X-Frame-Options deny |

### Failure Sequence

```
1. User on dashboard (/) — working fine
2. Click "Projects" link → client-side navigation
3. App fetches GET /v1/projects
4. API returns 401 Unauthorized
5. Client error: "Authentication required. Please log in again."
6. App redirects to /login
7. Silent SSO check fires → GET /oauth/authorize?prompt=none → 400
8. X-Frame-Options: deny blocks iframe-based silent check (3 attempts)
9. User stuck on /login page
```

### Root Cause Analysis

The access token received during the OAuth callback is **not being attached to subsequent client-side API requests**. Evidence:

- **Dashboard data loaded** — because it was fetched during the initial page render (server-side or during callback processing)
- **`/v1/activity?limit=10`** returned 200 during initial load
- **`/v1/dashboard/stats`** returned 200 during initial load
- **`/v1/projects`** returned 401 on client-side navigation

This indicates the token is either:
1. Not being persisted to cookies/localStorage after the callback
2. Not being read from storage by the API client on subsequent requests
3. Being stored but in a format the API middleware doesn't recognize

### Network Evidence

All initial API calls succeeded (200):
- `GET /v1/auth/silent-check` → 200
- `GET /v1/teams` → 200
- `GET /v1/activity?limit=10` → 200
- `GET /health` → 200
- `GET /v1/dashboard/stats` → 200

First client-side navigation API call failed:
- `GET /v1/projects` → **401**

## Phase 4: Console Errors Summary

### Critical (Auth-Related)
| Error | Source |
|-------|--------|
| `401` on `/v1/projects` | API auth failure |
| "Authentication required. Please log in again." | Client-side error handler |
| Silent SSO check `400` (x3) | `auth.madfam.io/oauth/authorize?prompt=none` |
| X-Frame-Options deny (x3) | Janua blocking iframe embed |

### Non-Critical (Cosmetic)
| Error | Source |
|-------|--------|
| Cloudflare beacon CSP block | `script-src` policy on app.enclii.dev and auth.madfam.io |
| Missing favicon 404 | enclii.dev, auth.madfam.io |

---

## Checklist Results

- [x] Landing page loads at enclii.dev
- [x] "Get Started" navigates to app.enclii.dev
- [x] SSO login completes successfully
- [x] Dashboard renders with data
- [ ] **Session survives navigation** — FAILED (401 on /v1/projects)
- [N/A] No logout cascade or auth-expired events — event removed but 401→redirect still occurs
- [ ] **Navigation between pages works without auth loss** — FAILED

---

## Recommendations

1. **Investigate token persistence**: Check if the auth callback page properly stores the access token in cookies or localStorage before redirecting to dashboard
2. **Check API client interceptor**: Verify the Axios/fetch interceptor reads the stored token and attaches it as `Authorization: Bearer <token>` on every request
3. **Fix silent SSO check**: The iframe-based silent check returns 400 because Janua sets `X-Frame-Options: deny`. Either configure Janua to allow framing from `app.enclii.dev` or use a different silent check mechanism (e.g., redirect-based)
4. **Add CSP exception**: Add `script-src-elem` directive to allow Cloudflare beacon script (low priority)
