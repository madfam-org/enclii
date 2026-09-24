---
title: TypeScript SDK
description: Official TypeScript/JavaScript SDK for the Enclii API
sidebar_position: 1
tags: [sdk, typescript, javascript, api]
---

# Enclii TypeScript SDK

The official TypeScript/JavaScript SDK for the Enclii control-plane API, published as `@madfam/enclii-sdk` (source: `packages/sdk-ts`).

## Installation

```bash
# pnpm
pnpm add @madfam/enclii-sdk

# npm
npm install @madfam/enclii-sdk

# yarn
yarn add @madfam/enclii-sdk
```

## Quick Start

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1', // required; must include /v1
  token: process.env.ENCLII_API_TOKEN,  // personal API token or OIDC access token
});

// List your projects
const projects = await enclii.projects.list();

// Fetch a deployment by Heroku-style v-number
const dep = await enclii.deployments.get('svc_123', 'v42');

// Iterate every service in a project, page by page
for await (const svc of enclii.services.iter('my-project')) {
  console.log(svc.name);
}
```

## Requirements

- Node.js 18+ (for `fetch` and `SubtleCrypto`), or a modern browser. Log streaming through the `@madfam/enclii-sdk/node` subpath is Node-only.

## Authentication

The SDK has a single `token` option, sent as `Authorization: Bearer <token>`. It accepts a string, an async function called before every request, or an `AuthStrategy` object. Use a personal API token from [`enclii tokens create`](../../cli/commands/tokens.md) for automation, or the signed-in user's OIDC access token in user-facing apps. The SDK does not read environment variables by itself.

See the [Authentication Guide](./authentication) for details.

## Resources

The client exposes these resource namespaces: `projects`, `services`, `deployments`, `rollback`, `canary`, `logs`, `audit`, `webhooks`, `secrets`, and `jobs`. There is no `domains` or `apiKeys` resource.

| Page | Covers |
|------|--------|
| [Projects](./projects) | Projects |
| [Services](./services) | Services |
| [Deployments](./deployments) | Deployments |
| [Domains](./domains) | Custom domains |

> The four module pages above were written against an earlier API sketch and still show methods the 0.1.0 SDK does not have (for example `services.deploy`, `projects.setVariable`, and the whole `domains` resource). Until they are rewritten, use the method list in `packages/sdk-ts/README.md` and the type definitions shipped with the package as the reference.

## Error Handling

Failed requests throw typed errors. Check them with `instanceof`:

```typescript
import {
  EncliiError,
  AuthenticationError,
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

Status mapping: 400/422 `ValidationError`, 401 `AuthenticationError`, 403 `AuthorizationError`, 404 `NotFoundError`, 409 `ConflictError`, 429 `RateLimitError`, 5xx `ServerError`. The client already retries 429 and 5xx responses with exponential backoff before throwing.

## Configuration Options

```typescript
const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1', // required, includes /v1

  // Bearer token: string, async provider, or AuthStrategy; omit for anonymous
  token: process.env.ENCLII_API_TOKEN,

  // Retries on 429/5xx (these are the defaults)
  retry: {
    maxAttempts: 3,
    initialDelayMs: 250,
    backoffFactor: 2,
    maxDelayMs: 10_000,
  },
  timeoutMs: 30_000,                        // default
  defaultHeaders: { 'x-client': 'my-app' }, // added to every request
  // fetch: customFetch,                    // defaults to globalThis.fetch
  // userAgent: 'my-app/1.0',               // defaults to @madfam/enclii-sdk/<version>
});
```

There are no `apiKey`, `accessToken`, `tokenProvider`, `timeout`, `retries`, or `onRequest`/`onResponse`/`onError` options.

## Framework Integration

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
  return Response.json(await enclii.projects.list());
}
```

`ENCLII_BASE_URL` and `ENCLII_API_TOKEN` here are your application's own variables; the SDK only sees the values you pass.

### GitHub Actions

The SDK is a library, not a command-line tool: there is no `npx @madfam/enclii-sdk deploy`. From CI, call it from a script (see the [CI/CD example](./authentication#cicd-github-actions)) or use the [`enclii` CLI](/cli/).

## Related Documentation

- **Package README**: `packages/sdk-ts/README.md` (full method list)
- **API Reference**: [OpenAPI Docs](/api-reference/)
- **Authentication**: [Auth Guide](./authentication)
- **Go SDK**: [Go SDK](https://github.com/madfam-org/enclii/tree/main/packages/sdk-go)
- **CLI**: [CLI Reference](/cli/)
