import { EventEmitter } from 'node:events';
import { afterEach, describe, expect, it, vi } from 'vitest';

/**
 * Stand-in for the `ws` package: records the constructor arguments and lets
 * the test drive the socket's events.
 */
const sockets: FakeWs[] = [];
class FakeWs extends EventEmitter {
  closed = false;
  constructor(
    public readonly url: string,
    public readonly opts: { headers?: Record<string, string>; origin?: string },
  ) {
    super();
    sockets.push(this);
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    this.emit('close');
  }
  terminate() {
    this.emit('error', new Error('WebSocket was closed before the connection was established'));
    this.close();
  }
}
vi.mock('ws', () => ({ default: FakeWs }));

const { nodeLogsTail } = await import('../node');
const { createStubFetch, jsonResponse, newClient } = await import('./test-helpers');

const tick = () => new Promise((r) => setTimeout(r, 0));

afterEach(() => {
  sockets.length = 0;
});

function client() {
  const { fetch } = createStubFetch(() => jsonResponse({}));
  return newClient({ fetch });
}

describe('nodeLogsTail', () => {
  // Contract: StreamServiceLogsWS (logs_handlers.go) authenticates via the
  // Authorization header (jwt_middleware.go), checks an Origin only when one
  // is sent (websocketOriginAllowed in ws_upgrade.go), and reads
  // env/lines/timestamps/since.
  it('sends Authorization and Origin headers to /logs/stream and yields frames', async () => {
    const frames: unknown[] = [];
    const done = (async () => {
      for await (const f of nodeLogsTail(client(), 'svc-1', {
        env: 'production',
        lines: 20,
        origin: 'https://app.example.com',
        maxReconnects: 0,
      })) {
        frames.push(f);
      }
    })();
    await tick();
    const ws = sockets[0]!;
    const u = new URL(ws.url);
    expect(u.protocol).toBe('wss:');
    expect(u.pathname).toBe('/v1/services/svc-1/logs/stream');
    expect(Object.fromEntries(u.searchParams)).toEqual({
      env: 'production',
      lines: '20',
    });
    expect(ws.opts.headers).toEqual({ Authorization: 'Bearer test-token' });
    expect(ws.opts.origin).toBe('https://app.example.com');

    ws.emit('message', Buffer.from(JSON.stringify({
      type: 'connected',
      timestamp: '2026-09-24T12:00:00Z',
      message: 'Connected to logs for api in enclii-acme-production',
    })));
    ws.emit('message', Buffer.from(JSON.stringify({
      type: 'log',
      pod: 'api-7d9f',
      container: 'api',
      timestamp: '2026-09-24T12:00:01Z',
      message: 'GET /health 200',
    })));
    ws.close();
    await done;
    expect(frames).toHaveLength(2);
    expect(frames[1]).toMatchObject({ type: 'log', pod: 'api-7d9f', message: 'GET /health 200' });
  });

  it('sends no Origin when options.origin is unset (Bearer-only upgrade)', async () => {
    const next = nodeLogsTail(client(), 'svc-1', { maxReconnects: 0 })
      [Symbol.asyncIterator]()
      .next();
    await tick();
    const ws = sockets[0]!;
    expect(ws.opts.headers).toEqual({ Authorization: 'Bearer test-token' });
    expect('origin' in ws.opts).toBe(false);
    ws.emit('message', Buffer.from(JSON.stringify({
      type: 'connected',
      timestamp: '2026-09-24T12:00:00Z',
      message: 'Connected',
    })));
    expect((await next).value).toMatchObject({ type: 'connected' });
    ws.close();
  });

  it('throws without retrying when the upgrade is rejected with 403', async () => {
    const onReconnect = vi.fn();
    const next = nodeLogsTail(client(), 'svc-1', { maxReconnects: 3, onReconnect })
      [Symbol.asyncIterator]()
      .next();
    await tick();
    const ws = sockets[0]!;
    expect(ws.opts.origin).toBeUndefined();
    ws.emit('unexpected-response', {}, { statusCode: 403 });
    await expect(next).rejects.toThrow(/HTTP 403.*set options\.origin/);
    expect(onReconnect).not.toHaveBeenCalled();
    expect(sockets).toHaveLength(1);
  });

  it('reconnects after a dropped connection, then completes', async () => {
    const onReconnect = vi.fn();
    const it = nodeLogsTail(client(), 'svc-1', {
      origin: 'https://app.example.com',
      maxReconnects: 1,
      initialReconnectMs: 1,
      onReconnect,
    })[Symbol.asyncIterator]();
    const first = it.next();
    await tick();
    sockets[0]!.emit('error', new Error('socket hang up'));
    sockets[0]!.close();
    await new Promise((r) => setTimeout(r, 20));
    expect(onReconnect).toHaveBeenCalledWith(1, 'error: socket hang up');
    expect(sockets).toHaveLength(2);
    sockets[1]!.close();
    expect((await first).done).toBe(true);
  });

  it('sends since on the upgrade and again on every reconnect', async () => {
    const it = nodeLogsTail(client(), 'svc-1', {
      since: '15m',
      maxReconnects: 1,
      initialReconnectMs: 1,
    })[Symbol.asyncIterator]();
    const first = it.next();
    await tick();
    expect(new URL(sockets[0]!.url).searchParams.get('since')).toBe('15m');
    sockets[0]!.emit('error', new Error('socket hang up'));
    sockets[0]!.close();
    await new Promise((r) => setTimeout(r, 20));
    expect(sockets).toHaveLength(2);
    expect(new URL(sockets[1]!.url).searchParams.get('since')).toBe('15m');
    sockets[1]!.close();
    expect((await first).done).toBe(true);
  });

  it('throws on a malformed since without opening a socket', async () => {
    await expect(
      nodeLogsTail(client(), 'svc-1', { since: 'yesterday', maxReconnects: 0 })
        [Symbol.asyncIterator]()
        .next(),
    ).rejects.toThrow(/since must be an RFC3339 timestamp .* or a positive duration/);
    expect(sockets).toHaveLength(0);
  });

  it('prefers options.token over the client token', async () => {
    const next = nodeLogsTail(client(), 'svc-1', { token: 'enclii_override', maxReconnects: 0 })
      [Symbol.asyncIterator]()
      .next();
    await tick();
    expect(sockets[0]!.opts.headers).toEqual({ Authorization: 'Bearer enclii_override' });
    expect(new URL(sockets[0]!.url).searchParams.has('token')).toBe(false);
    sockets[0]!.close();
    expect((await next).done).toBe(true);
  });
});
