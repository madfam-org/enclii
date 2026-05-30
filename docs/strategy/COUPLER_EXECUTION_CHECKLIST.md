# Coupler — Execution Checklist

**Use this checklist to start Phase 0 immediately.**  
Update checkboxes in PRs; mirror status in [COUPLER_REMEDIATION_PLAN.md](./COUPLER_REMEDIATION_PLAN.md) weekly.

---

## Phase 0 — Bootstrap (target complete: 2026-06-13)

### GitHub & legal

- [ ] Create `madfam-org/coupler` (public)
- [ ] Add `LICENSE` (AGPL-3.0-only)
- [ ] Add `README.md` (vision, boundaries, links to AGENT_TOOL_PLANE)
- [ ] Add `AGENTS.md` + `ECOSYSTEM.md` (from enclii template generator or hand copy)
- [ ] Grant `automation` team push access
- [ ] Enable branch protection on `main`

### Repo skeleton

- [ ] `apps/gateway/` — Go module, `/health`, Dockerfile
- [ ] `packages/sdk-typescript/` — stub package
- [ ] `packages/mcp-server/` — stub
- [ ] `connectors/github/` — manifest only
- [ ] `k8s/production/kustomization.yaml` — placeholder image
- [ ] `enclii.yaml` — project `coupler`, port, domains TBD
- [ ] `.github/workflows/ci.yml` — lint, test, build (match enclii patterns)

### Janua registration

- [ ] `janua.client.yaml` with audience `coupler-api`
- [ ] `janua provision apply` on staging
- [ ] Verify JWKS auth against `auth.madfam.io`

### Docs cross-link (enclii — this repo)

- [x] `docs/strategy/AGENT_TOOL_PLANE.md`
- [x] `docs/strategy/COUPLER_REMEDIATION_PLAN.md`
- [x] `docs/strategy/COUPLER_EXECUTION_CHECKLIST.md`
- [x] `ROADMAP.md` Coupler section
- [x] `ECOSYSTEM.md` Coupler row
- [x] `llms.txt` index entry

### Docs cross-link (janua)

- [x] `docs/COUPLER_PROGRAM.md`
- [x] `docs/ROADMAP.md` Coupler section
- [x] `docs/guides/ECOSYSTEM_INTEGRATION.md` Coupler appendix
- [x] ADR-002 status → Accepted / In progress

### Docs cross-link (selva-office)

- [x] `docs/COUPLER_INTEGRATION.md`
- [x] `ROADMAP.md` consumer note

**P0 gate:** CI green on empty gateway; Janua client provisioned; all doc links resolve.

---

## Phase 1 — Janua Keyring (blocker for prod)

- [ ] DB migrations merged
- [ ] Encryption service + tests
- [ ] Connections CRUD API
- [ ] OAuth: GitHub
- [ ] OAuth: Slack
- [ ] Token delegation API for ATP
- [ ] Audit ingest endpoint
- [ ] Staging E2E: connect → delegate → revoke

**P1 gate:** Coupler staging executes GitHub `list_repos` with delegated token.

---

## Phase 2 — Coupler core

- [ ] Gateway JWT middleware
- [ ] Executor + Postgres
- [ ] GitHub connector (live)
- [ ] Slack connector (live)
- [ ] Enclii onboard → namespace `coupler`
- [ ] Staging URL live (tunnel)
- [ ] Public smoke: `/health` 200

**P2 gate:** Slack message posted via API with Janua user connection.

---

## Phase 3 — Developer surface

- [ ] TypeScript SDK
- [ ] MCP server
- [ ] Tool search endpoint
- [ ] Selva `CouplerToolBackend` PoC
- [ ] Cursor staging doc

**P3 gate:** MCP execute from Cursor against staging.

---

## Phase 4 — Parity

- [ ] Sandbox runner
- [ ] triggerd (GitHub + Slack)
- [ ] Gmail, Notion, Linear, Calendar connectors
- [ ] `madfam.ops.*` Enclii proxy
- [ ] R2 payload store

**P4 gate:** Parity checklist ≥ 90% in remediation plan §10.

---

## Phase 5 — GA

- [ ] Production synthetics
- [ ] Incident runbook
- [ ] GA claim matrix
- [ ] status.madfam.io entries
- [ ] v1 announce

**P5 gate:** Sign-off recorded in coupler `docs/GA_READINESS.md`.

---

## First commands (Phase 0 day 1)

```bash
# After repo exists locally
mkdir -p coupler/{apps/gateway,pkg/api,k8s/production,docs/architecture}
cd coupler && git init && cp ../enclii/docs/strategy/AGENT_TOOL_PLANE.md docs/architecture/OVERVIEW.md

# Janua client (from coupler repo root, when schema finalized)
janua provision apply -f janua.client.yaml

# Enclii preflight (after enclii.yaml committed)
enclii admin onboard preflight --project coupler
```

---

*Last updated: 2026-05-30*
