import { describe, expect, it } from 'vitest';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('AuditResource', () => {
  it('list() passes filters through as query params', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        activities: [
          {
            id: 'a1',
            action: 'deploy.succeeded',
            resource_type: 'deployment',
            created_at: '2026-04-17T00:00:00Z',
          },
        ],
        next_cursor: null,
      }),
    );
    const client = newClient({ fetch });
    const page = await client.audit.list({
      action: 'deploy.succeeded',
      project_id: 'p-1',
      limit: 50,
    });
    expect(page.data).toHaveLength(1);
    const u = new URL(calls[0]!.url);
    expect(u.searchParams.get('action')).toBe('deploy.succeeded');
    expect(u.searchParams.get('project_id')).toBe('p-1');
    expect(u.searchParams.get('limit')).toBe('50');
  });

  it('actions() returns the list', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({ actions: ['deploy.succeeded', 'rollback.succeeded'] }),
    );
    const client = newClient({ fetch });
    const out = await client.audit.actions();
    expect(out).toContain('deploy.succeeded');
  });

  it('resourceTypes() returns the list', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({ resource_types: ['service', 'deployment'] }),
    );
    const client = newClient({ fetch });
    const out = await client.audit.resourceTypes();
    expect(out).toEqual(['service', 'deployment']);
  });
});
