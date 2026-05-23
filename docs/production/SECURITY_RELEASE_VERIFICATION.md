# Security release — production verification log

> **Checklist:** [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md)  
> **Program:** [COMMERCIAL_GA_EXECUTION_ROADMAP.md](./COMMERCIAL_GA_EXECUTION_ROADMAP.md)

Automated / operator verification on **2026-05-23** (post-deploy `848c8968` / `16bc227a`).

| # | Checklist item | Result | Evidence |
|---|----------------|--------|----------|
| 1 | `ENCLII_ROUNDHOUSE_API_KEY` on API + Roundhouse | **Configured** | `switchyard-api` deployment has `ENCLII_ROUNDHOUSE_API_KEY` from `switchyard-api-secrets` |
| 2 | Roundhouse sends Bearer to Switchyard | **Not automated** | Manual: confirm Roundhouse client config in cluster |
| 3 | Non-admin tenant isolation smoke | **Partial** | `go test -run 'CrossTenant|authz_matrix'` green; full matrix + manual cron/junction smoke still required |
| 4 | Dashboard stats require login | **Pass** | `GET /v1/dashboard/stats` → **401** unauthenticated |
| 5 | `go test ./...` switchyard-api | **Partial** | AuthZ matrix subset green; run full suite in CI on `main` |
| 6 | Migration 030 in prod | **Pass** | `rollout_blocked_reason` column in `enclii` database |
| 7 | Commercial GA API smokes | **Pass** | [Actions 26328015825](https://github.com/madfam-org/enclii/actions/runs/26328015825) |

**Sign-off:** Platform lead initials still required on [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md) after items 2 and 3 manual steps.
