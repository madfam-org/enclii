import { describe, expect, it } from 'vitest';
import { goDeployment } from '../fixtures';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

/**
 * The endpoints behind these iter() methods return every row in one response
 * and never send next_cursor (ListProjects, ListServices,
 * ListServiceDeployments, ListOutboundWebhooks). iter() therefore makes one
 * request, sends no paging parameters, and yields every row of it.
 */
describe('iter() over unpaged endpoints', () => {
  const cases = [
    {
      name: 'projects.iter',
      path: '/v1/projects',
      body: { projects: [{ id: 'p1' }, { id: 'p2' }, { id: 'p3' }] },
      run: (c: ReturnType<typeof newClient>) => c.projects.iter({ pageSize: 1 }),
    },
    {
      name: 'services.iter',
      path: '/v1/projects/proj-1/services',
      body: { services: [{ id: 'p1' }, { id: 'p2' }, { id: 'p3' }] },
      run: (c: ReturnType<typeof newClient>) =>
        c.services.iter('proj-1', { pageSize: 1 }),
    },
    {
      name: 'deployments.iter',
      path: '/v1/services/svc-1/deployments',
      body: {
        service_id: 'svc-1',
        deployments: [goDeployment('p1'), goDeployment('p2'), goDeployment('p3')],
        count: 3,
      },
      run: (c: ReturnType<typeof newClient>) =>
        c.deployments.iter('svc-1', { pageSize: 1 }),
    },
    {
      name: 'webhooks.iter',
      path: '/v1/projects/proj-1/lifecycle-webhooks',
      body: { subscriptions: [{ id: 'p1' }, { id: 'p2' }, { id: 'p3' }] },
      run: (c: ReturnType<typeof newClient>) =>
        c.webhooks.iter('proj-1', { pageSize: 1 }),
    },
  ];

  for (const tc of cases) {
    it(`${tc.name} yields every row from one request`, async () => {
      const { fetch, calls } = createStubFetch(() => jsonResponse(tc.body));
      const client = newClient({ fetch });
      const ids: string[] = [];
      for await (const row of tc.run(client) as AsyncIterable<{ id: string }>) {
        ids.push(row.id);
      }
      expect(ids).toEqual(['p1', 'p2', 'p3']);
      expect(calls).toHaveLength(1);
      expect(calls[0]!.method).toBe('GET');
      const u = new URL(calls[0]!.url);
      expect(u.pathname).toBe(tc.path);
      expect(u.search).toBe('');
    });
  }

  it('returns nothing for a null array', async () => {
    const { fetch } = createStubFetch(() => jsonResponse({ projects: null }));
    const client = newClient({ fetch });
    const rows: unknown[] = [];
    for await (const p of client.projects.iter()) rows.push(p);
    expect(rows).toEqual([]);
  });
});
