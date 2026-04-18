import { describe, expect, it, vi } from 'vitest';
import { EncliiClient } from '../client';
import {
  AuthenticationError,
  ConflictError,
  NotFoundError,
  RateLimitError,
  ServerError,
  ValidationError,
} from '../errors';
import {
  createStubFetch,
  errorResponse,
  jsonResponse,
  newClient,
} from './test-helpers';

describe('EncliiClient', () => {
  it('throws when baseUrl is missing', () => {
    expect(
      () =>
        new EncliiClient({
          baseUrl: '',
          token: 't',
          fetch: globalThis.fetch,
        }),
    ).toThrow(/baseUrl is required/);
  });

  it('strips a trailing slash from baseUrl', () => {
    const client = new EncliiClient({
      baseUrl: 'https://api.enclii.test/v1/',
      token: 't',
      fetch: globalThis.fetch,
    });
    expect(client.baseUrl).toBe('https://api.enclii.test/v1');
  });

  it('attaches the bearer token header to every request', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse({ ok: true }));
    const client = newClient({ fetch, token: 'secret-abc' });
    await client.get('/ping');
    expect(calls[0]!.headers['authorization']).toBe('Bearer secret-abc');
  });

  it('accepts a token provider function', async () => {
    const provider = vi
      .fn()
      .mockResolvedValueOnce('tok-1')
      .mockResolvedValueOnce('tok-2');
    const { fetch, calls } = createStubFetch(() => jsonResponse({ ok: true }));
    const client = newClient({ fetch, token: provider });
    await client.get('/a');
    await client.get('/b');
    expect(provider).toHaveBeenCalledTimes(2);
    expect(calls[0]!.headers['authorization']).toBe('Bearer tok-1');
    expect(calls[1]!.headers['authorization']).toBe('Bearer tok-2');
  });

  it('omits Authorization when the token is null', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse({ ok: true }));
    const client = newClient({ fetch, token: null });
    await client.get('/health');
    expect(calls[0]!.headers['authorization']).toBeUndefined();
  });

  it('serializes query params and drops undefined/null', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse({ ok: true }));
    const client = newClient({ fetch });
    await client.get('/services', {
      limit: 10,
      cursor: undefined,
      filter: null,
      active: true,
    });
    const u = new URL(calls[0]!.url);
    expect(u.searchParams.get('limit')).toBe('10');
    expect(u.searchParams.get('active')).toBe('true');
    expect(u.searchParams.has('cursor')).toBe(false);
    expect(u.searchParams.has('filter')).toBe(false);
  });

  it('sends JSON request bodies', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ ok: true }, { status: 201 }),
    );
    const client = newClient({ fetch });
    await client.post('/things', { name: 'alpha' });
    expect(calls[0]!.method).toBe('POST');
    expect(calls[0]!.body).toBe('{"name":"alpha"}');
    expect(calls[0]!.headers['content-type']).toBe('application/json');
  });

  it('returns undefined for 204 responses', async () => {
    const { fetch } = createStubFetch(
      () => new Response(null, { status: 204 }),
    );
    const client = newClient({ fetch });
    const result = await client.del('/svc/abc');
    expect(result).toBeUndefined();
  });

  it('maps 401 to AuthenticationError', async () => {
    const { fetch } = createStubFetch(() =>
      errorResponse(401, 'invalid token'),
    );
    const client = newClient({ fetch, retry: { maxAttempts: 1 } });
    await expect(client.get('/x')).rejects.toBeInstanceOf(AuthenticationError);
  });

  it('maps 404 to NotFoundError', async () => {
    const { fetch } = createStubFetch(() =>
      errorResponse(404, 'no such thing'),
    );
    const client = newClient({ fetch, retry: { maxAttempts: 1 } });
    await expect(client.get('/x')).rejects.toBeInstanceOf(NotFoundError);
  });

  it('maps 400/422 to ValidationError', async () => {
    const { fetch } = createStubFetch(() =>
      errorResponse(422, 'invalid input'),
    );
    const client = newClient({ fetch, retry: { maxAttempts: 1 } });
    await expect(client.get('/x')).rejects.toBeInstanceOf(ValidationError);
  });

  it('maps 409 to ConflictError', async () => {
    const { fetch } = createStubFetch(() =>
      errorResponse(409, 'already exists'),
    );
    const client = newClient({ fetch, retry: { maxAttempts: 1 } });
    await expect(client.get('/x')).rejects.toBeInstanceOf(ConflictError);
  });

  it('retries on 5xx and eventually succeeds', async () => {
    let count = 0;
    const { fetch } = createStubFetch(() => {
      count++;
      if (count < 3) return errorResponse(502, 'bad gateway');
      return jsonResponse({ ok: true });
    });
    const client = newClient({
      fetch,
      retry: { maxAttempts: 5, initialDelayMs: 1, maxDelayMs: 2 },
    });
    const result = await client.get<{ ok: boolean }>('/retryable');
    expect(result.ok).toBe(true);
    expect(count).toBe(3);
  });

  it('gives up after max attempts and surfaces ServerError', async () => {
    const { fetch } = createStubFetch(() => errorResponse(503, 'down'));
    const client = newClient({
      fetch,
      retry: { maxAttempts: 2, initialDelayMs: 1, maxDelayMs: 2 },
    });
    await expect(client.get('/down')).rejects.toBeInstanceOf(ServerError);
  });

  it('respects Retry-After header on 429', async () => {
    let attempts = 0;
    const { fetch } = createStubFetch(() => {
      attempts++;
      if (attempts < 2) {
        return errorResponse(429, 'slow down').clone
          ? new Response(JSON.stringify({ error: 'slow down' }), {
              status: 429,
              headers: {
                'content-type': 'application/json',
                'retry-after': '0',
              },
            })
          : errorResponse(429, 'slow down');
      }
      return jsonResponse({ ok: true });
    });
    const client = newClient({
      fetch,
      retry: { maxAttempts: 3, initialDelayMs: 1, maxDelayMs: 2 },
    });
    await expect(client.get('/rate')).resolves.toEqual({ ok: true });
  });

  it('surfaces RateLimitError when retries exhausted', async () => {
    const { fetch } = createStubFetch(() => errorResponse(429, 'slow'));
    const client = newClient({
      fetch,
      retry: { maxAttempts: 1 },
    });
    await expect(client.get('/rate')).rejects.toBeInstanceOf(RateLimitError);
  });

  it('does not retry on 4xx client errors (except 429)', async () => {
    let count = 0;
    const { fetch } = createStubFetch(() => {
      count++;
      return errorResponse(404, 'nope');
    });
    const client = newClient({
      fetch,
      retry: { maxAttempts: 5, initialDelayMs: 1, maxDelayMs: 2 },
    });
    await expect(client.get('/x')).rejects.toBeInstanceOf(NotFoundError);
    expect(count).toBe(1);
  });

  it('surfaces a request-id on errors when present', async () => {
    const { fetch } = createStubFetch(
      () =>
        new Response(JSON.stringify({ error: 'down' }), {
          status: 500,
          headers: {
            'content-type': 'application/json',
            'x-request-id': 'req-abc-123',
          },
        }),
    );
    const client = newClient({ fetch, retry: { maxAttempts: 1 } });
    const err = await client.get('/x').catch((e) => e as ServerError);
    expect(err).toBeInstanceOf(ServerError);
    expect(err.requestId).toBe('req-abc-123');
  });

  it('paginate iterates across multiple pages', async () => {
    let page = 0;
    const { fetch } = createStubFetch(() => {
      page++;
      if (page === 1) {
        return jsonResponse({
          data: [{ id: 1 }, { id: 2 }],
          next_cursor: 'cur-2',
        });
      }
      if (page === 2) {
        return jsonResponse({ data: [{ id: 3 }], next_cursor: null });
      }
      throw new Error('unexpected third page');
    });
    const client = newClient({ fetch });
    const out: number[] = [];
    for await (const item of client.paginate<{ id: number }>('/things')) {
      out.push(item.id);
    }
    expect(out).toEqual([1, 2, 3]);
  });
});
