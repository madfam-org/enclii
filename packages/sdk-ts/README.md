# @madfam/enclii-sdk

Official TypeScript SDK for the [Enclii](https://enclii.dev) DevOps platform.

- Type-safe client for the Enclii control plane API
- Retry with exponential backoff on 429/5xx
- `list()` returns `{ data, nextCursor }`; `iter()` yields every row as an AsyncIterable
- Canary, rollback (instant + manifest), v-number lookup
- Outbound lifecycle webhook subscriptions and signature verification
- Streaming logs via WebSocket (`logs.tail` in browsers, `nodeLogsTail` in Node)

## Install

```bash
pnpm add @madfam/enclii-sdk
# or: npm install @madfam/enclii-sdk / yarn add @madfam/enclii-sdk
```

Requires Node ≥18 (for `fetch` and `SubtleCrypto`). Works in modern browsers
for everything except Node-specific log streaming.

## Quickstart

```ts
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_TOKEN,
});

// Fetch a deployment by Heroku-style v-number
const dep = await enclii.deployments.get('svc_123', 'v42');

// Iterate every service of a project
for await (const svc of enclii.services.iter('my-project')) {
  console.log(svc.name);
}
```

## Client configuration

```ts
const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',

  // Static token, or a refresh-capable provider:
  token: 'eykjwt...',
  // token: async () => await getFreshToken(),

  // Retry configuration — defaults: 3 attempts, exp-backoff on 429/5xx
  retry: {
    maxAttempts: 3,
    initialDelayMs: 250,
    backoffFactor: 2,
    maxDelayMs: 10_000,
  },
  timeoutMs: 30_000,
  defaultHeaders: { 'x-client': 'my-dashboard' },
});
```

## Resources

### Projects

```ts
await enclii.projects.list(); // every project in one response
await enclii.projects.get('my-project');
await enclii.projects.create({ name: 'My Project', slug: 'my-proj' });
await enclii.projects.delete('old-proj');
```

### Services

```ts
await enclii.services.list('my-project');
await enclii.services.create('my-project', {
  name: 'api',
  git_repo: 'git@github.com:acme/api.git',
});
// Admin role. `environment` is sent as `env` and only labels the audit record.
await enclii.services.restart('svc_123', { reason: 'config reload' });
await enclii.services.scale('svc_123', 5);
```

### Deployments + v-number resolution

```ts
// Fetch by UUID
const byUuid = await enclii.deployments.get('dep_abc');

// Fetch by Heroku-style v-label
const byLabel = await enclii.deployments.get('svc_123', 'v42');

// Or the integer form
const byInt = await enclii.deployments.getByVersion('svc_123', 42);

// Trigger a deploy and wait until running
const dep = await enclii.deployments.deploy('svc_123', {
  release_id: 'rel_1',
  environment_name: 'prod',
});
await enclii.deployments.wait(dep.id, { timeoutMs: 10 * 60_000 });
```

### Rollback

```ts
// P0.5 instant rollback — traffic shifts at the selector layer in <30s
await enclii.rollback.instant('svc_123', {
  target_deployment_id: 'dep_previous',
  reason: 'bad push: p99 latency spike',
  change_ticket_url: 'https://jira/CHG-123',
});

// Roll a deployment back to the service's previous running deployment.
// The API takes no target; use instant() to choose one.
const { rolled_back_to } = await enclii.rollback.manifest('dep_current');
```

### Canary (P2.7)

```ts
const rollout = await enclii.canary.start('svc_123', {
  digest: 'sha256:...',
  percentage: 20,
  validation_window_minutes: 10,
  smoke_endpoint: '/health/deep',
  error_rate_threshold: 0.01,
});

// Poll until terminal
const final = await enclii.canary.wait('svc_123', rollout.id);

// Or short-circuit
await enclii.canary.promote('svc_123', rollout.id);
// or abort:
await enclii.canary.rollback('svc_123', rollout.id, { reason: '5xx spike' });
```

### Logs

```ts
// Recent logs as raw text (one request)
const { logs } = await enclii.logs.history('svc_123', {
  env: 'production',
  lines: 500,
});

// Live tail in a browser. The token goes in the URL (?token=) and the page's
// Origin must be one of the server's allowed WebSocket origins.
for await (const frame of enclii.logs.tail('svc_123', { env: 'production' })) {
  if (frame.type === 'log') console.log(frame.timestamp, frame.pod, frame.message);
}

// Node: Authorization header, explicit Origin, reconnect with backoff
import { nodeLogsTail } from '@madfam/enclii-sdk/node';
for await (const frame of nodeLogsTail(enclii, 'svc_123', {
  env: 'production',
  origin: process.env.ENCLII_WS_ORIGIN, // must match an allowed origin
  maxReconnects: 10,
  onReconnect: (n, reason) => console.warn(`reconnect #${n}: ${reason}`),
})) {
  /* ... */
}
```

### Audit / activity

```ts
// One page (limit 1-100, default 50); pass nextCursor back as cursor
const page = await enclii.audit.list({ action: 'deploy', limit: 100 });

// Every matching event, fetched page by page
for await (const event of enclii.audit.iter({ resource_type: 'service' })) {
  /* ... */
}
```

### Lifecycle webhooks (P2.3)

```ts
// Create — signing_secret is returned exactly ONCE, save immediately
const { subscription, signing_secret } = await enclii.webhooks.create(
  'my-project',
  {
    name: 'Slack #deploys',
    url: 'https://hooks.example.com/enclii',
    event_types: ['deploy.succeeded', 'deploy.failed', 'rollback.succeeded'],
  },
);
await persistSecret(signing_secret);

// Fire a synthetic test.ping
await enclii.webhooks.test(subscription.id);

// Rotate (returns another one-time secret)
const { signing_secret: rotated } = await enclii.webhooks.rotateSecret(
  subscription.id,
);
```

Verify inbound deliveries in your receiver:

```ts
import { verifyWebhookSignature } from '@madfam/enclii-sdk';

// In an Express handler with raw body capture
app.post('/enclii-webhook', async (req, res) => {
  try {
    await verifyWebhookSignature(
      req.rawBody,                                // exact bytes received
      req.header('x-enclii-signature'),           // t=<ts>,v1=<hex>
      process.env.ENCLII_WEBHOOK_SECRET!,
    );
  } catch (err) {
    return res.status(401).send('invalid signature');
  }
  const event = JSON.parse(req.rawBody.toString());
  // ... handle event
  res.status(204).end();
});
```

### Secrets / env-vars

```ts
await enclii.secrets.set('svc_123', {
  key: 'DATABASE_URL',
  value: 'postgres://...',
  is_secret: true,
});
const { count } = await enclii.secrets.bulkSet('svc_123', [
  { key: 'LOG_LEVEL', value: 'info', is_secret: false },
  { key: 'API_KEY', value: '...', is_secret: true },
]); // upsert by key; returns { message, count }
const revealed = await enclii.secrets.reveal('svc_123', 'var_abc');
```

### Jobs (Timetable)

```ts
await enclii.jobs.createCron('my-project', {
  name: 'nightly-sync',
  schedule: '0 2 * * *',
  command: 'npm run sync',
  service_id: 'svc_123',
});

await enclii.jobs.createOneOff('my-project', {
  name: 'migrate-users-v2',
  command: 'npm run migrate',
  service_id: 'svc_123',
});
```

## Errors

All errors extend `EncliiError`. Use `instanceof` to disambiguate:

```ts
import {
  AuthenticationError,
  NotFoundError,
  RateLimitError,
  ServerError,
  EncliiError,
} from '@madfam/enclii-sdk';

try {
  await enclii.deployments.get('svc_123', 'v42');
} catch (err) {
  if (err instanceof NotFoundError) { /* ... */ }
  if (err instanceof RateLimitError) {
    console.log(`retry after ${err.retryAfterSeconds}s`);
  }
  if (err instanceof EncliiError) {
    console.log(`request ${err.requestId} failed: ${err.status}`);
  }
}
```

| Error class | HTTP |
|-------------|------|
| `AuthenticationError` | 401 |
| `AuthorizationError` | 403 |
| `NotFoundError` | 404 |
| `ConflictError` | 409 |
| `ValidationError` | 400, 422 |
| `RateLimitError` | 429 |
| `ServerError` | 5xx |
| `NetworkError` | — (fetch/connect failures) |

## Type generation from OpenAPI

The SDK ships curated, hand-written types in `src/types.ts` and
`src/types-ops.ts`. A fully generated
companion from `docs/api/openapi.yaml` lives at `src/types.generated.ts` (run
`pnpm generate-types`). The hand-written types are ergonomic; the generated
types give full fidelity for edge cases.

## License

AGPL-3.0 — © Innovaciones MADFAM S.A.S. de C.V.
