# AuthZ handler audit — GA Phase 1

> **Updated:** 2026-05-22  
> **Scope:** UUID-scoped routes that must call `enforceUserProjectAccess` or `enforceServiceAccess`

## Fixed in `87c71cae` follow-up

| Handler | Route | Issue | Fix |
|---------|-------|-------|-----|
| `GetDeploymentByVersion` | `GET /v1/services/:id/deployments/v:version` | Only enforced acting-as, not member access | `enforceServiceAccess` |
| `ListServiceDeployments` | `GET /v1/services/:id/deployments` | Same | `enforceServiceAccess` |
| `GetLatestDeployment` | `GET /v1/services/:id/deployments/latest` | Same | `enforceServiceAccess` |

## Verified (tests in `authz_matrix_test.go`)

| Handler | Test |
|---------|------|
| `enforceUserProjectAccess` | Matrix: admin, member, denied, unauthenticated |
| `GetCronJob` | Cross-tenant denied |
| `UpdateService` | Cross-tenant denied |
| `GetJunction` | Cross-tenant denied |
| `GetDeploymentByVersion` | Cross-tenant denied |
| `GetDeployment` | `enforceUserProjectAccess` via service lookup |

## Slug-scoped routes

All routes under `protected` with `:slug` use `RequireProjectAccessBySlug()` middleware (`handlers.go`).

## Internal / callback routes (out of scope)

- `/v1/callbacks/*` — Roundhouse API key
- `GET /v1/services?git_repo=` — production bearer auth

## Next audit targets

- [ ] `GET /v1/releases/:id` and other release-by-ID routes
- [ ] Function handlers by UUID
- [ ] Preview environment handlers
