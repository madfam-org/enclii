---
title: TypeScript SDK
description: Official TypeScript/JavaScript SDK for the Enclii API
sidebar_position: 1
tags: [sdk, typescript, javascript, api]
---

# Enclii TypeScript SDK

The official TypeScript/JavaScript SDK for the Enclii control-plane API, published as `@madfam/enclii-sdk` (source: `packages/sdk-ts`, version 0.1.0). This page covers installation, client configuration, pagination, retries, and errors. Each resource namespace has its own page, listed under [Resources](#resources).

## Installation

```bash
# pnpm
pnpm add @madfam/enclii-sdk

# npm
npm install @madfam/enclii-sdk

# yarn
yarn add @madfam/enclii-sdk
```

## Requirements

- Node.js 18 or later (`engines.node` is `>=18`), for `fetch` and `SubtleCrypto`, or a modern browser.
- `logs.tail()` needs a global `WebSocket` (browsers, Node.js 22+). On older Node.js releases use `nodeLogsTail()` from the `@madfam/enclii-sdk/node` subpath, which uses the `ws` package. See [Logs](./logs.md).

## Quick Start

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1', // required; must include /v1
  token: process.env.ENCLII_API_TOKEN,  // your variable; the SDK reads no env vars
});

// First page of projects
const { data: projects } = await enclii.projects.list();

// Fetch a deployment by Heroku-style v-label
const dep = await enclii.deployments.get(serviceId, 'v42');

// Iterate every service in a project
for await (const svc of enclii.services.iter('my-project')) {
  console.log(svc.name);
}
```

## Client options

`new EncliiClient(options: EncliiClientOptions)` accepts:

| Option | Type | Default | Notes |
|--------|------|---------|-------|
| `baseUrl` | `string` | none (required) | Must include the `/v1` prefix, for example `https://api.enclii.dev/v1`. The client does not append `/v1`. A trailing slash is removed. The constructor throws if it is empty. |
| `token` | `string \| TokenProvider \| AuthStrategy \| null` | anonymous | Sent as `Authorization: Bearer <token>`. See [Authentication](./authentication.md). |
| `retry` | `RetryOptions` | see [Retries](#retries-and-timeouts) | Merged over the defaults. |
| `timeoutMs` | `number` | `30_000` | Per-attempt request timeout. |
| `defaultHeaders` | `Record<string, string>` | `{}` | Added to every request. |
| `fetch` | `typeof fetch` | `globalThis.fetch` | The constructor throws if no `fetch` implementation is available. |
| `userAgent` | `string` | `@madfam/enclii-sdk/0.1.0` | Sent as the `user-agent` header. |

The SDK has no default API URL and does not read environment variables. There are no `apiKey`, `accessToken`, `tokenProvider`, `timeout`, `retries`, or request/response hook options.

```typescript
const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
  retry: { maxAttempts: 3, initialDelayMs: 250, backoffFactor: 2, maxDelayMs: 10_000 }, // the defaults
  timeoutMs: 30_000,
  defaultHeaders: { 'x-client': 'my-app' },
});
```

Every request sends `content-type: application/json`, `accept: application/json`, and `user-agent`. `defaultHeaders`, then per-request `headers`, can override those. The `authorization` header is set last, whenever the token resolves to a non-empty value.

## Authentication

The only credential option is `token`, sent as `Authorization: Bearer <token>`. It accepts a string, an async function called before every request, an `AuthStrategy` object, or `null`/omitted for anonymous requests. Use a personal API token from [`enclii tokens create`](../../cli/commands/tokens.md) for automation, or the signed-in user's OIDC access token in user-facing apps. See the [Authentication guide](./authentication.md).

## Resources

The client exposes ten resource namespaces. There is no `domains`, `releases`, `environments`, `members`, or `apiKeys` namespace.

| Namespace | Page | Methods |
|-----------|------|---------|
| `projects` | [Projects](./projects.md) | `get`, `list`, `iter`, `create`, `delete` |
| `services` | [Services](./services.md) | `get`, `list`, `iter`, `create`, `delete`, `restart`, `scale` |
| `deployments` | [Deployments](./deployments.md) | `get`, `getByVersion`, `list`, `iter`, `latest`, `deploy`, `build`, `listReleases`, `wait` |
| `rollback` | [Rollback](./rollback.md) | `instant`, `manifest` |
| `canary` | [Canary](./canary.md) | `start`, `get`, `promote`, `rollback`, `wait` |
| `logs` | [Logs](./logs.md) | `history`, `iter`, `tail` |
| `secrets` | [Secrets](./secrets.md) | `list`, `set`, `bulkSet`, `delete`, `reveal` |
| `jobs` | [Jobs](./jobs.md) | `listCron`, `getCron`, `createCron`, `updateCron`, `deleteCron`, `listCronRuns`, `listOneOff`, `createOneOff` |
| `webhooks` | [Webhooks](./webhooks.md) | `list`, `iter`, `create`, `get`, `update`, `rotateSecret`, `delete`, `test`, `deliveries`, `eventTypes` |
| `audit` | [Audit](./audit.md) | `list`, `iter`, `actions`, `resourceTypes` |

Custom domains have no SDK resource; see [Domains](./domains.md).

Other exports from `@madfam/enclii-sdk`:

- `parseVersionLabel(label: string): number | null` converts `"v42"` to `42`.
- `isCanaryTerminal(state: CanaryRolloutState): boolean`.
- `verifyWebhookSignature(...)` and `DEFAULT_SIGNATURE_TOLERANCE_SECONDS` (300), for webhook receivers.
- The auth classes `StaticTokenAuth`, `TokenProviderAuth`, `AnonymousAuth`, and `resolveAuthStrategy`.
- All error classes and all types from `src/types.ts`.

`@madfam/enclii-sdk/node` re-exports all of the above and adds `nodeLogsTail()`.

### Low-level requests

For endpoints no resource wraps, the client exposes `request<T>(opts: RequestOptions)` and the shortcuts `get`, `post`, `patch`, `put`, and `del`. Paths are relative to `baseUrl`.

```typescript
const status = await enclii.request<Record<string, unknown>>({
  method: 'GET',
  path: `/services/${serviceId}/status`,
  query: { env: 'production' },
  skipRetry: true,          // single attempt
  signal: abortController.signal,
});
```

`RequestOptions` fields: `method` (default `GET`), `path`, `query`, `body` (JSON-encoded), `headers`, `skipRetry`, `signal`. The resource methods do not accept an abort signal; use `request()` when you need one.

Response handling: a `204` or `content-length: 0` response resolves to `undefined`; a response without an `application/json` content type resolves to its text; anything else is parsed as JSON. Responses are cast to the declared type, not validated.

## Pagination

List methods return `Page<T>`:

```typescript
interface Page<T> {
  data: T[];
  nextCursor: string | null;
}
```

They send `limit` and `cursor` as query parameters and read `next_cursor` from the response. The `iter()` methods return an `AsyncIterable<T>` built on `client.paginate()`, which requests one page at a time and stops when `next_cursor` is missing or empty.

```typescript
const page = await enclii.projects.list({ limit: 50 });
if (page.nextCursor) {
  const next = await enclii.projects.list({ limit: 50, cursor: page.nextCursor });
}

for await (const project of enclii.projects.iter({ pageSize: 50 })) {
  console.log(project.slug);
}
```

> **Current API behaviour:** none of the endpoints that the SDK's list methods call in the current API (`apps/switchyard-api`) return `next_cursor`, so `nextCursor` is always `null` and `iter()` yields only the first response. Most of these endpoints return every row in one response; `GET /activity` and webhook deliveries use `limit`/`offset` paging, which the SDK does not expose.

## Retries and timeouts

`RetryOptions` and their defaults:

| Field | Default | Meaning |
|-------|---------|---------|
| `maxAttempts` | `3` | Total attempts, including the first. `1` disables retries. |
| `initialDelayMs` | `250` | Delay before the second attempt. |
| `backoffFactor` | `2` | Multiplier per attempt. |
| `maxDelayMs` | `10_000` | Cap on any single delay. |

A request is retried when it fails with `NetworkError`, `ServerError` (5xx), or `RateLimitError` (429). The delay is `initialDelayMs * backoffFactor^(attempt - 1)` with plus or minus 20% jitter, capped at `maxDelayMs`. When a 429 carries `Retry-After`, the SDK waits that long instead (also capped at `maxDelayMs`).

Retries apply to every HTTP method, including `POST` calls such as `deployments.deploy()` and `canary.start()`. Pass `retry: { maxAttempts: 1 }` or use `request({ ..., skipRetry: true })` if a repeated `POST` is not acceptable for your use.

`timeoutMs` applies to each attempt. A timed-out attempt is aborted and surfaces as a `NetworkError`, which is retried like any other network failure.

## Error handling

Failed requests throw typed errors. All extend `EncliiError`:

| Class | Thrown for |
|-------|------------|
| `ValidationError` | HTTP 400, 422 |
| `AuthenticationError` | HTTP 401 |
| `AuthorizationError` | HTTP 403 |
| `NotFoundError` | HTTP 404 |
| `ConflictError` | HTTP 409 |
| `RateLimitError` | HTTP 429; has `retryAfterSeconds?: number` from `Retry-After` |
| `ServerError` | HTTP 500 and above |
| `NetworkError` | `fetch` failures, timeouts, and aborts (no HTTP status) |
| `EncliiError` | Any other non-2xx status |

Every error carries `method`, `path`, `status?`, `requestId?` (from the `x-request-id` response header), and `details?` (the parsed response body, or the underlying cause for `NetworkError`). The `message` is the response body's `error` or `message` string when present, otherwise `HTTP <status>`.

```typescript
import {
  EncliiError,
  NotFoundError,
  RateLimitError,
} from '@madfam/enclii-sdk';

try {
  await enclii.projects.get('missing-project');
} catch (err) {
  if (err instanceof RateLimitError) {
    console.warn(`Rate limited; retry after ${err.retryAfterSeconds ?? '?'}s`);
  } else if (err instanceof NotFoundError) {
    console.warn('No such project');
  } else if (err instanceof EncliiError) {
    console.error(`${err.method} ${err.path} -> ${err.status}: ${err.message}`);
    console.error(`Request ID: ${err.requestId}`);
  } else {
    throw err;
  }
}
```

Some methods throw a plain `Error` before any request is sent or while polling: `deployments.get()` with an invalid v-label, `deployments.wait()` and `canary.wait()` on timeout or abort, `webhooks.create()` with a non-`https://` URL, and `logs.tail()` when no `WebSocket` is available or the socket errors.

## Types

The hand-written types in `src/types.ts` are exported from the package root and are what the resource methods use. A generated companion, `src/types.generated.ts`, is built from `docs/api/openapi.yaml` with `pnpm generate-types`; it is not re-exported from the package entry points.

## Framework integration

### Next.js route handler

```typescript
// lib/enclii.ts
import { EncliiClient } from '@madfam/enclii-sdk';

export const enclii = new EncliiClient({
  baseUrl: process.env.ENCLII_BASE_URL!, // e.g. https://api.enclii.dev/v1
  token: process.env.ENCLII_API_TOKEN,
});

// app/api/projects/route.ts
import { enclii } from '@/lib/enclii';

export async function GET() {
  const { data } = await enclii.projects.list();
  return Response.json(data);
}
```

`ENCLII_BASE_URL` and `ENCLII_API_TOKEN` here are your application's own variables; the SDK only sees the values you pass.

### CI

The SDK is a library, not a command-line tool: there is no `npx @madfam/enclii-sdk deploy`. From CI, call it from a script (see the [CI/CD example](./authentication.md#cicd-github-actions)) or use the [`enclii` CLI](../../cli/README.md).

## Related documentation

- **Package README**: `packages/sdk-ts/README.md`
- **Runnable examples**: `packages/sdk-ts/examples/`
- **API reference**: [OpenAPI docs](../../api-reference/index.md)
- **Go SDK**: [packages/sdk-go](https://github.com/madfam-org/enclii/tree/main/packages/sdk-go)
- **CLI**: [CLI reference](../../cli/README.md)
