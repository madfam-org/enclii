# Security release — production verification log

> **Checklist:** [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md)  
> **Program:** [COMMERCIAL_GA_EXECUTION_ROADMAP.md](./COMMERCIAL_GA_EXECUTION_ROADMAP.md)

Automated / operator verification on **2026-05-23** (post-deploy `848c8968` / `16bc227a`).

| # | Checklist item | Result | Evidence |
|---|----------------|--------|----------|
| 1 | `ENCLII_ROUNDHOUSE_API_KEY` on API + Roundhouse | **Configured** | `enclii-secrets/internal-api-key`; Roundhouse `SWITCHYARD_API_KEY` (2026-05-30) |
| 2 | Roundhouse sends Bearer to Switchyard | **Pass** | `security-release-smoke.sh` roundhouse bearer → HTTP 200 |
| 3 | Non-admin tenant isolation smoke | **Partial** | `go test -run CrossTenant` green on `main`; manual cron/junction prod smoke still required |
| 4 | Dashboard stats require login | **Pass** | `GET /v1/dashboard/stats` → **401** unauthenticated |
| 5 | `go test ./...` switchyard-api | **Partial** | AuthZ matrix subset green; run full suite in CI on `main` |
| 6 | Migration 030 in prod | **Pass** | `rollout_blocked_reason` column in `enclii` database |
| 7 | Commercial GA API smokes | **Pass** | [Actions 26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825) |

**Sign-off:** Platform lead initials still required on [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) after items 2 and 3 manual steps.

### 2026-05-30 — `security-release-smoke.sh` prod probe (post `98be6d41`)

| Check | Result | Notes |
|-------|--------|-------|
| Dashboard stats 401 | **Pass** | Unauthenticated blocked |
| Build callback missing/invalid bearer | **Pass** | HTTP 401 |
| git_repo lookup without bearer | **Pass** | HTTP 401 |
| Roundhouse bearer accepted | **Pass** | HTTP 200 with shared key |
| Adapter routes | **Pass** | `post-deploy-ga-adapters.sh --public-only` 4/4 |

**Remaining:** SECURITY_RELEASE_PR step 3 manual tenant IDOR smoke; Vault backfill for `internal_api_key` (O-10).
