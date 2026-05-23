# Codebase audit — May 2026

Full read-only audit followed by remediation implementation (Phases 0–6).

## Completed in code (May 2026)

### Phase 0 — Security
- [x] Dashboard stats require authentication (`GET /v1/dashboard/stats` under protected routes)
- [x] Internal callbacks fail closed in production when `RoundhouseAPIKey` unset
- [x] `GET /v1/services?git_repo=` requires Roundhouse bearer auth in production
- [x] Rollback no longer skips deployment lookup errors
- [x] SEC-007: email admin elevation disabled when OIDC issuer unset

### Phase 1 — AuthZ
- [x] `RequireProjectAccessBySlug` middleware on protected routes
- [x] `enforceUserProjectAccess` for ID-based resources (cron, junction, deployments, services)
- [x] `ListProjects` scoped to `project_access` for non-admin users
- [x] Cron/junction create validates `service_id` belongs to project

### Phase 2 — Constants
- [x] Reconciler queue/retry/probe defaults in `internal/reconciler/defaults.go`

### Phase 5–6 — Docs & CI
- [x] `docs/ADAPTER_GAPS.md`, `docs/testing/GOLDEN_TESTS.md`
- [x] Makefile production namespace aligned to `enclii`
- [x] CI `test-summary` gates `security-scan`

## Phase 3 — UI/CLI consolidation (May 2026)

- [x] `lib/api.ts`: `getAuthHeadersRecord`, `apiPublicGet`, `apiPublicFetchResponse`, `apiFetchResponse`, exported `attemptTokenRefresh`
- [x] `lib/github-repo.ts`, `lib/ws-url.ts` (`buildSwitchyardWsUrl`, build/tail/stream helpers)
- [x] Migrated: system-health, audit export, signup wizard, template import, process SSE, LogViewer + BuildLogsViewer WS, AuthContext refresh + local auth routes
- [x] GitHub slug dedupe across dashboard/project cards
- [x] CLI: `billingRequest`, `jobsRequest`, `junctionsRequest` → `apiRequest` / `apiRequestResponse`; dev `api-endpoint` → `localhost:4200` when unset
- [ ] Janua-only routes (`AuthContext` /me, `services/import` GitHub link, `app/auth/callback`) — external to Switchyard API
- [ ] SDK-ts adoption in switchyard-ui

## Pending release PR (security)

Ship before or with next production deploy:

1. Set `ENCLII_ROUNDHOUSE_API_KEY` on API + Roundhouse
2. Communicate breaking change: `/v1/dashboard/stats` and `?git_repo=` require auth
3. Verify non-admin users see only entitled projects

## Phase 1 progress (GA execution — May 2026)

- [x] AuthZ integration test matrix (`authz_matrix_test.go`, cross-tenant cron/service)
- [x] Structured errors on access enforcement (`api_errors.go`, `access.go`)
- [x] Reconciler queue Prometheus metrics (`monitoring/reconciler_metrics.go`)
- [x] `services.rollout_blocked_reason` migration + reconciler persist
- [x] Doc-guard blocking on `main` (strict mode)
- [x] Deployment routes authZ fix (`GetDeploymentByVersion`, `ListServiceDeployments`, `GetLatestDeployment`)
- [x] Global `ErrorHandlerMiddleware` in `SetupRoutes`
- [x] Reconciler process duration/failure Prometheus metrics
- [x] `AUTHZ_HANDLER_AUDIT.md` + expanded cross-tenant tests
- [x] Resource authZ helpers + preview/function/addon/deployment/env/canary fixes

## Deferred (Phase 4, 7)

- SDK-ts adoption in switchyard-ui (remaining)
- StatefulSet rollout evaluation (EvaluateRolloutState)
- Product gaps: edge, managed DB marketplace, preview automation (see `GAP_ANALYSIS.md`)

## Reference

**Program plan:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) (stability + commercial GA).

Prior plans: `REMEDIATION_PLAN.md`, `REMAINING_ITEMS.md`, `GAP_ANALYSIS.md`.
