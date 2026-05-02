# Cross-App Public Surfaces Audit — 2026-05-02

Read-only source + curl audit of four public surfaces:

| Surface | Status | Findings | Headline |
| --- | --- | --- | --- |
| `status.enclii.dev` | 200 | 1 high, 2 med, 1 low, 1 ok | Switchyard API probe URL is 404 in prod but page reports it Operational |
| `docs.enclii.dev` | 200 | 1 high, 3 med, 2 low, 1 ok | Live build is **53 days stale** — POSTHOG guide still points at `analytics.enclii.dev` (dead) |
| `npm.madfam.io` | 200 | 1 high, 2 med, 1 ok | Public anonymous search leaks full package metadata for `@janua` and `@enclii` scopes |
| `analytics.madfam.io` | 200 (proxied) | 0 high, 1 med, 1 ok | Cloudflare Worker proxy to PostHog Cloud is live and functional |

Severity legend: 🔴 high (action required) · 🟡 medium (track) · 🟢 low (cosmetic) · ✅ ok / no finding.

---

## Triage Table

| ID | Surface | Severity | Headline |
| --- | --- | --- | --- |
| ST-1 | status.enclii.dev | 🔴 | `https://api.enclii.dev/health/ready` returns 404 but page reports Switchyard API "Operational" |
| ST-2 | status.enclii.dev | 🟡 | No build-info / commit SHA exposed — operators can't verify what version is live |
| ST-3 | status.enclii.dev | 🟡 | Two configmaps drift wildly (12 services vs 60 services); stale comments reference services intentionally excluded but no programmatic enforcement |
| ST-4 | status.enclii.dev | 🟢 | No CSP header on the Next.js render path |
| DC-1 | docs.enclii.dev | 🔴 | Deployed build dated `Mar 8 2026` (Cloudflare last-modified) but POSTHOG_INTEGRATION.md updated `Mar 14 2026` — live page shows **dead** `analytics.enclii.dev` host (returns 525) |
| DC-2 | docs.enclii.dev | 🟡 | 23 source-only CLI commands have no docs (admin sub-tree, addon, audit, billing, canary, db, vault, webhooks, etc.) |
| DC-3 | docs.enclii.dev | 🟡 | No HSTS, no CSP, no X-Frame-Options on docs origin (Cloudflare-only TLS) |
| DC-4 | docs.enclii.dev | 🟡 | Quickstart claims `Enclii CLI v0.5.x` but no version evidence in repo (no version embed); installer URL `get.enclii.dev` returns **HTTP 525** (origin SSL handshake failure) |
| DC-5 | docs.enclii.dev | 🟢 | Footer copyright is dynamic-year-correct; no last-build marker exposed |
| DC-6 | docs.enclii.dev | 🟢 | `onBrokenLinks: 'throw'` present in config — no placeholders survived |
| NP-1 | npm.madfam.io | 🔴 | Anonymous `/-/v1/search?text=@janua` returns full metadata (versions, repo URLs, descriptions) — `@madfam` scope correctly returns 401 but `@janua` and `@enclii` leak |
| NP-2 | npm.madfam.io | 🟡 | Verdaccio version `5.33.0` is publicly disclosed in `__VERDACCIO_BASENAME_UI_OPTIONS` JSON — fingerprintable for CVE matching |
| NP-3 | npm.madfam.io | 🟡 | No HSTS header on the Verdaccio origin — relies entirely on Cloudflare edge |
| AN-1 | analytics.madfam.io | 🟡 | Worker forwards everything verbatim — `static/array.js` returns 200, but root `/` 404s. No upstream branding suppression. Acceptable as-is. |

---

## status.enclii.dev

### a) Reachability + basic shape ✅
- `curl -i https://status.enclii.dev/` → **HTTP 200**, `text/html`, ~63 KB, 0.73 s response.
- Source: Next.js App Router, lives at `apps/status/` (file: `apps/status/app/page.tsx`).
- Server-rendered with `revalidate = 60` (rebuilds every 60 s).
- `x-nextjs-cache: STALE` confirms ISR.

### b) Content fidelity (claim-vs-reality)

**Source of truth**: `apps/status/lib/health-checker.ts:142-226` — actual `fetch` to each `service.url` with retry/backoff. Genuine live probes, not hardcoded.

**Live page** at the time of audit shows:
- **15 services** all "Operational" (extracted via `aria-label="Status: Operational"` count).
- 3 service groups visible: Dhanam, Enclii, Janua (`<h2 class="text-lg font-semibold">` extraction).
- 1 active incident: `[Auto] Routecraft API Outage`.

**Service list** comes from `SERVICES_CONFIG` env var, populated by `apps/status/k8s/enclii/configmap.yaml` (12 services) for `status.enclii.dev`. A separate `apps/status/k8s/madfam/configmap.yaml` (60 services across 25 groups) backs `status.madfam.io`.

#### 🔴 ST-1 — Switchyard API probe is broken but reports Operational

`apps/status/k8s/enclii/configmap.yaml:26` declares:
```yaml
"name": "Switchyard API",
"url": "https://api.enclii.dev/health/ready",
```

External curl evidence:
```
$ curl -sI https://api.enclii.dev/health/ready    → HTTP/2 404
$ curl -sI https://api.enclii.dev/healthz         → HTTP/2 404
$ curl -sI https://api.enclii.dev/health          → HTTP/2 404
$ curl -sI https://api.enclii.dev/                → HTTP/2 404
```

But `https://status.enclii.dev/` reports Switchyard API as Operational (no `aria-label="Status: Outage"` on the page).

**Hypothesis** (cannot confirm without cluster access): the status pod probes via in-cluster service URL (different routing than public Cloudflare tunnel) so the public URL is truly broken but internal probe succeeds. Either way, the public health URL listed on the status page does not work for users — and if Cloudflare goes down, status will still claim green.

**Recommendation**: either (a) fix the `/health/ready` route on the public Cloudflare tunnel for `api.enclii.dev`, or (b) add a tunnel route, or (c) update the configmap to point at a working public URL.

#### 🟡 ST-3 — Two configmaps with drift and stale exclusion comments

`apps/status/k8s/madfam/configmap.yaml:74-92` lists "Intentional exclusions" — but enforcement is purely a comment. No CI check prevents adding excluded services to either configmap. Risks: status.madfam.io has **60 services**, status.enclii.dev has **12** — an operator changing one easily forgets the other (validated against `claudedocs/app-fidelity-audit.md` which counts ~34 ecosystem projects; status.enclii.dev surfaces only 4 of these).

The audit file from the round-2 fidelity sweep listed: Dhanam, factlas, karafiel, pravara-mes, madfam-site, accionables-madlab, symbiosis-hcm, forj, coforma-studio, blueprint-harvester, bloom-scroll, ceq, digifab-quoting, primavera3d, fortuna, avala, forgesight, NPM Registry, Platform Infrastructure, Yantra4D, tezca, Enclii, Janua. **status.enclii.dev shows only Enclii, Janua, Dhanam.** The narrow tenant scope is by design (it's a per-product status page), but operators reading status.enclii.dev cannot infer overall platform health.

#### 🟢 ST-4 — Missing CSP header on the Next.js render path

```
$ curl -sI https://status.enclii.dev/
content-type: text/html; charset=utf-8
x-nextjs-cache: STALE
x-powered-by: Next.js
```

No `Content-Security-Policy`, no `X-Frame-Options`, no `Strict-Transport-Security` from origin. Cloudflare adds STS at the edge but no CSP. Low severity for a public read-only status page, but worth adding an HTML-level meta CSP as defence-in-depth.

### c) Headers
- `cf-cache-status: DYNAMIC`, `vary: rsc, ...` ✅ correct ISR setup.
- `x-powered-by: Next.js` (information disclosure — minor).
- No HSTS from origin (CF edge handles it).

### d) Build info / footers
- Footer: `© 2026 Enclii. All rights reserved.` + RSS link.
- **No commit SHA, no build date, no version marker** in HTML or response headers (file: `apps/status/app/layout.tsx` and the page itself).
- 🟡 **ST-2** finding: operators triaging incidents have no way to know what version of the status app is running. Add a build-info JSON at `/api/health` (or as an HTML comment) embedding `GIT_SHA` + `BUILD_TIME`.

---

## docs.enclii.dev

### a) Reachability + basic shape ✅
- `curl -i https://docs.enclii.dev/` → **HTTP 200**, `text/html`, ~50 KB.
- `last-modified: Mon, 09 Mar 2026 00:51:23 GMT` — **52 days old**.
- Source: Docusaurus 3 static build at `apps/docs-site/`.
- Local build artifact at `apps/docs-site/build/` is dated **Mar 8 15:22:41 2026** — this matches the deployed version exactly.

### b) Content fidelity

**Sidebar pages** (verified live with curl):
- `/getting-started/QUICKSTART/` → 200
- `/cli/` → 200
- `/api-reference/` → 200
- `/troubleshooting/` → 200
- `/faq/` → 200
- `/guides/ONBOARDING_GUIDE/` → 301 (redirects ok)
- `/guides/POSTHOG_INTEGRATION/` → 200

#### 🔴 DC-1 — Deployed docs are 53 days stale; live PostHog guide references a dead host

The repository source `docs/guides/POSTHOG_INTEGRATION.md` was updated on **2026-03-14** (commit `feat(vault,analytics): migrate domains to madfam.io + expand ExternalSecrets to 19 files (#64)`) to point analytics traffic at `https://analytics.madfam.io`.

But the deployed build was packaged **2026-03-08** (Cloudflare `last-modified` header confirms; local `apps/docs-site/build/index.html` mtime: `Mar 8 15:22:41 2026`).

The live page at `https://docs.enclii.dev/guides/POSTHOG_INTEGRATION/` therefore still tells operators to point PostHog at:

```
NEXT_PUBLIC_POSTHOG_HOST=https://analytics.enclii.dev
api_host: "https://analytics.enclii.dev"
ENCLII_POSTHOG_ENDPOINT=https://analytics.enclii.dev
```

But `analytics.enclii.dev` is **dead**:
```
$ curl -sI https://analytics.enclii.dev/    → HTTP/2 525  (origin SSL handshake failed)
```

The actually-live host is `analytics.madfam.io` (the running Cloudflare Worker at `infra/cloudflare/posthog-proxy/worker.js`). Any new operator following the published guide would integrate PostHog against a broken hostname.

**File evidence**:
- Source (current): `docs/guides/POSTHOG_INTEGRATION.md` — `analytics.madfam.io`
- Build artifact: `apps/docs-site/build/guides/POSTHOG_INTEGRATION/index.html` — `analytics.enclii.dev`
- Live: matches the build artifact (stale).

**Recommendation**: trigger a docs site rebuild + redeploy. Add a CI check that fails if `apps/docs-site/build/` is older than the most recent `docs/**` change.

#### 🟡 DC-2 — 23 CLI commands have no docs

Diff of `packages/cli/internal/cmd/*.go` (45 commands) vs `docs/cli/commands/*.md` (28 commands):

**Source-only (no docs)**: `addon`, `admin_clusters`, `admin_costs`, `admin_drift`, `admin_fleet`, `admin_governance`, `admin_propagation`, `admin_tenants`, `admin_topology`, `admin_vclusters`, `apirequest`, `billing`, `canary`, `db`, `export`, `httpclient`, `root`, `services_delete`, `services_sync`, `signup_os`, `vault`, `webhooks` (and a couple more). The `admin_*` family is operator-only and may be intentionally undocumented, but `db`, `canary`, `billing`, `vault`, `webhooks`, `addon` are user-facing primitives.

**Docs-only (no source)**: `logout`, `services-delete`, `services-sync`, `version`, `whoami` — these match real source files but with hyphenation differences (`services_delete.go` ↔ `services-delete.md`). Not a real gap.

#### 🟡 DC-4 — Quickstart references a CLI version with no source-of-truth, and `get.enclii.dev` is broken

Quickstart at `docs/quickstart.md:39-41`:
```
enclii --version
# Enclii CLI v0.5.x
```

But the repository has no `internal/version/` package and no embedded `Version` constant in `packages/cli/cmd/enclii/main.go`. The "v0.5.x" claim is documentation-only — there's no way to verify what version actually ships.

The same quickstart instructs Linux users to run:
```
curl -sSL https://get.enclii.dev | bash
```

External evidence:
```
$ curl -sI https://get.enclii.dev    → HTTP/2 525  (origin SSL handshake failed)
```

The installer endpoint is broken. Mac (Homebrew tap) and Windows (`get.enclii.dev/install.ps1`, also via the broken host) paths are similarly affected.

#### 🟢 DC-5 — No build-info marker

Docusaurus emits `lastUpdatedAt` per-page (visible in build JSON, e.g. `1773000236000` for the POSTHOG_INTEGRATION page) but no overall site-version marker. The footer reads `Copyright © ${new Date().getFullYear()} MADFAM. Built with Docusaurus.` — runtime-evaluated, no build pin.

#### 🟢 DC-6 — No surviving placeholders in published docs

`onBrokenLinks: 'throw'` in `apps/docs-site/docusaurus.config.ts:16` means broken markdown links would have failed the build. "Coming soon" / "TODO" mentions exist only in:
- `docs/faq/billing.md` ("MADFAM Bundle | TBD ... coming soon") — acceptable for pricing
- `docs/faq/security.md` ("Lockbox rotation features (coming soon)")
- `docs/guides/migrating-from-{vercel,railway,heroku}.md` — `<!-- TODO(post-first-customer): ... -->` HTML comments invisible to readers

No hard placeholder strings on rendered surface.

### c) Headers

```
$ curl -sI https://docs.enclii.dev/
content-type: text/html
last-modified: Mon, 09 Mar 2026 00:51:23 GMT
server: cloudflare
```

No HSTS, no CSP, no X-Frame-Options, no X-Content-Type-Options at the response level. Static-html-via-CF, so edge enforcement is the only line of defence. 🟡 **DC-3** — for a docs site that hosts code blocks copy-pasted by users, even a basic `frame-ancestors 'self'` would help.

A path-specific quirk:
```
$ curl -sI https://docs.enclii.dev/guides/POSTHOG_INTEGRATION
HTTP/2 301
location: http://docs.enclii.dev/guides/POSTHOG_INTEGRATION/
```
The trailing-slash redirect downgrades `https://` → `http://` (lowercase scheme). Cloudflare's "Always Use HTTPS" upgrades the follow-up so the user stays secure, but the `Location` header itself advertises HTTP. Cosmetic. The `https://` direct fetch with trailing slash returns 200.

### d) Build info / footers
- Footer copyright dynamic from `new Date().getFullYear()`.
- Per-page `lastUpdatedAt` is in the build JSON but not surfaced to readers (`showLastUpdateTime: true` in config but not visible in rendered HTML — possible Docusaurus version mismatch).
- No commit SHA in HTML.

---

## npm.madfam.io

### a) Reachability + basic shape ✅
- `curl -i https://npm.madfam.io/` → **HTTP 200**, ~1 KB shell HTML, fast (0.62 s).
- Source: Verdaccio `5.33.0` (disclosed in HTML `__VERDACCIO_BASENAME_UI_OPTIONS`).
- K8s manifest at `infra/k8s/base/verdaccio/{deployment,configmap,ingress}.yaml`.
- `/-/ping` → `HTTP 200` `{}` ✅

### b) Content fidelity

**Verdaccio search** is enabled (`flags.search: true`, `searchRemote: false`).

#### 🔴 NP-1 — Anonymous metadata leakage on `@janua`/`@enclii` scopes

```
$ curl -s 'https://npm.madfam.io/-/v1/search?text=@janua'
{"objects":[{"package":{"name":"@janua/nextjs","version":"0.1.6","description":"...","repository":{"url":"git+https://github.com/madfam-org/janua.git"}, ...}, ...], "total":4}

$ curl -s 'https://npm.madfam.io/-/v1/search?text=enclii'
{"objects":[{"package":{"name":"@enclii/config","version":"0.1.0","license":"AGPL-3.0"}, ...], "total":3}
```

Returns **full package metadata anonymously**: package names, versions, repo URLs, descriptions, modification timestamps, license, maintainer info.

```
$ curl -s 'https://npm.madfam.io/-/v1/search?text=madfam'
{"objects":[],"total":0}

$ curl -sI 'https://npm.madfam.io/@madfam/sdk-go'
HTTP/2 401
```

The `@madfam/*` scope correctly returns 401 (auth-protected) and is not searchable. So the leak is scope-specific — `@janua` and `@enclii` are configured as anonymous-readable.

This is a deliberate config choice (see `infra/k8s/base/verdaccio/configmap.yaml`) — the SDK packages are open-source AGPL-3.0 — but it does enumerate the exact set of packages, their pinned versions, and (for `@janua`) the GitHub repo `madfam-org/janua`. The `madfam-org/janua` repo URL is leaked across all 3 published Janua packages.

If those repositories are intentionally public, this is acceptable. If "private but reachable for SDK consumers" is the intent, the search endpoint should require auth (Verdaccio supports `auth: { all: deny }` for search).

The package fetch itself (`/@janua/typescript-sdk`) returns the entire package JSON anonymously — versions, scripts, exports, files list, dist URLs.

#### 🟡 NP-2 — Verdaccio version disclosed in HTML

`https://npm.madfam.io/` ships `version: "5.33.0"` in the `__VERDACCIO_BASENAME_UI_OPTIONS` global. This makes CVE-matching trivial. Verdaccio 5.33.0 was released Sep 2024; check current security advisories before assuming this is fine.

**Mitigation**: replace with a generic build label, or upgrade to latest 5.x.

#### 🟡 NP-3 — No origin HSTS

```
$ curl -sI https://npm.madfam.io/
content-security-policy: connect-src 'self'
x-content-type-options: nosniff
x-frame-options: deny
x-xss-protection: 1; mode=block
x-powered-by: hidden
```

CSP, XFO, XCTO, XSS-Protection ✅. But no `Strict-Transport-Security` from origin — relies on Cloudflare edge. Add `Strict-Transport-Security: max-age=31536000; includeSubDomains` at origin for defence-in-depth.

### c) Headers
Mostly good. Notes above.

### d) Build info / footers
- Verdaccio version `5.33.0` is the de-facto build marker (disclosed). 🟡 trade-off: useful for operators, valuable for attackers.

### Public read-token check
The audit prompt asked about "public read-token referenced in install instructions." Searched `docs/`:
- `docs/infrastructure/npm-registry.md:??` references `//npm.madfam.io/:_authToken=${NPM_MADFAM_TOKEN}` (env-var pattern, not a literal token). ✅ no hardcoded secrets in published docs.
- Anonymous-readable scopes (`@janua`, `@enclii`) don't need a token; consumers can `npm install` without auth. So there's no public token to audit.

---

## analytics.madfam.io

### a) Reachability + basic shape ✅

```
$ curl -sI https://analytics.madfam.io/                 → HTTP 404 ("404 - Page not found")
$ curl -sI https://analytics.madfam.io/api/health/      → HTTP 404
$ curl -sI https://analytics.madfam.io/_health          → HTTP 404
$ curl -sI https://analytics.madfam.io/static/array.js  → HTTP 200 (text/javascript, 14400s cache)
$ curl -sI https://analytics.madfam.io/i/               → HTTP 404
$ curl -sI https://analytics.madfam.io/api/users/@me/   → HTTP 401 (CORS-friendly auth-required)
$ curl -sH 'Origin: https://app.enclii.dev' https://analytics.madfam.io/decide/?v=3 -X POST
                                                        → HTTP 400 (with proper PostHog ACAO/ACAC headers)
```

Source: Cloudflare Worker proxy, `infra/cloudflare/posthog-proxy/worker.js`. 44 lines. Forwards everything to `us.i.posthog.com`. PostHog Cloud (US region), not self-hosted.

The 404 at `/` is **expected** — PostHog Cloud's ingestion endpoint doesn't serve a UI on `us.i.posthog.com/`, so the proxied 404 is correct behaviour. The actual ingestion endpoints (`/static/array.js`, `/decide/?v=3`, `/api/*`) all respond correctly with PostHog's CORS + CSP headers.

### b) Content fidelity

**Status page configmap** (`apps/status/k8s/enclii/configmap.yaml:54`) probes `https://analytics.madfam.io/static/array.js` — that's the right approach since `/` 404s by design. ✅

**App init**:
- `apps/switchyard-ui/lib/analytics/posthog.ts` → `analytics.madfam.io` ✅
- `apps/admin-console/lib/analytics/posthog.ts` → `analytics.madfam.io` ✅
- `apps/switchyard-api/internal/middleware/analytics_posthog.go` (defaultEndpoint) → `analytics.madfam.io` ✅

**Drift between runtime code and live docs**: see DC-1. Code is correct, deployed docs are stale.

#### 🟡 AN-1 — Worker forwards everything verbatim

The Worker preserves PostHog Cloud's response headers including the `posthog`-flavoured `report-to` and `reporting-endpoints` (visible on `/static/`). For first-party-domain analytics this leaks the underlying provider to anyone inspecting headers. Cosmetic, but if the goal is provider-opacity, the Worker would need to strip/rewrite report-to / CSP report-uri / server-timing headers. Acceptable as-is.

### c) Headers
PostHog Cloud sets sensible headers: HSTS, ACAO `*` (overridden by Worker to `*`), CSPs for static assets, `cross-origin-opener-policy: same-origin`. ✅

### d) Build info
- N/A. The proxy is one Cloudflare Worker file, deployed via `wrangler deploy`. No version marker exposed by the Worker (it's stateless).

---

## Summary

- **No exposed secrets**, **no PII**, **no attacker-relevant probe data** observed in any of the four surfaces.
- **Most urgent**: the **53-day-stale docs site** (DC-1) — POSTHOG integration guide tells users to use a dead host.
- **Second-most urgent**: status page reports Switchyard API "Operational" while its declared probe URL returns 404 publicly (ST-1) — risks underdetected outages.
- **npm.madfam.io** behaves correctly for `@madfam/*` (authenticated) but leaks full metadata + repo URLs for `@janua`/`@enclii` scopes anonymously (NP-1) — only a finding if those repos are meant to be private.
- **analytics.madfam.io** is well-architected and matches the running code; the docs are simply out of date.

Auditor: Claude (read-only, no mutations). Audit run: 2026-05-02 19:33-19:43 UTC.
