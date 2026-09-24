import { afterEach, describe, expect, it } from 'vitest';
import { assertLogsSince } from '../../resources/log-stream';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

/** gin.H written by GetLogsHistory (logs_handlers.go). */
const historyBody = {
  service_id: 'svc-1',
  service_name: 'api',
  environment: 'production',
  namespace: 'enclii-acme-production',
  logs: 'line one\nline two\n',
  lines: 500,
};

describe('LogsResource.history', () => {
  it('sends env/lines/since and returns the raw-text response', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse(historyBody));
    const client = newClient({ fetch });
    const out = await client.logs.history('svc-1', {
      env: 'production',
      lines: 500,
      since: '2026-09-24T00:00:00Z',
    });
    expect(out).toEqual(historyBody);
    expect(out.logs.split('\n')[0]).toBe('line one');
    expect(calls[0]!.method).toBe('GET');
    const u = new URL(calls[0]!.url);
    expect(u.pathname).toBe('/v1/services/svc-1/logs/history');
    expect(Object.fromEntries(u.searchParams)).toEqual({
      env: 'production',
      lines: '500',
      since: '2026-09-24T00:00:00Z',
    });
  });

  it('sends no query when called without options', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse(historyBody));
    const client = newClient({ fetch });
    await client.logs.history('svc-1');
    expect(new URL(calls[0]!.url).search).toBe('');
  });

  it('rejects a since value the API would answer with 400', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse(historyBody));
    const client = newClient({ fetch });
    await expect(client.logs.history('svc-1', { since: 'last tuesday' })).rejects.toThrow(
      /logs\.history: since must be/,
    );
    expect(calls).toHaveLength(0);
  });

  it('rejects a lines value the API would silently replace', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse(historyBody));
    const client = newClient({ fetch });
    await expect(client.logs.history('svc-1', { lines: 0 })).rejects.toThrow(/1 to 10000/);
    await expect(client.logs.history('svc-1', { lines: 10_001 })).rejects.toThrow(/1 to 10000/);
    expect(calls).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// logs.tail — against a fake browser WebSocket
// ---------------------------------------------------------------------------

type Listener = (ev: { data?: unknown }) => void;

class FakeBrowserWebSocket {
  static instances: FakeBrowserWebSocket[] = [];
  readonly listeners: Record<string, Listener[]> = {};
  closed = false;
  constructor(public readonly url: string, ...rest: unknown[]) {
    // A browser WebSocket takes (url, protocols?) and has no header option.
    expect(rest).toEqual([]);
    FakeBrowserWebSocket.instances.push(this);
  }
  addEventListener(type: string, fn: Listener) {
    (this.listeners[type] ??= []).push(fn);
  }
  emit(type: string, ev: { data?: unknown } = {}) {
    for (const fn of this.listeners[type] ?? []) fn(ev);
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    this.emit('close');
  }
}

const g = globalThis as { WebSocket?: unknown };
const originalWebSocket = g.WebSocket;
afterEach(() => {
  g.WebSocket = originalWebSocket;
  FakeBrowserWebSocket.instances = [];
});

const tick = () => new Promise((r) => setTimeout(r, 0));

describe('LogsResource.tail', () => {
  it('opens /logs/stream with env/lines/timestamps and ?token=, yielding every frame', async () => {
    g.WebSocket = FakeBrowserWebSocket;
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    const frames: unknown[] = [];
    const done = (async () => {
      for await (const f of client.logs.tail('svc-1', {
        env: 'production',
        lines: 50,
        timestamps: true,
      })) {
        frames.push(f);
      }
    })();
    await tick();
    const ws = FakeBrowserWebSocket.instances[0]!;
    const u = new URL(ws.url);
    expect(u.protocol).toBe('wss:');
    expect(u.host).toBe('api.enclii.test');
    expect(u.pathname).toBe('/v1/services/svc-1/logs/stream');
    expect(Object.fromEntries(u.searchParams)).toEqual({
      env: 'production',
      lines: '50',
      timestamps: 'true',
      token: 'test-token',
    });

    // Frames shaped like LogStreamMessage in logs_handlers.go.
    ws.emit('open');
    ws.emit('message', {
      data: JSON.stringify({
        type: 'connected',
        timestamp: '2026-09-24T12:00:00Z',
        message: 'Connected to logs for api in enclii-acme-production',
      }),
    });
    ws.emit('message', {
      data: JSON.stringify({
        type: 'log',
        pod: 'api-7d9f',
        container: 'api',
        timestamp: '2026-09-24T12:00:01Z',
        message: 'GET /health 200',
      }),
    });
    ws.emit('message', { data: 'not json' });
    ws.close();
    await done;
    expect(frames).toEqual([
      {
        type: 'connected',
        timestamp: '2026-09-24T12:00:00Z',
        message: 'Connected to logs for api in enclii-acme-production',
      },
      {
        type: 'log',
        pod: 'api-7d9f',
        container: 'api',
        timestamp: '2026-09-24T12:00:01Z',
        message: 'GET /health 200',
      },
    ]);
  });

  it('sends since, as parseLogsSince reads it, next to the other parameters', async () => {
    g.WebSocket = FakeBrowserWebSocket;
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    const it = client.logs
      .tail('svc-1', { env: 'production', since: '2026-09-24T10:00:00Z' })
      [Symbol.asyncIterator]();
    const next = it.next();
    await tick();
    const ws = FakeBrowserWebSocket.instances[0]!;
    expect(Object.fromEntries(new URL(ws.url).searchParams)).toEqual({
      env: 'production',
      since: '2026-09-24T10:00:00Z',
      token: 'test-token',
    });
    ws.close();
    expect((await next).done).toBe(true);
  });

  it('throws on a malformed since before opening a socket', async () => {
    g.WebSocket = FakeBrowserWebSocket;
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    await expect(
      client.logs.tail('svc-1', { since: '-5m' })[Symbol.asyncIterator]().next(),
    ).rejects.toThrow(/logs stream: since must be/);
    expect(FakeBrowserWebSocket.instances).toHaveLength(0);
  });

  it('omits the token parameter for anonymous clients', async () => {
    g.WebSocket = FakeBrowserWebSocket;
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch, token: null });
    const it = client.logs.tail('svc-1')[Symbol.asyncIterator]();
    const next = it.next();
    await tick();
    const ws = FakeBrowserWebSocket.instances[0]!;
    expect(new URL(ws.url).search).toBe('');
    ws.close();
    expect((await next).done).toBe(true);
  });

  it('throws a handshake error when the upgrade is refused', async () => {
    g.WebSocket = FakeBrowserWebSocket;
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    const next = client.logs.tail('svc-1')[Symbol.asyncIterator]().next();
    await tick();
    const ws = FakeBrowserWebSocket.instances[0]!;
    ws.emit('error');
    ws.close();
    await expect(next).rejects.toThrow(/handshake failed.*Origin/);
  });

  it('stops on abort', async () => {
    g.WebSocket = FakeBrowserWebSocket;
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    const abort = new AbortController();
    const next = client.logs
      .tail('svc-1', { signal: abort.signal })
      [Symbol.asyncIterator]()
      .next();
    await tick();
    abort.abort();
    expect((await next).done).toBe(true);
    expect(FakeBrowserWebSocket.instances[0]!.closed).toBe(true);
  });

  it('throws when no WebSocket implementation is available', async () => {
    g.WebSocket = undefined;
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    await expect(
      client.logs.tail('svc-1')[Symbol.asyncIterator]().next(),
    ).rejects.toThrow(/nodeLogsTail/);
  });
});

// ---------------------------------------------------------------------------
// since validation — the forms parseLogsSince (logs_since.go) accepts
// ---------------------------------------------------------------------------

describe('assertLogsSince', () => {
  it.each([
    '2026-09-24T10:00:00Z',
    '2026-09-24T10:00:00.123Z',
    '2026-09-24T10:00:00-06:00',
    '90s',
    '15m',
    '24h',
    '1h30m',
    '1.5h',
    '500ms',
    '+5m',
  ])('accepts %s', (since) => {
    expect(() => assertLogsSince(since, 't')).not.toThrow();
  });

  it.each([
    '',
    ' 5m',
    '5',
    '0s',
    '0h0m',
    '-5m',
    '5 minutes',
    'yesterday',
    '2026-09-24',
    '2026-09-24 10:00:00Z',
    '2026-09-24T10:00:00',
    '2026-13-24T10:00:00Z',
    '2026-09-24T25:00:00Z',
  ])('rejects %j', (since) => {
    expect(() => assertLogsSince(since, 't')).toThrow(/t: since must be/);
  });
});
