---
title: Secrets
description: Manage service environment variables and secrets with the Enclii TypeScript SDK
sidebar_position: 9
tags: [sdk, typescript, secrets, environment-variables]
---

# Secrets

`enclii.secrets` (`SecretsResource`, `packages/sdk-ts/src/resources/secrets.ts`) manages a service's environment variables. A variable is either plaintext or a secret (`is_secret: true`); secret values are masked in list responses and can be read back with `reveal()`, which the API records in its audit trail.

| Method | Signature | HTTP |
|--------|-----------|------|
| `list` | `list(serviceId: string, options?: { limit?: number; cursor?: string }): Promise<Page<EnvVar>>` | `GET /services/{id}/env-vars` |
| `set` | `set(serviceId: string, input: SetEnvVarRequest): Promise<EnvVar>` | `POST /services/{id}/env-vars` |
| `bulkSet` | `bulkSet(serviceId: string, vars: SetEnvVarRequest[]): Promise<EnvVar[]>` | `POST /services/{id}/env-vars/bulk` |
| `delete` | `delete(serviceId: string, varId: string): Promise<void>` | `DELETE /services/{id}/env-vars/{varId}` |
| `reveal` | `reveal(serviceId: string, varId: string): Promise<{ key: string; value: string }>` | `POST /services/{id}/env-vars/{varId}/reveal` |

Variables are addressed by ID (`EnvVar.id`), not by key. There is no `get` or `update` method, and no project-level variables.

> **Read the [current API behaviour](#current-api-behaviour) section:** `list()` and `bulkSet()` do not work against the current API.

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Set a variable

```typescript
const v = await enclii.secrets.set(serviceId, {
  key: 'DATABASE_URL',
  value: 'postgres://...',
  is_secret: true,
});
console.log(v.id, v.key, v.is_secret);
```

`SetEnvVarRequest`:

| Field | Type | Required |
|-------|------|----------|
| `key` | `string` | yes |
| `value` | `string` | yes |
| `is_secret` | `boolean` | no |

## Reveal a secret value

```typescript
const { key, value } = await enclii.secrets.reveal(serviceId, varId);
```

The API route requires the developer role and writes an audit entry for each reveal.

## Delete a variable

```typescript
await enclii.secrets.delete(serviceId, varId);
```

## List variables

```typescript
const { data } = await enclii.secrets.list(serviceId);
```

## Bulk set

```typescript
await enclii.secrets.bulkSet(serviceId, [
  { key: 'LOG_LEVEL', value: 'info', is_secret: false },
  { key: 'API_KEY', value: '...', is_secret: true },
]);
```

## Current API behaviour

Differences between the SDK and the current API (`apps/switchyard-api/internal/api/envvar_handlers.go`):

- **`list()`** reads the array from a response field named `env_vars`, but the API returns it as `environment_variables`. `list()` therefore always returns `data: []`. Until the SDK is fixed, read the endpoint directly:

  ```typescript
  import type { EnvVar } from '@madfam/enclii-sdk';

  const resp = await enclii.get<{ environment_variables: EnvVar[] }>(
    `/services/${serviceId}/env-vars`,
  );
  ```

- **`bulkSet()`** sends `{ env_vars: [...] }`, but the API requires `{ variables: [...] }` (at least one), so the call fails with `ValidationError`. On success the API returns `{ message, count }`, not the variables. To bulk-upsert today:

  ```typescript
  await enclii.post(`/services/${serviceId}/env-vars/bulk`, {
    variables: [
      { key: 'LOG_LEVEL', value: 'info', is_secret: false },
      { key: 'API_KEY', value: '...', is_secret: true },
    ],
  });
  ```

- Both the create and bulk endpoints also accept an optional `environment_id` to scope a variable to one environment (unset means all environments). `SetEnvVarRequest` does not declare it.

## Types

```typescript
interface EnvVar {
  id: UUID;
  service_id: UUID;
  key: string;
  value?: string; // masked or absent for secrets unless revealed
  is_secret: boolean;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

interface SetEnvVarRequest {
  key: string;
  value: string;
  is_secret?: boolean;
}
```

## Related documentation

- [Services](./services.md)
- [CLI: `enclii secrets`](../../cli/commands/secrets.md)
