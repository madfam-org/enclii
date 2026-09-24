import type { EncliiClient } from '../client';
import type {
  LogHistory,
  LogHistoryOptions,
  LogStreamMessage,
  LogTailOptions,
} from '../types-ops';
import { buildLogStreamUrl, parseLogFrame } from './log-stream';

/** Server-side bound on `lines` for the history endpoint (logs_handlers.go). */
const MAX_HISTORY_LINES = 10_000;

/**
 * Service logs (`apps/switchyard-api/internal/api/logs_handlers.go`).
 *
 *   - `history(serviceId, opts)` — one request; returns the recent raw log
 *     text of the service's pods in one environment.
 *   - `tail(serviceId, opts)` — live WebSocket stream for **browsers**. It
 *     authenticates with a `token` query parameter and relies on the browser
 *     sending the page's `Origin`, which the server must allow (a query-token
 *     upgrade without an allowed `Origin` is refused). In Node.js use
 *     `nodeLogsTail` from `@madfam/enclii-sdk/node` instead.
 */
export class LogsResource {
  constructor(private readonly client: EncliiClient) {}

  /** Fetch recent logs for a service as raw text. */
  async history(
    serviceId: string,
    options: LogHistoryOptions = {},
  ): Promise<LogHistory> {
    if (
      options.lines !== undefined &&
      (!Number.isInteger(options.lines) ||
        options.lines < 1 ||
        options.lines > MAX_HISTORY_LINES)
    ) {
      // The API silently replaces an out-of-range value with 100.
      throw new Error(
        `logs.history: lines must be an integer from 1 to ${MAX_HISTORY_LINES}`,
      );
    }
    return this.client.get<LogHistory>(
      `/services/${encodeURIComponent(serviceId)}/logs/history`,
      { env: options.env, lines: options.lines, since: options.since },
    );
  }

  /**
   * Tail live logs over `globalThis.WebSocket`. Yields every frame the server
   * sends, including the `connected` status frame; filter on
   * `frame.type === 'log'` for log lines.
   *
   * Browser-only in practice: this authenticates with a query token, and
   * the server refuses a query-token upgrade without an allowed `Origin`
   * header, which non-browser `WebSocket` implementations (Node.js 22+,
   * Deno, Bun) do not send. The token travels in the URL (`?token=`) because
   * a browser WebSocket cannot set headers.
   */
  async *tail(
    serviceId: string,
    options: LogTailOptions = {},
  ): AsyncIterable<LogStreamMessage> {
    const WS = (globalThis as { WebSocket?: typeof WebSocket }).WebSocket;
    if (typeof WS !== 'function') {
      throw new Error(
        'logs.tail: no WebSocket implementation found. ' +
          'Outside a browser, use nodeLogsTail from "@madfam/enclii-sdk/node".',
      );
    }

    const token = await this.client.resolveToken();
    const url = buildLogStreamUrl(
      this.client.baseUrl,
      serviceId,
      options,
      token,
    );
    if (options.signal?.aborted) return;
    const ws = new WS(url);

    const queue: LogStreamMessage[] = [];
    let wake: (() => void) | null = null;
    let opened = false;
    let closed = false;
    let failure: Error | null = null;
    const notify = () => {
      const w = wake;
      wake = null;
      w?.();
    };

    const onAbort = () => {
      closed = true;
      try {
        ws.close();
      } catch {
        /* no-op */
      }
      notify();
    };
    options.signal?.addEventListener('abort', onAbort);

    ws.addEventListener('open', () => {
      opened = true;
    });
    ws.addEventListener('message', (ev) => {
      const frame = parseLogFrame(
        typeof ev.data === 'string' ? ev.data : String(ev.data),
      );
      if (frame) {
        queue.push(frame);
        notify();
      }
    });
    ws.addEventListener('error', () => {
      failure = new Error(
        opened
          ? `logs.tail: WebSocket error (service ${serviceId})`
          : `logs.tail: WebSocket handshake failed (service ${serviceId}). ` +
            'The server refuses the upgrade when the token is missing or invalid, ' +
            "when the page's Origin is not in its allowed WebSocket origins, " +
            'or when the caller cannot access the service.',
      );
      notify();
    });
    ws.addEventListener('close', () => {
      closed = true;
      notify();
    });

    try {
      for (;;) {
        if (queue.length > 0) {
          yield queue.shift()!;
          continue;
        }
        if (failure && !options.signal?.aborted) throw failure;
        if (closed) return;
        await new Promise<void>((resolve) => {
          wake = resolve;
        });
      }
    } finally {
      options.signal?.removeEventListener('abort', onAbort);
      try {
        ws.close();
      } catch {
        /* no-op */
      }
    }
  }
}
