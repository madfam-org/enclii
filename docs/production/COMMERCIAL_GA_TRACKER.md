# Commercial GA tracker

> **Program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md)  
> **Default bets:** Preview environments (A) + Custom domains (B) + PVCs (C)  
> **Last updated:** 2026-05-23

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
| Migration `030` (`rollout_blocked_reason`) in prod | Ops | Open |

## Phase 1 — Engineering (code on `main`)

| Item | Status |
|------|--------|
| AuthZ handler audit + matrix tests | Done (PR #250) |
| Doc-guard active paths only (CI green) | Done (PR #250) |
| `GET /v1/deployments` user-scoped (non-admin) | Done (PR #250) |
| Log routes `mustServiceAccess` | Done (PR #250) |
| Reconciler queue metrics | Done (prior `main`) |
| OpenAPI → `sdk-ts` UI adoption | Open |
| Waybill budget **enforce** on deploy/build | Open (throttle table exists; API check pending) |

## Product bet A — Preview environments

| Capability | Status |
|------------|--------|
| API (`/v1/previews`, PR webhook) | Implemented — authZ hardened |
| Reconciler / URL lifecycle | Partial — verify E2E in staging |
| UI + CLI docs | Open |
| E2E test (SOFTWARE_SPEC) | Open |

## Product bet B — Custom domains + TLS

| Capability | Status |
|------------|--------|
| Domain API + cert-manager path | Implemented |
| `ToggleZeroTrust` authZ | Done (PR #250) |
| DNS verify + Junction routing E2E | Open — staging proof |

## Product bet C — Persistent volumes

| Capability | Status |
|------------|--------|
| Reconciler `generatePVCs` / mount | Implemented — unit tests exist |
| UI volume attach + lifecycle | Open |
| E2E stateful deploy | Open |

## Commercial wrap (GTM / legal)

| Deliverable | Status |
|-------------|--------|
| SLA (99.95%) | Open |
| Support tiers + status page | Open |
| GA changelog; retire “95% ready” externally | Open |
| Pricing + self-serve signup tested | Open |

## Next merge train

1. Merge [PR #250](https://github.com/madfam-org/enclii/pull/250) → `main`
2. Deploy API with migration 030 + security checklist
3. Branch `fix/ga-openapi-sdk-ts` for contract work; `feat/ga-budget-enforce` for Waybill throttle on deploy
