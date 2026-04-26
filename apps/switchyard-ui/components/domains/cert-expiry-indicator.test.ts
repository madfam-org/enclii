/**
 * Unit tests for components/domains/cert-expiry-indicator.tsx
 *
 * Covers the pure `describeCertExpiry` color-bucket helper. The React
 * component is rendered via the parent page (no @testing-library/react in
 * this app); this file isolates the bucket logic so we can pin every edge
 * of the threshold transitions.
 */

import { describeCertExpiry } from './cert-expiry-indicator';

const NOW = new Date('2026-04-26T12:00:00Z').getTime();
const DAY = 86_400_000;

function inDays(days: number): string {
  return new Date(NOW + days * DAY).toISOString();
}

describe('describeCertExpiry — null / invalid', () => {
  it('returns "unknown" when expiresAt is null', () => {
    const r = describeCertExpiry(null, NOW);
    expect(r.label).toBe('unknown');
    expect(r.tone).toBe('unknown');
    expect(r.toneClass).toContain('muted');
  });

  it('returns "unknown" when expiresAt is undefined', () => {
    const r = describeCertExpiry(undefined, NOW);
    expect(r.tone).toBe('unknown');
  });

  it('returns "unknown" when expiresAt is not a parseable date', () => {
    const r = describeCertExpiry('not-a-date', NOW);
    expect(r.tone).toBe('unknown');
    expect(r.label).toBe('unknown');
  });
});

describe('describeCertExpiry — expired', () => {
  it('returns "expired" red for past timestamps', () => {
    const r = describeCertExpiry(inDays(-1), NOW);
    expect(r.label).toBe('expired');
    expect(r.tone).toBe('critical');
    expect(r.toneClass).toContain('error');
  });

  it('returns "expired" red exactly at the now boundary (diffMs <= 0)', () => {
    const r = describeCertExpiry(new Date(NOW).toISOString(), NOW);
    expect(r.tone).toBe('critical');
    expect(r.label).toBe('expired');
  });
});

describe('describeCertExpiry — critical (<7d)', () => {
  it('returns red for 1d remaining', () => {
    const r = describeCertExpiry(inDays(1), NOW);
    expect(r.tone).toBe('critical');
    expect(r.label).toBe('in 1d');
    expect(r.toneClass).toContain('error');
  });

  it('returns red with "in <1d" when less than 1 full day remains', () => {
    // 12 hours from now → diffDays = 0 → still critical
    const r = describeCertExpiry(new Date(NOW + 12 * 3600_000).toISOString(), NOW);
    expect(r.tone).toBe('critical');
    expect(r.label).toBe('in <1d');
  });

  it('returns red at the 6-day mark (just under the 7d threshold)', () => {
    const r = describeCertExpiry(inDays(6), NOW);
    expect(r.tone).toBe('critical');
    expect(r.label).toBe('in 6d');
  });
});

describe('describeCertExpiry — warning (7-30d)', () => {
  it('returns amber at exactly 7 days', () => {
    const r = describeCertExpiry(inDays(7), NOW);
    expect(r.tone).toBe('warning');
    expect(r.label).toBe('in 7d');
    expect(r.toneClass).toContain('warning');
  });

  it('returns amber at 29 days (just under the 30d threshold)', () => {
    const r = describeCertExpiry(inDays(29), NOW);
    expect(r.tone).toBe('warning');
    expect(r.label).toBe('in 29d');
  });
});

describe('describeCertExpiry — ok (>30d)', () => {
  it('returns green at exactly 30 days', () => {
    const r = describeCertExpiry(inDays(30), NOW);
    expect(r.tone).toBe('ok');
    expect(r.label).toBe('in 30d');
    expect(r.toneClass).toContain('success');
  });

  it('returns green with day label below 90d', () => {
    const r = describeCertExpiry(inDays(60), NOW);
    expect(r.tone).toBe('ok');
    expect(r.label).toBe('in 60d');
  });

  it('returns green with month label at/over 90d', () => {
    const r = describeCertExpiry(inDays(120), NOW);
    expect(r.tone).toBe('ok');
    expect(r.label).toBe('in 4mo');
  });
});
