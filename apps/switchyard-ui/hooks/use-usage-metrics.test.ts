/**
 * Unit tests for the pure helper function exported from use-usage-metrics.ts
 *
 * The React hooks in this file (useUsageMetrics, useMetricByType, etc.)
 * require @testing-library/react renderHook which is not currently installed.
 * This file tests only the pure `getUsageColor` function.
 */

import { getUsageColor } from './use-usage-metrics';

describe('getUsageColor', () => {
  it('returns "success" for usage below 75%', () => {
    expect(getUsageColor(0)).toBe('success');
    expect(getUsageColor(50)).toBe('success');
    expect(getUsageColor(74)).toBe('success');
    expect(getUsageColor(74.9)).toBe('success');
  });

  it('returns "warning" for usage between 75% and 89%', () => {
    expect(getUsageColor(75)).toBe('warning');
    expect(getUsageColor(80)).toBe('warning');
    expect(getUsageColor(89)).toBe('warning');
    expect(getUsageColor(89.9)).toBe('warning');
  });

  it('returns "danger" for usage at or above 90%', () => {
    expect(getUsageColor(90)).toBe('danger');
    expect(getUsageColor(95)).toBe('danger');
    expect(getUsageColor(100)).toBe('danger');
  });

  it('handles edge case at exactly 75%', () => {
    expect(getUsageColor(75)).toBe('warning');
  });

  it('handles edge case at exactly 90%', () => {
    expect(getUsageColor(90)).toBe('danger');
  });
});
