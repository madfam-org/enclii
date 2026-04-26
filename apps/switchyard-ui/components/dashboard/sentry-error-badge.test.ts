/**
 * Unit tests for components/dashboard/sentry-error-badge.tsx
 *
 * Tests the pure helpers (sentryTone, sentryLabel, sentryTooltip). The
 * React component itself is exercised via the dashboard E2E flow — this
 * app intentionally does not pull in @testing-library/react.
 *
 * Coverage: the colour-threshold contract from parity audit gap #9 lives
 * here and is the single source of truth for the badge's visual states.
 */

import {
  sentryTone,
  sentryLabel,
  sentryTooltip,
} from './sentry-error-badge';
import type { SentryStats } from '@/hooks/use-sentry-stats';

function stats(partial: Partial<SentryStats>): SentryStats {
  return {
    configured: true,
    error_count: 0,
    stats_period: '24h',
    org_slug: 'innovaciones-madfam-sas-de-cv',
    sentry_project_slug: 'switchyard-api',
    ...partial,
  } as SentryStats;
}

describe('sentryTone', () => {
  it('uses success tone for zero errors', () => {
    const t = sentryTone(stats({ error_count: 0 }));
    expect(t.text).toContain('success');
    expect(t.dot).toContain('success');
  });

  it('uses success tone for low error counts (< 10)', () => {
    const t = sentryTone(stats({ error_count: 9 }));
    expect(t.text).toContain('success');
  });

  it('uses warning tone at the warning threshold (10)', () => {
    const t = sentryTone(stats({ error_count: 10 }));
    expect(t.text).toContain('warning');
    expect(t.dot).toContain('warning');
  });

  it('uses warning tone in the warning band (10..99)', () => {
    const t = sentryTone(stats({ error_count: 50 }));
    expect(t.text).toContain('warning');
  });

  it('uses error tone at the error threshold (100)', () => {
    const t = sentryTone(stats({ error_count: 100 }));
    expect(t.text).toContain('error');
    expect(t.dot).toContain('error');
  });

  it('uses error tone for very high counts', () => {
    const t = sentryTone(stats({ error_count: 5000 }));
    expect(t.text).toContain('error');
  });

  it('uses muted tone for no_sentry_project reason', () => {
    const t = sentryTone(stats({ reason: 'no_sentry_project', error_count: null }));
    expect(t.text).toContain('muted');
    expect(t.dot).toContain('muted');
  });

  it('uses muted tone when error_count is null without a reason', () => {
    const t = sentryTone(stats({ error_count: null }));
    expect(t.text).toContain('muted');
  });
});

describe('sentryLabel', () => {
  it('renders "0 errors / 24h" for zero count', () => {
    expect(sentryLabel(stats({ error_count: 0 }))).toBe('0 errors / 24h');
  });

  it('singularises at exactly 1 error', () => {
    expect(sentryLabel(stats({ error_count: 1 }))).toBe('1 error / 24h');
  });

  it('renders compact form for low counts', () => {
    expect(sentryLabel(stats({ error_count: 12 }))).toBe('12 errors / 24h');
  });

  it('uses "+" suffix at 100..999', () => {
    expect(sentryLabel(stats({ error_count: 300 }))).toBe('300+ errors / 24h');
  });

  it('caps display at 999+ for pathological counts', () => {
    expect(sentryLabel(stats({ error_count: 9999 }))).toBe('999+ errors / 24h');
  });

  it('honours custom stats_period in the label', () => {
    expect(sentryLabel(stats({ error_count: 5, stats_period: '7d' }))).toBe(
      '5 errors / 7d',
    );
  });

  it('shows "no Sentry project" when reason is no_sentry_project', () => {
    expect(
      sentryLabel(stats({ reason: 'no_sentry_project', error_count: null })),
    ).toBe('no Sentry project');
  });
});

describe('sentryTooltip', () => {
  it('mentions the count and window for the happy path', () => {
    const t = sentryTooltip(stats({ error_count: 12 }), 'switchyard-api');
    expect(t).toContain('12');
    expect(t).toContain('24h');
    expect(t).toContain('switchyard-api');
  });

  it('singularises in the tooltip for count=1', () => {
    const t = sentryTooltip(stats({ error_count: 1 }), 'svc');
    expect(t).toContain('1 error in last');
  });

  it('explains the override path when no Sentry project exists', () => {
    const t = sentryTooltip(
      stats({ reason: 'no_sentry_project', error_count: null }),
      'svc',
    );
    expect(t).toContain('Sentry project not found');
    expect(t).toContain('sentry_project_slug');
  });
});
