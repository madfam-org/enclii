# Session summary — 2026-05-30

Cross-repo session covering **Commercial GA remediation**, **Composio benchmark & strategy**, **Coupler (Agent Tool Plane) program planning**, **doc/roadmap sync**, and **repo hygiene** (commit, merge, branch prune).

Three repos touched: **enclii**, **janua**, **selva-office**. All documented work is on `main` and pushed unless noted below.

**Prior transcript:** Coupler + GA session (`691432de-8506-40df-877a-8dfbff68eb9b`)

---

## Strategic decisions (locked)

| Decision | Rationale |
|----------|-----------|
| **Zero Composio spend** | No cloud/enterprise dependency on `backend.composio.dev` |
| **Fourth platform: Coupler** | Public `madfam-org/coupler`, **AGPL-3.0-only** |
| **Separation of concerns** | Janua = identity + ConnectedAccount vault; Enclii = deploy + operator tools only; Selva = LLM/agents; Coupler = delegated SaaS tools, MCP, sandbox, triggers |
| **Two trust zones** | `coupler.*` (user-delegated SaaS) vs `madfam.ops.*` (admin proxy → Enclii Provider Hub) |
| **Janua ADR-002 blocker** | ConnectedAccount must ship (P1) before Coupler prod execute |
| **Build-only Composio reference** | MIT SDKs OK for API-shape reference; no prod integration |

**Combined Enclii+Janua vs Composio agent product (May 2026):** ~25–35% on user-delegated SaaS execution; strong on platform ops (Provider Hub), weak on connector catalog, MCP, sandbox, triggers.

---

## Commercial GA remediation (enclii)

### Shipped / verified this session arc

| Action | Result |
|--------|--------|
| Provider Hub deploy + digest pin | `d8a06b5e` — switchyard-api, switchyard-ui, dispatch |
| Dispatch Dockerfile + TS fixes | ecosystem-tenants build, Provider Hub TypeScript |
| CI `commit-digests` on `workflow_dispatch` | `force_build=true` path fixed |
| `GET /v1/admin/providers/catalog` | **404 → 401** (route live after deploy) |
| Resend ESO | Already `SecretSynced`; vault backfill not required |
| Gate 4 hygiene | Appended to `docs/production/GATE4_SLO_WINDOW_LOG.md` |
| Tier copy | Sovereign = Essentials in `docs/faq/billing.md` |
| Incident runbook | Janua dependency section in `docs/operations/INCIDENT_RESPONSE.md` |
| Post-deploy smoke (`--public-only`) | **4/4 pass** after rollout |
| Janua hosted-auth synthetics | **6 pass, 1 skip** (no synthetic client env) |

### Still open (do not block Coupler P0)

| Item | Notes |
|------|-------|
| Gate 5 | SLA, support, changelog publish |
| Full authenticated smoke | Needs operator JWT via `enclii login` (cluster internal-api-key insufficient) |
| Janua admin login | Session bridge deployed; token refresh / HttpOnly vs `checkSession` gaps |
| Janua claim matrix | Public page reconciliation |
| Commercial GA status | ~**78%**; Gate 4 SLO clock running |

---

## Coupler program — planning & documentation

### Canonical docs (enclii)

| Document | Purpose |
|----------|---------|
| [docs/strategy/AGENT_TOOL_PLANE.md](../docs/strategy/AGENT_TOOL_PLANE.md) | Architecture, boundaries, API sketch, phases 0–5 |
| [docs/strategy/COUPLER_REMEDIATION_PLAN.md](../docs/strategy/COUPLER_REMEDIATION_PLAN.md) | Full P0–P5 implementation plan, task IDs, gates |
| [docs/strategy/COUPLER_EXECUTION_CHECKLIST.md](../docs/strategy/COUPLER_EXECUTION_CHECKLIST.md) | Day-1 bootstrap checkboxes |
| [ROADMAP.md](../ROADMAP.md) | Coupler Program section (Jun–Dec 2026) |
| [ECOSYSTEM.md](../ECOSYSTEM.md) | Coupler platform row + routing rules |
| [llms.txt](../llms.txt) | Coupler index |
| [docs/ADAPTER_GAPS.md](../docs/ADAPTER_GAPS.md) | `madfam.ops.*` proxy gap (closes Coupler P4) |

### Cross-repo docs

| Repo | Document | Change |
|------|----------|--------|
| **janua** | `docs/COUPLER_PROGRAM.md` | P1 Keyring deliverables, API contract |
| **janua** | `docs/ROADMAP.md` | Coupler / ConnectedAccount section |
| **janua** | `docs/guides/ECOSYSTEM_INTEGRATION.md` | Appendix C + Coupler client row |
| **janua** | `ADR-002` | **PROPOSED → ACCEPTED** (implementation in progress) |
| **selva-office** | `docs/COUPLER_INTEGRATION.md` | `CouplerToolBackend`, trust zones, P3 PoC |
| **selva-office** | `ROADMAP.md` | Coupler consumer track |

### Phase calendar (targets)

| Phase | Focus | Target | Gate |
|-------|-------|--------|------|
| **P0** | Bootstrap repo + CI + Janua client | 2026-06-13 | Public repo + CI green |
| **P1** | Janua ConnectedAccount | 2026-07-25 | Delegated token → Coupler execute |
| **P2** | Gateway + executor + 2 connectors | 2026-09-05 | Staging demo |
| **P3** | SDK + MCP + Selva PoC | 2026-10-03 | Cursor MCP smoke |
| **P4** | Sandbox, triggers, 6 connectors, ops proxy | 2026-11-14 | Parity ≥90% |
| **P5** | Synthetics, runbooks, announce | 2026-12-12 | v1 GA sign-off |

**P0 doc cross-links:** complete. **P0 code:** `madfam-org/coupler` repo **not created yet**.

---

## Commits on `main` (this session close)

### enclii

| SHA | Message |
|-----|---------|
| `2492ead2` | docs(coupler): add Agent Tool Plane program docs and roadmap cross-links |

Prior session commits on `main` (same arc): Provider Hub deploy chain through `d8a06b5e`.

### janua

| SHA | Message |
|-----|---------|
| `1dc36cf1` | docs(coupler): add ConnectedAccount program docs and accept ADR-002 |
| `227deb62` | merge: silent-auth + per-user entitlements (PR #376) |

**PR #376 merge:** Resolved conflicts in OAuth client registration (client_key + organization_id), entitlements service, oauth_provider silent-auth paths, lockfile. Remote `feat/silent-auth-and-entitlements` deleted.

### selva-office

| SHA | Message |
|-----|---------|
| `6ada720` | docs(coupler): add Selva consumer integration plan for Agent Tool Plane |

---

## Repo hygiene (branch prune)

All three repos are **`main`-only** locally.

### enclii

- **35 local branches** deleted (merged squash-PR leftovers + stale fix/feat branches)
- **8 remote branches** deleted: `feat/granular-project-card-interactions`, `feat/projects-page-distinct-from-home`, `fix/npm-rotation-password-bootstrap`, `fix/npm-token-rotation-smoke`, `recovery/signed-enclii-core`, `feat/ga-volumes-cli-guides`, `fix/w0-pgbackrest-alerts-platform-rules`, `dependabot/github_actions/pnpm/action-setup-6`
- Stale worktrees pruned (`/private/tmp/enclii-recovery`, etc.)

### janua

- **11 local branches** deleted
- **5 remote branches** deleted (merged npm/CI fix branches)
- **11 dependabot remote branches** remain (open dependency PRs — not merged)

### Not merged / intentionally dropped

| Branch | Disposition |
|--------|-------------|
| `recovery/signed-enclii-core` | Deleted; mid-May recovery work largely superseded by `main`; golden conflicts |
| `fix/log-viewer-missing-tab-wrapper`, `fix/ui-components-build-order` | Closed PRs, never merged; local branches deleted |

---

## Untracked artifacts (not committed)

| Path | Reason |
|------|--------|
| `enclii/apps/admin-console/janua-hosted-auth-synthetic-summary.json` | Local synthetic run output |
| `janua/janua-hosted-auth-synthetic-summary.json` | Local synthetic run output |

---

## Dual-track model going forward

```text
Track A — Enclii Commercial GA (parallel)
  Gate 4 SLO hygiene → Gate 5 publish → operator JWT smoke → Janua admin login fix

Track B — Coupler Program
  P0: create madfam-org/coupler → Janua coupler-api client → CI green
  P1: Janua ConnectedAccount (blocks Coupler prod)
  P2–P5: per COUPLER_REMEDIATION_PLAN.md
```

Tracks do **not** block each other until Coupler needs prod Janua delegation (P1 gate).

---

## Next session — suggested first actions

1. **Coupler P0:** Create public `madfam-org/coupler` (AGPL, skeleton, CI, `enclii.yaml`)
2. **Janua P1 kickoff:** ConnectedAccount migrations + `GET/POST /api/v1/connections`
3. **Enclii GA:** Operator JWT smoke + Gate 5 doc publish
4. **Janua:** Admin login cookie/refresh fix + claim matrix

---

## Document index (quick links)

| Topic | Path |
|-------|------|
| This session | `claudedocs/session-2026-05-30-summary.md` |
| Coupler architecture | `docs/strategy/AGENT_TOOL_PLANE.md` |
| Coupler execution plan | `docs/strategy/COUPLER_REMEDIATION_PLAN.md` |
| Coupler checklist | `docs/strategy/COUPLER_EXECUTION_CHECKLIST.md` |
| Commercial GA tracker | `docs/production/COMMERCIAL_GA_TRACKER.md` |
| Remaining GA ops | `docs/production/REMAINING_OPS_GA.md` |
| Janua Coupler program | `janua/docs/COUPLER_PROGRAM.md` |
| Selva Coupler consumer | `selva-office/docs/COUPLER_INTEGRATION.md` |

---

*Session closed 2026-05-30. All repos on `main`, pushed to origin.*
