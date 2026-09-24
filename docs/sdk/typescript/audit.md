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
  console.log(e.timestamp, e.action, e.resource_type, e.resource_name, e.actor_email, e.outcome);
}
```

`AuditQueryOptions` (all optional, sent as query parameters):

| Field | Type | Notes |
|-------|------|-------|
| `action` | `string` | |
| `resource_type` | `string` | |
| `project_id` | `string` | A UUID. The API silently drops a value that is not one. |
| `actor_id` | `string` | A UUID. The API silently drops a value that is not one. |
| `limit` | `number` | Page size, 1 to 100; the API default is 50. |
| `cursor` | `string` | A `nextCursor` from a previous page. |

## Paging

`GET /activity` (`GetActivity` in `apps/switchyard-api/internal/api/activity_handlers.go`) pages with `limit` and `offset` and answers `{ activities, count, limit, offset }`. The SDK maps that onto `Page<T>`: `cursor` is sent as `offset`, and `nextCursor` is set whenever a page comes back full (as many rows as the `limit` the server applied), otherwise `null`. Treat the cursor as opaque.

```typescript
const first = await enclii.audit.list({ limit: 100 });
if (first.nextCursor) {
  const second = await enclii.audit.list({ limit: 100, cursor: first.nextCursor });
}

// Or let iter() walk every page, `limit` rows at a time (default 50)
for await (const e of enclii.audit.iter({ action: 'deploy', limit: 100 })) {
  console.log(e.timestamp, e.resource_name);
}
```

`list()` throws a plain `Error` before sending for a `limit` outside 1 to 100 (the API would silently use 50) or a cursor it did not return. Offset paging can skip or repeat a row when new events arrive between pages.

## Discover filter values

```typescript
const actions = await enclii.audit.actions();
const resourceTypes = await enclii.audit.resourceTypes();
```

Both return fixed lists compiled into the API.

## Types

```typescript
interface AuditEvent {
  id: UUID;
  timestamp: ISODateTime;
  actor_id?: UUID;
  actor_email: string;
  actor_role: string;
  action: string;
  resource_type: string;
  resource_id: string;
  resource_name: string;
  project_id?: UUID;
  environment_id?: UUID;
  ip_address: string;
  user_agent: string;
  outcome: string; // 'success' | 'failure' | 'denied'
  context: Record<string, unknown> | null;
  metadata?: Record<string, unknown>;
}
```

## Related documentation

- [TypeScript SDK overview](./index.md)
- [CLI: `enclii activity`](../../cli/commands/activity.md)
- [CLI: `enclii audit`](../../cli/commands/audit.md)
