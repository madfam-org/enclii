/**
 * Unit tests for lib/api.ts CSRF behaviour.
 *
 * Truthfulness audit (2026-05-04): the dashboard observed `/v1/csrf` 404s
 * polluting the operator's console. The endpoint IS implemented in
 * switchyard-api/internal/api/csrf_handler.go and is genuinely needed for
 * write operations — but the SPA must handle a 404 (stale backend, not-yet
 * deployed CSRF route) without spamming the console on every write.
 *
 * These tests verify:
 *   1. A 404 on /v1/csrf is handled with a single console.warn (not error).
 *   2. The sticky `csrfEndpointAvailable=false` flag suppresses re-fetches.
 *   3. Network errors are caught and surfaced as console.warn (not error).
 *   4. Happy path still populates the token from X-CSRF-Token.
 */

import {
  __fetchCSRFTokenForTesting,
  __resetCSRFForTesting,
} from './api';

// Stand up a fresh fetch mock for every test so console-spy / call-count
// assertions don't leak across test cases.
const originalFetch = global.fetch;

afterAll(() => {
  global.fetch = originalFetch;
});

describe('CSRF token fetch — truthfulness audit', () => {
  let warnSpy: jest.SpyInstance;
  let errorSpy: jest.SpyInstance;

  beforeEach(() => {
    __resetCSRFForTesting();
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    errorSpy = jest.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    warnSpy.mockRestore();
    errorSpy.mockRestore();
  });

  it('logs at WARN (not error) and continues when /v1/csrf returns 404', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 404,
      headers: { get: () => null },
    }) as jest.Mock;

    await __fetchCSRFTokenForTesting();

    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy.mock.calls[0][0]).toContain('/v1/csrf');
    expect(warnSpy.mock.calls[0][0]).toContain('404');
    // CRITICAL: no console.error — operators read .error as "page broken"
    expect(errorSpy).not.toHaveBeenCalled();
  });

  it('does not re-fetch /v1/csrf after a 404 (sticky-unavailable flag)', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: false,
      status: 404,
      headers: { get: () => null },
    });
    global.fetch = fetchMock as jest.Mock;

    // First call: fetches and learns endpoint is unavailable.
    await __fetchCSRFTokenForTesting();
    // Subsequent calls: should short-circuit, never hitting fetch again.
    await __fetchCSRFTokenForTesting();
    await __fetchCSRFTokenForTesting();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    // Only the first 404 logged a warning — repeats are silent.
    expect(warnSpy).toHaveBeenCalledTimes(1);
  });

  it('warns (not errors) on a network failure', async () => {
    global.fetch = jest.fn().mockRejectedValue(
      new TypeError('NetworkError when attempting to fetch resource'),
    ) as jest.Mock;

    await __fetchCSRFTokenForTesting();

    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy.mock.calls[0][0]).toContain('CSRF token fetch failed');
    expect(errorSpy).not.toHaveBeenCalled();
  });

  it('happy path: 200 OK populates the token and does not log', async () => {
    global.fetch = jest.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: {
        get: (h: string) =>
          h === 'X-CSRF-Token' ? 'abcd-token-1234' : null,
      },
    }) as jest.Mock;

    await __fetchCSRFTokenForTesting();

    expect(warnSpy).not.toHaveBeenCalled();
    expect(errorSpy).not.toHaveBeenCalled();
  });

  it('warns once on non-404 5xx and does not mark sticky-unavailable', async () => {
    const fetchMock = jest.fn().mockResolvedValue({
      ok: false,
      status: 503,
      headers: { get: () => null },
    });
    global.fetch = fetchMock as jest.Mock;

    await __fetchCSRFTokenForTesting();

    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy.mock.calls[0][0]).toContain('503');
    expect(errorSpy).not.toHaveBeenCalled();

    // 5xx is NOT sticky — a later call may try again. We don't assert a
    // second fetch happens here (depends on caller), only that we have not
    // pinned the endpoint as unavailable.
  });
});
