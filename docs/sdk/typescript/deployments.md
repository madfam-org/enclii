---
title: Deployments
description: Build, deploy, and inspect deployments with the Enclii TypeScript SDK
sidebar_position: 5
tags: [sdk, typescript, deployments, releases]
---

# Deployments

`enclii.deployments` (`DeploymentsResource`, `packages/sdk-ts/src/resources/deployments.ts`) covers builds (releases) and deployments for a service. Rollbacks and canaries have their own namespaces: see [Rollback](./rollback.md) and [Canary](./canary.md).

| Method | Signature | HTTP |
|--------|-----------|------|
| `get` | `get(deploymentId: string): Promise<Deployment>` | `GET /deployments/{id}` |
| `get` | `get(serviceId: string, vLabel: string): Promise<Deployment>` | `GET /services/{id}/versions/{n}` |
| `getByVersion` | `getByVersion(serviceId: string, versionNumber: number): Promise<Deployment>` | `GET /services/{id}/versions/{n}` |
| `list` | `list(serviceId: string): Promise<ServiceDeploymentsPage>` | `GET /services/{id}/deployments` |
| `iter` | `iter(serviceId: string): AsyncIterable<Deployment>` | `GET /services/{id}/deployments` |
| `latest` | `latest(serviceId: string): Promise<Deployment>` | `GET /services/{id}/deployments/latest` |
| `deploy` | `deploy(serviceId: string, input: DeployRequest): Promise<Deployment>` | `POST /services/{id}/deploy` |
| `build` | `build(serviceId: string, gitSha: string): Promise<Release>` | `POST /services/{id}/build` |
| `listReleases` | `listReleases(serviceId: string): Promise<Page<Release>>` | `GET /services/{id}/releases` |
| `wait` | `wait(deploymentId: string, options?: { intervalMs?: number; timeoutMs?: number; signal?: AbortSignal }): Promise<Deployment>` | polls `GET /deployments/{id}` |

(`get` is one method with an optional second argument; both call forms are shown.)

There is no `services.deploy`, `deployments.create`, `promote`, `abort`, `listEvents`, `getCanaryMetrics`, `updateCanary`, or `releases` namespace, and no deployment strategy, hook, or branch/tag option on `deploy`.

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Build a release

```typescript
const release = await enclii.deployments.build(serviceId, gitSha);
console.log(release.id, release.version, release.status); // status: 'building' | 'ready' | 'failed'
```

The request body is `{ git_sha: gitSha }`. The call returns the new `Release`; it does not wait for the build to finish. Poll `listReleases()` until the release's `status` is `ready` (or `failed`) before deploying it.

## Deploy

```typescript
const dep = await enclii.deployments.deploy(serviceId, {
  release_id: release.id,
  environment_name: 'production',
});
console.log(dep.id, dep.status); // usually 'pending' right after creation
```

`DeployRequest`:

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `release_id` | `string` | yes | The release must already be built. |
| `environment_name` | `string` | no | Environment name, for example `production` or `staging`. The API uses `development` when it is empty. |
| `environment` | `Record<string, string>` | no | Extra environment variables for this deployment. |
| `replicas` | `number` | no | |
| `change_ticket_url` | `string` | no | Change ticket for the deployment-approval check on production deployments. |

`deploy()` is a `POST` and is retried on 429/5xx/network errors like any other request; see [Retries and timeouts](./index.md#retries-and-timeouts).

## Wait for a deployment

```typescript
const final = await enclii.deployments.wait(dep.id, {
  intervalMs: 5_000,       // default 3_000
  timeoutMs: 10 * 60_000,  // default 600_000
});

if (final.status === 'running') {
  console.log(`Healthy: ${final.health}`);
} else {
  console.error(`Deployment ended as ${final.status}: ${final.error_message ?? ''}`);
}
```

`wait()` polls `get(deploymentId)` and resolves when `status` is `running`, `failed`, or `rolled_back`. It resolves (does not reject) on `failed`, so check `status`. It rejects with a plain `Error` when `timeoutMs` elapses or `signal` is aborted. A deployment that becomes `superseded` before reaching one of those states keeps polling until the timeout.

## Get a deployment

By deployment ID:

```typescript
const dep = await enclii.deployments.get(deploymentId);
```

By Heroku-style v-label or version number (per service):

```typescript
const v42 = await enclii.deployments.get(serviceId, 'v42');
const same = await enclii.deployments.getByVersion(serviceId, 42);
```

The v-label must be `v` or `V` followed by a positive integer; anything else throws a plain `Error` before any request is sent. The exported helper `parseVersionLabel(label)` returns the integer or `null`.

Most recent deployment for a service:

```typescript
const latest = await enclii.deployments.latest(serviceId);
```

`GET /services/{id}/deployments/latest` responds with `{ deployment, release }` (`release` is omitted when it cannot be loaded; `GetLatestDeployment` in `apps/switchyard-api/internal/api/deployment_handlers.go`). `latest()` returns the `deployment`. To also read the release, request the wrapper (typed `LatestDeploymentResponse`) directly:

```typescript
import type { LatestDeploymentResponse } from '@madfam/enclii-sdk';

const { deployment, release } = await enclii.get<LatestDeploymentResponse>(
  `/services/${serviceId}/deployments/latest`,
);
```

A service with no deployments yields `NotFoundError`.

## List deployments

```typescript
const { data } = await enclii.deployments.list(serviceId);
for (const d of data) {
  console.log(`v${d.version_number ?? '?'}`, d.status, d.created_at);
}

for await (const d of enclii.deployments.iter(serviceId)) {
  // ...
}
```

The API returns all of the service's deployments in one response (`{ service_id, deployments, count, truncated }`), newest release first, so `iter()` makes a single request and `nextCursor` is `null`.

The list is assembled release by release. When the API cannot read the deployments of some releases it still answers with the rows it read, sets `truncated: true`, and names the unread releases in `skipped_release_ids`. `list()` returns a `ServiceDeploymentsPage`, a `Page<Deployment>` with `truncated` and `skippedReleaseIds`; check `truncated` before choosing a rollback target or counting deployments. `iter()` throws a plain `Error` instead of yielding a truncated list. A server that predates PRNUM_LINK sends no `truncated`, which reads as `false`. The deprecated `limit`/`cursor` options of `list()` and `listReleases()`, and `pageSize` of `iter()`, are not sent. See [Pagination](./index.md#pagination).

## Releases

```typescript
const { data: releases } = await enclii.deployments.listReleases(serviceId);
const ready = releases.filter((r) => r.status === 'ready');
```

## End-to-end: build, deploy, wait

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});

const release = await enclii.deployments.build(serviceId, gitSha);

// Wait for the build to finish.
let built = release;
while (built.status === 'building') {
  await new Promise((r) => setTimeout(r, 10_000));
  const { data } = await enclii.deployments.listReleases(serviceId);
  built = data.find((r) => r.id === release.id) ?? built;
}
if (built.status !== 'ready') throw new Error(`build failed: ${built.error_message ?? ''}`);

const dep = await enclii.deployments.deploy(serviceId, {
  release_id: built.id,
  environment_name: 'staging',
});
const final = await enclii.deployments.wait(dep.id);
if (final.status !== 'running') process.exit(1);
```

`packages/sdk-ts/examples/deploy-and-wait.ts` is a runnable version of the deploy-and-wait half.

## Types

```typescript
type DeploymentStatus =
  | 'pending' | 'deploying' | 'running' | 'failed' | 'rolled_back' | 'superseded';

interface DeployRequest {
  release_id: string;
  environment_name?: string;
  environment?: Record<string, string>;
  replicas?: number;
  change_ticket_url?: string;
}

interface LatestDeploymentResponse {
  deployment: Deployment;
  release?: Release;
}

interface Deployment {
  id: UUID;
  release_id: UUID;
  environment_id: UUID;
  service_id?: UUID;
  version_number?: number | null; // Heroku-style v-number; null on older rows
  group_id?: UUID | null;
  deploy_order: number;
  replicas: number;
  status: DeploymentStatus;
  health: HealthStatus;
  error_message?: string | null;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

type ReleaseStatus = 'building' | 'ready' | 'failed';

interface Release {
  id: UUID;
  service_id: UUID;
  version: string;
  image_uri: string;
  git_sha: string;
  git_branch?: string;
  commit_message?: string;
  commit_author_name?: string;
  pr_number?: number;
  pr_title?: string;
  pr_url?: string;
  repo_url?: string;
  status: ReleaseStatus;
  error_message?: string | null;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}
```

## Related documentation

- [TypeScript SDK overview](./index.md)
- [Rollback](./rollback.md)
- [Canary](./canary.md)
- [CLI: `enclii deploy`](../../cli/commands/deploy.md)
- [CLI: `enclii deployments`](../../cli/commands/deployments.md)
- [Deployment troubleshooting](../../troubleshooting/deployment-issues.md)
