import { describe, expect, it } from 'vitest';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('LogsResource', () => {
  it('history() returns a page with paginated cursor', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        logs: [
          {
            timestamp: '2026-04-17T00:00:00Z',
            pod: 'api-0',
            message: 'hello',
            level: 'info',
          },
        ],
        next_cursor: 'cur-1',
      }),
    );
    const client = newClient({ fetch });
    const page = await client.logs.history('svc-1', {
      level: 'info',
      limit: 100,
    });
    expect(page.data).toHaveLength(1);
    expect(page.nextCursor).toBe('cur-1');
    expect(calls[0]!.url).toContain('/services/svc-1/logs/history');
    const u = new URL(calls[0]!.url);
    expect(u.searchParams.get('level')).toBe('info');
    expect(u.searchParams.get('limit')).toBe('100');
  });

  it('iter() walks multiple pages', async () => {
    let page = 0;
    const { fetch } = createStubFetch(() => {
      page++;
      if (page === 1) {
        return jsonResponse({
          logs: [
            {
              timestamp: '2026-04-17T00:00:00Z',
              pod: 'api-0',
              message: 'first',
            },
          ],
          next_cursor: 'c2',
        });
      }
      return jsonResponse({
        logs: [
          {
            timestamp: '2026-04-17T00:00:01Z',
            pod: 'api-1',
            message: 'second',
          },
        ],
        next_cursor: null,
      });
    });
    const client = newClient({ fetch });
    const collected: string[] = [];
    for await (const entry of client.logs.iter('svc-1')) {
      collected.push(entry.message);
    }
    expect(collected).toEqual(['first', 'second']);
  });

  it('tail() throws when no WebSocket implementation is available', async () => {
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    // Save and remove WebSocket to simulate an older runtime.
    const original = (globalThis as { WebSocket?: unknown }).WebSocket;
    (globalThis as { WebSocket?: unknown }).WebSocket = undefined;
    try {
      const iter = client.logs.tail('svc-1');
      // Error surfaces on the first .next() call
      await expect(iter[Symbol.asyncIterator]().next()).rejects.toThrow(
        /WebSocket/,
      );
    } finally {
      (globalThis as { WebSocket?: unknown }).WebSocket = original;
    }
  });
});
