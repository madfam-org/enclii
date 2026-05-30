# Commercial & Stability GA — readiness scorecard

> Canonical dashboard and sign-off view. Do not track independent task state here; task state belongs in `REMAINING_OPS_GA.md`.

> **Last updated:** 2026-05-30  
> **Master plan:** [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md)  
> **Canonical tracker:** [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md)  
> **Execution roadmap:** [COMMERCIAL_GA_EXECUTION_ROADMAP.md](./COMMERCIAL_GA_EXECUTION_ROADMAP.md)  
> **Ops execution:** [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md)  
> **Open ops tasks (O-1–O-22):** [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md)

Use this page for leadership and program reviews. Percentages are **judgment against program exit criteria**, not lines of code.

---

## Summary

| Track | Readiness | Blocker |
|-------|-----------|---------|
| **Stability GA** | **~85%** | Gate 4 SLO clock running; weekly hygiene logged |
| **Commercial GA (scoped)** | **~78%** | Gate 3 ops closed; Gate 5 publish + SLO remain |
| **Monetization path** | **~80%** | Ops proof green; wizard deferred; SLA publish open |
| **Retention / support** | **~40%** | Customer-visible status, support tiers, on-call proof, and incident workflow remain open |

**Target Commercial GA announce:** ~2026-07-14 (30-day SLO starting after Wave 0+1 sign-off). See [COMMERCIAL_GA_MASTER_PLAN.md §6](./COMMERCIAL_GA_MASTER_PLAN.md).

---

## Dimension scorecard

| Dimension | Target | Status | Evidence / next step |
|-----------|--------|--------|----------------------|
| **Security & tenancy** | Prod checklist + matrix green | 🟢 80% code / 90% prod | [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) — automatable + tenant IDOR pass 2026-05-30 |
| **Platform stability** | 99.95% API × 30 days | 🟡 Clock running | SLO window started **2026-05-30** (ends ~2026-06-29) |
| **Cluster health** | Disk &lt;40%, Longhorn, Argo synced, 0 degraded services | 🟡 Current apps green | 2026-05-25: Argo aggregate `bad=0`; Switchyard service health `degraded_count=0` |
| **DR / backups** | Restore drill logged | 🟡 Partial | Drill in Phase 0 step 2.5 |
| **CI quality** | Blocking on `main` | 🟢 Done | Unit, UI, ecosystem E2E smokes |
| **Bet A — Previews** | Staging lifecycle pass | 🟢 Proven | Actions run 26328015825 (2026-05-23) |
| **Bet B — Domains** | Staging lifecycle pass | 🟢 Proven | Same run |
| **Bet C — Volumes** | Staging + deploy pass | 🟢 Smoke proven | Same run (full deploy slice optional) |
| **Billing enforce** | Throttle on deploy/build | 🟢 Done | Waybill + `enclii billing throttles` |
| **Self-serve signup** | End-to-end in prod | 🟢 Ops done | API/UI/Resend proven; wizard deferred (GTM, non-blocking) |
| **Pricing / checkout** | Landing + Dhanam aligned | 🟡 Partial | Landing + Dhanam CI green; tier copy drift in FAQ |
| **SLA / legal** | Published externally | 🟡 Draft | [SLA_DRAFT.md](./SLA_DRAFT.md) |
| **Support / status** | Customer-visible | 🟡 Draft | [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md) |
| **GA messaging** | Changelog, no “95% ready” | 🟡 Draft | [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md) |

Legend: 🟢 Done · 🟡 In progress / draft / unproven · 🔴 Not started / blocking

---

## Sign-off gates (checkbox)

### Gate 1 — Phase 0 ops (required before SLO clock)

| Item | Owner | Date | Initials |
|------|-------|------|----------|
| Deploy `main` + migration 030 | Ops | 2026-05-30 | `core-services` @ `98be6d41`; `rollout_blocked_reason` verified |
| Runtime health exception cleanup | Ops | 2026-05-25 | Argo aggregate `bad=0`; 59/59 project cards healthy (2026-05-30) |
| SECURITY_RELEASE_PR complete | Ops | 2026-05-30 | Automatable smokes + tenant IDOR pass (`security-release-tenant-smoke.sh` via port-forward) |
| REMAINING_ITEMS P0 (disk, Longhorn, PostHog) | Ops | 2026-05-30 | Wave 0 closed |
| Restore drill evidence filed | Ops | 2026-05-30 | [RESTORE_DRILL_LOG.md](../runbooks/RESTORE_DRILL_LOG.md) |

### Gate 2 — Product staging proof

| Bet | Workflow / local pass | Date | Initials |
|-----|----------------------|------|----------|
| A Previews | 2026-05-23 / **2026-05-30** | [26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825) / **[26676409679](https://github.com/madfam-org/enclii/actions/runs/26676409679)** | |
| B Domains | 2026-05-23 / **2026-05-30** | same / **26676409679** (6 passed) | |
| C Storage | 2026-05-30 | [26676106111](https://github.com/madfam-org/enclii/actions/runs/26676106111) / **26676409679** (4 passed incl. stateful deploy) | |

> Full **bets=all** green 2026-05-30 [26676409679](https://github.com/madfam-org/enclii/actions/runs/26676409679) after platform-domain verify fix (`f3bcadde`).

Refs: [STAGING_SECRETS_SETUP.md](./STAGING_SECRETS_SETUP.md) · Actions **Commercial GA staging proof**

### Gate 3 — Monetization QA

| Item | Date | Initials |
|------|------|----------|
| [Signup & pricing checklist](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) | 2026-05-30 (ops complete; wizard deferred) | Ops |

### Gate 4 — Stability GA (30 days)

| Item | Start date | End date | Met? |
|------|------------|----------|------|
| SLO window (99.95% API) | 2026-05-30 | ~2026-06-29 | In progress — [checkpoint log](./GATE4_SLO_WINDOW_LOG.md) |
| Zero Sev-1 &gt;7 days unmitigated | | | |

### Gate 5 — Commercial GA announce

| Item | Date | Initials |
|------|------|----------|
| SLA published (legal approved) | | |
| Support + status page live | | |
| GA changelog published | | |
| External “95% ready” retired | | |

---

## Explicitly post-GA (do not block announce)

- Multi-region / edge  
- Managed DB marketplace (bet D)  
- Full sdk-ts in all UI surfaces  
- Structured errors on every handler  
- 99.9%+ multi-region HA  

See [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) Phase 4.

---

## Related

- [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md)  
- [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md)  
- [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md)

## Runtime execution reference

Use `GA_RUNTIME_EXECUTION.md` for the Enclii command surfaces and `internal-devops/runbooks/ga-ops-execution-pack.md` for private operator execution details.
