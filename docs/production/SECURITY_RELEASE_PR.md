# Pending PR: Security remediation release

Track as a **dedicated release PR** before the next production deploy of Switchyard API.

## Included changes (already on branch)

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

## Follow-up (Phase 3 — same branch, non-blocking for security deploy)

- UI/CLI HTTP consolidation (`lib/api.ts`, `apiRequest`)
- See `docs/production/CODEBASE_AUDIT_2026-05.md`
