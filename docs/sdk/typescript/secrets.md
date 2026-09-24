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
| `list` | `list(serviceId: string, options?: { environment_id?: string }): Promise<Page<EnvVar>>` | `GET /services/{id}/env-vars` |
| `set` | `set(serviceId: string, input: SetEnvVarRequest): Promise<EnvVar>` | `POST /services/{id}/env-vars` |
| `bulkSet` | `bulkSet(serviceId: string, vars: BulkEnvVar[], options?: { environment_id?: string }): Promise<BulkSetEnvVarsResponse>` | `POST /services/{id}/env-vars/bulk` |
| `delete` | `delete(serviceId: string, varId: string): Promise<void>` | `DELETE /services/{id}/env-vars/{varId}` |
| `reveal` | `reveal(serviceId: string, varId: string): Promise<{ key: string; value: string }>` | `POST /services/{id}/env-vars/{varId}/reveal` |

Variables are addressed by ID (`EnvVar.id`), not by key. There is no `get` or `update` method, and no project-level variables. The API handlers are in `apps/switchyard-api/internal/api/envvar_handlers.go`.

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
console.log(v.id, v.key, v.is_secret, v.value); // value is '••••••••' for secrets
```

`SetEnvVarRequest`:

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `key` | `string` | yes | Starts with a letter or underscore; letters, digits, and underscores only. |
| `value` | `string` | yes | |
| `is_secret` | `boolean` | no | |
| `environment_id` | `string` | no | UUID of one environment. Omit to apply the variable to every environment. |

Creating a key that already exists fails with `ConflictError`; use `bulkSet()` to upsert.

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
const staging = await enclii.secrets.list(serviceId, { environment_id: stagingEnvId });
```

The endpoint returns every variable in one response (`{ environment_variables: [...] }`), so `nextCursor` is always `null`. `environment_id` is sent as a query parameter to narrow the list. Secret values come back masked as `••••••••`.

## Bulk set

```typescript
const { count } = await enclii.secrets.bulkSet(serviceId, [
  { key: 'LOG_LEVEL', value: 'info', is_secret: false },
  { key: 'API_KEY', value: '...', is_secret: true },
]);

// Scope the whole batch to one environment
await enclii.secrets.bulkSet(serviceId, [{ key: 'LOG_LEVEL', value: 'debug' }], {
  environment_id: stagingEnvId,
});
```

`bulkSet()` creates or updates each variable by key. It sends `{ variables: [...], environment_id? }` and resolves to the API's `{ message, count }`; the API does not return the variables, so call `list()` if you need their IDs. It throws a plain `Error` before sending when the batch is empty or has more than 100 entries (the API's limit).

## Types

```typescript
interface EnvVar {
  id: UUID;
  service_id: UUID;
  environment_id?: UUID; // absent when the variable applies to every environment
  key: string;
  value: string;         // '••••••••' when is_secret is true
  is_secret: boolean;
  created_at: ISODateTime;
  updated_at: ISODateTime;
}

interface SetEnvVarRequest {
  key: string;
  value: string;
  is_secret?: boolean;
  environment_id?: string;
}

type BulkEnvVar = Omit<SetEnvVarRequest, 'environment_id'>;

interface BulkSetEnvVarsResponse {
  message: string;
  count: number;
}
```

## Related documentation

- [Services](./services.md)
- [CLI: `enclii secrets`](../../cli/commands/secrets.md)
