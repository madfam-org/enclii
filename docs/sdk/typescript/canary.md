---
title: Canary
description: Run canary rollouts with the Enclii TypeScript SDK
sidebar_position: 7
tags: [sdk, typescript, canary, deployments]
---

# Canary

`enclii.canary` (`CanaryResource`, `packages/sdk-ts/src/resources/canary.ts`) starts and drives canary rollouts. Traffic is split by replica count, not by a service mesh: a 20% canary at 5 total replicas runs 4 stable and 1 canary pod.

| Method | Signature | HTTP |
|--------|-----------|------|
| `start` | `start(serviceId: string, input: CanaryStartRequest): Promise<CanaryRollout>` | `POST /services/{id}/canary` |
| `get` | `get(serviceId: string, rolloutId: string): Promise<CanaryRollout>` | `GET /services/{id}/canary/{rolloutId}` |
| `promote` | `promote(serviceId: string, rolloutId: string): Promise<void>` | `POST /services/{id}/canary/{rolloutId}/promote` |
| `rollback` | `rollback(serviceId: string, rolloutId: string, options?: { reason?: string }): Promise<void>` | `POST /services/{id}/canary/{rolloutId}/rollback` |
| `wait` | `wait(serviceId: string, rolloutId: string, options?: { intervalMs?: number; timeoutMs?: number; signal?: AbortSignal }): Promise<CanaryRollout>` | polls `get` |

There is no method to list a service's canaries; the API route `GET /services/{id}/canary` exists and can be called with [`client.get()`](./index.md#low-level-requests).

## Lifecycle

```
pending -> running -> validating -> promoting -> succeeded
                   \-> auto_rolled_back
                   \-> manual_rolled_back
any non-terminal state -> failed (reconciler error)
```

`succeeded`, `auto_rolled_back`, `manual_rolled_back`, and `failed` are terminal. The exported helper `isCanaryTerminal(state)` returns `true` for those four.

## Setup

```typescript
import { EncliiClient, isCanaryTerminal } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Start a canary

```typescript
const rollout = await enclii.canary.start(serviceId, {
  digest: 'sha256:...',            // image digest of an already-built release
  percentage: 20,
  validation_window_minutes: 10,
  smoke_endpoint: '/health/deep',
  error_rate_threshold: 0.01,
});
console.log(rollout.id, rollout.state);
```

The call throws `ConflictError` (409) if a rollout is already active for the service, or if the service has no running stable deployment to split traffic from.

`CanaryStartRequest`:

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `digest` | `string` | yes | Image digest of the candidate; must already be built. |
| `percentage` | `number` | yes | 5 to 50. |
| `validation_window_minutes` | `number` | no | 1 to 60. |
| `smoke_endpoint` | `string` | no | Path that must return 200, for example `/health/deep`. |
| `error_rate_threshold` | `number` | no | 0.0 to 0.5; fraction of 5xx responses allowed during validation. The SDK type documents a default of 0.05. |
| `environment_name` | `string` | no | |
| `change_ticket_url` | `string` | no | |
| `total_replicas` | `number` | no | |

The SDK only sends the optional fields you set.

## Wait for a terminal state

```typescript
const final = await enclii.canary.wait(serviceId, rollout.id, {
  intervalMs: 10_000,     // default 5_000
  timeoutMs: 45 * 60_000, // default 30 minutes
});

if (final.state === 'succeeded') {
  console.log('Promoted to stable');
} else {
  console.error(`Canary ended as ${final.state}: ${final.last_error ?? final.rollback_reason ?? ''}`);
}
```

`wait()` resolves with the `CanaryRollout` once its state is terminal, including the rollback and failure states. It rejects with a plain `Error` on timeout or abort.

To poll yourself, call `get()` and check `isCanaryTerminal(r.state)`; `packages/sdk-ts/examples/canary-rollout.ts` does this.

## Promote or roll back

```typescript
// Skip the rest of the validation window and promote now
await enclii.canary.promote(serviceId, rollout.id);

// Or abort and return all traffic to stable
await enclii.canary.rollback(serviceId, rollout.id, { reason: '5xx spike' });
```

Both resolve to `undefined`.

## Types

```typescript
type CanaryRolloutState =
  | 'pending' | 'running' | 'validating' | 'promoting'
  | 'succeeded' | 'auto_rolled_back' | 'manual_rolled_back' | 'failed';

interface CanaryRollout {
  id: UUID;
  service_id: UUID;
  environment_id: UUID;
  stable_deployment_id: UUID;
  canary_deployment_id: UUID;
  new_stable_deployment_id?: UUID | null;
  canary_digest: string;
  canary_percentage: number;
  total_replicas: number;
  canary_replicas: number;
  stable_replicas: number;
  validation_window_seconds: number;
  smoke_endpoint?: string;
  error_rate_threshold: number;
  state: CanaryRolloutState;
  started_at?: ISODateTime | null;
  validating_started_at?: ISODateTime | null;
  promoting_started_at?: ISODateTime | null;
  terminal_at?: ISODateTime | null;
  change_ticket_url?: string;
  last_error?: string;
  rollback_reason?: string;
  actual_percentage?: number; // canary_replicas / total_replicas
  created_at: ISODateTime;
  updated_at: ISODateTime;
}
```

## Related documentation

- [Deployments](./deployments.md)
- [Rollback](./rollback.md)
- [CLI: `enclii canary`](../../cli/commands/canary.md)
