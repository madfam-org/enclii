import { formatDate, formatRelativeTime, formatCompact, formatBytes } from '../src/utils/format';

describe('formatDate', () => {
  it('returns "Never" for null', () => {
    expect(formatDate(null)).toBe('Never');
  });

  it('returns "Never" for empty string', () => {
    expect(formatDate('')).toBe('Never');
  });

  it('formats a valid ISO date string', () => {
    const result = formatDate('2025-06-15T14:30:00Z');
    expect(result).toContain('2025');
    expect(result).toContain('Jun');
    expect(result).toContain('15');
  });
});

describe('formatRelativeTime', () => {
  it('returns "just now" for recent timestamps', () => {
    const now = new Date().toISOString();
    expect(formatRelativeTime(now)).toBe('just now');
  });

  it('returns "just now" for future dates', () => {
    const future = new Date(Date.now() + 60000).toISOString();
    expect(formatRelativeTime(future)).toBe('just now');
  });

  it('returns minutes for < 1 hour', () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString();
    expect(formatRelativeTime(fiveMinAgo)).toBe('5m ago');
  });

  it('returns hours for < 1 day', () => {
    const threeHoursAgo = new Date(Date.now() - 3 * 3600 * 1000).toISOString();
    expect(formatRelativeTime(threeHoursAgo)).toBe('3h ago');
  });

  it('returns days for < 1 week', () => {
    const twoDaysAgo = new Date(Date.now() - 2 * 86400 * 1000).toISOString();
    expect(formatRelativeTime(twoDaysAgo)).toBe('2d ago');
  });

  it('returns formatted date for > 1 week', () => {
    const twoWeeksAgo = new Date(Date.now() - 14 * 86400 * 1000).toISOString();
    const result = formatRelativeTime(twoWeeksAgo);
    // Should fall back to formatDate
    expect(result).not.toContain('ago');
  });
});

describe('formatCompact', () => {
  it('formats small numbers as-is', () => {
    expect(formatCompact(5)).toBe('5');
  });

  it('formats thousands with K suffix', () => {
    const result = formatCompact(1500);
    expect(result).toContain('K');
  });

  it('formats millions with M suffix', () => {
    const result = formatCompact(2500000);
    expect(result).toContain('M');
  });

  it('handles zero', () => {
    expect(formatCompact(0)).toBe('0');
  });
});

describe('formatBytes', () => {
  it('returns "0 Bytes" for zero', () => {
    expect(formatBytes(0)).toBe('0 Bytes');
  });

  it('formats bytes', () => {
    expect(formatBytes(500)).toBe('500 Bytes');
  });

  it('formats kilobytes', () => {
    const result = formatBytes(1536);
    expect(result).toContain('KB');
  });

  it('formats megabytes', () => {
    const result = formatBytes(1048576);
    expect(result).toBe('1 MB');
  });

  it('formats gigabytes', () => {
    const result = formatBytes(1610612736);
    expect(result).toContain('GB');
  });

  it('respects decimal precision', () => {
    const result = formatBytes(1536, 0);
    expect(result).toBe('2 KB');
  });
});
