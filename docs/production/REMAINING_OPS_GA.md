# Remaining ops — Commercial & Stability GA

> **Doctrine:** Enclii web, API, or CLI first. Break-glass `kubectl`/SSH only with documented reason; record gaps in [ADAPTER_GAPS.md](../ADAPTER_GAPS.md).  
> **Execution order:** [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md) · **Sign-off:** [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md)  
> **Cluster detail:** [REMAINING_ITEMS.md](./REMAINING_ITEMS.md) (full command reference)

This document lists **only open ops work** blocking Stability GA and Commercial GA. Engineering for bets A/B/C is on `main`; do not wait on new feature code.

---

## P0 — Blocking (do first, ~3 hours)

| ID | Task | Est. | Done when | Reference |
|----|------|------|-----------|-----------|
| O-1 | Deploy Switchyard API + UI from `main` | 30m | Running image = target SHA | [PHASE0 §1](./PHASE0_OPS_RUNBOOK.md) |
| O-2 | Apply DB migration **030** (`rollout_blocked_reason`) | 10m | Column live in prod | `apps/switchyard-api/internal/db/migrations/030_*` |
| O-3 | Complete [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) | 30m | Roundhouse key + tenant smoke signed | Steps 1–7 |
| O-4 | PostHog cleanup + orphaned Longhorn volumes | 15m | `df` improved; posthog ns empty | [REMAINING_ITEMS §1A](./REMAINING_ITEMS.md) |
| O-5 | Longhorn helm upgrade (committed CPU values) | 10m | instance-managers &lt;200m | [REMAINING_ITEMS §1B](./REMAINING_ITEMS.md) |
| O-6 | Disk prune (crictl, journal, logs) | 10m | Disk &lt;40% | [REMAINING_ITEMS §1C](./REMAINING_ITEMS.md) |
| O-7 | API post-deploy smoke | 10m | `/health/ready` OK; ecosystem smokes green | Enclii or CI rerun on prod URL |

**Track in GitHub:** use issue template **Commercial GA — Phase 0 ops gate**.

---

## P1 — Before announcing Stability GA (~2 hours)

| ID | Task | Est. | Done when | Reference |
|----|------|------|-----------|-----------|
| O-8 | ArgoCD sync sweep (OutOfSync apps) | 10m | Apps Synced/Healthy (known exceptions documented) | [REMAINING_ITEMS §1D](./REMAINING_ITEMS.md) |
| O-9 | Backup credentials + restore drill | 25m | Drill log archived | [REMAINING_ITEMS §1E](./REMAINING_ITEMS.md) |
| O-10 | Vault init → unseal → ESO syncing | 60m | Sealed=false; secrets path canonical | [REMAINING_ITEMS §1F](./REMAINING_ITEMS.md) |
| O-11 | Cosign enforce (phased namespaces) | 20m | Policy verified per namespace | [REMAINING_ITEMS §1G](./REMAINING_ITEMS.md) |
| O-12 | Start **30-day SLO clock** (99.95% API) | — | Start date recorded in scorecard | [GA_READINESS_SCORECARD §Gate 4](./GA_READINESS_SCORECARD.md) |

---

## P1 — Product staging proof (after O-1–O-7)

Requires API token + throwaway services. Configure secrets per [STAGING_SECRETS_SETUP.md](./STAGING_SECRETS_SETUP.md).

| ID | Bet | Command / workflow | Secrets |
|----|-----|-------------------|---------|
| O-13 | A Previews | `npx playwright test --project=preview-lifecycle` | `PREVIEW_E2E_*` |
| O-14 | B Domains | `npx playwright test --project=domains-lifecycle` | `DOMAIN_E2E_*` |
| O-15 | C Storage | `npx playwright test --project=storage-smoke` | `STORAGE_E2E_*` (+ `RELEASE_ID` for deploy) |

**Or:** Actions → **Commercial GA staging proof** → `all`.

Record pass date in [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md) and [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md) Gate 2.

---

## P1 — Monetization QA (after deploy)

| ID | Task | Owner | Reference |
|----|------|-------|-----------|
| O-16 | Signup + pricing manual checklist | GTM/QA | [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) |
| O-17 | Confirm `ENCLII_SIGNUP_ENABLED` in target env | Ops | API env on Switchyard deployment |
| O-18 | Landing pricing section deployed (or document skip) | GTM | `enclii-paywall` E2E |

---

## P2 — Commercial GA announce (after SLO window + legal)

| ID | Task | Owner | Reference |
|----|------|-------|-----------|
| O-19 | Publish SLA (legal approved) | Legal/GTM | [SLA_DRAFT.md](./SLA_DRAFT.md) |
| O-20 | Publish support tiers + status page | GTM/Ops | [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md) |
| O-21 | Publish GA changelog; retire “95% ready” | GTM | [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md) |
| O-22 | Dhanam checkout / tier alignment smoke | GTM | [docs/faq/billing.md](../faq/billing.md) |

---

## Explicitly not blocking GA (defer)

| Item | Why defer |
|------|-----------|
| ESO CRD 0.9 → 0.16 | Maintenance window |
| Multi-region / edge | Post-GA program |
| Managed DB marketplace | Bet D / post-GA |
| PagerDuty | Email/Slack sufficient for Stability GA |
| PostgreSQL HA | When SLA &gt;99.9% sold |

---

## Enclii adapter gaps (ops should not normalize raw access)

When an ops step has no Enclii command yet, file a row in [ADAPTER_GAPS.md](../ADAPTER_GAPS.md) instead of adding permanent `kubectl` to runbooks.

| Area | Prefer | Break-glass only |
|------|--------|------------------|
| Deploy / rollback | Enclii UI, API, `enclii deploy` | GitOps emergency |
| Secrets | `enclii secrets`, Vault via runbook | `kubectl` into pods |
| Storage health | `enclii ops storage` | Longhorn UI |
| Domains / DNS | `enclii domains`, `enclii providers` | Provider console |

---

## Quick status dashboard

| Gate | Open items |
|------|------------|
| Phase 0 (O-1–O-7) | 7 |
| Stability P1 (O-8–O-12) | 5 |
| Staging proof (O-13–O-15) | 3 |
| Monetization QA (O-16–O-18) | 3 |
| Commercial publish (O-19–O-22) | 4 |

**Total open ops tasks:** 22 (many parallelizable after O-1).
