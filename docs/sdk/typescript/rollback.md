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
| `manifest` | `manifest(deploymentId: string, input?: RollbackRequest): Promise<void>` | `POST /deployments/{id}/rollback` |

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

The slower path. The SDK source describes it as a manifest commit that the GitOps controller reconciles with a rolling update (a few minutes), so the rollback is recorded in git.

```typescript
await enclii.rollback.manifest(currentDeploymentId);
```

`RollbackRequest` is `{ to_release?: string }` and defaults to `{}`. The method resolves to `undefined`; the SDK discards the response body.

> **Current API behaviour:** the `POST /deployments/{id}/rollback` handler does not read a request body. It always rolls back to the service's previous successful deployment, so `to_release` has no effect. To go back to a specific deployment, use `instant()` with `target_deployment_id`.

## Related documentation

- [Deployments](./deployments.md)
- [Canary](./canary.md)
- [CLI: `enclii rollback`](../../cli/commands/rollback.md)
