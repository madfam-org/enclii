# Commercial GA — staging proof runbook

> **Program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) · [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md)  
> **Doctrine:** Enclii web, API, or CLI for routine production work. Break-glass cluster access only with documented reason.

This runbook is the **ROI-ordered proof sequence** after engineering merges to `main`. Blocking CI already runs API **smoke** tests (auth gates, no 502/503). Full **lifecycle** proofs require staging credentials and are **not** blocking on every `main` push.

---

## 0. Phase 0 ops gate (before bet proofs)

Complete in order; details in [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) and [REMAINING_ITEMS.md](./REMAINING_ITEMS.md).

| Step | Done when |
|------|-----------|
| Deploy `main` to production (Enclii / GitOps) | Switchyard API + UI match `main` |
| Run DB migration **030** (`rollout_blocked_reason`) | Column present; reconciler writes blocked reason |
| Security release checklist (Roundhouse key, tenant smoke) | [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) signed |
| Cluster P0 (disk, Longhorn, PostHog cleanup) | [REMAINING_ITEMS.md](./REMAINING_ITEMS.md) quick-ref green |
| Restore drill | Evidence logged |

**Do not** run destructive lifecycle E2E against production tenants without a dedicated staging service.

---

## 1. Credentials

Create a **staging-only** API token (developer role on a throwaway project) with access to one **non-production-critical** service per bet.

Store in GitHub **environment** `commercial-ga-staging` (recommended) or repository secrets:

| Secret | Used for |
|--------|----------|
| `PREVIEW_E2E_TOKEN` | Bet A |
| `PREVIEW_E2E_SERVICE_ID` | Bet A |
| `DOMAIN_E2E_TOKEN` | Bet B (can reuse same token) |
| `DOMAIN_E2E_SERVICE_ID` | Bet B |
| `DOMAIN_E2E_ENVIRONMENT_ID` | Bet B |
| `DOMAIN_E2E_DOMAIN` | Bet B optional FQDN |
| `STORAGE_E2E_TOKEN` | Bet C |
| `STORAGE_E2E_SERVICE_ID` | Bet C |
| `STORAGE_E2E_RELEASE_ID` | Bet C stateful deploy (ready release) |
| `STORAGE_E2E_ENVIRONMENT_NAME` | Bet C optional (default `production`) |

`API_BASE_URL` defaults to `https://api.enclii.dev` in specs; override for a dedicated staging API host if you have one.

---

## 2. Local proof (ROI order: A → B → C)

From repo root:

```bash
cd tests/e2e-ecosystem
npm ci
npx playwright install chromium

export API_BASE_URL=https://api.enclii.dev   # or staging API

# Bet A — previews
export PREVIEW_E2E_TOKEN=...
export PREVIEW_E2E_SERVICE_ID=...
npx playwright test --project=preview-lifecycle

# Bet B — custom domains
export DOMAIN_E2E_TOKEN=...
export DOMAIN_E2E_SERVICE_ID=...
export DOMAIN_E2E_ENVIRONMENT_ID=...
# optional: export DOMAIN_E2E_DOMAIN=app.example.com
npx playwright test --project=domains-lifecycle

# Bet C — volumes (+ optional deploy)
export STORAGE_E2E_TOKEN=...
export STORAGE_E2E_SERVICE_ID=...
npx playwright test --project=storage-smoke

# Bet C — stateful deploy slice (requires ready release on service)
export STORAGE_E2E_RELEASE_ID=...
# optional: export STORAGE_E2E_ENVIRONMENT_NAME=production
npx playwright test --project=storage-smoke -g "stateful deploy"
```

Smoke-only (no secrets): `npx playwright test` runs all always-on tests; opt-in suites skip when env is unset.

---

## 3. GitHub Actions (manual)

Workflow: **Commercial GA staging proof** (`.github/workflows/commercial-ga-staging-proof.yml`).

1. Actions → **Commercial GA staging proof** → **Run workflow**
2. Choose bets: `preview`, `domains`, `storage`, or `all`
3. Ensure secrets above are configured on the environment or repo

Artifacts: Playwright report uploaded on completion.

---

## 4. Sign-off

Record results in [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md) (bet rows + date). Stability GA still requires 30-day SLO window after Phase 0 deploy.

| Bet | Proof | Spec |
|-----|-------|------|
| A Previews | create → get → close | `tests/e2e-ecosystem/tests/enclii-preview-lifecycle.spec.ts` |
| B Domains | add → verify path → remove | `tests/e2e-ecosystem/tests/enclii-domains-lifecycle.spec.ts` |
| C Storage | volumes round-trip | `tests/e2e-ecosystem/tests/enclii-storage-smoke.spec.ts` |
| C Storage (deploy) | patch volumes → deploy → running | same spec, `STORAGE_E2E_RELEASE_ID` |

---

## 5. Troubleshooting

| Symptom | Likely cause |
|---------|----------------|
| Opt-in tests skipped | Missing `*_E2E_*` env vars |
| Deploy 403 | Production approval policy / PR provenance — use staging env name or disable policy on test project |
| Deploy stuck pending | Reconciler queue; check Enclii ops dashboards (not raw `kubectl` unless break-glass) |
| Volume PATCH 200 but empty on GET | Service not loaded with volumes column — ensure migration genesis + API on `main` ≥ #258 |
