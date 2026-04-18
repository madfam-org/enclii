import { describe, expect, it } from 'vitest';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('ServicesResource', () => {
  it('lists services for a project', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        services: [{ id: 's1', name: 'api' }],
        next_cursor: null,
      }),
    );
    const client = newClient({ fetch });
    const page = await client.services.list('proj-1');
    expect(page.data).toHaveLength(1);
    expect(calls[0]!.url).toContain('/projects/proj-1/services');
  });

  it('creates a service', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ id: 's1', name: 'api' }, { status: 201 }),
    );
    const client = newClient({ fetch });
    await client.services.create('proj-1', {
      name: 'api',
      git_repo: 'git@example.com:acme/api.git',
    });
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      name: 'api',
      git_repo: 'git@example.com:acme/api.git',
    });
  });

  it('restarts a service', async () => {
    const { fetch, calls } = createStubFetch(
      () => new Response(null, { status: 202 }),
    );
    const client = newClient({ fetch });
    await client.services.restart('s1', { environment: 'prod' });
    expect(calls[0]!.url).toContain('/services/s1/restart');
    expect(JSON.parse(calls[0]!.body!)).toEqual({ environment: 'prod' });
  });

  it('scales a service', async () => {
    const { fetch, calls } = createStubFetch(
      () => new Response(null, { status: 202 }),
    );
    const client = newClient({ fetch });
    await client.services.scale('s1', 5);
    expect(JSON.parse(calls[0]!.body!)).toEqual({ replicas: 5 });
  });
});
