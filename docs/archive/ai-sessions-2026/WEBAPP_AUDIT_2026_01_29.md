# Switchyard UI Browser Audit Report

**Date:** January 29, 2026
**URL:** https://app.enclii.dev
**Auth:** OIDC via Janua SSO (auth.madfam.io)
**User:** admin@madfam.io (role: developer, IDP roles: owner/admin)
**Tool:** Playwright MCP (Chromium)

---

## Executive Summary

The Switchyard UI is functionally rich with a well-designed dashboard, project/service inventory, and comprehensive navigation. However, **session persistence is critically broken** — the app loses authentication on any page refresh or direct URL navigation, requiring re-login every time. This is the #1 blocker for production usability.

### Severity Overview

| Severity | Count | Description |
|----------|-------|-------------|
| CRITICAL | 2 | Session persistence broken, silent auth iframe blocked |
| HIGH | 2 | Nav overlap bug, 16 services in "unknown" status |
| MEDIUM | 3 | 0 deployments tracked, Janua project has 0 services, orphaned "Checking for existing session" text |
| LOW | 3 | All versions N/A, no recent activity data, "1 services" grammar |

---

## Phase 1: Auth & Session Persistence

### SSO Login Flow
- **Status:** WORKING — clicking "Sign in with Janua SSO" correctly redirects through the API to Janua and back with tokens in URL params
- **Callback processing:** WORKING — `/auth/callback` page correctly extracts `access_token`, `refresh_token`, `expires_at`, `idp_token` from URL and stores them
- **Token storage:** WORKING — `enclii_tokens` and `enclii_user` keys present in localStorage after login

### Session Persistence
- **CRITICAL BUG: Session lost on page refresh (F5)**
  - After login, pressing F5 redirects to `/login`
  - API calls return 401, triggering `enclii:auth-expired` event
  - Silent auth iframe to `auth.madfam.io` blocked by `X-Frame-Options: deny`
  - The `useSilentAuth.ts` hook has been deleted from the codebase (staged deletion in git) but the login page still shows "Checking for existing session..." text suggesting it's still referenced

- **CRITICAL BUG: Session lost on direct URL navigation**
  - Navigating to `app.enclii.dev/projects` directly → 401 → redirect to login
  - Same for any non-root URL

- **Client-side navigation works** — clicking nav links (Projects, Services, etc.) within the SPA preserves the session because React state stays intact

### Root Cause Analysis

The session persistence failure has a specific chain:

1. **`auth-storage.ts:35`** — `setTokens()` sets cookie with `max-age` calculated from `tokens.expiresAt - Date.now()`. The `expiresAt` from the callback is `parseInt(expiresAt) * 1000` (Unix epoch seconds × 1000 to get ms).

2. **`auth-storage.ts:37`** — Cookie set: `enclii_auth=${tokens.accessToken}; path=/; secure; samesite=lax; max-age=${maxAge}`

3. **On page reload**, `AuthContext.tsx:106-129` runs the init effect:
   - Reads localStorage → tokens exist
   - Calls `isTokenExpired()` with 5-minute buffer
   - If token appears expired or close to expiry, tries `refreshTokens()`
   - `refreshTokens()` calls `POST /v1/auth/refresh` with the refresh token
   - If refresh fails → calls `logout()` → clears everything → redirect to login

4. **The `enclii_auth` cookie is NOT being set** — browser evaluation showed `document.cookie` is empty string. This means either:
   - The `Secure` flag blocks it in the Playwright Chromium context (no trusted certificate)
   - Or the `maxAge` calculation yields 0 or negative (token appears already expired)

5. **Even if the cookie were set**, the middleware at `middleware.ts:50` checks for it but does NOT redirect — it says "let client-side handle it". So the middleware is not the direct cause.

6. **The actual cause**: On reload, the `AuthProvider` init effect runs, finds tokens in localStorage, but when API calls are made with the stored token, the API returns 401. This fires the `enclii:auth-expired` event, which tries `refreshTokens()`, which fails, which calls `logout()`, which clears storage and redirects to `/login`.

### Recommendation
- **Verify `expiresAt` calculation** in callback page — `parseInt(expiresAt) * 1000` may double-multiply if the API already returns milliseconds
- **Add a grace period** to the init flow — don't call `logout()` on failed refresh during init; instead, try the existing token first
- **Fix cookie setting** — verify the `Secure` flag works with Cloudflare tunnel HTTPS
- **Remove or fix silent auth** — the iframe approach is dead (`X-Frame-Options: deny` on `auth.madfam.io`). Either remove the "Checking for existing session..." text or implement a redirect-based silent auth

---

## Phase 2: Dashboard

### Stat Cards
| Metric | Value | Assessment |
|--------|-------|------------|
| Healthy Services | 24 | Matches healthy count from services table |
| Deployments Today | 0 | No deployments tracked at all |
| Active Projects | 5 | Matches projects page |
| Avg Deploy Time | N/A | No deployment data to compute |

### System Health Badge
- **Status:** "Operational" (green dot) — displayed in top nav

### Notifications
- **2 unread notifications** shown in bell icon badge

### Recent Activity
- **"No recent activity"** — empty state

### Services Overview Table
- **Total services listed:** 40 (matches Services page count)
- **24 healthy** (green, replicas at desired count)
- **16 unknown** (amber/yellow, all with 0/0 replicas)
- **All versions:** N/A (no version tracking data)

### Unknown Status Services (all in Solarpunk Foundry)
These are infrastructure metric/webhook endpoints with 0/0 replicas:
1. longhorn-frontend, longhorn-backend, longhorn-recovery-backend
2. longhorn-conversion-webhook, longhorn-admission-webhook
3. argocd-metrics, argocd-server-metrics, argocd-image-updater-metrics
4. argocd-notifications-controller-metrics
5. kyverno-svc, kyverno-svc-metrics
6. kyverno-background-controller-metrics, kyverno-reports-controller-metrics
7. kyverno-cleanup-controller-metrics
8. cloudflared-metrics
9. cnpg-webhook-service

**Assessment:** These are Kubernetes ClusterIP services for metrics scraping that don't have deployments/pods behind them (or are scaled to 0). They pollute the dashboard and should be filtered or categorized differently.

---

## Phase 3: Projects & Services Inventory

### Projects (5 total)
| Project | Slug | Services | Assessment |
|---------|------|----------|------------|
| The Anvil | anvil | 1 (runner-monitor) | OK |
| Solarpunk Foundry | solarpunk-foundry | 28 | HIGH — 16 of 28 are infra metrics/webhook services with "unknown" status |
| Janua SSO | janua | 0 | MEDIUM — expected to have at least the Janua service |
| Enclii Platform | enclii | 8 | OK — all expected services present |
| Dhanam Ledger | dhanam | 3 | OK |

### Service Counts Cross-Reference
- Dashboard says 24 healthy + 16 unknown = 40 total
- Services page confirms "40 total"
- Projects: 1 + 28 + 0 + 8 + 3 = 40

### Enclii Platform Services (8)
| Service | Status | Replicas |
|---------|--------|----------|
| dispatch | healthy | 1/1 |
| waybill | healthy | 1/1 |
| switchyard-ui | healthy | 1/1 |
| switchyard-api | healthy | 1/1 |
| status-page | healthy | 1/1 |
| roundhouse | healthy | 1/1 |
| landing-page | healthy | 1/1 |
| docs-site | healthy | 1/1 |

### Dhanam Ledger Services (3)
| Service | Status | Replicas |
|---------|--------|----------|
| dhanam-admin | healthy | 1/1 |
| dhanam-api | healthy | 1/1 |
| dhanam-web | healthy | 1/1 |

### The Anvil Services (1)
| Service | Status | Replicas |
|---------|--------|----------|
| runner-monitor | healthy | 2/2 |

---

## Phase 4: Deployments

- **0 deployments found** across all services
- Empty state page shows correctly with guidance text
- **Assessment:** Despite operational build pipelines and ArgoCD GitOps, the Deployments page shows zero entries. The deployment tracking pipeline appears disconnected from the UI/API.

---

## Phase 5: Interactive Element Walkthrough

### Navigation
| Element | Status | Notes |
|---------|--------|-------|
| Dashboard link | Works | Client-side nav |
| Projects link | Works | Client-side nav |
| Services link | Works | Client-side nav |
| Deployments link | Works | Client-side nav |
| Observability link | BLOCKED | Search bar overlay intercepts clicks (z-index bug) |
| "More" dropdown | Not tested | Could not reach due to session loss |

### Header Components
| Element | Status | Notes |
|---------|--------|-------|
| Search (Cmd+K) | Present | Button visible, not click-tested |
| Notification Bell | Shows "2" | Badge visible |
| System Health | "Operational" | Green dot |
| User Menu ("admin") | Present | Button visible |
| Scope Switcher | "Personal Account" | Dropdown button visible |

### HIGH BUG: Observability Nav Link Blocked
- The search bar `<span>Search...</span>` overlaps the Observability nav link
- Playwright error: `<span>Search...</span> subtree intercepts pointer events`
- On certain viewport widths, users cannot click the Observability link
- **Fix:** Add `z-index` to nav links or constrain the search bar width

### Services Page Features
- Search bar, Filter button, Sort button present
- "Import from GitHub" and "New Service" buttons present
- Table with Service, Project, Environment, Status, Version, Replicas columns
- All names are clickable links to detail pages

### Projects Page Features
- "Create Project" button present
- Project cards show: name, slug, creation date, service count, service name tags
- All cards are clickable links to detail pages

---

## Phase 6: Health & Status

### System-Level Health
- **Overall:** "Operational" (green) in nav badge

### Service Health Summary
| Category | Count |
|----------|-------|
| Healthy (1/1 or 2/2 replicas) | 24 |
| Unknown (0/0 replicas) | 16 |

### Alerts & Errors
- Not tested — Observability page inaccessible due to nav overlap + session loss

---

## Phase 7: Responsive & UX Quality

### Desktop (default viewport ~1280px)
- Clean dark theme by default
- Nav bar properly shows all primary links
- Tables are readable with proper column widths
- **Issue:** Nav link overlap with search bar at certain widths

### Responsive/Mobile/Tablet
- Not tested due to session persistence issues

### Dark Mode
- Default theme is dark, professional appearance
- Good contrast on card backgrounds vs text
- Status badges (green/amber) clearly visible
- Environment badges properly styled

### Loading States
- "Checking for existing session..." on login page (orphaned from deleted hook)
- No excessive spinners during client-side navigation
- Dashboard loads quickly after auth

### Empty States
- Deployments: well-designed with guidance text
- Recent Activity: minimal "No recent activity" text

---

## Prioritized Action Items

1. **Fix session persistence** — Debug `expiresAt` calculation, prevent init-time logout on refresh failure, verify cookie `Secure` flag with Cloudflare HTTPS
2. **Fix nav bar z-index** — Prevent search bar from overlapping Observability link
3. **Filter infrastructure services** — Hide or categorize k8s metrics/webhook services (0/0 replicas)
4. **Wire deployment tracking** — Connect ArgoCD/build pipeline events to the deployments API
5. **Remove orphaned silent auth UI** — Clean up "Checking for existing session..." text
6. **Add Janua services** — Register auth.madfam.io under the Janua SSO project
7. **Populate service versions** — Track container image tags/git SHAs
8. **Fix "1 services" grammar** — Pluralization logic for service counts
9. **Populate activity feed** — Wire system events to the Recent Activity section

---

## Screenshots Captured

| File | Description |
|------|-------------|
| `.playwright-mcp/phase1-initial-load.png` | Login page on first load |
| `.playwright-mcp/phase1-dashboard-after-login.png` | Dashboard after SSO login (full page) |
| `.playwright-mcp/phase3-projects-page.png` | Projects page with 5 project cards |
| `.playwright-mcp/phase4-deployments.png` | Deployments page showing 0 deployments |
| `.playwright-mcp/phase5-observability.png` | Failed — shows login page due to session loss |
