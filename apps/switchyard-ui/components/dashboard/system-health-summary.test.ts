/**
 * Unit tests for components/dashboard/system-health-summary.tsx
 *
 * Truthfulness audit (2026-05-04): the dashboard's System Health card was
 * observed stuck on "Loading…" indefinitely. The fix is a hard timeout so
 * the loading state always resolves into either real data or a clear
 * "Health check timed out" error within a bounded window.
 *
 * The component itself uses React state and a timer hook, so these tests
 * exercise the pure helpers (timeout-message formatter + render-state
 * decision) which encode the truthfulness contract. The component-level
 * timer behaviour is exercised by the dashboard E2E flow.
 */

import {
  SYSTEM_HEALTH_LOAD_TIMEOUT_MS,
  systemHealthTimeoutMessage,
  systemHealthRenderState,
} from './system-health-summary';

describe('SYSTEM_HEALTH_LOAD_TIMEOUT_MS', () => {
  it('is bounded above by 60s — operator UX requires definite resolution', () => {
    // The truthfulness contract is "loading state always resolves inside
    // ~30s". 40s is the chosen value (35s apiGet timeout + 5s headroom);
    // we test against an upper bound rather than an exact value so future
    // tuning within the safe window doesn't break the test.
    expect(SYSTEM_HEALTH_LOAD_TIMEOUT_MS).toBeLessThanOrEqual(60_000);
  });

  it('is bounded below by the apiGet client timeout (35s) — earlier and we trip on every slow first load', () => {
    expect(SYSTEM_HEALTH_LOAD_TIMEOUT_MS).toBeGreaterThanOrEqual(35_000);
  });
});

describe('systemHealthTimeoutMessage', () => {
  it('mentions a number of seconds the operator can act on', () => {
    const msg = systemHealthTimeoutMessage(40_000);
    expect(msg).toContain('40s');
    expect(msg.toLowerCase()).toContain('timed out');
  });

  it('uses "system may be unhealthy" — not "loading", not "broken"', () => {
    // Operators were observed reading "Loading…" as "the API is gone" —
    // we want a phrase that's actionable but not alarmist. "may be
    // unhealthy" is the chosen wording.
    const msg = systemHealthTimeoutMessage(40_000);
    expect(msg).toContain('system may be unhealthy');
    expect(msg).not.toContain('Loading');
  });

  it('rounds non-integer seconds for legibility', () => {
    const msg = systemHealthTimeoutMessage(40_400);
    expect(msg).toContain('40s'); // 40.4 → 40
  });
});

describe('systemHealthRenderState', () => {
  it("returns 'error' when an error is set, regardless of other state", () => {
    expect(
      systemHealthRenderState({
        error: 'boom',
        loading: true,
        hasData: true,
      }),
    ).toBe('error');
  });

  it("returns 'loading' on initial mount (no data, no error, loading=true)", () => {
    expect(
      systemHealthRenderState({
        error: null,
        loading: true,
        hasData: false,
      }),
    ).toBe('loading');
  });

  it("returns 'data' once a fetch has populated state", () => {
    expect(
      systemHealthRenderState({
        error: null,
        loading: false,
        hasData: true,
      }),
    ).toBe('data');
  });

  it("returns 'empty' if the timeout fires after a fetch produced nothing", () => {
    // The truthfulness guard sets loading=false even when the first fetch
    // never resolved with data — the render path needs an explicit empty
    // state rather than falling back to "loading".
    expect(
      systemHealthRenderState({
        error: null,
        loading: false,
        hasData: false,
      }),
    ).toBe('empty');
  });
});
