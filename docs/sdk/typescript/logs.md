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

`since` is sent as the `since` query parameter: an RFC3339 timestamp (`2026-09-24T10:00:00Z`) or a positive Go duration (`15m`, `24h`), the two forms `parseLogsSince` (`apps/switchyard-api/internal/api/logs_since.go`) accepts. `history()` throws a plain `Error` before sending when `since` is neither; the server also answers 400 for a timestamp in the future and caps the window at 30 days. A switchyard-api that includes [#622](https://github.com/madfam-org/enclii/pull/622) returns only lines newer than it; an older one ignores it and returns the most recent `lines` regardless.

## How the stream authenticates

The stream route checks two things on the upgrade (`websocketOriginAllowed` in `apps/switchyard-api/internal/api/ws_upgrade.go`):

- **Token:** an `Authorization: Bearer <token>` header, or a `token` query parameter when a header cannot be set (`apps/switchyard-api/internal/auth/jwt_middleware.go`).
- **Origin:** when an `Origin` header is sent, it must exactly match one of the server's configured WebSocket origins (`ENCLII_WEBSOCKET_ALLOWED_ORIGINS`), or the upgrade is refused with HTTP 403. An upgrade without `Origin` is accepted only when it authenticated with the `Authorization` header; a query-token upgrade without `Origin` is refused with 403.

Browsers always send the page's origin and cannot set headers, so the allow-list is what stops another site from opening a stream with a victim's credentials. A Bearer header is never attached by a browser on its own, so a client that sends one already holds the token and needs no `Origin`. That splits the two helpers by runtime:

| Helper | Runtime | Token | Origin |
|--------|---------|-------|--------|
| `logs.tail()` | Browser | `?token=` query parameter | The page's origin, which the server must allow |
| `nodeLogsTail()` | Node.js | `Authorization` header | None needed; `options.origin` is sent when set and must then be allowed |

`logs.tail()` does not work outside a browser: it authenticates with a query token, and `globalThis.WebSocket` in Node.js 22+, Deno, and Bun sends no `Origin`, so the server refuses the upgrade. Use `nodeLogsTail()` there.

A switchyard-api that predates [#625](https://github.com/madfam-org/enclii/pull/625) refuses every upgrade without an allowed `Origin`, including Bearer-authenticated ones; against such a server `nodeLogsTail()` needs `options.origin`, and `enclii logs --follow` fails with 403.

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

`nodeLogsTail()` uses the `ws` package, sends the bearer token in the `Authorization` header (as `enclii logs --follow` does), sends `options.origin` as the `Origin` header only when it is set, and reconnects with exponential backoff (starting at `initialReconnectMs`, doubling, with jitter, capped at 30 seconds).

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
| `origin` | `string` | none; not sent. Needed only against a server that predates Bearer upgrades without `Origin` |
| `token` | `string` | the client's token |
| `maxReconnects` | `number` | `5` |
| `initialReconnectMs` | `number` | `1_000` |
| `onReconnect` | `(attempt: number, reason: string) => void` | none |
| `onParseError` | `(raw: string) => void` | none; called for frames that are not stream messages |

When the server rejects the upgrade with a 4xx status (for example 403 for a disallowed `origin`, 404 for an unknown `env`), `nodeLogsTail()` throws an `Error` naming the status and does not retry; a 403 without `origin` says that older servers need it. Other disconnects are retried up to `maxReconnects` times, after which the iterator completes. Each reconnect replays the server's `lines` backlog (limited by `since` when set), so lines can repeat across a reconnect.

`packages/sdk-ts/examples/tail-logs.ts` is a runnable version.

## Stream options and frames

`LogTailOptions` maps onto these query parameters of the stream:

| Field | Query parameter | Default on the server |
|-------|-----------------|-----------------------|
| `env` | `env` | `development`; an env the project does not have is a 404 before the upgrade |
| `lines` | `lines` | `100` (backlog per pod before following) |
| `timestamps` | `timestamps=true` | off |
| `since` | `since` | none: the last `lines` lines per pod, however old |
| `signal` | none | |

`since` limits the backlog to lines newer than an RFC3339 timestamp (`2026-09-24T10:00:00Z`) or a positive Go duration (`15m`, `24h`), as `enclii logs --follow --since` sends it; `lines` still caps it per pod. Both helpers throw a plain `Error` before connecting when `since` is neither form, because a refused upgrade shows no 400 body in a browser. `nodeLogsTail()` sends the same `since` on every reconnect, so a duration is measured from the reconnect and a timestamp keeps its fixed start. A switchyard-api that predates [#625](https://github.com/madfam-org/enclii/pull/625) ignores `since` on the stream.

```typescript
for await (const frame of nodeLogsTail(enclii, serviceId, { env: 'production', since: '15m' })) {
  if (frame.type === 'log') console.log(frame.timestamp, frame.message);
}
```

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
  since?: string;  // RFC3339 timestamp or positive Go duration
}

interface LogTailOptions {
  env?: string;
  lines?: number;
  timestamps?: boolean;
  since?: string;  // RFC3339 timestamp or positive Go duration
  signal?: AbortSignal;
}
```

`LogEntry` is a deprecated alias of `LogStreamMessage`, and `LogLevel` is deprecated: no log endpoint filters by level.

## Related documentation

- [TypeScript SDK overview](./index.md)
- [CLI: `enclii logs`](../../cli/commands/logs.md)
