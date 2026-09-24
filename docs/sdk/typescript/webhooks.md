---
title: Webhooks
description: Manage outbound lifecycle webhook subscriptions and verify signatures with the Enclii TypeScript SDK
sidebar_position: 11
tags: [sdk, typescript, webhooks]
---

# Webhooks

`enclii.webhooks` (`WebhooksResource`, `packages/sdk-ts/src/resources/webhooks.ts`) manages **outbound lifecycle webhook** subscriptions: Enclii POSTs signed JSON to your HTTPS endpoint when deploys, rollbacks, secret rotations, and scaling events happen. The module also exports `verifyWebhookSignature()` for your receiver.

These are not the notification webhooks (Slack, Discord, Telegram, custom) managed under `/projects/{slug}/webhooks`; the SDK has no resource for those.

| Method | Signature | HTTP |
|--------|-----------|------|
| `list` | `list(projectSlug: string): Promise<Page<OutboundWebhookSubscription>>` | `GET /projects/{slug}/lifecycle-webhooks` |
| `iter` | `iter(projectSlug: string): AsyncIterable<OutboundWebhookSubscription>` | `GET /projects/{slug}/lifecycle-webhooks` |
| `create` | `create(projectSlug: string, input: CreateWebhookSubscriptionRequest): Promise<CreateWebhookSubscriptionResponse>` | `POST /projects/{slug}/lifecycle-webhooks` |
| `get` | `get(subscriptionId: string): Promise<OutboundWebhookSubscription>` | `GET /lifecycle-webhooks/{id}` |
| `update` | `update(subscriptionId: string, input: UpdateWebhookSubscriptionRequest): Promise<OutboundWebhookSubscription>` | `PATCH /lifecycle-webhooks/{id}` |
| `rotateSecret` | `rotateSecret(subscriptionId: string): Promise<CreateWebhookSubscriptionResponse>` | `POST /lifecycle-webhooks/{id}/rotate-secret` |
| `delete` | `delete(subscriptionId: string): Promise<void>` | `DELETE /lifecycle-webhooks/{id}` |
| `test` | `test(subscriptionId: string): Promise<OutboundWebhookDelivery>` | `POST /lifecycle-webhooks/{id}/test` |
| `deliveries` | `deliveries(subscriptionId: string, options?: { limit?: number; cursor?: string }): Promise<Page<OutboundWebhookDelivery>>` | `GET /lifecycle-webhooks/{id}/deliveries` |
| `eventTypes` | `eventTypes(): Promise<OutboundWebhookEventTypeInfo[]>` | `GET /lifecycle-webhooks/event-types` |

The API handlers are in `apps/switchyard-api/internal/api/outbound_webhook_handlers.go`. The subscriptions endpoint returns every subscription in one response, so `list()` has `nextCursor: null` and `iter()` makes one request (their deprecated `limit`/`cursor`/`pageSize` options are not sent).

There is no redeliver method; the API route `POST /lifecycle-webhooks/{id}/deliveries/{deliveryId}/redeliver` can be called with [`client.post()`](./index.md#low-level-requests).

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Create a subscription

The signing secret is returned **once**, at create time (and again only when you rotate). The server stores a SHA-256 hash and cannot return the raw value later, so persist it immediately.

```typescript
const { subscription, signing_secret } = await enclii.webhooks.create('my-project', {
  name: 'deploys-to-chat',
  url: 'https://hooks.example.com/enclii',
  event_types: ['deploy.succeeded', 'deploy.failed', 'rollback.succeeded'],
});

await saveSecretSomewhereSafe(subscription.id, signing_secret);
```

`url` must start with `https://`; otherwise `create()` throws a plain `Error` before sending the request. An empty or omitted `event_types` subscribes to every event type.

`CreateWebhookSubscriptionRequest`:

| Field | Type | Required |
|-------|------|----------|
| `name` | `string` | yes |
| `url` | `string` | yes (`https://` only) |
| `event_types` | `OutboundWebhookEventType[]` | no |

## Update, rotate, delete

```typescript
await enclii.webhooks.update(subscription.id, { active: false });

const { signing_secret: rotated } = await enclii.webhooks.rotateSecret(subscription.id);

await enclii.webhooks.delete(subscription.id); // soft delete; the API route requires the admin role
```

`UpdateWebhookSubscriptionRequest` has the optional fields `name`, `url`, `event_types`, and `active`.

## Test and inspect deliveries

```typescript
const delivery = await enclii.webhooks.test(subscription.id); // enqueues a synthetic test.ping
console.log(delivery.id, delivery.status);

const { data } = await enclii.webhooks.deliveries(subscription.id, { limit: 20 });
for (const d of data) {
  console.log(d.event_type, d.status, d.http_status, d.attempt_number);
}
```

The deliveries endpoint pages with `limit` (1 to 200, default 50) and `offset`. `deliveries()` sends `cursor` as the `offset`, and returns a `nextCursor` whenever a page comes back full, so you can walk every delivery:

```typescript
let cursor: string | undefined;
do {
  const page = await enclii.webhooks.deliveries(subscription.id, { limit: 200, cursor });
  for (const d of page.data) console.log(d.id, d.status);
  cursor = page.nextCursor ?? undefined;
} while (cursor);
```

Treat the cursor as opaque. `deliveries()` throws a plain `Error` before sending for a `limit` outside 1 to 200 (the API answers 400; servers before [#625](https://github.com/madfam-org/enclii/pull/625) silently used 50) or a cursor it did not return. See [Pagination](./index.md#pagination).

## Verify deliveries in your receiver

Each delivery carries an `X-Enclii-Signature` header in the form `t=<unix>,v1=<hex>` (more than one `v1=` value may be present). The signature is HMAC-SHA256 over `<t>.<raw body>` with the signing secret.

```typescript
import { verifyWebhookSignature } from '@madfam/enclii-sdk';

// Express, with the raw body captured (for example express.raw({ type: 'application/json' }))
app.post('/enclii-webhook', async (req, res) => {
  try {
    await verifyWebhookSignature(
      req.body,                           // raw bytes (Buffer is a Uint8Array) or string
      req.header('x-enclii-signature'),
      process.env.ENCLII_WEBHOOK_SECRET!, // your variable holding the signing secret
    );
  } catch {
    return res.status(401).send('invalid signature');
  }
  const event = JSON.parse(req.body.toString());
  // event: { id, type, created_at, api_version, data }
  res.status(204).end();
});
```

Signature:

```typescript
function verifyWebhookSignature(
  rawBody: string | Uint8Array,
  signatureHeader: string | null | undefined,
  secret: string,
  options?: { toleranceSeconds?: number; nowSeconds?: () => number },
): Promise<void>;
```

- Resolves when any `v1` signature matches; otherwise rejects with a plain `Error` (missing header, no `t=` or `v1=` values, timestamp outside the tolerance, or no match).
- `toleranceSeconds` defaults to `DEFAULT_SIGNATURE_TOLERANCE_SECONDS` (300).
- Pass the exact bytes you received. Re-serializing parsed JSON changes whitespace and breaks the HMAC.
- Uses Web Crypto (`globalThis.crypto.subtle`), available in browsers and Node.js 19+. On Node.js 18 set `globalThis.crypto = require('crypto').webcrypto` first.

The delivered JSON body has the shape `OutboundWebhookEnvelope`: `{ id, type, created_at, api_version, data }`.

## Event types

`OutboundWebhookEventType` is `'deploy.started' | 'deploy.succeeded' | 'deploy.failed' | 'rollback.succeeded' | 'secret.rotated' | 'service.scaled'`.

`eventTypes()` returns the subscribable lifecycle event types with a description of each:

```typescript
const types = await enclii.webhooks.eventTypes();
// [{ type: 'deploy.started', description: '...' }, ...]
```

The route requires authentication. It is distinct from `GET /webhooks/event-types`, which lists the notification-webhook event types (for example `deployment.succeeded`).

## Types

```typescript
interface OutboundWebhookSubscription {
  id: UUID;
  project_id: UUID;
  name: string;
  url: string;
  secret_sha256_prefix: string; // first 8 hex chars of SHA-256(secret)
  event_types: OutboundWebhookEventType[]; // empty = all
  active: boolean;
  created_by: string;
  created_at: ISODateTime;
  updated_at: ISODateTime;
  last_success_at?: ISODateTime | null;
  last_failure_at?: ISODateTime | null;
  consecutive_failures: number;
  auto_disabled_at?: ISODateTime | null;
}

interface OutboundWebhookEventTypeInfo {
  type: OutboundWebhookEventType;
  description: string;
}

interface CreateWebhookSubscriptionResponse {
  subscription: OutboundWebhookSubscription;
  signing_secret: string; // returned exactly once
  note: string;
}

type OutboundWebhookDeliveryStatus = 'pending' | 'delivering' | 'delivered' | 'failed' | 'dlq';

interface OutboundWebhookDelivery {
  id: UUID;
  subscription_id: UUID;
  lifecycle_event_id?: UUID | null;
  event_id: string;
  event_type: OutboundWebhookEventType | string;
  payload?: Record<string, unknown>;
  payload_sha256: string;
  attempt_number: number;
  status: OutboundWebhookDeliveryStatus;
  http_status?: number | null;
  response_snippet?: string;
  error_message?: string;
  attempted_at?: ISODateTime | null;
  delivered_at?: ISODateTime | null;
  duration_ms?: number | null;
  next_retry_at?: ISODateTime | null;
  created_at: ISODateTime;
}

interface OutboundWebhookEnvelope<TData = Record<string, unknown>> {
  id: string;
  type: OutboundWebhookEventType | string;
  created_at: ISODateTime;
  api_version: string;
  data: TData;
}
```

`packages/sdk-ts/examples/webhook-subscription.ts` is a runnable create, test, and verify example.

## Related documentation

- [TypeScript SDK overview](./index.md)
- [CLI: `enclii webhooks`](../../cli/commands/webhooks.md)
