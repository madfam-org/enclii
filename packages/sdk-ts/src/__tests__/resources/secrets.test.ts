import { describe, expect, it } from 'vitest';
import { goEnvVar } from '../fixtures';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('SecretsResource', () => {
  // Contract: ListEnvVars (envvar_handlers.go) writes
  // {"environment_variables": [...]} and reads only `environment_id`.
  it('list() reads environment_variables and sends only environment_id', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        environment_variables: [
          goEnvVar('e1', 'DATABASE_URL', true),
          goEnvVar('e2', 'LOG_LEVEL', false),
        ],
      }),
    );
    const client = newClient({ fetch });
    const page = await client.secrets.list('svc-1', {
      environment_id: 'env-1',
      limit: 5,
    });
    expect(page.data.map((v) => v.key)).toEqual(['DATABASE_URL', 'LOG_LEVEL']);
    expect(page.data[0]!.value).toBe('••••••••');
    expect(page.nextCursor).toBeNull();
    expect(calls[0]!.method).toBe('GET');
    const u = new URL(calls[0]!.url);
    expect(u.pathname).toBe('/v1/services/svc-1/env-vars');
    expect([...u.searchParams.keys()]).toEqual(['environment_id']);
    expect(u.searchParams.get('environment_id')).toBe('env-1');
  });

  it('list() returns an empty page for a null array', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({ environment_variables: null }),
    );
    const client = newClient({ fetch });
    expect((await client.secrets.list('svc-1')).data).toEqual([]);
  });

  it('set() posts the variable, including environment_id', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse(goEnvVar('e1', 'FOO', false), { status: 201 }),
    );
    const client = newClient({ fetch });
    const out = await client.secrets.set('svc-1', {
      key: 'FOO',
      value: 'bar',
      is_secret: false,
      environment_id: 'env-1',
    });
    expect(out.id).toBe('e1');
    expect(calls[0]!.method).toBe('POST');
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      key: 'FOO',
      value: 'bar',
      is_secret: false,
      environment_id: 'env-1',
    });
  });

  // Contract: BulkUpsertEnvVars binds {"variables": [...] (min=1),
  // "environment_id"?} and answers {"message", "count"}.
  it('bulkSet() sends { variables, environment_id } and returns { message, count }', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ message: 'Environment variables created/updated', count: 2 }),
    );
    const client = newClient({ fetch });
    const out = await client.secrets.bulkSet(
      'svc-1',
      [
        { key: 'A', value: '1' },
        { key: 'B', value: '2', is_secret: true },
      ],
      { environment_id: 'env-1' },
    );
    expect(out).toEqual({
      message: 'Environment variables created/updated',
      count: 2,
    });
    expect(calls[0]!.method).toBe('POST');
    expect(new URL(calls[0]!.url).pathname).toBe('/v1/services/svc-1/env-vars/bulk');
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      variables: [
        { key: 'A', value: '1' },
        { key: 'B', value: '2', is_secret: true },
      ],
      environment_id: 'env-1',
    });
  });

  it('bulkSet() omits environment_id when unscoped', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ message: 'ok', count: 1 }),
    );
    const client = newClient({ fetch });
    await client.secrets.bulkSet('svc-1', [{ key: 'A', value: '1' }]);
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      variables: [{ key: 'A', value: '1' }],
    });
  });

  it('bulkSet() rejects an empty or oversized batch before sending', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    await expect(client.secrets.bulkSet('svc-1', [])).rejects.toThrow(
      /1 to 100/,
    );
    const many = Array.from({ length: 101 }, (_, i) => ({
      key: `K${i}`,
      value: 'v',
    }));
    await expect(client.secrets.bulkSet('svc-1', many)).rejects.toThrow(
      /got 101/,
    );
    expect(calls).toHaveLength(0);
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
