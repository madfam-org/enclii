/**
 * Node-only entrypoint for @madfam/enclii-sdk.
 *
 * The browser entry (`@madfam/enclii-sdk`) uses `globalThis.WebSocket` for log
 * streaming — that works in modern browsers and Node ≥22 but throws on older
 * Node releases. This subpath re-exports everything from the main entry and
 * adds a `nodeLogsTail()` helper that uses the `ws` library directly and
 * handles graceful reconnect with exponential backoff.
 *
 * ```ts
 * import { EncliiClient, nodeLogsTail } from '@madfam/enclii-sdk/node';
 *
 * const enclii = new EncliiClient({...});
 * for await (const entry of nodeLogsTail(enclii, 'svc_123', {level: 'error'})) {
 *   console.log(entry.timestamp, entry.message);
 * }
 * ```
 */

export * from './index';

import WebSocket from 'ws';
import type { EncliiClient } from './client';
import type { LogEntry, LogTailOptions } from './types';

export interface NodeLogsTailOptions extends LogTailOptions {
  /** Max reconnect attempts before giving up. Defaults to 5. */
  maxReconnects?: number;
  /** Initial reconnect delay in ms. Defaults to 1_000. */
  initialReconnectMs?: number;
  /** Called once per reconnect attempt for observability. */
  onReconnect?: (attempt: number, reason: string) => void;
  /** Called when a non-fatal message couldn't be parsed. */
  onParseError?: (raw: string) => void;
  /** Bearer token for the WS upgrade (defaults to resolving from the client). */
  token?: string;
}

/**
 * Node-side log streaming with reconnect-on-disconnect.
 *
 * The returned AsyncIterable yields log entries; consumers break the loop to
 * stop streaming. An AbortSignal on `options.signal` also stops streaming.
 * The function reconnects up to `maxReconnects` times on transient
 * disconnects; after that it gives up and completes the iterator.
 */
export async function* nodeLogsTail(
  client: EncliiClient,
  serviceId: string,
  options: NodeLogsTailOptions = {},
): AsyncIterable<LogEntry> {
  const maxReconnects = options.maxReconnects ?? 5;
  const initialBackoff = options.initialReconnectMs ?? 1_000;
  let attempt = 0;
  let closedByCaller = false;

  options.signal?.addEventListener('abort', () => {
    closedByCaller = true;
  });

  while (!closedByCaller && attempt <= maxReconnects) {
    const url = buildWsUrl(client, serviceId, options);
    const token =
      options.token ??
      (await (
        client as unknown as {
          // Internal — resolve the auth token without re-implementing auth.
          auth: { getToken(): Promise<string | null | undefined> };
        }
      ).auth.getToken());

    const headers: Record<string, string> = {};
    if (token) headers['Authorization'] = `Bearer ${token}`;

    let ws: WebSocket;
    try {
      ws = new WebSocket(url, { headers });
    } catch (err) {
      if (attempt >= maxReconnects) throw err;
      await sleep(backoff(initialBackoff, attempt));
      attempt++;
      options.onReconnect?.(attempt, `open failed: ${(err as Error).message}`);
      continue;
    }

    const queue: LogEntry[] = [];
    let resolver: ((r: IteratorResult<LogEntry>) => void) | null = null;
    let closed = false;
    let closeReason = 'clean';

    ws.on('message', (data) => {
      const raw = data.toString();
      try {
        const parsed = JSON.parse(raw) as LogEntry;
        if (resolver) {
          const r = resolver;
          resolver = null;
          r({ value: parsed, done: false });
        } else {
          queue.push(parsed);
        }
      } catch {
        options.onParseError?.(raw);
      }
    });
    ws.on('error', (err) => {
      closeReason = `error: ${err.message}`;
    });
    ws.on('close', () => {
      closed = true;
      if (resolver) {
        const r = resolver;
        resolver = null;
        r({ value: undefined as unknown as LogEntry, done: true });
      }
    });

    try {
      while (!closed || queue.length > 0) {
        if (closedByCaller) {
          ws.close();
          return;
        }
        if (queue.length > 0) {
          yield queue.shift()!;
          continue;
        }
        const next = await new Promise<IteratorResult<LogEntry>>((resolve) => {
          resolver = resolve;
        });
        if (next.done) break;
        yield next.value;
      }
    } finally {
      try {
        ws.close();
      } catch {
        /* no-op */
      }
    }

    if (closedByCaller) return;
    if (attempt >= maxReconnects) {
      // Exhausted retries; complete the iterator.
      return;
    }
    attempt++;
    options.onReconnect?.(attempt, closeReason);
    await sleep(backoff(initialBackoff, attempt));
  }
}

function buildWsUrl(
  client: EncliiClient,
  serviceId: string,
  options: NodeLogsTailOptions,
): string {
  const baseHttp = (client as unknown as { baseUrl: string }).baseUrl;
  const base = baseHttp.replace(/^http(s?):\/\//, 'ws$1://');
  const params = new URLSearchParams();
  if (options.level) params.append('level', String(options.level));
  if (options.pod) params.append('pod', options.pod);
  if (options.container) params.append('container', options.container);
  const qs = params.toString();
  return `${base}/services/${encodeURIComponent(serviceId)}/logs/stream${qs ? `?${qs}` : ''}`;
}

function backoff(initial: number, attempt: number): number {
  const base = initial * Math.pow(2, attempt - 1);
  const jitter = base * (0.8 + Math.random() * 0.4);
  return Math.min(Math.round(jitter), 30_000);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
