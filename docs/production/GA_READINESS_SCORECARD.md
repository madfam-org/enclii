# Commercial & Stability GA — readiness scorecard

> **Last updated:** 2026-05-22  
> **Canonical tracker:** [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md)  
> **Ops execution:** [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md)

Use this page for leadership and program reviews. Percentages are **judgment against program exit criteria**, not lines of code.

---

## Summary

| Track | Readiness | Blocker |
|-------|-----------|---------|
| **Stability GA** | **~55%** | Prod deploy, cluster P0, 30-day SLO not started |
| **Commercial GA (scoped)** | **~45–50%** | Stability GA + staging proofs + GTM publish |
| **Monetization path** | **~60–65%** | Signup/pricing live proof, published SLA |
| **Retention / support** | **~35–40%** | Live status page, support tiers, on-call proof |

**Engineering for bets A+B+C on `main`:** ~**85%** (proof, not net-new features, is the gap).

---

## Dimension scorecard

| Dimension | Target | Status | Evidence / next step |
|-----------|--------|--------|----------------------|
| **Security & tenancy** | Prod checklist + matrix green | 🟡 80% code / 40% prod | [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) in prod |
| **Platform stability** | 99.95% API × 30 days | 🔴 ~20% | Start clock after [PHASE0_OPS_RUNBOOK](./PHASE0_OPS_RUNBOOK.md) |
| **Cluster health** | Disk &lt;40%, Longhorn, Argo synced | 🔴 Open | [REMAINING_ITEMS.md](./REMAINING_ITEMS.md) P0 |
| **DR / backups** | Restore drill logged | 🟡 Partial | Drill in Phase 0 step 2.5 |
| **CI quality** | Blocking on `main` | 🟢 Done | Unit, UI, ecosystem E2E smokes |
| **Bet A — Previews** | Staging lifecycle pass | 🟡 Built / unproven | `PREVIEW_E2E_*` |
| **Bet B — Domains** | Staging lifecycle pass | 🟡 Built / unproven | `DOMAIN_E2E_*` |
| **Bet C — Volumes** | Staging + deploy pass | 🟡 Built / unproven | `STORAGE_E2E_*` |
| **Billing enforce** | Throttle on deploy/build | 🟢 Done | Waybill + `enclii billing throttles` |
| **Self-serve signup** | End-to-end in prod | 🟡 Checklist | [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) |
| **Pricing / checkout** | Landing + Dhanam aligned | 🟡 Partial | Paywall E2E may skip if not deployed |
| **SLA / legal** | Published externally | 🟡 Draft | [SLA_DRAFT.md](./SLA_DRAFT.md) |
| **Support / status** | Customer-visible | 🟡 Draft | [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md) |
| **GA messaging** | Changelog, no “95% ready” | 🟡 Draft | [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md) |

Legend: 🟢 Done · 🟡 In progress / draft / unproven · 🔴 Not started / blocking

---

## Sign-off gates (checkbox)

### Gate 1 — Phase 0 ops (required before SLO clock)

| Item | Owner | Date | Initials |
|------|-------|------|----------|
| Deploy `main` + migration 030 | Ops | | |
| SECURITY_RELEASE_PR complete | Ops | | |
| REMAINING_ITEMS P0 (disk, Longhorn, PostHog) | Ops | | |
| Restore drill evidence filed | Ops | | |

### Gate 2 — Product staging proof

| Bet | Workflow / local pass | Date | Initials |
|-----|----------------------|------|----------|
| A Previews | | | |
| B Domains | | | |
| C Storage | | | |

Refs: [STAGING_SECRETS_SETUP.md](./STAGING_SECRETS_SETUP.md) · Actions **Commercial GA staging proof**

### Gate 3 — Monetization QA

| Item | Date | Initials |
|------|------|----------|
| [Signup & pricing checklist](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) | | |

### Gate 4 — Stability GA (30 days)

| Item | Start date | End date | Met? |
|------|------------|----------|------|
| SLO window (99.95% API) | | | |
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

- [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md)  
- [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md)
