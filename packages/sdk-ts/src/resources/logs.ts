import type { EncliiClient } from '../client';
import type {
  LogEntry,
  LogHistoryOptions,
  LogTailOptions,
  Page,
} from '../types';

/**
 * Log retrieval.
 *
 * Two variants:
 *   - `history(serviceId, opts)` — synchronous, cursor-paginated. Good for
 *     "give me the last N lines" use cases.
 *   - `tail(serviceId, opts)` — AsyncIterable streaming. Browser-safe via
 *     WebSocket (the platform's `WebSocket` global); for long-running
 *     Node sessions with reconnect backoff, import from
 *     `@madfam/enclii-sdk/node` instead.
 */
export class LogsResource {
  constructor(private readonly client: EncliiClient) {}

  /** Fetch historical logs for a service. Cursor-paginated. */
  async history(
    serviceId: string,
    options: LogHistoryOptions = {},
  ): Promise<Page<LogEntry>> {
    const resp = await this.client.get<{
      logs: LogEntry[];
      next_cursor?: string | null;
    }>(`/services/${encodeURIComponent(serviceId)}/logs/history`, {
      limit: options.limit,
      level: options.level,
      since: options.since,
      until: options.until,
      cursor: options.cursor,
    });
    return {
      data: resp.logs ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  /** Iterate every historical log line lazily. */
  iter(
    serviceId: string,
    options: Omit<LogHistoryOptions, 'cursor'> = {},
  ): AsyncIterable<LogEntry> {
    return this.client.paginate<LogEntry>(
      `/services/${encodeURIComponent(serviceId)}/logs/history`,
      {
        itemsField: 'logs',
        query: {
          level: options.level,
          since: options.since,
          until: options.until,
        },
        pageSize: options.limit,
      },
    );
  }

  /**
   * Tail live logs via WebSocket. AsyncIterable so consumers can use
   * `for await (const entry of enclii.logs.tail(id)) { ... }`.
   *
   * Uses `globalThis.WebSocket` — fine in browsers and Node ≥22. For
   * Node-specific reconnect-with-backoff behavior, import
   * `nodeLogsTail` from `@madfam/enclii-sdk/node`.
   */
  async *tail(
    serviceId: string,
    options: LogTailOptions = {},
  ): AsyncIterable<LogEntry> {
    const WS = (globalThis as { WebSocket?: typeof WebSocket }).WebSocket;
    if (typeof WS !== 'function') {
      throw new Error(
        'logs.tail: no WebSocket implementation found. ' +
          'In Node < 22 or custom runtimes, import from "@madfam/enclii-sdk/node" instead.',
      );
    }

    const url = this.buildStreamUrl(serviceId, options);
    const ws = new WS(url);

    // Graceful teardown plumbing.
    let closed = false;
    const onAbort = () => {
      closed = true;
      try {
        ws.close();
      } catch {
        /* no-op */
      }
    };
    if (options.signal) {
      if (options.signal.aborted) onAbort();
      else options.signal.addEventListener('abort', onAbort);
    }

    try {
      // Drive a queue from the WS event callbacks.
      const queue: LogEntry[] = [];
      let resolver: ((v: IteratorResult<LogEntry>) => void) | null = null;
      let errored: Error | null = null;

      ws.addEventListener('message', (ev) => {
        try {
          const parsed = JSON.parse(
            typeof ev.data === 'string' ? ev.data : String(ev.data),
          ) as LogEntry;
          if (resolver) {
            const r = resolver;
            resolver = null;
            r({ value: parsed, done: false });
          } else {
            queue.push(parsed);
          }
        } catch {
          // Ignore malformed frames — platform never sends these but guard anyway.
        }
      });
      ws.addEventListener('error', () => {
        errored = new Error(`logs.tail: WebSocket error (service ${serviceId})`);
        if (resolver) {
          const r = resolver;
          resolver = null;
          r({ value: undefined as unknown as LogEntry, done: true });
        }
      });
      ws.addEventListener('close', () => {
        closed = true;
        if (resolver) {
          const r = resolver;
          resolver = null;
          r({ value: undefined as unknown as LogEntry, done: true });
        }
      });

      while (!closed || queue.length > 0) {
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
      if (errored) throw errored;
    } finally {
      try {
        ws.close();
      } catch {
        /* no-op */
      }
      options.signal?.removeEventListener?.('abort', onAbort);
    }
  }

  private buildStreamUrl(
    serviceId: string,
    options: LogTailOptions,
  ): string {
    // Replace http(s) with ws(s) so callers don't have to.
    const baseHttp = (this.client as unknown as { baseUrl: string }).baseUrl;
    const base = baseHttp.replace(/^http(s?):\/\//, 'ws$1://');
    const params = new URLSearchParams();
    if (options.level) params.append('level', String(options.level));
    if (options.pod) params.append('pod', options.pod);
    if (options.container) params.append('container', options.container);
    const qs = params.toString();
    return `${base}/services/${encodeURIComponent(serviceId)}/logs/stream${qs ? `?${qs}` : ''}`;
  }
}
