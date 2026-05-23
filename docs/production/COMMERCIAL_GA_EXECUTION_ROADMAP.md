# Commercial GA — execution roadmap to 100%

> **Program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md)  
> **Live checklist:** [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md) · [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md)  
> **Execution started:** 2026-05-23  
> **Doctrine:** Enclii-first; record gaps in [ADAPTER_GAPS.md](../ADAPTER_GAPS.md)

This is the **time-ordered execution roadmap** from current state (~50% Commercial GA) to **100% scoped Commercial GA**. Percentages below are **gate completion**, not LOC.

---

## Where we are now (2026-05-23)

| Track | Completion | Notes |
|-------|------------|--------|
| Engineering (bets A+B+C on `main`) | **~85%** | Code merged; staging proofs open |
| Phase 0 ops | **~25%** | `enclii-infrastructure` Argo **Synced** at `main` (`848c8968`); security checklist + cluster P0 queue open |
| Staging proof (Gate 2) | **0%** | Workflow on `main`; needs GH secrets |
| SLO window (Gate 4) | **0%** | **30 calendar days** — starts after Phase 0 sign-off |
| GTM / legal (Gate 5) | **~15%** | Drafts only |

**Critical path:** Finish Phase 0 ops → run staging proofs → **start SLO clock** → monetization QA → publish SLA/support/changelog.

---

## Wave 0 — Phase 0 ops (target: 1 working day)

**Exit:** Gates 1 checklist in scorecard signed; O-1–O-7 in [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md) marked done.

| ID | Task | Owner | Status (2026-05-23) |
|----|------|-------|---------------------|
| O-1 | Deploy API+UI from `main` | Ops | **Done** — Argo `enclii-infrastructure` revision `848c8968` |
| O-2 | Migration 030 (`rollout_blocked_reason`) | Ops | **Verify** — runs on API startup; confirm column in prod DB |
| O-3 | [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) | Ops | Open |
| O-4 | PostHog + Longhorn orphans | Ops | **Partial** — `posthog` namespace absent |
| O-5 | Longhorn helm CPU | Ops | Open |
| O-6 | Disk prune &lt;40% | Ops | Open — nodes not in DiskPressure |
| O-7 | Post-deploy smoke | Ops | **Partial** — `/health/public` + `/health/ready` OK |

**Commands (Enclii-first):**

```bash
# Confirm deploy revision
kubectl get application enclii-infrastructure -n argocd -o jsonpath='{.status.sync.revision}{"\n"}'

# Argo reconcile (idempotent)
enclii ops apps sync enclii-infrastructure --apply --reason "Commercial GA Phase 0"

# Post-deploy
curl -fsS https://api.enclii.dev/health/public
curl -fsS https://api.enclii.dev/health/ready
```

---

## Wave 1 — Staging product proof (target: 2–3 days after Wave 0)

**Exit:** Gate 2 signed; tracker bet rows dated.

| ID | Bet | Action |
|----|-----|--------|
| O-13 | A Previews | Configure [STAGING_SECRETS_SETUP.md](./STAGING_SECRETS_SETUP.md) → `workflow_dispatch` **Commercial GA staging proof** → `preview` |
| O-14 | B Domains | Same workflow → `domains` |
| O-15 | C Storage | Same workflow → `storage` (set `STORAGE_E2E_RELEASE_ID`) |

```bash
gh workflow run commercial-ga-staging-proof -f bets=all
gh run watch --exit-status
```

Local fallback: `tests/e2e-ecosystem` Playwright projects per [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md).

---

## Wave 2 — Monetization QA (parallel with Wave 1 after deploy)

**Exit:** Gate 3 signed.

| ID | Task |
|----|------|
| O-16 | [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) |
| O-17 | `ENCLII_SIGNUP_ENABLED` in prod Switchyard env |
| O-18 | Landing pricing / paywall deployed or documented skip |

---

## Wave 3 — Stability GA: 30-day SLO clock (calendar-bound)

**Exit:** Gate 4 — 99.95% API availability over 30 days; zero Sev-1 &gt;7 days unmitigated.

| ID | Task | When |
|----|------|------|
| O-8–O-11 | Argo sweep, backup drill, Vault, Cosign | Before or early in window |
| O-12 | **Record SLO start date** in scorecard | Day 0 after Wave 0 sign-off |

**Cannot accelerate:** Commercial GA announce (Wave 4) waits for this window unless leadership accepts risk and scopes SLA differently.

---

## Wave 4 — Commercial GA 100% (after Wave 3)

**Exit:** Gate 5 signed; external “95% ready” retired.

| ID | Deliverable |
|----|-------------|
| O-19 | [SLA_DRAFT.md](./SLA_DRAFT.md) — legal approved, published |
| O-20 | [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md) + customer status page |
| O-21 | [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md) |
| O-22 | Dhanam checkout / tier alignment smoke |

**Engineering polish (non-blocking):** sdk-ts in remaining UI surfaces; structured errors on all handlers — see scorecard “post-GA”.

---

## Progress model (how we reach 100%)

| Milestone | Cumulative program % |
|-----------|----------------------|
| Wave 0 complete | ~60% Stability · ~55% Commercial |
| Wave 1 + 2 complete | ~70% Commercial (proof + monetization QA) |
| Wave 3 complete (30d) | **100% Stability GA** · ~85% Commercial |
| Wave 4 complete | **100% scoped Commercial GA** |

---

## Adapter gaps to close during execution

| Gap | Wave | Target fix |
|-----|------|------------|
| `POST .../reconcile-services` has no CLI | 0–1 | `enclii projects reconcile-services` |
| Tunnel routes read-only in `providers cloudflare tunnels` | 0–1 | Document junctions as write path; optional `tunnels-apply` |
| DB migration 030 no one-shot Enclii Job | 0 | Pre-deploy hook or documented `enclii jobs run-once` |
| Staging proof secrets not in GH environment | 1 | Create `commercial-ga-staging` environment |

Log new rows in [ADAPTER_GAPS.md](../ADAPTER_GAPS.md).

---

## Daily execution ritual

1. Update status column in [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md).  
2. Run blocking smokes on `main` (CI) + prod curl checks above.  
3. Move **one** Wave 0/P1 ops item to done (Enclii-first).  
4. If blocked, append adapter gap — do not add permanent `kubectl` to runbooks.

---

## Related

- [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md)  
- [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md)  
- [.github/workflows/commercial-ga-staging-proof.yml](../../.github/workflows/commercial-ga-staging-proof.yml)
