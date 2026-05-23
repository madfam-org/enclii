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
| `loadEnvVarWithAccess(c, evID)` | `/v1/services/:id/env-vars/:var_id` mutations and reveal |
| `loadAddonWithAccess(c, addonID)` | `/v1/addons/:id` reads and binding mutations |
| `loadWebhookWithAccess(c, webhookID)` | `/v1/webhooks/:id` (notification destinations) |
| `loadOutboundWebhookWithAccess(c, subID)` | `/v1/lifecycle-webhooks/:sub_id` |
| `loadDeploymentGroupWithSlugAccess(c, groupID)` | `/v1/projects/:slug/deployment-groups/:group_id` |

## Fixed (2026-05-22)

| Area | Handlers | Fix |
|------|----------|-----|
| Deployments | `GetDeploymentByVersion`, `ListServiceDeployments`, `GetLatestDeployment` | `enforceServiceAccess` (was acting-as only) |
| Deployments | `GetServiceStatus`, `GetLogs`, `GetDeployment` | Service / deployment access |
| Previews | `GetPreview`, `ClosePreview`, `WakePreview`, `DeletePreview`, comments, `RecordPreviewAccess` | `loadPreviewWithAccess` |
| Functions | `GetFunction`, `UpdateFunction`, `DeleteFunction`, `InvokeFunction`, logs, metrics | `loadFunctionWithAccess` |
| Addons | `GetAddon`, `GetAddonCredentials`, `RefreshAddonStatus`, `DeleteAddon` | `enforceUserProjectAccess` (was acting-as only) |
| Domains | `ListCustomDomains`, `GetCustomDomain` | `enforceUserProjectAccess` |
| Services | `GetServiceSettings`, `ListReleases` | Service / project access |
| Env vars | All `/v1/services/:id/env-vars/*` | `mustServiceAccess` / `loadEnvVarWithAccess` (incl. `RevealEnvVar`, bulk, sync) |
| Build / deploy | `BuildService`, `DeployService`, `RollbackDeployment`, `InstantRollback` | Service / deployment access |
| Topology / metrics | `GetServiceDependencies`, `GetServiceImpact`, `GetServiceResourceMetrics`, `FindDependencyPath` | `mustServiceAccess` / dual-service check |
| Addons | `CreateAddonBinding`, `DeleteAddonBinding`, `GetServiceBindings`, `GetAddonEvents` | `loadAddonWithAccess` + service access |
| Canary | `StartCanary`, `GetCanary`, `PromoteCanary`, `RollbackCanary`, `ListServiceCanaries` | `mustServiceAccess` / `enforceServiceAccess` |
| Webhooks | `GetWebhook`, `UpdateWebhook`, `DeleteWebhook`, `TestWebhook`, deliveries, retry | `loadWebhookWithAccess` |
| Lifecycle webhooks | All `/v1/lifecycle-webhooks/:sub_id/*` | `loadOutboundWebhookWithAccess` |
| Deployment groups | `GetDeploymentGroup`, `ExecuteDeploymentGroup`, `RollbackDeploymentGroup` | `loadDeploymentGroupWithSlugAccess` |
| Service deps | `AddServiceDependency`, `ListServiceDependencies`, `ListServiceDependents`, `RemoveServiceDependency` | `mustServiceAccess` + target service access |

## Verified (tests in `authz_matrix_test.go`, `access_resource_test.go`)

- `enforceUserProjectAccess` matrix
- Cross-tenant: cron, service PATCH, junction, deployment-by-version, preview, function, webhook

## Slug-scoped routes

All routes under `protected` with `:slug` use `RequireProjectAccessBySlug()` middleware (`handlers.go`). Deployment group handlers additionally verify `group.project_id` matches the slug project.

## Internal / callback routes (out of scope)

- `/v1/callbacks/*` — Roundhouse API key
- `GET /v1/services?git_repo=` — production bearer auth
- `POST /v1/webhooks/github` — GitHub signature

## Next audit targets

- [ ] `GET /v1/audit` and other global list endpoints — confirm user scoping
- [ ] `GET /v1/services` (git_repo query) — production bearer only (documented)
