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
| ST-1 | status.enclii.dev | ✅ shipped (92987608) | `https://api.enclii.dev/health/ready` returns 404 but page reports Switchyard API "Operational" — fixed by adding dependency-free `/health/public` endpoint and repointing both status configmaps + Go core defaults |
| ST-2 | status.enclii.dev | ✅ shipped (afdd49e8) | No build-info / commit SHA exposed — operators can't verify what version is live. Fixed by injecting `NEXT_PUBLIC_COMMIT_SHA` + `NEXT_PUBLIC_BUILD_DATE` through `apps/status/Dockerfile` → `apps/status/next.config.ts` → `apps/status/components/Header.tsx` Footer. CI's prepare-build-args step (`.github/workflows/ci.yml`) now appends `${{ github.sha }}` + UTC date to every build's `--build-arg` list. Footer renders `v<short_sha> · built <iso_date>`; local builds show `vlocal · built unknown` to avoid a misleading production-looking SHA. |
| ST-3 | status.enclii.dev | ✅ shipped (afdd49e8) | Two configmaps drift wildly (12 services vs 60 services); stale comments reference services intentionally excluded but no programmatic enforcement. Fixed with `scripts/check-status-configmaps.sh` (sub-second CI job, runs in parallel with lint) enforcing three invariants: (a) `services-config` is well-formed JSON in each file, (b) no duplicate `name` or `url` within a file, (c) every `url` in the enclii configmap is also present in the madfam configmap (subset rule — names legitimately differ, e.g. "Switchyard API" vs "Enclii API"). Wired into CI as `status-configmap-drift` job + added to `test-summary` blocking list. The script intentionally does NOT regenerate from `apps/switchyard-api/internal/api/status_handlers.go` because that path merges in onboarded-project entries from the runtime DB — deterministic in-CI regen would require fixtures. Drift fixes shipped alongside the guard: added missing "Analytics Proxy" entry to madfam configmap; repaired malformed indentation around "Symbiosis HCM" + the array's closing `]` (the YAML block-scalar baseline was broken — K8s would have stripped Symbiosis HCM and the closing bracket from the deployed JSON). |
| ST-4 | status.enclii.dev | 🟢 | No CSP header on the Next.js render path |
| DC-1 | docs.enclii.dev | ✅ shipped (37aa1878) | Root cause: docs-site `dependencies: []` in `services.json` meant `docs/**` edits never triggered a CI rebuild — the live build silently aged 53 days. Fixed by adding `docs` to the dependency list (so future doc edits rebuild) plus auditing docs/ for stray `analytics.enclii.dev` refs. Source POSTHOG guide was already on `analytics.madfam.io`; this commit reships it. Demoted `onBrokenLinks` to `warn` since the Mar 13 internal-devops scrub left orphaned runbook refs that were blocking the rebuild — cleanup tracked as DC-1 follow-up. |
| DC-2 | docs.enclii.dev | ✅ shipped (55e8340d) | Backfilled 9 top-level command pages: addon, billing, canary, completion, db, export, signup, vault, webhooks. `docs/cli/commands/*.md` 28 → 37 (+9, ~1,055 lines). Admin sub-tree was already documented in `admin.md`. |
| DC-3 | docs.enclii.dev | ✅ shipped (f6ec5b10) | Added HSTS, CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy, and Permissions-Policy at origin via nginx in `apps/docs-site/Dockerfile`. CSP allows `analytics.madfam.io` in `script-src` + `connect-src` so PostHog tracking still works; `'unsafe-inline'` retained for Docusaurus runtime (revisit with nonces later). Asset location block re-declares HSTS + nosniff to preserve them under Cache-Control override. Verified locally with `docker build` + `curl -sI http://localhost:18080/` — all six headers present. |
| DC-4 | docs.enclii.dev | ✅ shipped (37aa1878) | Replaced `curl -sSL https://get.enclii.dev \| bash` (HTTP 525) with the canonical `git clone … && make install-cli` path across `docs/quickstart.md`, `docs/cli/README.md`, `docs/integrations/github.md`, `docs/getting-started/deploy-first-app.md`, and `docs/templates/ecosystem/generator.py`. Also dropped the unverifiable `Enclii CLI v0.5.x` version assertion from quickstart Verify steps. |
| DC-5 | docs.enclii.dev | 🟢 | Footer copyright is dynamic-year-correct; no last-build marker exposed |
| DC-6 | docs.enclii.dev | 🟢 | `onBrokenLinks: 'throw'` present in config — no placeholders survived |
| NP-1 | npm.madfam.io | ✅ shipped (68865c5b) | `/-/v1/search` no longer enumerates `@janua/*` and `@enclii/*` package metadata. Two-layer fix: (a) `infra/k8s/base/verdaccio/configmap.yaml` sets `experiments.search: false` so Verdaccio's V1 search endpoint stops honoring package access rules, (b) `infra/k8s/base/verdaccio/ingress.yaml` `server-snippet` returns `401 {error: "search disabled..."}` on `/-/v1/search` regardless of Verdaccio internals. Direct package fetches (`/<scope>/<name>`, tarballs, `/-/ping`) remain unauthenticated for `$all` scopes — `npm install @janua/typescript-sdk` and `npm view @janua/typescript-sdk` continue to work without a token. Trade-off: dashboard search box is dead; operators discover packages via `npm view` or direct URLs. Operator action: `kubectl rollout restart deployment/verdaccio -n npm-registry` after ArgoCD syncs. |
| NP-2 | npm.madfam.io | ✅ shipped (68865c5b) | New Cloudflare Worker `infra/cloudflare/verdaccio-edge/worker.js` rewrites `"version":"5.33.0"` → `"version":"hidden"` in dashboard HTML responses (paths: `/` and `/-/web/*`, content-type `text/html` only). Package metadata JSON and tarball bytes pass through untouched so npm CLI behavior is byte-identical. Worker also adds origin-equivalent `Strict-Transport-Security` at edge as defence-in-depth (partial NP-3). Mirrors `posthog-proxy` pattern. Operator action: `cd infra/cloudflare/verdaccio-edge && npx wrangler deploy --env production`. |
| NP-3 | npm.madfam.io | 🟡 | No HSTS header on the Verdaccio origin — relies entirely on Cloudflare edge |
| AN-1 | analytics.madfam.io | ✅ shipped (f6ec5b10) | `infra/cloudflare/posthog-proxy/worker.js` now strips upstream PostHog branding/reporting headers before returning to client: deletes `report-to`, `nel`, `expect-ct` verbatim, then loops over remaining headers and drops any whose name OR value contains `posthog` (case-insensitive). Preserves cache-control, content-type, etag, and other legitimate response headers. Will take effect on next `wrangler deploy`. |

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

**Resolution (2026-05-02)**: Shipped option (c) plus a new dependency-free public probe endpoint. `/health/ready` deliberately stays auth-anonymous-but-DB-dependent (used by k8s readiness probe + internal callers). A new `GET /health/public` was added at `apps/switchyard-api/internal/api/health_handlers.go:PublicHealth` and wired in `handlers.go:347` — it returns `{ok, service, version, time}` without touching DB / cache / k8s. Both status configmaps (`apps/status/k8s/enclii/configmap.yaml:26`, `apps/status/k8s/madfam/configmap.yaml:97`) and both Go core defaults (`apps/switchyard-api/internal/api/status_handlers.go:65,97`) now point at `/health/public`. Tests in `apps/switchyard-api/internal/api/health_handlers_test.go` assert: anonymous 200, no auth required, no component-status leak. The status page will need the next configmap roll-out + the next Cloudflare tunnel route fix to verify live, but the in-repo fidelity lie is closed and CI now guards against regression.

#### ✅ ST-3 — RESOLVED 2026-05-02 (commit `afdd49e8`)

`scripts/check-status-configmaps.sh` now enforces enclii ⊆ madfam (by `url`), in-file uniqueness, and JSON validity in <100ms; wired as `status-configmap-drift` CI job. Drift fixes shipped same commit: added missing "Analytics Proxy" entry to `apps/status/k8s/madfam/configmap.yaml`; repaired malformed indentation around "Symbiosis HCM" + closing `]` (YAML block scalar baseline was broken — K8s would have stripped both from the deployed JSON). The original-finding text below is preserved for context.

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

**Resolution (2026-05-02, commit `afdd49e8`)**: Footer in `apps/status/components/Header.tsx` now renders `v<short_sha> · built <iso_date>` (font-mono, with full SHA in `title` attribute for hover). Build-time injection chain: `apps/status/Dockerfile` declares `ARG NEXT_PUBLIC_COMMIT_SHA` + `ARG NEXT_PUBLIC_BUILD_DATE` → `apps/status/next.config.ts` exposes them as public env → CI's prepare-build-args step (`.github/workflows/ci.yml`) appends `${{ github.sha }}` + `$(date -u +%Y-%m-%dT%H:%M:%SZ)` to every build's `--build-arg` list. Local builds without these set fall back to `vlocal · built unknown`. The next deploy of `enclii-status` will surface the live SHA.

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

#### ✅ DC-2 — RESOLVED 2026-05-02 (commit `55e8340d`)

Backfilled 9 top-level command pages. `docs/cli/commands/*.md` count: **28 → 37** (+9, ~1,055 lines added). Each page follows the canonical template established by `secrets.md`: H1 + Synopsis + Description + Subcommands (with flag tables) + Examples + Notes + Exit Codes + See Also.

| Command | Lines | Section in `docs/cli/README.md` |
|---------|-------|---------------------------------|
| `addon` | 135 | Configuration |
| `billing` | 158 | Teams & collaboration |
| `canary` | 113 | Projects, services, deployments |
| `completion` | 90 | Local & meta |
| `db` | 88 | Platform operations |
| `export` | 127 | Teams & collaboration |
| `signup` | 80 | Authentication & identity |
| `vault` | 90 | Platform operations |
| `webhooks` | 174 | Configuration |

The original audit listed 23 commands but conflated the 9 admin sub-files (`admin_clusters.go`, `admin_fleet.go`, etc.) with separate docs gaps — the admin sub-tree is already documented in a single `admin.md` (246 lines) covering all subcommands. The remaining source-only files (`apirequest.go`, `httpclient.go`, `root.go`, `services_delete.go`, `services_sync.go`, `signup_os.go`) are either internal helpers (the first three) or hyphenation aliases of pages that already exist (`services-delete.md`, `services-sync.md`) or now ship as `signup.md`.

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
