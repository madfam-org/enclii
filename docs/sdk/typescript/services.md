---
title: Services
description: Manage Enclii services with the TypeScript SDK
sidebar_position: 4
tags: [sdk, typescript, services]
---

# Services

`enclii.services` (`ServicesResource`, `packages/sdk-ts/src/resources/services.ts`) covers services inside a project. Services are listed and created under a project **slug** and addressed afterwards by service **ID** (a UUID).

| Method | Signature | HTTP |
|--------|-----------|------|
| `get` | `get(serviceId: string): Promise<Service>` | `GET /services/{id}` |
| `list` | `list(projectSlug: string): Promise<Page<Service>>` | `GET /projects/{slug}/services` |
| `iter` | `iter(projectSlug: string): AsyncIterable<Service>` | `GET /projects/{slug}/services` |
| `create` | `create(projectSlug: string, input: CreateServiceRequest): Promise<Service>` | `POST /projects/{slug}/services` |
| `delete` | `delete(serviceId: string): Promise<void>` | `DELETE /services/{id}` |
| `restart` | `restart(serviceId: string, options?: ServiceOperationOptions & { reason?: string }): Promise<void>` | `POST /services/{id}/restart` |
| `scale` | `scale(serviceId: string, replicas: number, options?: ServiceOperationOptions): Promise<void>` | `POST /services/{id}/scale` |

There is no `update`, `deploy`, `rollback`, `listReleases`, `logs`, `streamLogs`, `metrics`, `exec`, or environment-variable method on `services`. The related operations live elsewhere:

| Task | Use |
|------|-----|
| Build and deploy | [`deployments.build()` / `deployments.deploy()`](./deployments.md) |
| Releases | [`deployments.listReleases()`](./deployments.md#releases) |
| Roll back | [`rollback`](./rollback.md) |
| Logs | [`logs`](./logs.md) |
| Environment variables and secrets | [`secrets`](./secrets.md) |

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Get a service

```typescript
const svc = await enclii.services.get(serviceId);
console.log(svc.name, svc.status, svc.health, `${svc.ready_replicas}/${svc.desired_replicas}`);
```

## List services in a project

```typescript
const { data } = await enclii.services.list('my-project');
for (const svc of data) {
  console.log(svc.id, svc.name, svc.health);
}

// Or lazily
for await (const svc of enclii.services.iter('my-project')) {
  console.log(svc.name);
}
```

`GET /projects/{slug}/services` returns every service of the project in one response (`{ services }`), so `nextCursor` is `null` and `iter()` makes one request. The deprecated `limit`/`cursor`/`pageSize` options are not sent. See [Pagination](./index.md#pagination).

## Create a service

```typescript
const svc = await enclii.services.create('my-project', {
  name: 'api',
  git_repo: 'https://github.com/acme/api',
  app_path: 'apps/api',
  build_config: { type: 'dockerfile', dockerfile: 'Dockerfile' },
});
```

`CreateServiceRequest`:

| Field | Type | Required |
|-------|------|----------|
| `name` | `string` | yes |
| `git_repo` | `string` | yes |
| `build_config` | `Partial<BuildConfig>` | no |
| `app_path` | `string` | no |

`BuildConfig` has `type: 'auto' | 'dockerfile' | 'buildpack'` and the optional fields `dockerfile`, `buildpack`, `context`, `build_args` (`Record<string, string>`), and `target`.

## Delete a service

```typescript
await enclii.services.delete(serviceId);
```

The API route requires the admin role.

## Restart

```typescript
await enclii.services.restart(serviceId, { reason: 'pick up rotated config' });
```

Triggers a rolling restart. The request body is `{ env, reason }`; `reason` defaults to `manual-restart` on the server. Resolves to `undefined`.

## Scale

```typescript
await enclii.services.scale(serviceId, 3);
```

The request body is `{ replicas, env }`. Resolves to `undefined`.

### Environment and limits

Both routes require the admin role (`RestartService` / `ScaleService` in `apps/switchyard-api/internal/api/infra_handlers.go`).

- **`environment` does not select an environment.** The SDK sends it as `env` (the server default is `production`), and the API uses it only as a label in its log, audit record, response, and the `service.scaled` webhook. The restart or scale is applied to the service's workload in the project's namespace, whatever the option says.
- `scale()` accepts 1 to 10 replicas. The API rejects more than 10, and it also rejects `0` with `ValidationError`: its `replicas` field is marked required, which treats zero as missing. Scaling to zero is not possible through this route.

## Types

```typescript
interface ServiceOperationOptions {
  environment?: string; // sent as `env`; a label only
}

type HealthStatus = 'unknown' | 'healthy' | 'unhealthy' | 'degraded';

interface Service {
  id: UUID;
  project_id: UUID;
  name: string;
  git_repo: string;
  app_path?: string;
  watch_paths?: string[];
  build_config: BuildConfig;
  health: HealthStatus;
  status: string;
  desired_replicas: number;
  ready_replicas: number;
  auto_deploy: boolean;
  auto_deploy_branch?: string;
  auto_deploy_env?: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}
```

## Error handling

```typescript
import { AuthorizationError, NotFoundError } from '@madfam/enclii-sdk';

try {
  await enclii.services.delete(serviceId);
} catch (err) {
  if (err instanceof AuthorizationError) {
    console.error('Deleting a service needs the admin role');
  } else if (err instanceof NotFoundError) {
    console.error('No such service');
  } else {
    throw err;
  }
}
```

## Related documentation

- [TypeScript SDK overview](./index.md)
- [Deployments](./deployments.md)
- [Secrets](./secrets.md)
- [CLI: `enclii ps`](../../cli/commands/ps.md)
