# Commercial GA tracker

> **Program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md)  
> **Master plan:** [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md)  
> **Scorecard:** [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md)  
> **Open ops queue:** [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md)  
> **Default bets:** Preview environments (A) + Custom domains (B) + PVCs (C)  
> **Last updated:** 2026-05-29  
> **Execution roadmap:** [COMMERCIAL_GA_EXECUTION_ROADMAP.md](./COMMERCIAL_GA_EXECUTION_ROADMAP.md)

## Gate summary

| Track | Target | Status |
|-------|--------|--------|
| Stability GA | Phase 0–2 exit criteria | **In progress** — runtime health green on 2026-05-25; restore proof, support proof, and SLO clock open |
| Commercial GA | Phase 3 + Section 1 commercial checklist | **In progress** — blocked on paid self-serve proof, webhook replay proof, support/SLA publish, and 30-day SLO |

## Phase 0 — Ops (Enclii-first)

| Item | Owner | Status |
|------|-------|--------|
| `SECURITY_RELEASE_PR.md` production checklist | Ops | Open |
| `REMAINING_ITEMS.md` cluster P0/P1 | Ops | In progress (PostHog ns removed) |
| Migration `030` (`rollout_blocked_reason`) in prod | Ops | Verify column — API migrates on startup |
| Deploy `main` (Argo `enclii-infrastructure`) | Ops | **Done** 2026-05-23 (`848c8968`) |
| Argo aggregate and service registry health | Ops | **Done** 2026-05-25 (`bad=0`, `degraded_count=0`) |
| Stale Blueprint Harvester service rows | Ops | **Done** 2026-05-25 (6 stale `pod_count=0` rows removed) |
| Lifecycle delete FK migration 031 | Ops | **Done** 2026-05-25 (`deployment_lifecycle_events` refs detach on delete) |
| Staging lifecycle proofs (bets A/B/C) | Ops | **Done** 2026-05-23 — [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md) |
| Phase 0 execution order | Ops | [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md) |

## Phase 1 — Engineering (code on `main`)

| Item | Status |
|------|--------|
| AuthZ handler audit + matrix tests | Done (PR #250) |
| Doc-guard active paths only (CI green) | Done (PR #250) |
| `GET /v1/deployments` user-scoped (non-admin) | Done (PR #250) |
| Log routes `mustServiceAccess` | Done (PR #250) |
| Reconciler queue metrics | Done (prior `main`) |
| OpenAPI canonical path + sdk-ts CI drift check | Done (PR #253–254) |
| OpenAPI → `sdk-ts` UI adoption | In progress — previews + billing via `@madfam/enclii-sdk`; domains/deployments local |
| Waybill budget **enforce** on deploy/build | Done — `enforceBudgetNotThrottled` on deploy + build (main, post-#250) |
| Budget/throttle visibility in CLI | Done — `enclii billing throttles`; UI at `/projects/:slug/billing` |

## Product bet A — Preview environments

| Capability | Status |
|------------|--------|
| API (`/v1/previews`, PR webhook) | Implemented — `CreatePreview` authZ + lifecycle tests |
| Reconciler / URL lifecycle | Partial — verify E2E in staging |
| UI + CLI docs | UI wired (Previews tab); CLI + [preview-environments.md](../guides/preview-environments.md) |
| E2E test (SOFTWARE_SPEC) | **Staging proof green** 2026-05-23 (Actions 26328015825) |

## Product bet B — Custom domains + TLS

| Capability | Status |
|------------|--------|
| Domain API + cert-manager path | Implemented |
| `ToggleZeroTrust` authZ | Done (PR #250) |
| DNS verify + Junction routing E2E | **Staging proof green** 2026-05-23 |
| User guide + CLI | [custom-domains.md](../guides/custom-domains.md), `enclii domains` |

## Product bet C — Persistent volumes

| Capability | Status |
|------------|--------|
| Reconciler `generatePVCs` / mount | Implemented — unit tests exist |
| UI volume attach + lifecycle | Settings editor + API persist volumes (`ServiceVolumesEditor`) |
| CLI + user guide | `enclii volumes` + [persistent-volumes.md](../guides/persistent-volumes.md) |
| E2E stateful deploy | **Smoke green** 2026-05-23; full deploy slice optional via `STORAGE_E2E_RELEASE_ID` |

## Commercial wrap (GTM / legal)

| Deliverable | Status |
|-------------|--------|
| SLA (99.95%) | Draft — [SLA_DRAFT.md](./SLA_DRAFT.md) (legal review) |
| Support tiers + status page | Draft — [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md) |
| GA changelog; retire “95% ready” externally | Draft — [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md) |
| Pricing + self-serve signup tested | Checklist — [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md); signup API smoke in CI |
| Commercial GA proof harness | Added 2026-05-25 — `.github/workflows/commercial-ga-proof.yml` / `make commercial-ga-proof`; strict mode requires billing token, project slug, and Dhanam checkout URL |

## Execution log (2026-05-23)

| Action | Result |
|--------|--------|
| PR #266 merged (`REMAINING_OPS_GA`, sdk-ts billing, structured errors) | Done |
| Argo `enclii-infrastructure` at `848c8968` | Synced / Healthy |
| `gh workflow run commercial-ga-staging-proof` | **Green** — [run 26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825) (A/B/C passed) |
| Migration 030 `rollout_blocked_reason` | Verified in `enclii` Postgres |
| Security release automated checks | [SECURITY_RELEASE_VERIFICATION.md](./SECURITY_RELEASE_VERIFICATION.md) (2026-05-23) |
| Longhorn detached orphan cleanup | 5 volumes deleted (break-glass; 1 detached remains) |
| Signup in prod | **Disabled** — enable `ENCLII_SIGNUP_ENABLED` for Wave 2 (O-17) |
| Execution roadmap published | [COMMERCIAL_GA_EXECUTION_ROADMAP.md](./COMMERCIAL_GA_EXECUTION_ROADMAP.md) |

## Execution log (2026-05-25)

| Action | Result |
|--------|--------|
| Argo aggregate health checked | **Green** - 63 applications, `bad=0` |
| Switchyard service health checked | **Green** - 104 healthy services, `degraded_count=0`, `unhealthy_count=0` |
| Stale Blueprint Harvester registry rows removed | **Done** - removed 6 legacy unprefixed rows with `pod_count=0`; live prefixed services remain healthy |
| Lifecycle delete constraint fixed | **Done** - migration 031 added so lifecycle event references can detach when releases/services are deleted |
| Platform backup job cleanup hardened | **Done** - CronJob TTL cleanup added for backup and restore drill jobs |
| Public endpoints checked | **Green** - `api.enclii.dev`, `api.janua.dev`, and `auth.madfam.io` health/discovery returned HTTP 200 |

## Execution log (2026-05-29)

| Action | Result |
|--------|--------|
| Commercial GA master plan published | [COMMERCIAL_GA_MASTER_PLAN.md](./COMMERCIAL_GA_MASTER_PLAN.md) |
| `enclii projects reconcile-services` CLI | Shipped — closes reconcile-services adapter gap |
| `enclii db schema` + `GET /v1/admin/db/schema` | Shipped — migration 030 verify |
| `enclii ops storage settings-apply` + `enclii admin ga-verify` | Shipped — O-5 + Gate 1 automation |
| `enclii ops storage prune-detached` + `scripts/wave0-ga-ops.sh` | Shipped — O-4 orphan prune + Wave 0 orchestration |
| `commercial-ga-staging` GH environment wired | Workflow references environment; create env + secrets per STAGING_SECRETS_SETUP |
| `enclii ops apps sync-sweep` + `scripts/wave1-ga-ops.sh` | Shipped — O-8 + Wave 1 orchestration |
| ROADMAP.md + docs index updated | GA program section; scorecard percentages canonical |
| Target announce date recorded | ~2026-07-14 (contingent on SLO start ~2026-06-07) |

## Next merge train

1. ~~AuthZ / OpenAPI / preview + domains + storage E2E~~ → `main` (2026-05-22)
2. ~~PVC persist + settings UI (#258)~~ → `main`
3. ~~Deploy `main` + migration 030~~ — runtime deploy complete; **security sign-off (O-3) remains open**
4. ~~Staging proofs (Gate 2)~~ — **Done** 2026-05-23
5. ~~Preview CLI + docs (bet A)~~ → `main`
6. ~~Volumes CLI + bet B/C guides~~ → `main`
7. **Wave 0 close:** O-3, O-5, O-6 → **Wave 1:** O-8–O-11 → **Wave 2:** monetization QA → **Wave 3:** 30-day SLO → **Wave 4:** legal publish
