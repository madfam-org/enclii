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
| O-3 | Complete [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) | 30m | **Done** — automatable + tenant IDOR smoke pass (2026-05-30, port-forward) | Steps 1–7 |
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
| O-9 | Backup credentials + restore drill | 25m | **Done** — backup jobs green; restore drill logged 2026-05-30 ([RESTORE_DRILL_LOG](../runbooks/RESTORE_DRILL_LOG.md)) | [REMAINING_ITEMS §1E](./REMAINING_ITEMS.md) |
| O-10 | Vault init → unseal → ESO syncing | 60m | **Done** — merge ESO **SecretSynced**; Vault backfill optional (skipped) | [REMAINING_ITEMS §1F](./REMAINING_ITEMS.md) |
| O-11 | Cosign enforce (phased namespaces) | 20m | **Done** — `verify-image-signatures` ClusterPolicy Enforce (2026-05-30) | [REMAINING_ITEMS §1G](./REMAINING_ITEMS.md) |
| O-12 | Start **30-day SLO clock** (99.95% API) | — | **Started 2026-05-30** — Gate 1 signed off | [GA_READINESS_SCORECARD §Gate 4](./GA_READINESS_SCORECARD.md) |

---

## P1 — Product staging proof (after O-1–O-7)

Requires API token + throwaway services. Configure secrets per [STAGING_SECRETS_SETUP.md](./STAGING_SECRETS_SETUP.md).

| ID | Bet | Status (2026-05-23) | Evidence |
|----|-----|---------------------|----------|
| O-13 | A Previews | **Done** | [26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825); revalidated **2026-05-30** [26676409679](https://github.com/madfam-org/enclii/actions/runs/26676409679) |
| O-14 | B Domains | **Done** | Same; platform-domain verify fix `f3bcadde` — 6 passed in [26676409679](https://github.com/madfam-org/enclii/actions/runs/26676409679) |
| O-15 | C Storage | **Done** | Auth smokes 2026-05-23 [26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825); **full deploy slice** 2026-05-30 [26676106111](https://github.com/madfam-org/enclii/actions/runs/26676106111) — 4 passed |

**Or:** Actions → **Commercial GA staging proof** → `all`.

Record pass date in [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md) and [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md) Gate 2.

---

## P1 — Monetization QA (after deploy)

| ID | Task | Owner | Reference |
|----|------|-------|-----------|
| O-16 | Signup + pricing manual checklist | GTM/QA | **Partial** — automated proof green; Resend wired (`ee30d703`); manual wizard steps 3–7 pending |
| O-18 | Landing pricing section deployed (or document skip) | GTM | **Done** — pricing live on enclii.dev (Sovereign/$20, 2026-05-30) |

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
| Phase 0 (O-1–O-7) | 0 | 7 |
| Stability P1 (O-8–O-12) | 0 | 5 |
| Staging proof (O-13–O-15) | 0 | 3 |
| Monetization QA (O-16–O-18) | 1 | 2 |
| Commercial publish (O-19–O-22) | 4 | 0 |

**Total open ops tasks:** 4 of 22 (18 complete or mostly complete).

## 2026-05-30 Wave 2 — monetization automated proof

- **O-18:** Landing pricing deployed — `Simple, Transparent Pricing`, Sovereign **$20**, CTAs → `app.enclii.dev`.
- **O-16 (automated):** Signup API smoke **201**; signup page **200**; Dhanam checkout **200**; billing cost/budgets/throttles **200** — CI green [26676748746](https://github.com/madfam-org/enclii/actions/runs/26676748746) (9 pass, 1 warn: admin db/schema).
- **O-16 (manual):** Resend wired via Janua bridge; sender `noreply@janua.dev` (interim). Full wizard (verify → GitHub → provision) + tier copy sign-off remain.

## 2026-05-30 Wave 2 — signup enable (O-17)

GitOps @ `e420cbd7`: `ENCLII_SIGNUP_ENABLED=true`, `ENCLII_SELF_SERVICE_API_BASE_URL=https://api.enclii.dev`, `ENCLII_APP_BASE_URL=https://app.enclii.dev`. Verified: POST `/v1/signup` invalid email → **400**, valid email → **201** + `signup_id`. Verification emails require `ENCLII_RESEND_API_KEY` (optional — log-only without it). Manual flow: [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md).

## 2026-05-30 Wave 1 apply

Applied via `wave1-ga-ops.sh --apply`: Argo sync (`arc-runners`, `arc-runners-blue`), cosign labels (`monitoring`, `status`), ESO sweep green, manual backup/drill jobs triggered. Longhorn orphan prune + StorageClass apply blocked on `switchyard-api` SA RBAC — **fixed** in `85ad80a3`; Enclii-first prune verified.

## 2026-05-30 Staging env secrets

GitHub environment `commercial-ga-staging` populated **8/8** required secrets. Re-run proof: `gh workflow run commercial-ga-staging-proof -f bets=all`. Bet B (domains) requires live DNS for `DOMAIN_E2E_DOMAIN` or skips verify failure.

## 2026-05-25 adapter progress

`enclii secrets sync` now covers audited ExternalSecret reconciliation refresh. `enclii secrets rotate` exists as a plan-first operation contract only; rotation apply remains blocked on safe Vault writer and dual-consumer cutover implementation.

## Runtime execution reference

Remaining production-state work should follow `GA_RUNTIME_EXECUTION.md` and the private `internal-devops/runbooks/ga-ops-execution-pack.md` execution pack. Keep this file as the task queue only.
