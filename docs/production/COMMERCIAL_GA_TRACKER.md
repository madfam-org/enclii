# Commercial GA tracker

> **Program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md)  
> **Scorecard:** [GA_READINESS_SCORECARD.md](./GA_READINESS_SCORECARD.md)  
> **Open ops queue:** [REMAINING_OPS_GA.md](./REMAINING_OPS_GA.md)  
> **Default bets:** Preview environments (A) + Custom domains (B) + PVCs (C)  
> **Last updated:** 2026-05-22

## Gate summary

| Track | Target | Status |
|-------|--------|--------|
| Stability GA | Phase 0–2 exit criteria | In progress — authZ code largely complete; ops queue open |
| Commercial GA | Phase 3 + Section 1 commercial checklist | Not started (blocked on Stability GA deploy proof) |

## Phase 0 — Ops (Enclii-first)

| Item | Owner | Status |
|------|-------|--------|
| `SECURITY_RELEASE_PR.md` production checklist | Ops | Open |
| `REMAINING_ITEMS.md` cluster P0/P1 | Ops | Open |
| Migration `030` (`rollout_blocked_reason`) in prod | Ops | Open — see [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) |
| Staging lifecycle proofs (bets A/B/C) | Ops | [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md) · [STAGING_SECRETS_SETUP.md](./STAGING_SECRETS_SETUP.md) |
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
| E2E test (SOFTWARE_SPEC) | Smoke in CI; full lifecycle opt-in via `PREVIEW_E2E_*` env |

## Product bet B — Custom domains + TLS

| Capability | Status |
|------------|--------|
| Domain API + cert-manager path | Implemented |
| `ToggleZeroTrust` authZ | Done (PR #250) |
| DNS verify + Junction routing E2E | Smoke in CI; full lifecycle opt-in via `DOMAIN_E2E_*` env |
| User guide + CLI | [custom-domains.md](../guides/custom-domains.md), `enclii domains` |

## Product bet C — Persistent volumes

| Capability | Status |
|------------|--------|
| Reconciler `generatePVCs` / mount | Implemented — unit tests exist |
| UI volume attach + lifecycle | Settings editor + API persist volumes (`ServiceVolumesEditor`) |
| CLI + user guide | `enclii volumes` + [persistent-volumes.md](../guides/persistent-volumes.md) |
| E2E stateful deploy | Smoke in CI; volumes opt-in via `STORAGE_E2E_*`; deploy slice needs `STORAGE_E2E_RELEASE_ID` |

## Commercial wrap (GTM / legal)

| Deliverable | Status |
|-------------|--------|
| SLA (99.95%) | Draft — [SLA_DRAFT.md](./SLA_DRAFT.md) (legal review) |
| Support tiers + status page | Draft — [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md) |
| GA changelog; retire “95% ready” externally | Draft — [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md) |
| Pricing + self-serve signup tested | Checklist — [COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md](./COMMERCIAL_GA_SIGNUP_PRICING_CHECKLIST.md); signup API smoke in CI |

## Next merge train

1. ~~AuthZ / OpenAPI / preview + domains + storage E2E~~ → `main` (2026-05-22)
2. ~~PVC persist + settings UI (#258)~~ → `main`
3. **Deploy `main` + migration 030 + security checklist** — [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md) (ops — critical path)
4. **Staging proofs** — [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md) or Actions workflow `Commercial GA staging proof`
5. ~~Preview CLI + docs (bet A)~~ → `main`
6. ~~Volumes CLI + bet B/C guides~~ → `main`
7. GTM drafts (SLA, support, changelog) — legal review + publish after SLO window
