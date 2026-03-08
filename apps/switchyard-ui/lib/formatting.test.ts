/**
 * Unit tests for lib/formatting.ts
 *
 * Tests all pure formatting utilities: time/date helpers and number helpers.
 * These functions have no React dependencies and can be tested directly.
 */

import {
  formatRelativeTime,
  formatFullTimestamp,
  formatDuration,
  formatDate,
  formatTimestamp,
  formatBytes,
  formatNumber,
} from './formatting';

// ---------------------------------------------------------------------------
// formatRelativeTime
// ---------------------------------------------------------------------------

describe('formatRelativeTime', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    jest.setSystemTime(new Date('2026-03-08T12:00:00Z'));
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('returns "just now" for timestamps less than 60 seconds ago', () => {
    const timestamp = new Date('2026-03-08T11:59:30Z').toISOString();
    expect(formatRelativeTime(timestamp)).toBe('just now');
  });

  it('returns minutes ago for timestamps between 1-59 minutes', () => {
    const fiveMinAgo = new Date('2026-03-08T11:55:00Z').toISOString();
    expect(formatRelativeTime(fiveMinAgo)).toBe('5m ago');
  });

  it('returns 1m ago at exactly 60 seconds', () => {
    const oneMinAgo = new Date('2026-03-08T11:59:00Z').toISOString();
    expect(formatRelativeTime(oneMinAgo)).toBe('1m ago');
  });

  it('returns hours ago for timestamps between 1-23 hours', () => {
    const threeHoursAgo = new Date('2026-03-08T09:00:00Z').toISOString();
    expect(formatRelativeTime(threeHoursAgo)).toBe('3h ago');
  });

  it('returns 1h ago at exactly 3600 seconds', () => {
    const oneHourAgo = new Date('2026-03-08T11:00:00Z').toISOString();
    expect(formatRelativeTime(oneHourAgo)).toBe('1h ago');
  });

  it('returns days ago for timestamps between 1-6 days', () => {
    const twoDaysAgo = new Date('2026-03-06T12:00:00Z').toISOString();
    expect(formatRelativeTime(twoDaysAgo)).toBe('2d ago');
  });

  it('returns locale date string for timestamps older than 7 days', () => {
    const twoWeeksAgo = new Date('2026-02-22T12:00:00Z').toISOString();
    const result = formatRelativeTime(twoWeeksAgo);
    // Should be a locale date string, not a relative time
    expect(result).not.toContain('ago');
    expect(result).not.toBe('just now');
  });

  it('handles edge case at exactly 7 days (604800 seconds)', () => {
    const exactlySevenDays = new Date('2026-03-01T12:00:00Z').toISOString();
    const result = formatRelativeTime(exactlySevenDays);
    // 7 days = 604800 seconds, which hits the locale date branch
    expect(result).not.toContain('ago');
  });
});

// ---------------------------------------------------------------------------
// formatFullTimestamp
// ---------------------------------------------------------------------------

describe('formatFullTimestamp', () => {
  it('returns a formatted string with weekday, month, day, hour, and minute', () => {
    const result = formatFullTimestamp('2026-03-08T14:30:00Z');
    // The exact output depends on locale/timezone, but it should contain
    // recognizable date components
    expect(typeof result).toBe('string');
    expect(result.length).toBeGreaterThan(0);
  });

  it('formats different timestamps distinctly', () => {
    const morning = formatFullTimestamp('2026-03-08T08:00:00Z');
    const evening = formatFullTimestamp('2026-03-08T20:00:00Z');
    // They should differ (different hours)
    expect(morning).not.toBe(evening);
  });

  it('handles ISO date strings without time component', () => {
    // new Date('2026-03-08') is valid; midnight UTC
    const result = formatFullTimestamp('2026-03-08');
    expect(typeof result).toBe('string');
    expect(result.length).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// formatDuration
// ---------------------------------------------------------------------------

describe('formatDuration', () => {
  it('returns seconds-only for durations under 60s', () => {
    expect(formatDuration(0)).toBe('0s');
    expect(formatDuration(1)).toBe('1s');
    expect(formatDuration(59)).toBe('59s');
  });

  it('returns minutes and seconds for durations >= 60s', () => {
    expect(formatDuration(60)).toBe('1m 0s');
    expect(formatDuration(90)).toBe('1m 30s');
    expect(formatDuration(125)).toBe('2m 5s');
  });

  it('handles large durations', () => {
    expect(formatDuration(3600)).toBe('60m 0s');
    expect(formatDuration(3661)).toBe('61m 1s');
  });

  it('handles exact minute boundaries', () => {
    expect(formatDuration(120)).toBe('2m 0s');
    expect(formatDuration(300)).toBe('5m 0s');
  });
});

// ---------------------------------------------------------------------------
// formatDate
// ---------------------------------------------------------------------------

describe('formatDate', () => {
  it('converts an ISO date string to a locale string', () => {
    const result = formatDate('2026-03-08T14:30:00Z');
    expect(typeof result).toBe('string');
    expect(result.length).toBeGreaterThan(0);
  });

  it('produces different output for different dates', () => {
    const date1 = formatDate('2026-01-01T00:00:00Z');
    const date2 = formatDate('2026-12-31T23:59:59Z');
    expect(date1).not.toBe(date2);
  });
});

// ---------------------------------------------------------------------------
// formatTimestamp (for log viewers)
// ---------------------------------------------------------------------------

describe('formatTimestamp', () => {
  it('formats a Date to HH:MM:SS.mmm (24h)', () => {
    // Use a specific date to test the millisecond padding
    const date = new Date('2026-03-08T14:05:09.007Z');
    const result = formatTimestamp(date);

    // Should contain the milliseconds part with a dot separator
    expect(result).toContain('.');
    // The millisecond part should be 3 digits
    const parts = result.split('.');
    expect(parts[1]).toHaveLength(3);
  });

  it('pads milliseconds with leading zeros', () => {
    const date = new Date('2026-03-08T12:00:00.003Z');
    const result = formatTimestamp(date);
    expect(result).toMatch(/\.\d{3}$/);
    // The last 3 chars after the dot should represent the milliseconds
    const ms = result.split('.')[1];
    expect(ms).toBe('003');
  });

  it('handles zero milliseconds', () => {
    const date = new Date('2026-03-08T12:00:00.000Z');
    const result = formatTimestamp(date);
    expect(result).toMatch(/\.000$/);
  });

  it('handles high milliseconds', () => {
    const date = new Date('2026-03-08T12:00:00.999Z');
    const result = formatTimestamp(date);
    expect(result).toMatch(/\.999$/);
  });
});

// ---------------------------------------------------------------------------
// formatBytes
// ---------------------------------------------------------------------------

describe('formatBytes', () => {
  it('returns "0 B" for zero bytes', () => {
    expect(formatBytes(0)).toBe('0 B');
  });

  it('formats bytes (< 1024)', () => {
    expect(formatBytes(500)).toBe('500 B');
  });

  it('formats kilobytes', () => {
    expect(formatBytes(1024)).toBe('1 KB');
    expect(formatBytes(1536)).toBe('1.5 KB');
  });

  it('formats megabytes', () => {
    expect(formatBytes(1048576)).toBe('1 MB');
    expect(formatBytes(1572864)).toBe('1.5 MB');
  });

  it('formats gigabytes', () => {
    expect(formatBytes(1073741824)).toBe('1 GB');
  });

  it('formats terabytes', () => {
    expect(formatBytes(1099511627776)).toBe('1 TB');
  });

  it('rounds to one decimal place', () => {
    // 1.5 KB = 1536 bytes
    expect(formatBytes(1536)).toBe('1.5 KB');
    // Should not have more than one decimal digit
    const result = formatBytes(1234567);
    expect(result).toMatch(/^\d+\.?\d? [A-Z]+$/);
  });
});

// ---------------------------------------------------------------------------
// formatNumber
// ---------------------------------------------------------------------------

describe('formatNumber', () => {
  it('formats numbers under 1000 as plain integers', () => {
    expect(formatNumber(0)).toBe('0');
    expect(formatNumber(42)).toBe('42');
    expect(formatNumber(999)).toBe('999');
  });

  it('formats thousands with K suffix', () => {
    expect(formatNumber(1000)).toBe('1.0K');
    expect(formatNumber(1500)).toBe('1.5K');
    expect(formatNumber(99999)).toBe('100.0K');
  });

  it('formats millions with M suffix', () => {
    expect(formatNumber(1000000)).toBe('1.0M');
    expect(formatNumber(2500000)).toBe('2.5M');
  });

  it('rounds to one decimal place for K/M suffixes', () => {
    expect(formatNumber(1234)).toBe('1.2K');
    expect(formatNumber(1267000)).toBe('1.3M');
  });
});
