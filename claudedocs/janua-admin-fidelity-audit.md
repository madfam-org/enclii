# admin.janua.dev fidelity audit — 2026-05-02

Authenticated session attempt: `admin@madfam.io` (master Janua admin / `@madfam.io` allowed domain).
Browser: Playwright. Screenshots in `claudedocs/janua-admin-screenshots-2026-05-02/`.
Source repo: `/Users/aldoruizluna/labspace/janua/apps/admin` (Next.js 14 App Router).

Severity: 🔴 critical · 🟡 important · 🟢 nit · ✅ verified-correct.

---

## Headline finding

**🔴 admin.janua.dev is unauthenticatable for `admin@madfam.io` via the email+password path.** The login flow stores tokens in `localStorage` while the middleware that gates every protected route reads them from cookies. Submitting valid credentials returns a 200, persists `janua_access_token` / `janua_refresh_token` to `localStorage`, then leaves the user on `/login` — and a manual navigation to `/` simply redirects back to `/login`. No error message is shown. The audit could not progress beyond `/login` for this reason.

---

## Findings

### AJ-1 🔴 Login token persistence vs middleware mismatch

- **Where**:
  - Login UI: `/Users/aldoruizluna/labspace/janua/apps/admin/app/login/page.tsx`. Renders `<SignIn ... enableJanuaSSO={true} showEmailPassword={false} />`. Despite the `showEmailPassword={false}` prop, the rendered form *does* include Email + Password inputs — investigate whether `<SignIn>` defaults to email/password when the SSO bootstrap fails.
  - Token persistence: the post-submit state shows `localStorage` keys `janua_access_token`, `janua_refresh_token`, `janua_token_expires_at` (all populated, expiry far in the future).
  - Middleware: `/Users/aldoruizluna/labspace/janua/apps/admin/middleware.ts:73-75` reads `request.cookies.get('janua_access_token')`, `janua_admin_email`, `janua_admin_roles`. None of these are ever set by the login flow.
- **Symptom**: post-login UI bounces to `/login` indefinitely. `GET /api/v1/auth/me` returns 401 (no `Authorization` header from a localStorage-only auth state, no cookie either).
- **Fix paths** (any one of):
  - **(a)** Have the login flow ALSO set HttpOnly cookies for `janua_access_token`, `janua_admin_email`, `janua_admin_roles` after a successful auth, mirroring the values it persists to localStorage. Low-risk; matches the middleware contract.
  - **(b)** Switch the middleware to consult an `Authorization: Bearer …` header read from a server-side route handler that bridges localStorage → server. More invasive.
  - **(c)** If the intended UX is SSO-only (per the `enableJanuaSSO={true}` prop), make the email+password fallback hard-fail with a clear error rather than silently storing localStorage tokens that nothing can use.

### AJ-2 🟡 No user-visible error after a doomed login

The user submits credentials, the page silently does nothing, and the form clears. There's no error toast, no banner, no feedback that `/api/v1/auth/me` returned 401. An operator would assume the system is broken and start filing tickets. Add explicit error handling on the `<SignIn onError>` callback (already wired in `login/page.tsx:51` to `console.error`, but never surfaced to the UI).

### AJ-3 🟡 CSP blocks Cloudflare Insights

Same XJ-1 pattern as `app.janua.dev`. `script-src 'self' 'unsafe-inline'` blocks `static.cloudflareinsights.com/beacon.min.js`. 1 console error per pageload. Fix in `apps/admin/next.config.js` (or middleware CSP config, depending on layout). The dashboard fix that just landed in `app.janua.dev` (commit `628e3a85`, January 2026) extended `script-src-elem` for Cloudflare Insights — apply the same diff here.

### AJ-4 🟢 The "Restricted Access" copy is good

`apps/admin/app/login/page.tsx:71-76` renders an explicit "Only authorized platform operators may access Janua Admin." It's accurate (the middleware enforces a `@madfam.io` / `@janua.dev` domain allowlist + `superadmin`/`admin` role check at `middleware.ts:21-43`). The copy sets correct expectations even if the flow that enforces it is broken.

### AJ-5 🟢 SignIn component prop mismatch

Login page sets `showEmailPassword={false}` (`login/page.tsx:88`) but the rendered form includes email + password inputs. Either the `<SignIn>` component from `@janua/ui` ignores the prop, or it's silently falling back to email/password when the Janua SSO bootstrap fails. Worth a one-line repro test in the `@janua/ui` package; in the meantime, audit log AJ-1 still stands regardless.

---

## Routes I attempted to walk

| Route | Result |
|-------|--------|
| `/login` | renders correctly (modulo AJ-3, AJ-5) |
| `/` | redirects to `/login` (per AJ-1) |
| Any protected route | unreachable until AJ-1 is fixed |

The remaining audit surface (Dashboard / Users / Sessions / Organizations / OAuth Clients / etc. as the admin app sees them) is **untestable** end-to-end until login persists cookies. Source-walked routes from the source-inventory pass earlier are listed in the queued section below.

---

## What's queued (after AJ-1 lands)

After the cookie-persistence fix:
- Walk every protected route (the admin app likely mirrors `app.janua.dev`'s tabs but with an operator-scoped data view).
- Cross-check the operator dashboard's stat cards against the source-of-truth queries in `apps/api`.
- Verify the role-allowlist banner renders for users who have `@madfam.io` email but no admin role.

---

## Cross-app patterns observed

| Enclii / app.janua.dev | admin.janua.dev equivalent |
|------------------------|---------------------------|
| XJ-1 CSP blocks Google Fonts + Cloudflare Insights | AJ-3 (CSP blocks Cloudflare Insights only — Google Fonts not loaded here) |
| Silent failures with no UI affordance (enclii SV-1, OB-1) | AJ-2 (login silently fails) |
| Mismatch between auth contract and middleware (none seen on enclii) | AJ-1 (genuinely new pattern for this audit) |

---

## Triage

| ID | Sev | Where | Est |
|----|-----|-------|-----|
| AJ-1 | 🔴 | `apps/admin/lib/auth.ts` (or wherever the login fetch is wired) — set cookies on success | S |
| AJ-2 | 🟡 | `apps/admin/app/login/page.tsx:51` — surface `onError` to the user | XS |
| AJ-3 | 🟡 | `apps/admin/next.config.js` CSP — apply same diff as `apps/dashboard` from `628e3a85` | XS |
| AJ-5 | 🟢 | `@janua/ui` SignIn component prop handling | XS (in `packages/ui`, not admin) |
