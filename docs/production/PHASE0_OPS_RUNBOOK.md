# Phase 0 ops runbook — Stability GA deploy gate

> **Program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) · [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md)  
> **Task checklist:** [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md)  
> **Doctrine:** Enclii web, API, or CLI for routine production work. Record adapter gaps; use break-glass only with reason.

This is the **single execution order** for ops before Commercial GA staging proofs and the 30-day SLO window.

**Estimated time:** ~3 hours (cluster P0 ~2.5h + deploy/smoke ~30m), excluding Vault migration if not started.

---

## Preconditions

- [ ] `main` CI green (unit, integration, ecosystem E2E smoke on last push)
- [ ] Operator has Enclii admin/deploy access (not raw `kubectl` as default)
- [ ] Change window communicated if customer-facing

---

## Step 1 — Deploy application (`main`)

| # | Action | Done when |
|---|--------|-----------|
| 1.1 | Deploy Switchyard API + UI from `main` (Enclii / GitOps) | Running image matches `main` SHA |
| 1.2 | Apply DB migration **030** (`rollout_blocked_reason`) | Column exists; reconciler persists blocked reason — verify with `enclii db schema` |
| 1.3 | Verify Roundhouse ↔ API callback auth | See [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) steps 1–4 |
| 1.4 | Run `enclii admin ga-verify` (admin token) | Gate 1 automated evidence |
| 1.5 | Optional: `./scripts/wave0-ga-ops.sh` (dry-run) then `--apply --reason "..."` | O-4–O-6 Enclii-first queue |

**Sign-off:** Platform lead initials security checklist in SECURITY_RELEASE_PR.

---

## Step 2 — Cluster P0 (WS-B)

Execute in order from [REMAINING_ITEMS.md](./REMAINING_ITEMS.md) quick reference:

| # | Task | Est. |
|---|------|------|
| 2.1 | PostHog scale-down + orphaned Longhorn volume cleanup | 15m |
| 2.2 | Longhorn helm upgrade (committed CPU values) | 10m |
| 2.3 | Disk prune (crictl, journal, logs) | 10m |
| 2.4 | ArgoCD sync sweep | 10m |
| 2.5 | Backup credentials + restore drill | 25m |

**Targets:** disk &lt;40%, Longhorn healthy, ArgoCD synced, restore drill log archived.

Vault init/unseal (2.6) and Cosign enforce (2.7) are **P1** for Stability GA but not blocking the application deploy in step 1.

---

## Step 3 — Post-deploy smoke (automated)

Blocking smokes already run on every `main` push (ecosystem E2E). After prod deploy, confirm:

| Check | How |
|-------|-----|
| API readiness | `GET /health/ready` on production API |
| Auth gates | Preview/domains/storage smoke specs (401/403, not 502) |
| Tenant isolation | Non-admin cannot read other project UUID resources |

Optional manual: `enclii login` + `enclii whoami` against production API.

---

## Step 4 — Commercial GA staging proofs (bets A → B → C)

**Only after steps 1–3.** Use a **non-production-critical** staging service and API token.

| Order | Bet | Runbook section | Secrets |
|-------|-----|-----------------|---------|
| 1 | A Previews | [COMMERCIAL_GA_STAGING_PROOF.md §2](./COMMERCIAL_GA_STAGING_PROOF.md#2-local-proof-roi-order-a--b--c) | `PREVIEW_E2E_*` |
| 2 | B Domains | same | `DOMAIN_E2E_*` |
| 3 | C Storage | same | `STORAGE_E2E_*`, optional `STORAGE_E2E_RELEASE_ID` |

**GitHub:** Actions → **Commercial GA staging proof** → `all` (configure secrets per [STAGING_SECRETS_SETUP.md](./STAGING_SECRETS_SETUP.md)).

Record pass/fail + date in [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md).

---

## Step 5 — Start Stability GA clock

| # | Action |
|---|--------|
| 5.1 | Note deploy date as **SLO window day 0** (99.95% API availability target per [SOFTWARE_SPEC.md](../architecture/SOFTWARE_SPEC.md)) |
| 5.2 | Enable/on-call rotation per [INCIDENT_RESPONSE.md](../runbooks/INCIDENT_RESPONSE.md) |
| 5.3 | Schedule GTM artifacts after window (see [SLA_DRAFT.md](./SLA_DRAFT.md) — legal review required) |

---

## Rollback

- Application: Enclii rollback to previous known-good release (GitOps revision or `enclii` deploy flow).
- Migration 030: `030_services_rollout_blocked_reason.down.sql` only if column causes issues (unlikely; column has safe default).
- Do **not** delete Longhorn volumes during rollback without backup verification.

---

## References

| Doc | Purpose |
|-----|---------|
| [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) | AuthZ / Roundhouse release |
| [REMAINING_ITEMS.md](./REMAINING_ITEMS.md) | Full cluster queue |
| [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md) | Bet lifecycle proofs |
| [DR_RUNBOOK.md](./DR_RUNBOOK.md) | Restore drill detail |
