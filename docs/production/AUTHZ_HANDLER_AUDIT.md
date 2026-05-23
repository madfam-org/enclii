# AuthZ handler audit — GA Phase 1

> **Updated:** 2026-05-22  
> **Scope:** UUID-scoped routes that must call `enforceUserProjectAccess` or `enforceServiceAccess`

## Helpers (`access_resource.go`)

| Helper | Use |
|--------|-----|
| `mustServiceAccess(c)` | Routes under `/v1/services/:id/...` |
| `enforceDeploymentAccess(c, deploymentID)` | `/v1/deployments/:id` and deployment logs |
| `loadPreviewWithAccess(c, previewID)` | All `/v1/previews/:id` mutations and reads |
| `loadFunctionWithAccess(c, functionID)` | All `/v1/functions/:id` operations |

## Fixed (2026-05-22)

| Area | Handlers | Fix |
|------|----------|-----|
| Deployments | `GetDeploymentByVersion`, `ListServiceDeployments`, `GetLatestDeployment` | `enforceServiceAccess` (was acting-as only) |
| Deployments | `GetServiceStatus`, `GetLogs` | Service / deployment access |
| Previews | `GetPreview`, `ClosePreview`, `WakePreview`, `DeletePreview`, comments, `RecordPreviewAccess` | `loadPreviewWithAccess` |
| Functions | `GetFunction`, `UpdateFunction`, `DeleteFunction`, `InvokeFunction`, logs, metrics | `loadFunctionWithAccess` |
| Addons | `GetAddon`, `GetAddonCredentials`, `RefreshAddonStatus`, `DeleteAddon` | `enforceUserProjectAccess` (was acting-as only) |
| Domains | `ListCustomDomains`, `GetCustomDomain` | `enforceUserProjectAccess` |
| Services | `GetServiceSettings`, `ListReleases` | Service / project access |
| Env vars | `ListEnvVars`, `GetEnvVar` | `mustServiceAccess` + service ID match |
| Canary | `GetCanary` | `enforceServiceAccess` on rollout's service |

## Verified (tests in `authz_matrix_test.go`, `access_resource_test.go`)

- `enforceUserProjectAccess` matrix
- Cross-tenant: cron, service PATCH, junction, deployment-by-version, preview, function

## Slug-scoped routes

All routes under `protected` with `:slug` use `RequireProjectAccessBySlug()` middleware (`handlers.go`).

## Internal / callback routes (out of scope)

- `/v1/callbacks/*` — Roundhouse API key
- `GET /v1/services?git_repo=` — production bearer auth

## Next audit targets

- [ ] Remaining `/v1/services/:id/*` handlers (bulk env-var mutations, build, deploy)
- [ ] `GET /v1/addons/:id/events`, binding mutations
- [ ] Webhook handlers by UUID
- [ ] Release-by-ID routes if any exist outside service scope
