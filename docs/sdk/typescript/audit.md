---
title: Audit
description: Query the activity and audit log with the Enclii TypeScript SDK
sidebar_position: 12
tags: [sdk, typescript, audit, activity]
---

# Audit

`enclii.audit` (`AuditResource`, `packages/sdk-ts/src/resources/audit.ts`) reads the activity log, backed by the API's `/activity` endpoints. Resource mutations such as deploys, rollbacks, secret changes, and webhook changes produce activity rows.

| Method | Signature | HTTP |
|--------|-----------|------|
| `list` | `list(options?: AuditQueryOptions): Promise<Page<AuditEvent>>` | `GET /activity` |
| `iter` | `iter(options?: Omit<AuditQueryOptions, 'cursor'>): AsyncIterable<AuditEvent>` | `GET /activity` |
| `actions` | `actions(): Promise<string[]>` | `GET /activity/actions` |
| `resourceTypes` | `resourceTypes(): Promise<string[]>` | `GET /activity/resource-types` |

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Query events

```typescript
const { data } = await enclii.audit.list({
  project_id: projectId,
  resource_type: 'service',
  limit: 100,
});

for (const e of data) {
  console.log(e.action, e.resource_type, e.resource_id, e.actor_email);
}
```

`AuditQueryOptions` (all optional, sent as query parameters):

| Field | Type |
|-------|------|
| `action` | `string` |
| `resource_type` | `string` |
| `project_id` | `string` |
| `actor_id` | `string` |
| `limit` | `number` |
| `cursor` | `string` |

`iter()` takes the same filters without `cursor` and uses `limit` as the page size.

## Discover filter values

```typescript
const actions = await enclii.audit.actions();
const resourceTypes = await enclii.audit.resourceTypes();
```

## Current API behaviour

- `GET /activity` pages with `limit` and `offset` and does not return `next_cursor`, so `cursor` is ignored and `list()`/`iter()` only reach the first page. Use `limit` to size it, or call the endpoint directly with `offset`:

  ```typescript
  import type { AuditEvent } from '@madfam/enclii-sdk';

  const resp = await enclii.get<{ activities: AuditEvent[]; count: number; limit: number; offset: number }>(
    '/activity',
    { limit: 100, offset: 100 },
  );
  ```

- The API's activity rows carry their time in a `timestamp` field, not `created_at`, and have no `service_id`. `AuditEvent.created_at` and `AuditEvent.service_id` are therefore `undefined` at runtime. Rows also include fields the SDK type does not declare, such as `actor_role`, `resource_name`, `environment_id`, `outcome`, and `context`.

## Types

```typescript
interface AuditEvent {
  id: UUID;
  actor_id?: UUID;
  actor_email?: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  project_id?: UUID;
  service_id?: UUID;
  metadata?: Record<string, unknown>;
  created_at: ISODateTime;
}
```

## Related documentation

- [TypeScript SDK overview](./index.md)
- [CLI: `enclii activity`](../../cli/commands/activity.md)
- [CLI: `enclii audit`](../../cli/commands/audit.md)
