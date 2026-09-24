---
title: Logs
description: Read and stream service logs with the Enclii TypeScript SDK
sidebar_position: 8
tags: [sdk, typescript, logs, websocket]
---

# Logs

`enclii.logs` (`LogsResource`, `packages/sdk-ts/src/resources/logs.ts`) reads recent logs and streams live logs for a service. The Node.js subpath adds `nodeLogsTail()` (`packages/sdk-ts/src/node.ts`). The API handlers are in `apps/switchyard-api/internal/api/logs_handlers.go`.

| API | Signature | HTTP |
|-----|-----------|------|
| `logs.history` | `history(serviceId: string, options?: LogHistoryOptions): Promise<LogHistory>` | `GET /services/{id}/logs/history` |
| `logs.tail` | `tail(serviceId: string, options?: LogTailOptions): AsyncIterable<LogStreamMessage>` | WebSocket `/services/{id}/logs/stream` |
| `nodeLogsTail` (from `@madfam/enclii-sdk/node`) | `nodeLogsTail(client: EncliiClient, serviceId: string, options?: NodeLogsTailOptions): AsyncIterable<LogStreamMessage>` | WebSocket `/services/{id}/logs/stream` |

The stream URL is `baseUrl` with `http`/`https` replaced by `ws`/`wss`.

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Recent logs

```typescript
const history = await enclii.logs.history(serviceId, {
  env: 'production', // default: development
  lines: 500,        // 1 to 10000; default: 100
});

for (const line of history.logs.split('\n')) {
  console.log(line);
}
```

`history()` sends `env`, `lines`, and `since` as query parameters and resolves to the API's response:

```typescript
interface LogHistory {
  service_id: UUID;
  service_name: string;
  environment: string;
  namespace: string;
  logs: string;  // raw log text of the service's pods, newline-separated
  lines: number; // lines requested per pod
}
```

The endpoint returns one block of text; it does not page, and there is no `iter()`. `history()` throws a plain `Error` before sending when `lines` is outside 1 to 10000 (the API would silently use 100 instead).

`since` is sent as the `since` query parameter, but the current API ignores it. It is honored once the server-side change that adds `since` to this endpoint is deployed; until then you get the most recent `lines` regardless.

## How the stream authenticates

The stream route accepts the upgrade only when both hold:

- **Token:** an `Authorization: Bearer <token>` header, or a `token` query parameter when a header cannot be set (`apps/switchyard-api/internal/auth/jwt_middleware.go`).
- **Origin:** an `Origin` header that exactly matches one of the server's configured WebSocket origins (`ENCLII_WEBSOCKET_ALLOWED_ORIGINS`; `CheckOrigin` in `logs_handlers.go`). A request without `Origin` is refused with HTTP 403.

Browsers always send the page's origin and cannot set headers. Node.js can set both headers but sends no `Origin` unless told to. That splits the two helpers by runtime:

| Helper | Runtime | Token | Origin |
|--------|---------|-------|--------|
| `logs.tail()` | Browser | `?token=` query parameter | The page's origin, which the server must allow |
| `nodeLogsTail()` | Node.js | `Authorization` header | `options.origin`, which must be an allowed origin |

`logs.tail()` does not work outside a browser: `globalThis.WebSocket` in Node.js 22+, Deno, and Bun sends no `Origin`, so the server refuses the upgrade. Use `nodeLogsTail()` there.

## Stream live logs in a browser

```typescript
for await (const frame of enclii.logs.tail(serviceId, { env: 'production' })) {
  if (frame.type === 'log') {
    console.log(frame.timestamp, frame.pod, frame.message);
  }
}
```

`logs.tail()` resolves the client's token and appends it as `token` to the stream URL, because a browser `WebSocket` cannot send an `Authorization` header. The token is therefore part of the URL, which proxies and server access logs may record; prefer a short-lived token (for example the signed-in user's OIDC access token) over a long-lived API token.

It throws a plain `Error` when no `WebSocket` global exists, or when the socket reports an error. A refused handshake (missing or invalid token, disallowed origin, no access to the service) surfaces as that error; browsers do not expose the HTTP status. It does not reconnect. Aborting `signal`, breaking out of the loop, or the server closing the stream ends the iterator.

## Stream live logs in Node.js

`nodeLogsTail()` uses the `ws` package, sends the bearer token in the `Authorization` header (as `enclii logs --follow` does), sends `options.origin` as the `Origin` header, and reconnects with exponential backoff (starting at `initialReconnectMs`, doubling, with jitter, capped at 30 seconds).

```typescript
import { EncliiClient, nodeLogsTail } from '@madfam/enclii-sdk/node';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});

const abort = new AbortController();
process.on('SIGINT', () => abort.abort());

for await (const frame of nodeLogsTail(enclii, serviceId, {
  env: 'production',
  origin: process.env.ENCLII_WS_ORIGIN, // one of the server's allowed WebSocket origins
  signal: abort.signal,
  maxReconnects: 10,
  onReconnect: (attempt, reason) => console.error(`reconnect #${attempt}: ${reason}`),
})) {
  if (frame.type === 'log') console.log(frame.timestamp, frame.pod, frame.message);
}
```

`NodeLogsTailOptions` extends `LogTailOptions` with:

| Field | Type | Default |
|-------|------|---------|
| `origin` | `string` | none; without it the server answers 403 |
| `token` | `string` | the client's token |
| `maxReconnects` | `number` | `5` |
| `initialReconnectMs` | `number` | `1_000` |
| `onReconnect` | `(attempt: number, reason: string) => void` | none |
| `onParseError` | `(raw: string) => void` | none; called for frames that are not stream messages |

When the server rejects the upgrade with a 4xx status, `nodeLogsTail()` throws an `Error` naming the status and does not retry; a 403 without `origin` says to set it. Other disconnects are retried up to `maxReconnects` times, after which the iterator completes. Each reconnect replays the server's `lines` backlog, so lines can repeat across a reconnect.

`packages/sdk-ts/examples/tail-logs.ts` is a runnable version.

## Stream options and frames

`LogTailOptions` maps onto the only query parameters the stream reads:

| Field | Query parameter | Default on the server |
|-------|-----------------|-----------------------|
| `env` | `env` | `development` |
| `lines` | `lines` | `100` (backlog per pod before following) |
| `timestamps` | `timestamps=true` | off |
| `signal` | none | |

Both helpers yield every frame the server sends, typed `LogStreamMessage`:

```typescript
type LogStreamMessageType = 'log' | 'error' | 'info' | 'connected' | 'disconnected';

interface LogStreamMessage {
  type: LogStreamMessageType;
  pod?: string;       // set on 'log' frames
  container?: string; // set on 'log' frames
  timestamp: ISODateTime;
  message: string;
}
```

Only `log` frames carry log lines. The server sends `connected` first; `error` frames report a Kubernetes log-stream error while the stream stays open. Frames carry no log level. Frames that are not JSON stream messages are skipped (`nodeLogsTail()` reports them to `onParseError`).

## Types

```typescript
interface LogHistoryOptions {
  env?: string;
  lines?: number;  // 1 to 10000
  since?: string;  // sent; honored once the server change deploys
}

interface LogTailOptions {
  env?: string;
  lines?: number;
  timestamps?: boolean;
  signal?: AbortSignal;
}
```

`LogEntry` is a deprecated alias of `LogStreamMessage`, and `LogLevel` is deprecated: no log endpoint filters by level.

## Related documentation

- [TypeScript SDK overview](./index.md)
- [CLI: `enclii logs`](../../cli/commands/logs.md)
