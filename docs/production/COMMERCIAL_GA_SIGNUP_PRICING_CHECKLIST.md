# Signup & pricing GA checklist

> **Tracker item:** Pricing + self-serve signup tested  
> **Owner:** GTM + QA (after Phase 0 deploy)

---

## Automated (CI)

| Check | Spec / workflow | Pass when |
|-------|-----------------|-----------|
| Landing hero loads | `enclii-paywall.spec.ts` | No auth redirect; hero visible |
| Pricing section | `enclii-paywall.spec.ts` | Sovereign / $20 visible **or** skipped if not deployed |
| Signup API smoke | `enclii-signup-smoke.spec.ts` | No 502/503 on `/v1/signup` and status route |
| Signup page loads | `enclii-signup-smoke.spec.ts` | `app.enclii.dev/signup` returns &lt;500 |
| Commercial GA proof harness | `.github/workflows/commercial-ga-proof.yml` / `make commercial-ga-proof` | Public health, landing/pricing, signup shell, Dhanam checkout, and authenticated billing endpoints pass or produce explicit warnings |

---

## Manual — pricing (15 min)

| # | Step | Pass |
|---|------|------|
| 1 | Open https://enclii.dev — pricing section visible or documented skip | ☑ (2026-05-30) |
| 2 | CTA links to signup or app (no broken href) | ☑ → `app.enclii.dev` |
| 3 | Tier copy matches [docs/faq/billing.md](../faq/billing.md) Essentials/Pro table | ☐ **Drift:** landing shows **Sovereign** @ $20; FAQ lists **Essentials** @ $20 / **Pro** @ $49 |
| 4 | Dhanam checkout URL works for Pro upgrade (if applicable) | ☑ HTTP 200 — `DHANAM_ENCLII_CHECKOUT_URL` |

---

## Manual — self-serve signup (30 min)

Requires `ENCLII_SIGNUP_ENABLED=true` on API in target environment.

| # | Step | Pass |
|---|------|------|
| 1 | `enclii signup --no-browser` — URL prints `https://app.enclii.dev/signup` | ☑ (2026-05-30) |
| 2 | Submit email on `/signup` — receive verification email (or stub in dev) | ☐ **Blocked:** no `ENCLII_RESEND_API_KEY` in prod (log-only) |
| 3 | Click verify link → `/signup/verify` → status advances | ☐ |
| 4 | Connect GitHub → OAuth callback succeeds | ☐ |
| 5 | Provision completes → redirect to project dashboard | ☐ |
| 6 | `enclii login` + `enclii whoami` — new user authenticated | ☐ |
| 7 | First deploy to preview or development env succeeds | ☐ |

**Automated API smoke (2026-05-30):** POST `/v1/signup` → **201**; GET `app.enclii.dev/signup` → **200**; Playwright API tests pass (page test needs `npx playwright install` locally).

**Opt-in E2E:** `SIGNUP_E2E_RUN=1 npx playwright test --project=signup-smoke` (page shell only).

---

## Automated commercial proof inputs

Strict mode requires:

| Input | Purpose |
| --- | --- |
| `ENCLII_SYNTHETICS_BEARER_TOKEN` | Authenticated billing endpoint probe without printing token material. |
| `ENCLII_SYNTHETIC_PROJECT_SLUG` | Project used for billing cost, budgets, and throttles proof. |
| `DHANAM_ENCLII_CHECKOUT_URL` | Paid checkout reachability proof. |
| `ENCLII_COMMERCIAL_GA_STRICT=true` | Converts missing optional proof inputs from warnings into failures. |

## Sign-off

| Role | Name | Date |
|------|------|------|
| QA | | |
| Product | | |

Record completion in [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md).

---

## Related

- [signup CLI](../cli/commands/signup.md)
- [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md) — run after platform deploy
