# Commercial GA tracker

> **Program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md)  
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
| Staging lifecycle proofs (bets A/B/C) | Ops | Runbook: [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md) |

## Phase 1 — Engineering (code on `main`)

| Item | Status |
|------|--------|
| AuthZ handler audit + matrix tests | Done (PR #250) |
| Doc-guard active paths only (CI green) | Done (PR #250) |
| `GET /v1/deployments` user-scoped (non-admin) | Done (PR #250) |
| Log routes `mustServiceAccess` | Done (PR #250) |
| Reconciler queue metrics | Done (prior `main`) |
| OpenAPI canonical path + sdk-ts CI drift check | Done (PR #253–254) |
| OpenAPI → `sdk-ts` UI adoption | Done — preview types via `@madfam/enclii-sdk` workspace dep |
| Waybill budget **enforce** on deploy/build | Done — `enforceBudgetNotThrottled` on deploy + build (main, post-#250) |

## Product bet A — Preview environments

| Capability | Status |
|------------|--------|
| API (`/v1/previews`, PR webhook) | Implemented — `CreatePreview` authZ + lifecycle tests |
| Reconciler / URL lifecycle | Partial — verify E2E in staging |
| UI + CLI docs | UI wired (Previews tab); CLI `enclii previews` + [previews.md](../cli/commands/previews.md) |
| E2E test (SOFTWARE_SPEC) | Smoke in CI; full lifecycle opt-in via `PREVIEW_E2E_*` env |

## Product bet B — Custom domains + TLS

| Capability | Status |
|------------|--------|
| Domain API + cert-manager path | Implemented |
| `ToggleZeroTrust` authZ | Done (PR #250) |
| DNS verify + Junction routing E2E | Smoke in CI; full lifecycle opt-in via `DOMAIN_E2E_*` env |

## Product bet C — Persistent volumes

| Capability | Status |
|------------|--------|
| Reconciler `generatePVCs` / mount | Implemented — unit tests exist |
| UI volume attach + lifecycle | Settings editor + API persist volumes (`ServiceVolumesEditor`) |
| E2E stateful deploy | Smoke in CI; volumes opt-in via `STORAGE_E2E_*`; deploy slice needs `STORAGE_E2E_RELEASE_ID` |

## Commercial wrap (GTM / legal)

| Deliverable | Status |
|-------------|--------|
| SLA (99.95%) | Open |
| Support tiers + status page | Open |
| GA changelog; retire “95% ready” externally | Open |
| Pricing + self-serve signup tested | Open |

## Next merge train

1. ~~AuthZ / OpenAPI / preview + domains + storage E2E~~ → `main` (2026-05-22)
2. ~~PVC persist + settings UI (#258)~~ → `main`
3. **Deploy `main` + migration 030 + security checklist** (ops — critical path)
4. **Staging proofs** — [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md) or Actions workflow `Commercial GA staging proof`
5. ~~Preview CLI + docs (bet A)~~ → `main`
6. GTM wrap (SLA, pricing, changelog) after Stability GA window starts
