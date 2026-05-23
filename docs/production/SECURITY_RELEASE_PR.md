# Security remediation release checklist

> **Code status:** Merged to `main` (authZ matrix, handler audit, tenant-scoped listings).  
> **Ops status:** Apply this checklist on the **next production deploy** of Switchyard API.

## Included changes (on `main`)

- Authenticated `/v1/dashboard/stats`
- Roundhouse callback + `git_repo` lookup fail-closed in production
- Project-scoped authZ middleware and IDOR guards
- SEC-007 issuer check hardened

## Release checklist

1. Set `ENCLII_ROUNDHOUSE_API_KEY` on Switchyard API and Roundhouse (same value).
2. Confirm Roundhouse `switchyard` client sends `Authorization: Bearer <key>`.
3. Smoke-test non-admin user: project list is scoped; cannot read other tenants' cron/junction IDs.
4. Announce UI change: dashboard stats require login (no change for logged-in users).
5. Run `go test ./...` in `apps/switchyard-api` and deploy via Enclii/GitOps.
6. Apply DB migration **030** (`rollout_blocked_reason`) if not already applied in prod.
7. Run Commercial GA API smokes on deployed API (blocking CI on `main`); optional lifecycle proofs per [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md).

## Follow-up (Phase 3 — same branch, non-blocking for security deploy)

- UI/CLI HTTP consolidation (`lib/api.ts`, `apiRequest`)
- See `docs/production/CODEBASE_AUDIT_2026-05.md`
