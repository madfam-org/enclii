/**
 * Node-only entrypoint for @madfam/enclii-sdk.
 *
 * Re-exports everything from the main entry and adds `nodeLogsTail()`, which
 * streams `GET /services/{id}/logs/stream` with the `ws` library. Unlike a
 * browser WebSocket, `ws` can send the `Authorization` header (as the CLI
 * does) and an explicit `Origin` header, and it reconnects with exponential
 * backoff.
 *
 * ```ts
 * import { EncliiClient, nodeLogsTail } from '@madfam/enclii-sdk/node';
 *
 * const enclii = new EncliiClient({...});
 * for await (const frame of nodeLogsTail(enclii, 'svc_123', {
 *   env: 'production',
 *   origin: 'https://app.example.com', // one of the server's allowed WS origins
 * })) {
 *   if (frame.type === 'log') console.log(frame.timestamp, frame.message);
 * }
 * ```
 */

export * from './index';

import WebSocket from 'ws';
import type { EncliiClient } from './client';
import type { LogStreamMessage, LogTailOptions } from './types-ops';
import { buildLogStreamUrl, parseLogFrame } from './resources/log-stream';

export interface NodeLogsTailOptions extends LogTailOptions {
  /** Max reconnect attempts before giving up. Defaults to 5. */
  maxReconnects?: number;
  /** Initial reconnect delay in ms. Defaults to 1_000. */
  initialReconnectMs?: number;
  /** Called once per reconnect attempt for observability. */
  onReconnect?: (attempt: number, reason: string) => void;
  /** Called when a frame is not a JSON stream message. */
  onParseError?: (raw: string) => void;
  /** Bearer token for the upgrade (defaults to resolving from the client). */
  token?: string;
  /**
   * Value of the `Origin` header. The server refuses the upgrade unless this
   * exactly matches one of its configured WebSocket origins
   * (`ENCLII_WEBSOCKET_ALLOWED_ORIGINS`); without it the upgrade gets 403.
   */
  origin?: string;
}

/**
 * Node-side log streaming with reconnect-on-disconnect.
 *
 * Yields every frame (`log`, `error`, `info`, `connected`, `disconnected`);
 * consumers break the loop or abort `options.signal` to stop. A dropped
 * connection is retried up to `maxReconnects` times, after which the iterator
 * completes. An upgrade the server rejects with a 4xx status (bad token,
 * disallowed or missing `Origin`, no access) is not retried: the iterator
 * throws. Each reconnect replays the server's `lines` backlog.
 */
export async function* nodeLogsTail(
  client: EncliiClient,
  serviceId: string,
  options: NodeLogsTailOptions = {},
): AsyncIterable<LogStreamMessage> {
  const maxReconnects = options.maxReconnects ?? 5;
  const initialBackoff = options.initialReconnectMs ?? 1_000;
  let attempt = 0;
  let closedByCaller = options.signal?.aborted ?? false;

  let current: WebSocket | null = null;
  let wake: (() => void) | null = null;
  const onAbort = () => {
    closedByCaller = true;
    current?.close();
    const w = wake;
    wake = null;
    w?.();
  };
  options.signal?.addEventListener('abort', onAbort);

  try {
    while (!closedByCaller) {
      const url = buildLogStreamUrl(client.baseUrl, serviceId, options);
      const token = options.token ?? (await client.resolveToken());
      const headers: Record<string, string> = {};
      if (token) headers['Authorization'] = `Bearer ${token}`;

      const ws = new WebSocket(url, {
        headers,
        ...(options.origin ? { origin: options.origin } : {}),
      });
      current = ws;

      const queue: LogStreamMessage[] = [];
      let closed = false;
      let closeReason = 'connection closed';
      let rejectedStatus: number | null = null;
      const notify = () => {
        const w = wake;
        wake = null;
        w?.();
      };

      ws.on('unexpected-response', (_req, res) => {
        rejectedStatus = res.statusCode ?? 0;
        closeReason = `upgrade rejected with HTTP ${rejectedStatus}`;
        ws.terminate();
      });
      ws.on('message', (data) => {
        const raw = data.toString();
        const frame = parseLogFrame(raw);
        if (frame) queue.push(frame);
        else options.onParseError?.(raw);
        notify();
      });
      ws.on('error', (err) => {
        if (rejectedStatus === null) closeReason = `error: ${err.message}`;
      });
      ws.on('close', () => {
        closed = true;
        notify();
      });

      try {
        for (;;) {
          if (queue.length > 0) {
            yield queue.shift()!;
            continue;
          }
          if (closed || closedByCaller) break;
          await new Promise<void>((resolve) => {
            wake = resolve;
          });
        }
      } finally {
        ws.close();
      }

      if (closedByCaller) return;
      const status = rejectedStatus as number | null;
      if (status !== null && status >= 400 && status < 500) {
        throw new Error(
          `nodeLogsTail: ${closeReason} (service ${serviceId}). ` +
            (status === 403 && !options.origin
              ? 'The server requires an Origin header from its allowed WebSocket origins; set options.origin.'
              : 'Check the token, options.origin, and access to the service.'),
        );
      }
      if (attempt >= maxReconnects) return;
      attempt++;
      options.onReconnect?.(attempt, closeReason);
      await sleep(backoff(initialBackoff, attempt));
    }
  } finally {
    options.signal?.removeEventListener('abort', onAbort);
  }
}

function backoff(initial: number, attempt: number): number {
  const base = initial * Math.pow(2, attempt - 1);
  const jitter = base * (0.8 + Math.random() * 0.4);
  return Math.min(Math.round(jitter), 30_000);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
