/**
 * Unit tests for components/dashboard/last-sync-badge.tsx
 *
 * Tests the pure `describeFreshness` helper. The React component
 * itself is exercised manually + via the dashboard E2E flow (no
 * @testing-library/react in this app).
 */

import { describeFreshness } from './last-sync-badge';

const NOW = new Date('2026-04-26T12:00:00Z').getTime();

function tSecondsAgo(seconds: number): string {
  return new Date(NOW - seconds * 1000).toISOString();
}

describe('describeFreshness', () => {
  it('returns "never synced" when lastSyncedAt is null', () => {
    const r = describeFreshness(null, 60, NOW);
    expect(r.label).toBe('never synced');
    expect(r.toneClass).toContain('muted');
  });

  it('returns "never synced" when lastSyncedAt is invalid', () => {
    const r = describeFreshness('not-a-date', 60, NOW);
    expect(r.label).toBe('never synced');
  });

  it('returns "synced just now" for sub-5s recency', () => {
    const r = describeFreshness(tSecondsAgo(2), 60, NOW);
    expect(r.label).toBe('synced just now');
    expect(r.toneClass).toContain('success');
  });

  it('returns "synced Ns ago" for sub-minute recency', () => {
    const r = describeFreshness(tSecondsAgo(15), 60, NOW);
    expect(r.label).toBe('synced 15s ago');
    expect(r.toneClass).toContain('success');
  });

  it('returns "synced Nm ago" for minute-level recency', () => {
    const r = describeFreshness(tSecondsAgo(120), 300, NOW);
    expect(r.label).toBe('synced 2m ago');
  });

  it('returns "synced Nh ago" for hour-level recency', () => {
    const r = describeFreshness(tSecondsAgo(7200), 86400, NOW);
    expect(r.label).toBe('synced 2h ago');
  });

  it('flips to "stale ..." when older than the staleAfter threshold', () => {
    const r = describeFreshness(tSecondsAgo(90), 60, NOW);
    expect(r.label).toBe('stale 1m ago');
    expect(r.toneClass).toContain('warning');
  });

  it('uses success tone exactly at the freshness boundary', () => {
    // diffSec = 59, threshold = 60 → not stale yet
    const r = describeFreshness(tSecondsAgo(59), 60, NOW);
    expect(r.label).toBe('synced 59s ago');
    expect(r.toneClass).toContain('success');
  });

  it('uses warning tone exactly at the staleness boundary', () => {
    // diffSec = 60, threshold = 60 → first moment of stale
    const r = describeFreshness(tSecondsAgo(60), 60, NOW);
    expect(r.label).toBe('stale 1m ago');
    expect(r.toneClass).toContain('warning');
  });

  it('clamps negative diffs to zero (clock skew defense)', () => {
    const future = new Date(NOW + 30_000).toISOString();
    const r = describeFreshness(future, 60, NOW);
    // 0 seconds → "just now"
    expect(r.label).toBe('synced just now');
  });
});
