# Commercial GA — execution roadmap to 100%

> **Master plan:** [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md) — full remediation blueprint  
> **Program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md)  
> **Live checklist:** [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md) · [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md)  
> **Execution started:** 2026-05-23 · **Plan published:** 2026-05-29  
> **Doctrine:** Enclii-first; record gaps in [ADAPTER_GAPS.md](../ADAPTER_GAPS.md)

Time-ordered summary of waves from current state to **100% scoped Commercial GA**. Percentages are **gate completion**, not LOC.

---

## Where we are now (2026-05-29)

| Track | Completion | Notes |
|-------|------------|--------|
| Engineering (bets A+B+C on `main`) | **~90%** | Code merged; staging proofs **green** (2026-05-23) |
| Phase 0 ops | **~70%** | O-1/2/4/7 done; O-3 partial; O-5–O-6 open |
| Staging proof (Gate 2) | **100%** | [Actions run 26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825) |
| SLO window (Gate 4) | **0%** | **30 calendar days** — starts after Wave 0+1 sign-off |
| GTM / legal (Gate 5) | **~15%** | Drafts only |

**Critical path:** Finish Wave 0 → Wave 1 → **start SLO clock** → monetization QA → publish SLA/support/changelog.

**Target Commercial GA announce:** ~2026-07-14 (if SLO starts ~2026-06-07).

---

## Wave 0 — Phase 0 ops (target: 1–2 days)

**Exit:** Gate 1 signed; O-1–O-7 in [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md) marked done.

| ID | Task | Owner | Status (2026-05-29) |
|----|------|-------|---------------------|
| O-1 | Deploy API+UI from `main` | Ops | **Done** — Argo `enclii-infrastructure` revision `848c8968` |
| O-2 | Migration 030 (`rollout_blocked_reason`) | Ops | **Done** — verified in prod DB (2026-05-23) |
| O-3 | [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) | Ops | Open — platform sign-off required |
| O-4 | PostHog + Longhorn orphans | Ops | Adapter shipped — `enclii ops storage prune-detached` |
| O-5 | Longhorn helm CPU | Ops | Open |
| O-6 | Disk prune &lt;40% | Ops | Open — nodes not in DiskPressure |
| O-7 | Post-deploy smoke | Ops | **Done** — `/health/public` + `/health/ready` OK |

**Commands (Enclii-first):**

```bash
enclii ops apps sync enclii-infrastructure --apply --reason "Commercial GA Phase 0"
curl -fsS https://api.enclii.dev/health/public
curl -fsS https://api.enclii.dev/health/ready
```

Detail: [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md) · [COMMERCIAL_GA_MASTER_PLAN.md §Wave 0](./COMMERCIAL_GA_MASTER_PLAN.md).

---

## Wave 1 — Operational maturity (target: 3–5 days)

**Exit:** DR/Vault/Cosign complete; ready to start SLO clock.

| ID | Task | Status (2026-05-29) |
|----|------|---------------------|
| O-8 | ArgoCD sync sweep | Ops | Adapter shipped — `enclii ops apps sync-sweep` |
| O-9 | Backup credentials + restore drill | Open |
| O-10 | Vault init → unseal → ESO sync | Open |
| O-11 | Cosign enforce (phased) | Open |
| O-13 | A Previews staging proof | **Done** 2026-05-23 |
| O-14 | B Domains staging proof | **Done** 2026-05-23 |
| O-15 | C Storage staging proof | **Done** 2026-05-23 |

Gate 2 (O-13–O-15) is complete. Wave 1 focuses on O-8–O-11.

---

## Wave 2 — Monetization QA (parallel after Wave 0 deploy)

**Exit:** Gate 3 signed.

| ID | Task |
|----|------|
| O-16 | [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) |
| O-17 | `ENCLII_SIGNUP_ENABLED` in prod Switchyard env |
| O-18 | Landing pricing / paywall deployed or documented skip |
| O-22 | Dhanam checkout / tier alignment smoke |

---

## Wave 3 — Stability GA: 30-day SLO clock (calendar-bound)

**Exit:** Gate 4 — 99.95% API availability over 30 days; zero Sev-1 &gt;7 days unmitigated.

| ID | Task | When |
|----|------|------|
| O-12 | **Record SLO start date** in scorecard | Day 0 after O-3 + O-9 sign-off |

**Cannot accelerate:** Commercial GA announce (Wave 4) waits for this window unless leadership accepts risk.

---

## Wave 4 — Commercial GA 100% (after Wave 3)

**Exit:** Gate 5 signed; external “95% ready” retired.

| ID | Deliverable |
|----|-------------|
| O-19 | [SLA_DRAFT.md](./SLA_DRAFT.md) — legal approved, published |
| O-20 | [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md) + customer status page |
| O-21 | [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md) |
| — | Privacy Policy + Terms of Service (legal — draft during Wave 3) |

---

## Progress model (how we reach 100%)

| Milestone | Cumulative program % |
|-----------|----------------------|
| Wave 0 complete | ~65% Stability · ~72% Commercial |
| Wave 1 complete (Gate 2 already done) | ~85% Stability-ready · ~68% Commercial |
| Wave 2 complete | ~75% Commercial (monetization QA) |
| Wave 3 complete (30d) | **100% Stability GA** · ~85% Commercial |
| Wave 4 complete | **100% scoped Commercial GA** |

---

## Adapter gaps to close during execution

| Gap | Wave | Target fix |
|-----|------|------------|
| `POST .../reconcile-services` has no CLI | 0–1 | **Done** — `enclii projects reconcile-services` |
| Tunnel routes read-only in `providers cloudflare tunnels` | 0–1 | **Done** — `enclii providers cloudflare tunnels-apply --project <slug>` |
| Longhorn helm CPU upgrade | 0 | **Done** — `enclii ops storage settings-apply` |
| Prod DB migration verify | 0 | **Done** — `enclii db schema` |
| Staging proof secrets not in GH environment | 1 | **Workflow wired** — create `commercial-ga-staging` environment |

Log new rows in [ADAPTER_GAPS.md](../ADAPTER_GAPS.md).

---

## Daily execution ritual

1. Update status column in [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md).  
2. Run blocking smokes on `main` (CI) + prod curl checks above.  
3. Move **one** Wave 0/P1 ops item to done (Enclii-first).  
4. If blocked, append adapter gap — do not add permanent `kubectl` to runbooks.

---

## Related

- [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md)  
- [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md)  
- [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md)  
- [.github/workflows/commercial-ga-staging-proof.yml](../../.github/workflows/commercial-ga-staging-proof.yml)
