# Commercial GA — master remediation & implementation plan

> **Created:** 2026-05-29  
> **Status:** Active — canonical program blueprint for 100% scoped Commercial GA  
> **Audience:** Platform engineering, SRE, product, GTM, legal  
> **Supersedes narrative only:** “95% ready” and “production-running beta” customer-facing copy until Gate 5 is signed

This document consolidates the full path from **~70% Commercial GA** (May 2026) to **100% scoped Commercial GA**. It does not replace task-level tracking — use the linked artifacts for execution and sign-off.

---

## Document map

| Role | Document |
|------|----------|
| **This plan** | Waves, timeline, workstreams, risks, definition of done |
| [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md) | Leadership dashboard and gate sign-off |
| [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md) | Open ops task queue (O-1–O-22) |
| [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md) | Program status and execution log |
| [COMMERCIAL_GA_EXECUTION_ROADMAP.md](./COMMERCIAL_GA_EXECUTION_ROADMAP.md) | Time-ordered wave summary |
| [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) | Phase 0–4 program structure and exit criteria |
| [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md) | Step-by-step Phase 0 ops order |

**Operating doctrine:** Enclii web, API, or CLI for routine production work. Break-glass `kubectl`/SSH only with documented reason; record gaps in [ADAPTER_GAPS.md](../ADAPTER_GAPS.md).

---

## 1. Definition of 100% (scoped Commercial GA)

**100% Commercial GA** means all **scoped** exit criteria in [GA_REMEDIATION_PLAN.md §1](./GA_REMEDIATION_PLAN.md) are met — **not** 100% of the [GAP_ANALYSIS.md](./GAP_ANALYSIS.md) Vercel/Railway matrix.

### Stability GA (prerequisite)

| Criterion | Evidence artifact |
|-----------|-------------------|
| Security deploy signed off | [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) checklist complete |
| AuthZ matrix green | `authz_matrix_test.go` + manual tenant smoke |
| Restore drill passed | [RESTORE_DRILL_LOG.md](../runbooks/RESTORE_DRILL_LOG.md) archived |
| Vault unsealed, ESO syncing | Vault health + ExternalSecret sync status |
| Cluster healthy | Disk &lt;40%, Longhorn &lt;200m CPU, Argo `bad=0` |
| 30-day SLO | 99.95% API availability; zero Sev-1 &gt;7 days unmitigated |
| Incident-ready | On-call rotation + tabletop exercised |

### Commercial GA adds

| Criterion | Evidence artifact |
|-----------|-------------------|
| Product bets A+B+C proven | Staging green (2026-05-23); prod smoke optional |
| Billing enforced | Waybill throttle on deploy/build |
| Monetization path proven | [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) signed |
| Legal/GTM published | SLA, support tiers, privacy/terms, GA changelog |
| Messaging harmonized | “95% ready” / “beta” retired externally |

### Explicitly post-GA (do not block 100%)

Multi-region/edge, managed DB marketplace (Bet D), full sdk-ts UI migration, structured errors on every handler, PostgreSQL HA, PagerDuty, ESO CRD 0.16 upgrade. See [GA_REMEDIATION_PLAN.md Phase 4](./GA_REMEDIATION_PLAN.md).

---

## 2. Current baseline (2026-05-29)

| Dimension | Readiness | Gap to 100% |
|-----------|-----------|-------------|
| Engineering (bets A/B/C) | ~90% | Prod E2E optional; adapter gaps |
| Phase 0 ops | ~70% | O-3, O-5, O-6 |
| Staging proof (Gate 2) | 100% | — |
| Security prod sign-off | ~60% | Manual Roundhouse + tenant smoke |
| DR/Vault | ~40% | O-9, O-10 |
| Monetization | ~15% | Signup disabled; checklist open |
| SLO window (Gate 4) | 0% | Not started |
| Legal/GTM (Gate 5) | ~15% | All drafts |

**Critical path:** Wave 0 → Wave 1 (DR/Vault) → start SLO clock → monetization QA → legal publish.

**Target announce date (if Waves 0–2 complete by 2026-06-07):** ~2026-07-14 (30-day SLO + legal publish).

---

## 3. Execution waves

### Wave 0 — Phase 0 ops closure (Days 1–2, ~4 hours)

**Goal:** Gate 1 signed in [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md).

| ID | Task | Owner | Effort | Done when |
|----|------|-------|--------|-----------|
| O-3 | Complete [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) | Ops + Backend | 45m | Steps 1–7 signed |
| O-3.1 | Set `ENCLII_ROUNDHOUSE_API_KEY` on API + Roundhouse | Ops | 10m | Callbacks + `git_repo` auth work |
| O-3.2 | Non-admin tenant isolation smoke | QA | 20m | Cross-tenant cron/junction/deployment denied |
| O-3.3 | Platform lead sign-off | Platform | 5m | Initials on SECURITY_RELEASE_PR |
| O-5 | Longhorn helm CPU upgrade | Ops | 10m | All `instance-manager` pods &lt;200m |
| O-6 | Disk prune | Ops | 10m | All nodes disk &lt;40% |
| O-4 | Final Longhorn orphan cleanup | Ops | 10m | 0 detached orphans |
| O-7 | Post-deploy smoke | CI/Ops | 10m | `/health/public`, `/health/ready`, auth gates |

**Execution order:** [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md).

**Engineering (parallel, non-blocking):**

| Task | Priority | Effort |
|------|----------|--------|
| Deploy `cloudflare.credentials` handler to prod | P1 | 1 PR |
| `enclii projects reconcile-services` CLI | P1 | **Done** 2026-05-29 |
| GitHub Environment `commercial-ga-staging` | P1 | **Workflow wired** — create env + secrets per STAGING_SECRETS_SETUP |
| Prod DB migration verify | — | **Done** — `enclii db schema` |

**Wave 0 exit → ~72% Commercial, ~65% Stability-ready**

---

### Wave 1 — Operational maturity (Days 3–7, ~3 hours + monitoring)

**Goal:** Prerequisites for SLO clock; Gate 1 fully signed.

| ID | Task | Owner | Effort | Done when |
|----|------|-------|--------|-----------|
| O-8 | ArgoCD sync sweep | Ops | 15m | Apps Synced/Healthy (document known exceptions) |
| O-9 | Backup credentials + restore drill | Ops + SRE | 45m | Drill log archived |
| O-10 | Vault init → unseal → ESO sync | Ops | 60m | `sealed=false`; ExternalSecrets syncing |
| O-11 | Cosign enforce (phased) | Ops + Security | 30m | Policy verified per namespace |
| 2.4.1 | Incident tabletop | SRE | 90m | [INCIDENT_RESPONSE.md](../runbooks/INCIDENT_RESPONSE.md) exercised |
| 2.1.1 | Loki operational | SRE | 4h | Retention, dashboard, runbook linked |
| 2.1.3 | AlertManager → Slack | SRE | 2h | Critical alerts route to on-call channel |

**Adapter gaps to close:** see [ADAPTER_GAPS.md](../ADAPTER_GAPS.md) — Longhorn settings apply, detached volume prune, and DB migration verify are **shipped** (2026-05-29); tunnel route mutation remains open.

**Wave 1 exit → ~85% Stability-ready (SLO clock may start)**

---

### Wave 2 — Monetization QA (Days 5–10, parallel with Wave 1 tail)

**Goal:** Gate 3 signed.

| ID | Task | Owner | Effort | Done when |
|----|------|-------|--------|-----------|
| O-17 | Enable `ENCLII_SIGNUP_ENABLED=true` in prod | Ops | 15m | `/v1/signup/*` not 404 |
| O-16 | Execute signup/pricing checklist | GTM + QA | 2h | All boxes in [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md) |
| O-18 | Landing pricing deployed | GTM | 1d | `enclii.dev` pricing live; paywall E2E green |
| O-22 | Dhanam checkout smoke | GTM | 30m | Pro tier checkout URL works |
| — | Configure strict GA proof secrets | Ops | 1h | Bearer token, project slug, checkout URL |
| — | Run `commercial-ga-proof` strict mode | CI | 30m | `ENCLII_COMMERCIAL_GA_STRICT=true` green |

**Wave 2 exit → ~75% Commercial (monetization proven)**

---

### Wave 3 — 30-day SLO clock (calendar-bound, ~30 days)

**Goal:** Gate 4 signed → **100% Stability GA**.

| ID | Task | Owner | When |
|----|------|-------|------|
| O-12 | Record SLO start date | Platform lead | Day 0 after O-3 + O-9 sign-off |
| — | SLO dashboard live | SRE | Day 0 |
| — | On-call rotation active | Ops | Day 0 |
| — | Weekly SLO review | SRE | Every Monday |
| — | Zero Sev-1 &gt;7 days | On-call | Continuous |

**SLO target:** 99.95% API availability per [SOFTWARE_SPEC.md](../architecture/SOFTWARE_SPEC.md). Error budget: 21.6 min/month.

**Parallel during window (non-blocking announce):** sdk-ts UI adoption, structured errors (incremental), HPA for core services, legal review of SLA/support/privacy.

**Wave 3 exit → 100% Stability GA, ~85% Commercial**

---

### Wave 4 — Commercial GA announce (~1–2 weeks after Wave 3)

**Goal:** Gate 5 signed → **100% scoped Commercial GA**.

| ID | Deliverable | Owner | Done when |
|----|-------------|-------|-----------|
| O-19 | SLA published | Legal + GTM | Customer-facing URL; credits finalized |
| O-20 | Support tiers + status | GTM + Ops | [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md) published |
| — | Privacy Policy + Terms of Service | Legal | Pages on enclii.dev + signup flow |
| O-21 | GA changelog | GTM | [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md) published |
| — | Retire “95% ready” / “beta” messaging | GTM | README, docs, landing, trust center |
| — | Security questionnaire / trust center | GTM | `apps/status` trust page reflects commitments |
| — | Commercial license path | Legal | [COMMERCIAL_LICENSE.md](../../COMMERCIAL_LICENSE.md) linked from pricing |

**GA announcement checklist:**

```
[ ] Stability GA signed (Gate 4)
[ ] SLA legal-approved and published
[ ] Support email + escalation path live
[ ] Privacy + Terms published
[ ] Signup enabled and tested in prod
[ ] Pricing page aligned with billing FAQ
[ ] GA changelog published
[ ] Status page shows component health
[ ] External messaging updated (no "beta")
[ ] Eng + Ops + Product + Legal initials on scorecard Gate 5
```

**Wave 4 exit → 100% scoped Commercial GA**

---

## 4. Progress model

| Milestone | Stability GA | Commercial GA |
|-----------|--------------|---------------|
| Baseline (2026-05-29) | ~74% | ~70% |
| Wave 0 complete | ~65% ready for clock | ~72% |
| Wave 1 complete (Gate 2 done) | ~85% ready | ~68% |
| Wave 2 complete | — | ~75% |
| Wave 3 complete (30d) | **100%** | ~85% |
| Wave 4 complete | 100% | **100%** |

---

## 5. Engineering workstreams

| WS | Name | Lead | GA-blocking items |
|----|------|------|-------------------|
| **WS-A** | Security & tenancy | Backend | Security release sign-off only |
| **WS-B** | Platform SRE & DR | Ops | Restore drill, Vault/ESO, cluster P0 |
| **WS-C** | API contract & quality | Backend + FE | None (incremental post-GA) |
| **WS-D** | Observability & incident | SRE | Loki operational for SLO evidence |
| **WS-E** | Product bets A/B/C | Product | Done on `main`; staging proven |
| **WS-F** | Commercial & GTM | Product + Legal | SLA, support, privacy, signup proof |

Detailed task IDs: [GA_REMEDIATION_PLAN.md Appendix A](./GA_REMEDIATION_PLAN.md).

---

## 6. Timeline (calendar)

Assuming program execution start **2026-05-29**:

| Week | Dates | Milestone |
|------|-------|-----------|
| W0 | May 29–30 | Wave 0 complete |
| W1 | Jun 2–6 | Wave 1 complete |
| W2 | Jun 9–13 | Wave 2 complete |
| W3–W6 | Jun 7–Jul 6 | 30-day SLO window (start ~Jun 7) |
| W7 | Jul 7–11 | Legal review complete |
| W8 | Jul 14–18 | **Commercial GA announce** |

**Total calendar time:** ~7–8 weeks, dominated by the non-compressible 30-day SLO window.

---

## 7. RACI & sign-off

| Gate | Accountable | Consulted | Evidence |
|------|-------------|-----------|----------|
| Gate 1 — Phase 0 ops | Platform lead (Ops) | Backend, Security | Scorecard §Gate 1 |
| Gate 2 — Staging proof | Ops | Product | Done 2026-05-23 |
| Gate 3 — Monetization | Product + GTM | QA, Ops | Signup checklist signed |
| Gate 4 — Stability GA | Platform lead (SRE) | Backend, Ops | 30-day SLO report |
| Gate 5 — Commercial GA | Product + GTM | Legal, Platform | Published SLA + changelog |

---

## 8. Risk register

| Risk | Impact | Mitigation |
|------|--------|------------|
| SLO clock fails mid-window | Delays GA 30+ days | Do not start until Wave 0+1 complete |
| Signup flow breaks in prod | Blocks Gate 3 | Enable in staging first; feature flag rollback |
| Legal review delays SLA | Blocks Gate 5 | Start legal review during Wave 3 |
| Vault migration complexity | Blocks Gate 1 | Timebox 60m; defer full migration if ESO works |
| Longhorn/disk spike during SLO | SLO breach | Complete O-5/O-6 before clock start |
| AGPL confusion for commercial buyers | Sales friction | Publish commercial license path in Wave 4 |
| Missing privacy/terms | Legal exposure | Legal draft during Wave 3 |
| Adapter gaps force break-glass | Doctrine violation | Log gaps; prioritize P1 adapters |

---

## 9. Immediate next actions

**Day 1 (Ops — ~2h):**

1. Complete [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) steps 1–7
2. Break-glass Longhorn helm upgrade (O-5) + disk prune (O-6)
3. Initial Gate 1 initials on scorecard

**Day 2 (Ops + SRE — ~2h):**

4. Restore drill (O-9)
5. Vault unseal + ESO verify (O-10)
6. Argo sweep (O-8) + Cosign phased enforce (O-11)

**Day 3 (Platform lead):**

7. Record SLO start date (O-12) if Gate 1 prerequisites met
8. Enable on-call rotation

**Parallel (GTM/Legal — start now):**

9. Kick legal review of [SLA_DRAFT.md](./SLA_DRAFT.md) + privacy/terms draft
10. Schedule Wave 2 monetization QA window

**Parallel (Engineering):**

11. Ship `cloudflare.credentials` to prod
12. `enclii projects reconcile-services` CLI
13. `enclii ops storage settings apply` for Longhorn

---

## 10. Daily execution ritual

1. Update [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md) status.
2. Run blocking smokes on `main` + prod health curls.
3. Move **one** Wave 0/P1 ops item to done (Enclii-first).
4. If blocked → append [ADAPTER_GAPS.md](../ADAPTER_GAPS.md); do not normalize permanent `kubectl` in runbooks.

---

## Related

- [CODEBASE_AUDIT_2026-05.md](./CODEBASE_AUDIT_2026-05.md) — May 2026 code remediation status  
- [SECURITY_RELEASE_VERIFICATION.md](./SECURITY_RELEASE_VERIFICATION.md) — automated security checks  
- [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md) — bet lifecycle proofs  
- [GA_RUNTIME_EXECUTION.md](./GA_RUNTIME_EXECUTION.md) — Enclii command surfaces for ops
