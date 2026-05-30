# Security release — production verification log

> **Checklist:** [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md)  
> **Program:** [COMMERCIAL_GA_EXECUTION_ROADMAP.md](./COMMERCIAL_GA_EXECUTION_ROADMAP.md)

Automated / operator verification on **2026-05-23** (post-deploy `848c8968` / `16bc227a`).

| # | Checklist item | Result | Evidence |
|---|----------------|--------|----------|
| 1 | `ENCLII_ROUNDHOUSE_API_KEY` on API + Roundhouse | **Configured** | `enclii-secrets/internal-api-key`; Roundhouse `SWITCHYARD_API_KEY` (2026-05-30) |
| 2 | Roundhouse sends Bearer to Switchyard | **Pass** | `security-release-smoke.sh` roundhouse bearer → HTTP 200 |
| 3 | Non-admin tenant isolation smoke | **Pass** | `security-release-tenant-smoke.sh` — junction 404 NOT_FOUND (2026-05-30, port-forward) |
| 4 | Dashboard stats require login | **Pass** | `GET /v1/dashboard/stats` → **401** unauthenticated |
| 5 | `go test ./...` switchyard-api | **Partial** | AuthZ matrix subset green; run full suite in CI on `main` |
| 6 | Migration 030 in prod | **Pass** | `rollout_blocked_reason` column in `enclii` database |
| 7 | Commercial GA API smokes | **Pass** | [Actions 26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825) |

**Sign-off:** Platform lead initials still required on [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) for formal Gate 1 close.

### 2026-05-30 — O-3 tenant isolation + O-10 ESO bridge

| Check | Result | Notes |
|-------|--------|-------|
| Dashboard stats 401 | **Pass** | `security-release-smoke.sh` public URL |
| Build callback / git_repo auth | **Pass** | HTTP 401 unauthenticated |
| Roundhouse bearer accepted | **Pass** | HTTP 200 with shared key |
| Tenant project list scoped | **Pass** | `ga-test-dev@madfam.io` → symbiosis-hcm only |
| Cross-tenant junction IDOR | **Pass** | `092a8580-…` → HTTP 404 NOT_FOUND |
| Cross-tenant cron IDOR | **N/A** | 0 cron jobs in prod |
| Merge ESO `enclii-internal-api-key` | **Pass** | kubernetes-store bridge → **SecretSynced** (`c8c24ecd`) |

**Optional:** Vault backfill `secret/enclii/internal_api_key` when write `VAULT_TOKEN` available (`ga-o10-enclii-vault-backfill.sh`).
