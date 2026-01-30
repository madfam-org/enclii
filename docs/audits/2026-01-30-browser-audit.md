# Browser Audit Report — 2026-01-30

**Auditor:** Claude Code (Playwright MCP)
**Target:** Production user journey `enclii.dev` → `app.enclii.dev` → Janua SSO → Dashboard
**Purpose:** Validate login loop fix (commit `4104eb7`) under real conditions
**Result:** **FAIL** — Login loop regression confirmed. Session lost within ~20 seconds of dashboard load.

---

## Summary Table

| # | Check | Result | Notes |
|---|-------|--------|-------|
| 1 | Landing page loads | PASS | `enclii.dev` renders correctly, "Get Started" link present |
| 2 | Navigation to login | PASS | Redirects to `app.enclii.dev/login` with SSO button |
| 3 | SSO redirect to Janua | PASS | Redirects through `api.enclii.dev/v1/auth/login` → `auth.madfam.io` |
| 4 | Janua login form | PASS | Email + password form, "Signing in to Enclii Platform" |
| 5 | Credential submission | PASS | Auth successful, tokens issued, "Redirecting to dashboard..." |
| 6 | Dashboard render | PASS | Dashboard loads: 24 healthy services, 5 active projects, stats cards visible |
| 7 | Session persistence (20s) | **FAIL** | Redirected back to `/login` within ~20 seconds |
| 8 | Re-login attempt | PASS | Janua session still valid, SSO skip-prompts back to callback |
| 9 | Session persistence (2nd attempt, 10s) | **FAIL** | Same loop — back to `/login` within ~10 seconds |
| 10 | 5-minute soak test | **SKIPPED** | Cannot execute — session never survives long enough |
| 11 | No 401s on `/v1/activity` | **FAIL** | 401 on `GET /v1/activity?limit=10` triggers logout cascade |
| 12 | No hydration errors | PASS | No "Hydration failed" in console |
| 13 | No session loss redirect | **FAIL** | URL becomes `/login` after auth |
| 14 | Silent token refresh | **FAIL** | `auth.madfam.io` returns 400 for `prompt=none` silent auth |

---

## Login Loop Root Cause Analysis

The fix in commit `4104eb7` ("resolve login loop caused by premature 401 logout cascade") has **not held**. The exact sequence observed:

### Failure Sequence (reproduced twice)

```
1. SSO login succeeds → tokens stored in browser
2. Dashboard renders at app.enclii.dev/ (24 services, 5 projects visible)
3. Background poll: GET /v1/activity?limit=10 → 401
4. UI triggers logout cascade → redirects to /login
5. Silent check: GET /v1/auth/silent-check → 200
6. Silent auth: GET auth.madfam.io/...?prompt=none → 400 (Janua doesn't support prompt=none)
7. Silent check loop repeats 3x (3 separate 400 responses observed)
8. Final state: stuck at /login
```

### Key Observations

1. **The 401 on `/v1/activity` is the trigger.** Dashboard stats (`/v1/dashboard/stats`) and health check (`/health`) both return 200 — the token works for some endpoints but not `/v1/activity`.

2. **The activity endpoint uses a different auth path** or the token sent for activity polling differs from the one used for dashboard stats.

3. **Silent refresh is broken.** Janua returns 400 for `prompt=none` OAuth authorize requests. This means the "silent check" recovery path cannot work — it will always fail, making any 401 a terminal logout event.

4. **The cascade is immediate.** A single 401 on a non-critical background poll (activity feed) triggers full logout instead of gracefully degrading.

---

## Console Errors

| Error | Source | Severity |
|-------|--------|----------|
| 401 on `/v1/activity?limit=10` | API | **Critical** — triggers logout |
| "Authentication required. Please log in again." | `6a336a217edf3ae4.js` | **Critical** — logout cascade |
| "Failed to fetch notifications" | `29d5150fe26eda19.js` | **Critical** — same cascade |
| CSP blocks Cloudflare beacon.min.js | All pages | Low — analytics only |
| 404 on favicon.ico | `enclii.dev`, `auth.madfam.io` | Low — cosmetic |
| X-Frame-Options deny for auth.madfam.io | Silent check iframe | Medium — breaks silent auth |
| 400 on silent OAuth authorize (prompt=none) | Janua | **Critical** — silent refresh broken |

---

## Network Failures

| Method | URL | Status | Impact |
|--------|-----|--------|--------|
| GET | `/v1/activity?limit=10` | 401 | Triggers logout cascade |
| GET | `auth.madfam.io/.../authorize?prompt=none` | 400 | Silent refresh fails (3 attempts) |

All other API calls (`/health`, `/v1/dashboard/stats`, `/v1/teams`, RSC navigations) returned 200.

---

## Screenshots

| File | Description |
|------|-------------|
| `01-landing-page.png` | enclii.dev landing — "Deploy Without the Bill Shock" |
| `02-login-page.png` | app.enclii.dev/login — SSO button |
| `03-janua-form.png` | auth.madfam.io login form |
| `04-dashboard.png` | Dashboard loaded successfully (before loop) |
| `04b-login-loop-detected.png` | Back at /login after loop (~20s) |
| `06-final-health-check.png` | Back at /login after second attempt (~10s) |

---

## Recommendations

### P0 — Fix the login loop (blocks all usage)

1. **Stop treating activity 401 as a logout signal.** The `/v1/activity` endpoint returns 401 even with a valid session. Either:
   - Fix the API so `/v1/activity` accepts the same token that `/v1/dashboard/stats` accepts
   - Or make the UI catch 401 on non-critical endpoints (activity, notifications) without triggering logout

2. **Remove or fix silent check.** The `prompt=none` silent auth flow returns 400 from Janua. Either:
   - Implement `prompt=none` support in Janua
   - Or remove the silent check iframe approach entirely and use refresh tokens directly

3. **Add 401 retry with refresh token before logout.** On any 401:
   - First attempt: retry with refresh token exchange
   - Second failure: then redirect to login
   - Never logout on a single 401 from a background poll

### P1 — Improve resilience

4. **Differentiate critical vs non-critical API failures.** Activity feed and notification polls should degrade gracefully (show "unable to load" in the UI widget) rather than triggering auth flows.

5. **Add X-Frame-Options exception for silent check.** If keeping iframe-based silent auth, Janua needs to allow framing from `app.enclii.dev` for the silent callback endpoint.

### P2 — Cosmetic

6. **Add favicon.ico** to enclii.dev and auth.madfam.io
7. **Update CSP** to allow Cloudflare analytics beacon, or remove the Cloudflare script injection

---

## Conclusion

The login loop fix from commit `4104eb7` is **not effective**. The fundamental issue is that a 401 on the `/v1/activity` background poll triggers an immediate, unrecoverable logout cascade. The silent token refresh mechanism cannot recover because Janua does not support `prompt=none` OAuth flows, and the iframe-based approach is blocked by X-Frame-Options. The dashboard is functionally unusable — users are logged out within seconds of arriving.
