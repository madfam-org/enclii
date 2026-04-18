import { describe, expect, it } from 'vitest';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('SecretsResource', () => {
  it('lists env vars for a service', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        env_vars: [{ id: 'e1', key: 'DATABASE_URL', is_secret: true }],
        next_cursor: null,
      }),
    );
    const client = newClient({ fetch });
    const page = await client.secrets.list('svc-1');
    expect(page.data[0]!.key).toBe('DATABASE_URL');
    expect(calls[0]!.url).toContain('/services/svc-1/env-vars');
  });

  it('sets a single env var', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ id: 'e1', key: 'FOO', is_secret: false }, { status: 201 }),
    );
    const client = newClient({ fetch });
    await client.secrets.set('svc-1', {
      key: 'FOO',
      value: 'bar',
      is_secret: false,
    });
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      key: 'FOO',
      value: 'bar',
      is_secret: false,
    });
  });

  it('bulk sets env vars', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        env_vars: [
          { id: 'e1', key: 'A', is_secret: false },
          { id: 'e2', key: 'B', is_secret: true },
        ],
      }),
    );
    const client = newClient({ fetch });
    const out = await client.secrets.bulkSet('svc-1', [
      { key: 'A', value: '1' },
      { key: 'B', value: '2', is_secret: true },
    ]);
    expect(out).toHaveLength(2);
    expect(calls[0]!.url).toContain('/services/svc-1/env-vars/bulk');
  });

  it('reveal() hits the reveal endpoint', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ key: 'DATABASE_URL', value: 'postgres://...' }),
    );
    const client = newClient({ fetch });
    const out = await client.secrets.reveal('svc-1', 'e1');
    expect(out.value).toContain('postgres://');
    expect(calls[0]!.url).toContain('/services/svc-1/env-vars/e1/reveal');
  });
});
