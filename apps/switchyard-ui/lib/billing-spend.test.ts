import type { ProjectCostResponse } from '@/types/billing';
import { spendState, spendUnknownLabel } from './billing-spend';

function cost(overrides: Partial<ProjectCostResponse>): ProjectCostResponse {
  return {
    project_id: '00000000-0000-0000-0000-000000000001',
    period_start: '2026-10-01T00:00:00Z',
    period_end: '2026-11-01T00:00:00Z',
    total_cents: 0,
    group_by: 'service',
    ...overrides,
  } as ProjectCostResponse;
}

const metering = (status: 'ok' | 'not_running' | 'stale' | 'unknown') => ({
  status,
  detail: `aggregator ${status}`,
  last_heartbeat_at: null,
  last_aggregated_hour: null,
  project_usage_rows: 0,
  unaggregated_events: 0,
  stale_after_seconds: 3600,
  checked_at: '2026-10-18T17:00:00Z',
});

describe('spendState', () => {
  it('treats a zero with metering not running as unknown, not $0.00', () => {
    const state = spendState(cost({ spend_known: false, metering: metering('not_running') }));
    expect(state).toEqual({ known: false, status: 'not_running', detail: 'aggregator not_running' });
    expect(spendUnknownLabel(state)).toBe('Metering is not running');
  });

  it('flags stale metering, whose total is only a floor', () => {
    const state = spendState(cost({ total_cents: 1234, spend_known: false, metering: metering('stale') }));
    expect(state.known).toBe(false);
    expect(spendUnknownLabel(state)).toMatch(/stale/);
  });

  it('trusts the figure when metering is ok', () => {
    expect(spendState(cost({ spend_known: true, metering: metering('ok') }))).toEqual({ known: true });
    expect(spendUnknownLabel({ known: true })).toBeNull();
  });

  it('distrusts a non-ok metering block even if spend_known is missing', () => {
    expect(spendState(cost({ metering: metering('unknown') })).known).toBe(false);
  });

  it('shows the figure from older Waybill builds that send neither field', () => {
    expect(spendState(cost({ total_cents: 500 }))).toEqual({ known: true });
    expect(spendState(null)).toEqual({ known: true });
  });
});
