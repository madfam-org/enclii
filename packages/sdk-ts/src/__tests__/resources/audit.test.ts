import { describe, expect, it } from 'vitest';
import { goAuditLog } from '../fixtures';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

/** ActivityListResponse (activity_handlers.go). */
function activityPage(ids: string[], limit: number, offset: number) {
  return {
    activities: ids.map(goAuditLog),
    count: ids.length,
    limit,
    offset,
  };
}

describe('AuditResource', () => {
  it('list() passes filters through and maps limit/offset to a cursor', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse(activityPage(['a1', 'a2'], 2, 0)),
    );
    const client = newClient({ fetch });
    const page = await client.audit.list({
      action: 'deploy',
      project_id: 'p-1',
      limit: 2,
    });
    expect(page.data.map((e) => e.id)).toEqual(['a1', 'a2']);
    expect(page.data[0]!.timestamp).toBe('2026-09-20T09:00:00Z');
    expect(page.data[0]!.outcome).toBe('success');
    expect(page.nextCursor).toBe('2');
    const u = new URL(calls[0]!.url);
    expect(u.pathname).toBe('/v1/activity');
    expect(u.searchParams.get('action')).toBe('deploy');
    expect(u.searchParams.get('project_id')).toBe('p-1');
    expect(u.searchParams.get('limit')).toBe('2');
    expect(u.searchParams.has('offset')).toBe(false);
    expect(u.searchParams.has('cursor')).toBe(false);
  });

  it('list() sends a cursor as offset and ends on a short page', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse(activityPage(['a3'], 50, 100)),
    );
    const client = newClient({ fetch });
    const page = await client.audit.list({ cursor: '100' });
    expect(new URL(calls[0]!.url).searchParams.get('offset')).toBe('100');
    expect(page.nextCursor).toBeNull();
  });

  // The gap: iter() used to stop after the first page because /activity
  // never returns next_cursor. It now walks offsets until a short page.
  it('iter() walks every page by offset', async () => {
    const pages: Record<string, ReturnType<typeof activityPage>> = {
      '0': activityPage(['a1', 'a2'], 2, 0),
      '2': activityPage(['a3', 'a4'], 2, 2),
      '4': activityPage(['a5'], 2, 4),
    };
    const { fetch, calls } = createStubFetch((call) => {
      const offset = new URL(call.url).searchParams.get('offset') ?? '0';
      return jsonResponse(pages[offset]);
    });
    const client = newClient({ fetch });
    const ids: string[] = [];
    for await (const e of client.audit.iter({ resource_type: 'service', limit: 2 })) {
      ids.push(e.id);
    }
    expect(ids).toEqual(['a1', 'a2', 'a3', 'a4', 'a5']);
    expect(calls).toHaveLength(3);
    for (const call of calls) {
      const u = new URL(call.url);
      expect(u.searchParams.get('limit')).toBe('2');
      expect(u.searchParams.get('resource_type')).toBe('service');
    }
  });

  it('iter() pages by the limit the server applied, not the one requested', async () => {
    // No limit sent: the server uses 50 and echoes it.
    const full = Array.from({ length: 50 }, (_, i) => `a${i}`);
    const { fetch, calls } = createStubFetch((call) => {
      const offset = new URL(call.url).searchParams.get('offset');
      return jsonResponse(
        offset === '50' ? activityPage([], 50, 50) : activityPage(full, 50, 0),
      );
    });
    const client = newClient({ fetch });
    let n = 0;
    for await (const _ of client.audit.iter()) n++;
    expect(n).toBe(50);
    expect(calls).toHaveLength(2);
  });

  it('rejects a limit the API would silently replace', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    await expect(client.audit.list({ limit: 500 })).rejects.toThrow(/1 to 100/);
    await expect(client.audit.list({ cursor: 'abc' })).rejects.toThrow(/invalid cursor/);
    expect(calls).toHaveLength(0);
  });

  it('actions() returns the list', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({ actions: ['deploy', 'rollback'] }),
    );
    const client = newClient({ fetch });
    expect(await client.audit.actions()).toEqual(['deploy', 'rollback']);
  });

  it('resourceTypes() returns the list', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({ resource_types: ['service', 'deployment'] }),
    );
    const client = newClient({ fetch });
    expect(await client.audit.resourceTypes()).toEqual(['service', 'deployment']);
  });
});
