---
title: Rollback
description: Roll back deployments with the Enclii TypeScript SDK
sidebar_position: 6
tags: [sdk, typescript, rollback, deployments]
---

# Rollback

`enclii.rollback` (`RollbackResource`, `packages/sdk-ts/src/resources/rollback.ts`) has two rollback paths.

| Method | Signature | HTTP |
|--------|-----------|------|
| `instant` | `instant(serviceId: string, input: InstantRollbackRequest): Promise<InstantRollbackResponse>` | `POST /services/{id}/rollback` |
| `manifest` | `manifest(deploymentId: string): Promise<ManifestRollbackResponse>` | `POST /deployments/{id}/rollback` |

To cancel a canary, use [`canary.rollback()`](./canary.md#promote-or-roll-back) instead.

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Instant rollback

Flips the service's traffic selector back to an earlier deployment. The SDK source describes this as taking under 30 seconds when the target's pods are still running and under 90 seconds when they must be scaled back up.

```typescript
const result = await enclii.rollback.instant(serviceId, {
  target_deployment_id: previousDeploymentId,
  reason: 'p99 latency regression',
  change_ticket_url: 'https://tickets.example.com/CHG-123', // required for production environments
});

console.log(result.message, `${result.took_ms}ms`, `v${result.from_version} -> v${result.to_version}`);
```

`InstantRollbackRequest`:

| Field | Type | Required |
|-------|------|----------|
| `target_deployment_id` | `string` | yes |
| `reason` | `string` | no |
| `change_ticket_url` | `string` | required for production environments |

To find a target, list earlier deployments with [`deployments.list()`](./deployments.md#list-deployments) or resolve a v-label with `deployments.get(serviceId, 'v41')`.

`InstantRollbackResponse`:

```typescript
interface InstantRollbackResponse {
  message: string;
  took_ms: number;
  scaled_up: boolean;
  from_deployment_id?: string;
  to_deployment_id: string;
  from_version?: number | null;
  to_version?: number | null;
  ready_replicas: number;
  strategy: string;
  namespace: string;
}
```

## Manifest rollback

Rolls a deployment's service back to its previous `running` deployment. The handler (`RollbackDeployment` in `apps/switchyard-api/internal/api/deployment_handlers.go`) reads no request body: it picks the target itself by walking the service's other releases, newest first, and taking the first deployment whose status is `running`. It then marks `deploymentId` as `failed` and asks the reconciler to roll the workload back.

```typescript
const result = await enclii.rollback.manifest(currentDeploymentId);
console.log(result.message, result.rolled_back_to.id, `v${result.rolled_back_to.version_number}`);
```

`manifest()` sends no body and takes no target. Passing a second argument throws a plain `Error` before any request, instead of being silently ignored; to go back to a specific deployment, use `instant()` with `target_deployment_id`. The call fails with `ValidationError` when the service has no other `running` deployment.

```typescript
interface ManifestRollbackResponse {
  message: string;
  rolled_back_to: Deployment;     // the earlier running deployment
  current_deployment: Deployment; // as loaded before the rollback
}
```

`current_deployment` is the row as it was loaded before the rollback: the API marks it `failed` but returns its previous `status`.

## Related documentation

- [Deployments](./deployments.md)
- [Canary](./canary.md)
- [CLI: `enclii rollback`](../../cli/commands/rollback.md)
