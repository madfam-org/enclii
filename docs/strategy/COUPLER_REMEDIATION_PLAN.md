# Coupler (Agent Tool Plane) — Full Remediation & Implementation Plan

**Status:** execution-ready  
**Program start:** 2026-06-02 (target)  
**Program end (v1 GA):** 2026-10-31 (target)  
**License:** AGPL-3.0-only public repo `madfam-org/coupler`  
**Policy:** zero Composio spend; sovereign build only

**Canonical architecture:** [AGENT_TOOL_PLANE.md](./AGENT_TOOL_PLANE.md)  
**Execution checklist:** [COUPLER_EXECUTION_CHECKLIST.md](./COUPLER_EXECUTION_CHECKLIST.md)

---

## 1. Program objective

Deliver **Composio-class platform parity** (delegated auth, tool execute, MCP, sandbox, triggers, connector SDK) as a **fourth MADFAM platform** without embedding tool logic in Enclii or Janua.

| Metric | v1 GA target |
|--------|----------------|
| Tier-1 connectors | ≥ 6 (GitHub, Slack, Gmail, Notion, Linear, Google Calendar) |
| MCP tools | search, execute, manage_connections |
| Janua ConnectedAccount | GA with OAuth broker + ATP token delegation |
| Production deploy | Enclii onboard, digest-pinned, Gate 4-style synthetics |
| Ecosystem SDK | `@madfam/coupler` + Python package |
| Operator zone | `madfam.ops.*` proxy to Enclii (admin JWT) |

**Explicitly out of v1 scope:** 1,000-connector long tail, Composio-style ML tool learning, Dhanam metering (v1.1).

---

## 2. Cross-repo ownership

| Repo | Program role | Program lead surface |
|------|--------------|----------------------|
| **coupler** (new) | ATP gateway, executor, sandbox, triggers, connectors, MCP | `COUPLER_EXECUTION_CHECKLIST.md` |
| **janua** | ConnectedAccount, OAuth broker, token delegation API, audit ingest | `docs/COUPLER_PROGRAM.md` |
| **enclii** | Onboard Coupler, ops proxy contract, ecosystem docs | `docs/strategy/*`, `ROADMAP.md` |
| **selva-office** | Primary agent consumer; replace direct SaaS calls with Coupler | `docs/COUPLER_INTEGRATION.md` (to create in selva) |

**Hard rule:** no Coupler connector code in `switchyard-api` or `janua-api` beyond Janua keyring APIs.

---

## 3. Phase map (parallel tracks)

```text
Week →  1   2   3   4   5   6   7   8   9  10  11  12  13  14  15  16  17  18  19  20  21  22
P0 Bootstrap      ████
P1 Janua Keyring      ████████
P2 Coupler core           ████████████
P3 Dev surface                    ████████
P4 Parity                               ████████████
P5 Ecosystem GA                                   ████████
```

| Phase | Duration | Gate | Blocks |
|-------|----------|------|--------|
| **P0** Bootstrap | 2 wk | Public repo + CI green + Janua client registered | P2 staging |
| **P1** Janua Keyring | 4–6 wk | ConnectedAccount + token API + 2 OAuth providers | P2 prod |
| **P2** Coupler core | 6–8 wk | Execute + 2 connectors on staging | P3 |
| **P3** Dev surface | 4 wk | MCP + TS SDK + Selva PoC | P4 |
| **P4** Parity | 8–10 wk | Sandbox + triggers + 4 more connectors + ops proxy | P5 |
| **P5** Ecosystem GA | 4 wk | Synthetics, runbooks, claim matrix, announce | — |

---

## 4. Phase 0 — Bootstrap (coupler repo)

**Target:** 2026-06-02 → 2026-06-13

| ID | Task | Repo | Owner | Done when |
|----|------|------|-------|-----------|
| C0-1 | Create `madfam-org/coupler` public repo, AGPL-3.0 | coupler | Platform | Repo live, LICENSE, README |
| C0-2 | Monorepo skeleton per AGENT_TOOL_PLANE §3.2 | coupler | Platform | CI lint/test pass |
| C0-3 | `enclii.yaml` + `k8s/production/` stub | coupler | Platform | Preflight passes |
| C0-4 | `janua.client.yaml` + provision staging client `coupler-api` | coupler + janua | Identity | Token smoke |
| C0-5 | OpenAPI stub `docs/openapi/coupler-v1.yaml` | coupler | Platform | Reviewed |
| C0-6 | Cross-link docs in enclii/janua/selva roadmaps | enclii | Docs | This plan + ROADMAP rows |
| C0-7 | GitHub team `automation` write on coupler | enclii ops | Ops | Digest CI works |

**Exit gate P0:** `gh repo view madfam-org/coupler` public; CI green; Janua client exists.

---

## 5. Phase 1 — Janua Keyring (blocker)

**Target:** 2026-06-16 → 2026-07-25  
**ADR:** [janua/docs/architecture/ADR-002_UNIVERSAL_KEYRING.md](https://github.com/madfam-org/janua/blob/main/docs/architecture/ADR-002_UNIVERSAL_KEYRING.md)

| ID | Task | Repo | Done when |
|----|------|------|-----------|
| J1-1 | Migrations: `connected_accounts`, `provider_types` | janua | Applied staging + prod |
| J1-2 | Encrypt/decrypt service (Vault or AES-GCM per ADR) | janua | Unit tests pass |
| J1-3 | `GET/POST/DELETE /api/v1/connections` | janua | OpenAPI + tests |
| J1-4 | OAuth authorize + callback (GitHub, Slack) | janua | E2E connect flow |
| J1-5 | `POST /api/v1/connections/:id/token` (ATP service scope) | janua | Coupler executor smoke |
| J1-6 | Audit ingest: `connection.*`, `tool.delegation.*` | janua | Events queryable |
| J1-7 | Provider registry seed: github, slack, google, notion, linear | janua | Admin UI list |
| J1-8 | Document contract in `docs/COUPLER_PROGRAM.md` | janua | Linked from Coupler |

**Exit gate P1:** Coupler staging can fetch delegated token for GitHub and execute one read-only API call.

---

## 6. Phase 2 — Coupler core

**Target:** 2026-07-28 → 2026-09-05

| ID | Task | Repo | Done when |
|----|------|------|-----------|
| K2-1 | `coupler-gateway`: JWT via Janua JWKS, `/health`, rate limits | coupler | Staging deploy |
| K2-2 | Postgres schema: tools, executions, sessions | coupler | Migrations applied |
| K2-3 | `coupler-executor`: execute pipeline + Janua token fetch | coupler | Integration tests |
| K2-4 | Connector: GitHub (3 tools) | coupler | Dry-run + live staging |
| K2-5 | Connector: Slack (2 tools) | coupler | Dry-run + live staging |
| K2-6 | `POST /v1/tools/execute` + `GET /v1/tools` | coupler | OpenAPI matches impl |
| K2-7 | Enclii onboard production namespace `coupler` | enclii + coupler | Argo Synced |
| K2-8 | Synthetics: health + unauthenticated 401 on execute | coupler | CI + cron |

**Exit gate P2:** Staging demo — Janua login → connect Slack → post message via Coupler API.

---

## 7. Phase 3 — Developer surface

**Target:** 2026-09-08 → 2026-10-03

| ID | Task | Repo | Done when |
|----|------|------|-----------|
| K3-1 | `@madfam/coupler` TypeScript SDK | coupler | Published npm (GitHub pkg) |
| K3-2 | `madfam-coupler` Python SDK | coupler | PyPI / ghcr |
| K3-3 | MCP server package | coupler | Cursor smoke doc |
| K3-4 | `GET /v1/tools/search` (keyword + optional Selva embed) | coupler | Tests |
| K3-5 | Selva adapter: `CouplerToolBackend` | selva-office | Example agent run |
| K3-6 | Janua ECOSYSTEM appendix row for Coupler | janua | Doc merged |

**Exit gate P3:** Cursor MCP connects to staging; Selva worker executes one Coupler tool.

---

## 8. Phase 4 — Parity features

**Target:** 2026-10-06 → 2026-11-14

| ID | Task | Repo | Done when |
|----|------|------|-----------|
| K4-1 | `coupler-sandbox` namespace + Job runner | coupler | Multi-step sample |
| K4-2 | `coupler-triggerd` GitHub + Slack inbound | coupler | Webhook verify tests |
| K4-3 | Connectors: Gmail, Notion, Linear, Calendar | coupler | 4× staging smoke |
| K4-4 | `madfam.ops.*` Enclii proxy (admin only) | coupler | DNS dry-run via proxy |
| K4-5 | R2 large-payload store + signed browse URLs | coupler | Agent filesystem demo |
| K4-6 | `CONNECTOR_SDK.md` + external registry format | coupler | Sample third-party connector |

**Exit gate P4:** Parity checklist in §10 ≥ 90%.

---

## 9. Phase 5 — Ecosystem GA

**Target:** 2026-11-17 → 2026-12-12

| ID | Task | Repo | Done when |
|----|------|------|-----------|
| K5-1 | Production synthetics workflow | coupler | `.github/workflows/` |
| K5-2 | Incident runbook + on-call escalation | coupler + internal-devops | Runbook linked |
| K5-3 | GA claim matrix (public wording) | coupler | No over-claim |
| K5-4 | Ecosystem app integration guide | coupler | 1 external app PoC |
| K5-5 | Status page entries via enclii.yaml | coupler | status.madfam.io |
| K5-6 | Announce + ECOSYSTEM.md sync all repos | all | Map row live |

**Exit gate P5:** v1 GA sign-off (platform + security + docs).

---

## 10. Composio parity checklist (v1 GA)

| Capability | Target | Phase |
|------------|--------|-------|
| Delegated OAuth (end-user) | Janua ConnectedAccount | P1 |
| Tool schema registry | Coupler Postgres | P2 |
| Execute API | `/v1/tools/execute` | P2 |
| Dry-run / plan | Execute `dry_run` + optional plan | P2/P4 |
| MCP server | packages/mcp-server | P3 |
| Tool search | `/v1/tools/search` | P3 |
| Sandboxed execution | coupler-sandbox | P4 |
| Triggers (inbound) | triggerd | P4 |
| Session / large payloads | sessions + R2 | P4 |
| Framework SDKs | TS + Python | P3 |
| Operator infra tools | madfam.ops.* | P4 |
| 6+ SaaS connectors | tier-1 set | P2–P4 |
| Sovereign / self-hosted | MADFAM k3s only | P2 |
| Zero Composio dependency | No Composio API keys | All |

---

## 11. Enclii remediation (minimal touch)

Enclii changes are **docs + onboard + ops proxy only** — no SaaS connectors in switchyard-api.

| ID | Task | When |
|----|------|------|
| E-1 | ECOSYSTEM.md + ROADMAP.md Coupler program row | P0 (this PR) |
| E-2 | `POST /v1/admin/onboard` for project `coupler` | P2 |
| E-3 | Machine token for Coupler → Enclii ops proxy | P4 |
| E-4 | ADAPTER_GAPS: Coupler ops proxy row → close at P4 | P4 |
| E-5 | Optional Switchyard UI link to Coupler docs (not embedded UI) | P5 |

---

## 12. Selva integration strategy

Selva already ships **268 builtin tools** and ecosystem adapters. Coupler is **not a replacement** for Selva office workflows.

| Use Selva builtins | Use Coupler |
|--------------------|-------------|
| Karafiel, Dhanam, PhyndCRM, internal graphs | User-connected Gmail, Slack, Notion, GitHub |
| Tulana campaigns (internal) | Ecosystem apps needing delegated SaaS |
| `/v1` LLM routing | MCP for Cursor/Claude |

**Remediation:** add `CouplerToolBackend` in selva-office Phase P3; document in `selva-office/docs/COUPLER_INTEGRATION.md`.

---

## 13. Risk register

| Risk | Mitigation |
|------|------------|
| P1 Janua slips | Stub token API for staging; do not block P0 repo |
| OAuth app approval delays | Register GitHub/Slack apps in P0 |
| Scope creep (1000 connectors) | Tier-1 only until P5; CONNECTOR_SDK for long tail |
| Selva/Coupler overlap confusion | TRUST_BOUNDARIES doc + Selva integration guide |
| AGPL network use questions | README + legal review before public announce |
| Security: token exfil | Janua delegation only; no refresh tokens in Coupler DB |

---

## 14. Weekly execution rhythm

1. **Monday:** Phase lead standup — gate status, blockers.  
2. **Wednesday:** Cross-repo contract review (Janua ↔ Coupler OpenAPI).  
3. **Friday:** Demo recording (staging) + update `COUPLER_EXECUTION_CHECKLIST.md`.  
4. **Per merge:** Append progress log in coupler `CHANGELOG.md`.

---

## 15. Document index (post-sync)

| Document | Location |
|----------|----------|
| Architecture & boundaries | enclii `docs/strategy/AGENT_TOOL_PLANE.md` |
| This remediation plan | enclii `docs/strategy/COUPLER_REMEDIATION_PLAN.md` |
| Execution checklist | enclii `docs/strategy/COUPLER_EXECUTION_CHECKLIST.md` |
| Janua program | janua `docs/COUPLER_PROGRAM.md` |
| Janua Keyring ADR | janua `docs/architecture/ADR-002_UNIVERSAL_KEYRING.md` |
| Enclii roadmap track | enclii `ROADMAP.md` § Coupler Program |
| Janua roadmap track | janua `docs/ROADMAP.md` § Coupler Program |
| Selva consumer guide | selva-office `docs/COUPLER_INTEGRATION.md` |

---

## Changelog

| Date | Change |
|------|--------|
| 2026-05-30 | Initial execution-ready remediation plan |
| 2026-05-30 | Session wrap: cross-repo docs synced, commits pushed, branch prune; see `claudedocs/session-2026-05-30-summary.md` |
