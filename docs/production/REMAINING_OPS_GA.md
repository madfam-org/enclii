# Remaining ops — Commercial & Stability GA

> Canonical execution queue. Update this file for open GA ops work; use `GA_READINESS_SCORECARD.md` only as the dashboard and sign-off view. Historical plans should point here instead of carrying independent status.

> **Master plan:** [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md)  
> **Doctrine:** Enclii web, API, or CLI first. Break-glass `kubectl`/SSH only with documented reason; record gaps in [ADAPTER_GAPS.md](../ADAPTER_GAPS.md).  
> **Execution order:** [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md) · **Sign-off:** [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md)  
> **Time-ordered roadmap:** [COMMERCIAL_GA_EXECUTION_ROADMAP.md](./COMMERCIAL_GA_EXECUTION_ROADMAP.md)  
> **Cluster detail:** [REMAINING_ITEMS.md](./REMAINING_ITEMS.md) (full command reference)

This document lists **only open ops work** blocking Stability GA and Commercial GA. Engineering for bets A/B/C is on `main`; do not wait on new feature code.

---

## P0 — Blocking (do first, ~3 hours)

| ID | Task | Est. | Status (2026-05-23) | Reference |
|----|------|------|---------------------|-----------|
| O-1 | Deploy Switchyard API + UI from `main` | 30m | **Done** — Argo `enclii-infrastructure` @ `848c8968` | [PHASE0 §1](./PHASE0_OPS_RUNBOOK.md) |
| O-2 | Apply DB migration **030** (`rollout_blocked_reason`) | 10m | **Done** — column present in prod `enclii.services` (2026-05-30) | `apps/switchyard-api/internal/db/migrations/030_*` |
| O-3 | Complete [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) | 30m | **Partial** — automatable smokes pass; manual tenant IDOR (step 3) remains | Steps 1–7 |
| O-4 | PostHog cleanup + orphaned Longhorn volumes | 15m | **Done** — PostHog ns gone; 8 detached orphans pruned (2026-05-30) | [REMAINING_ITEMS §1A](./REMAINING_ITEMS.md) |
| O-5 | Longhorn CPU settings apply | 10m | **Done** — `guaranteed-instance-manager-cpu=3` live | [REMAINING_ITEMS §0.2.2](./REMAINING_ITEMS.md) |
| O-6 | Disk prune (crictl, journal, logs) | 10m | **Done** — `node-maintenance-ga-*` jobs ran; 9 stale images pruned | [REMAINING_ITEMS §1C](./REMAINING_ITEMS.md) |
| O-7 | API post-deploy smoke | 10m | **Done** — adapters live 2026-05-30 (`post-deploy-ga-adapters.sh --public-only` 4/4) | Enclii or CI rerun on prod URL |

**Track in GitHub:** use issue template **Commercial GA — Phase 0 ops gate**.

---

## P1 — Before announcing Stability GA (~2 hours)

| ID | Task | Est. | Done when | Reference |
|----|------|------|-----------|-----------|
| O-8 | ArgoCD sync sweep (OutOfSync apps) | 10m | **Done** — 0 OutOfSync apps; `core-services` @ `98be6d41` (2026-05-30) | [REMAINING_ITEMS §1D](./REMAINING_ITEMS.md) |
| O-9 | Backup credentials + restore drill | 25m | **Partial** — backup jobs green; restore evidence logged 2026-05-30 ([RESTORE_DRILL_LOG](../runbooks/RESTORE_DRILL_LOG.md)); CronJob fix pending git sync | [REMAINING_ITEMS §1E](./REMAINING_ITEMS.md) |
| O-10 | Vault init → unseal → ESO syncing | 60m | **Partial** — merge ESO live; run `scripts/ga-o10-enclii-vault-backfill.sh` with write `VAULT_TOKEN` | [REMAINING_ITEMS §1F](./REMAINING_ITEMS.md) |
| O-11 | Cosign enforce (phased namespaces) | 20m | **Done** — `verify-image-signatures` ClusterPolicy Enforce (2026-05-30) | [REMAINING_ITEMS §1G](./REMAINING_ITEMS.md) |
| O-12 | Start **30-day SLO clock** (99.95% API) | — | Start date recorded in scorecard | [GA_READINESS_SCORECARD §Gate 4](./GA_READINESS_SCORECARD.md) |

---

## P1 — Product staging proof (after O-1–O-7)

Requires API token + throwaway services. Configure secrets per [STAGING_SECRETS_SETUP.md](./STAGING_SECRETS_SETUP.md).

| ID | Bet | Status (2026-05-23) | Evidence |
|----|-----|---------------------|----------|
| O-13 | A Previews | **Done** | Actions run [26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825) — 3 passed |
| O-14 | B Domains | **Done** | Same run — 5 passed |
| O-15 | C Storage | **Done** | Same run — 2 passed |

**Or:** Actions → **Commercial GA staging proof** → `all`.

Record pass date in [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md) and [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md) Gate 2.

---

## P1 — Monetization QA (after deploy)

| ID | Task | Owner | Reference |
|----|------|-------|-----------|
| O-16 | Signup + pricing manual checklist | GTM/QA | [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) |
| O-17 | Confirm `ENCLII_SIGNUP_ENABLED` in target env | Ops | **Open** — not set on prod; `/v1/signup/*` returns 404 (disabled-by-default) |
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

| Gate | Open items | Done |
|------|------------|------|
| Phase 0 (O-1–O-7) | 1 (O-3 manual tenant smoke) | 6 |
| Stability P1 (O-8–O-12) | 2 (O-9 partial, O-12 SLO clock) | 3 |
| Staging proof (O-13–O-15) | 0 | 3 |
| Monetization QA (O-16–O-18) | 3 | 0 |
| Commercial publish (O-19–O-22) | 4 | 0 |

**Total open ops tasks:** 9 of 22 (13 complete or mostly complete).

## 2026-05-25 adapter progress

`enclii secrets sync` now covers audited ExternalSecret reconciliation refresh. `enclii secrets rotate` exists as a plan-first operation contract only; rotation apply remains blocked on safe Vault writer and dual-consumer cutover implementation.

## Runtime execution reference

Remaining production-state work should follow `GA_RUNTIME_EXECUTION.md` and the private `internal-devops/runbooks/ga-ops-execution-pack.md` execution pack. Keep this file as the task queue only.
