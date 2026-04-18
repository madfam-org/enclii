/**
 * Tiny fetch mock — we deliberately don't pull in msw to keep the test suite
 * fast and dep-free. Every test registers a handler that returns a Response.
 */

import type { EncliiClientOptions } from '../client';
import { EncliiClient } from '../client';

export interface RecordedCall {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: string | null;
}

export interface StubHandler {
  (call: RecordedCall): Promise<Response> | Response;
}

export function createStubFetch(handler: StubHandler): {
  fetch: typeof fetch;
  calls: RecordedCall[];
} {
  const calls: RecordedCall[] = [];
  const fetchImpl: typeof fetch = async (input, init) => {
    const url = typeof input === 'string' ? input : (input as URL | Request).toString();
    const method = init?.method ?? 'GET';
    const headerRecord: Record<string, string> = {};
    const rawHeaders = init?.headers;
    if (rawHeaders) {
      if (rawHeaders instanceof Headers) {
        rawHeaders.forEach((v, k) => {
          headerRecord[k.toLowerCase()] = v;
        });
      } else if (Array.isArray(rawHeaders)) {
        for (const [k, v] of rawHeaders) {
          headerRecord[k.toLowerCase()] = v;
        }
      } else {
        for (const [k, v] of Object.entries(rawHeaders)) {
          headerRecord[k.toLowerCase()] = String(v);
        }
      }
    }
    const bodyStr =
      init?.body == null
        ? null
        : typeof init.body === 'string'
          ? init.body
          : '';
    const call = { url, method, headers: headerRecord, body: bodyStr };
    calls.push(call);
    return handler(call);
  };
  return { fetch: fetchImpl as typeof fetch, calls };
}

export function jsonResponse(
  body: unknown,
  init: ResponseInit = {},
): Response {
  return new Response(JSON.stringify(body), {
    status: init.status ?? 200,
    headers: {
      'content-type': 'application/json',
      ...(init.headers ?? {}),
    },
  });
}

export function textResponse(
  body: string,
  init: ResponseInit = {},
): Response {
  return new Response(body, {
    status: init.status ?? 200,
    headers: {
      'content-type': 'text/plain',
      ...(init.headers ?? {}),
    },
  });
}

export function errorResponse(
  status: number,
  message = 'error',
  extra: Record<string, unknown> = {},
): Response {
  return jsonResponse({ error: message, ...extra }, { status });
}

export function newClient(
  overrides: Partial<EncliiClientOptions> & { fetch: typeof fetch },
): EncliiClient {
  return new EncliiClient({
    baseUrl: 'https://api.enclii.test/v1',
    token: 'test-token',
    // Keep retries fast so tests don't sit on real timers.
    retry: { maxAttempts: 2, initialDelayMs: 1, maxDelayMs: 5 },
    timeoutMs: 5_000,
    ...overrides,
  });
}
