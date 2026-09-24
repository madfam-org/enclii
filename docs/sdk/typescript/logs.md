---
title: Logs
description: Read and stream service logs with the Enclii TypeScript SDK
sidebar_position: 8
tags: [sdk, typescript, logs, websocket]
---

# Logs

`enclii.logs` (`LogsResource`, `packages/sdk-ts/src/resources/logs.ts`) reads historical logs and streams live logs for a service. The Node.js subpath adds `nodeLogsTail()` (`packages/sdk-ts/src/node.ts`).

| API | Signature | HTTP |
|-----|-----------|------|
| `logs.history` | `history(serviceId: string, options?: LogHistoryOptions): Promise<Page<LogEntry>>` | `GET /services/{id}/logs/history` |
| `logs.iter` | `iter(serviceId: string, options?: Omit<LogHistoryOptions, 'cursor'>): AsyncIterable<LogEntry>` | `GET /services/{id}/logs/history` |
| `logs.tail` | `tail(serviceId: string, options?: LogTailOptions): AsyncIterable<LogEntry>` | WebSocket `/services/{id}/logs/stream` |
| `nodeLogsTail` (from `@madfam/enclii-sdk/node`) | `nodeLogsTail(client: EncliiClient, serviceId: string, options?: NodeLogsTailOptions): AsyncIterable<LogEntry>` | WebSocket `/services/{id}/logs/stream` |

The stream URL is `baseUrl` with `http`/`https` replaced by `ws`/`wss`.

> **Read the [current API behaviour](#current-api-behaviour) section before relying on any of these.** The SDK's log methods and the current API's log endpoints disagree on parameters, response shape, and WebSocket handshake requirements.

## Setup

```typescript
import { EncliiClient } from '@madfam/enclii-sdk';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});
```

## Stream live logs in Node.js

`nodeLogsTail()` uses the `ws` package, sends the client's bearer token in the `Authorization` header of the WebSocket upgrade, and reconnects with exponential backoff (starting at `initialReconnectMs`, doubling, with jitter, capped at 30 seconds).

```typescript
import { EncliiClient, nodeLogsTail } from '@madfam/enclii-sdk/node';

const enclii = new EncliiClient({
  baseUrl: 'https://api.enclii.dev/v1',
  token: process.env.ENCLII_API_TOKEN,
});

const abort = new AbortController();
process.on('SIGINT', () => abort.abort());

for await (const entry of nodeLogsTail(enclii, serviceId, {
  signal: abort.signal,
  maxReconnects: 10,
  onReconnect: (attempt, reason) => console.error(`reconnect #${attempt}: ${reason}`),
})) {
  console.log(entry.timestamp, entry.pod, entry.message);
}
```

`NodeLogsTailOptions` extends `LogTailOptions` with:

| Field | Type | Default |
|-------|------|---------|
| `maxReconnects` | `number` | `5` |
| `initialReconnectMs` | `number` | `1_000` |
| `onReconnect` | `(attempt: number, reason: string) => void` | none |
| `onParseError` | `(raw: string) => void` | none; called for frames that are not valid JSON |
| `token` | `string` | the client's token |

After `maxReconnects` reconnects the iterator completes (it does not throw). Breaking out of the loop or aborting `signal` stops streaming.

`packages/sdk-ts/examples/tail-logs.ts` is a runnable version.

## Stream live logs with `logs.tail`

```typescript
for await (const entry of enclii.logs.tail(serviceId)) {
  console.log(entry.timestamp, entry.message);
}
```

`logs.tail()` uses `globalThis.WebSocket` and throws a plain `Error` if none exists (Node.js before 22, some custom runtimes). It does not reconnect, and it throws a plain `Error` if the socket reports an error. It does not send the client's token; see below.

## Historical logs

```typescript
const page = await enclii.logs.history(serviceId, {
  level: 'error',
  since: '2026-09-01T00:00:00Z',
  limit: 200,
});
```

`history()` sends `limit`, `level`, `since`, `until`, and `cursor` as query parameters and returns `{ data: resp.logs, nextCursor }`. `iter()` walks the same endpoint through `client.paginate()`.

## Current API behaviour

These are differences between the SDK and the current API (`apps/switchyard-api/internal/api/logs_handlers.go`, `apps/switchyard-api/internal/auth/jwt_middleware.go`):

- **`history()` / `iter()`:** the endpoint reads only the `env` (default `development`) and `lines` query parameters; `limit`, `level`, `since`, `until`, and `cursor` are ignored. It returns `logs` as a single string of raw log text, not an array, so at runtime `page.data` is a string despite the `LogEntry[]` type, and `iter()` yields nothing useful. The SDK has no option for `env` or `lines`; call the endpoint directly if you need them:

  ```typescript
  const resp = await enclii.get<{ logs: string; lines: number }>(
    `/services/${serviceId}/logs/history`,
    { env: 'production', lines: 500 },
  );
  console.log(resp.logs);
  ```

- **WebSocket handshake:** the stream endpoint needs both a token (in the `Authorization` header or a `token` query parameter) and an `Origin` header that matches the server's configured list of allowed WebSocket origins.
  - `logs.tail()` sends neither a header nor a `token` query parameter (the `WebSocket` constructor cannot set headers, and the SDK does not add the token to the URL), so the request is rejected as unauthenticated and the iterator throws.
  - `nodeLogsTail()` sends the `Authorization` header, but the `ws` package sends no `Origin` header by default and the SDK does not set one, so the upgrade is refused. Each refusal counts as a reconnect, and the iterator completes without yielding once `maxReconnects` is reached.

  As a result, neither streaming function opens a stream against the current API.
- **Stream filters:** the stream endpoint reads `env` (default `development`), `lines`, and `timestamps`; the SDK's `level`, `pod`, and `container` options are sent but ignored, and neither streaming function can set `env`.
- **Stream frames:** each frame is `{ type, pod?, container?, timestamp, message }`, where `type` is one of `log`, `error`, `info`, `connected`, or `disconnected`. The SDK yields every frame as a `LogEntry`, including the `connected` and `disconnected` status frames, and frames carry no `level`. Filter on `(entry as { type?: string }).type === 'log'` if you only want log lines.

## Types

```typescript
type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'fatal';

interface LogEntry {
  timestamp: ISODateTime;
  pod: string;
  message: string;
  level?: LogLevel | string;
  container?: string;
}

interface LogHistoryOptions {
  limit?: number;
  level?: LogLevel | string;
  since?: string;  // ISO-8601, inclusive
  until?: string;  // ISO-8601, exclusive
  cursor?: string;
}

interface LogTailOptions {
  level?: LogLevel | string;
  pod?: string;
  container?: string;
  signal?: AbortSignal;
}
```

## Related documentation

- [TypeScript SDK overview](./index.md)
- [CLI: `enclii logs`](../../cli/commands/logs.md)
