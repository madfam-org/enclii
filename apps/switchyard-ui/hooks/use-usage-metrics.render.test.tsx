import { renderHook, waitFor } from '@testing-library/react';
import { apiGet } from '@/lib/api';
import { useMetricByType } from './use-usage-metrics';

jest.mock('@/lib/api', () => ({ apiGet: jest.fn() }));
jest.mock('@/hooks/use-polling', () => ({ usePolling: jest.fn() }));

describe('useMetricByType meter availability', () => {
  it.each([true, false])('preserves unavailable=%s independently of a zero used field', async (unavailable) => {
    jest.mocked(apiGet).mockResolvedValue({ metrics: [{
      type: 'build', label: 'Build Minutes', used: 0, included: 500, unit: 'minutes', cost: 0,
      unavailable, note: 'Meter status', source: 'test-meter',
    }] });
    const { result } = renderHook(() => useMetricByType('build'));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current).toMatchObject({
      used: unavailable ? null : 0, percentage: unavailable ? null : 0,
      cost: unavailable ? null : 0, unavailable, note: 'Meter status', source: 'test-meter',
    });
  });

  it('does not invent a zero for an absent meter', async () => {
    jest.mocked(apiGet).mockResolvedValue({ metrics: [] });
    const { result } = renderHook(() => useMetricByType('build'));
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current).toMatchObject({ used: null, cost: null, percentage: null, unavailable: true });
  });
});
