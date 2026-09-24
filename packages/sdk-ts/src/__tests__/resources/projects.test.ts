import { describe, expect, it } from 'vitest';
import {
  createStubFetch,
  errorResponse,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('ProjectsResource', () => {
  // Contract: ListProjects answers {"projects": [...]} with every row.
  it('lists every project in one request without paging params', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        projects: [
          { id: 'p1', name: 'alpha', slug: 'alpha' },
          { id: 'p2', name: 'beta', slug: 'beta' },
        ],
      }),
    );
    const client = newClient({ fetch });
    const page = await client.projects.list({ limit: 2 });
    expect(page.data).toHaveLength(2);
    expect(page.nextCursor).toBeNull();
    expect(calls[0]!.url).toBe('https://api.enclii.test/v1/projects');
  });

  it('gets a project by slug and url-encodes special characters', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ id: 'p1', slug: 'my/proj', name: 'x' }),
    );
    const client = newClient({ fetch });
    await client.projects.get('my/proj');
    expect(calls[0]!.url).toContain('/projects/my%2Fproj');
  });

  it('creates a project', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse(
        { id: 'p1', slug: 'x', name: 'X', ci_runner_mode: 'github' },
        { status: 201 },
      ),
    );
    const client = newClient({ fetch });
    const out = await client.projects.create({ name: 'X', slug: 'x' });
    expect(out.id).toBe('p1');
    expect(calls[0]!.method).toBe('POST');
    expect(JSON.parse(calls[0]!.body!)).toEqual({ name: 'X', slug: 'x' });
  });

  it('deletes a project', async () => {
    const { fetch, calls } = createStubFetch(
      () => new Response(null, { status: 204 }),
    );
    const client = newClient({ fetch });
    await client.projects.delete('alpha');
    expect(calls[0]!.method).toBe('DELETE');
    expect(calls[0]!.url).toContain('/projects/alpha');
  });

  it('surfaces a typed NotFoundError for missing projects', async () => {
    const { fetch } = createStubFetch(() =>
      errorResponse(404, 'project not found'),
    );
    const client = newClient({ fetch, retry: { maxAttempts: 1 } });
    await expect(client.projects.get('ghost')).rejects.toThrow(
      /project not found/,
    );
  });
});
