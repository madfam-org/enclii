import type { ProjectCostResponse } from '@/types/billing';
import {
  alertDelivery,
  compareToBudget,
  comparisonLabel,
  coverageLine,
  parseThresholds,
  spendState,
  spendUnknownLabel,
} from './billing-spend';

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

const budget = (o: Partial<{ amount_cents: number; currency: string; period: string }> = {}) => ({
  amount_cents: 10000,
  currency: 'USD',
  period: 'monthly',
  ...o,
});

describe('compareToBudget', () => {
  it('compares month-to-date spend with a monthly USD budget', () => {
    const c = compareToBudget(budget(), cost({ total_cents: 2500, spend_known: true, metering: metering('ok') }));
    expect(c).toEqual({ kind: 'pct', pct: 25 });
    expect(comparisonLabel(c)).toBe('25% used this month');
  });

  it('caps the printed percentage', () => {
    const c = compareToBudget(budget(), cost({ total_cents: 99999999 }));
    expect(comparisonLabel(c)).toBe('Over 300% used');
  });

  it('never computes a percentage over unmeasured spend', () => {
    const c = compareToBudget(budget(), cost({ spend_known: false, metering: metering('not_running') }));
    expect(c).toEqual({ kind: 'unknown' });
    expect(comparisonLabel(c)).toBe('Usage against budget unknown');
    expect(compareToBudget(budget(), null)).toEqual({ kind: 'unknown' });
  });

  it('does not compare a budget in another currency with USD spend', () => {
    const c = compareToBudget(budget({ currency: 'mxn' }), cost({ total_cents: 2500 }));
    expect(c).toEqual({ kind: 'currency', budgetCurrency: 'MXN' });
    expect(comparisonLabel(c)).toMatch(/budget is in MXN, spend is metered in USD/);
  });

  it('does not compare a weekly or quarterly budget with a month-to-date read', () => {
    const c = compareToBudget(budget({ period: 'weekly' }), cost({ total_cents: 2500 }));
    expect(c).toEqual({ kind: 'period', period: 'weekly' });
    expect(comparisonLabel(c)).toMatch(/this budget is weekly/);
  });
});

describe('coverageLine', () => {
  it('names the source, the window and the last aggregated hour', () => {
    const line = coverageLine(
      cost({ metering: { ...metering('ok'), last_aggregated_hour: '2026-10-18T16:00:00Z' } }),
    );
    expect(line).toMatch(/^Source: Waybill · /);
    expect(line).toMatch(/\(UTC\)/);
    expect(line).toMatch(/aggregated through .* UTC$/);
  });

  it('is empty without a window', () => {
    expect(coverageLine(null)).toBe('');
  });
});

describe('parseThresholds', () => {
  it('keeps valid percentages sorted and unique', () => {
    expect(parseThresholds('80, 50,100,50')).toEqual({ values: [50, 80, 100], invalid: [] });
  });

  it('reports entries it cannot use instead of dropping them', () => {
    expect(parseThresholds('50, abc, 0, 12.5, 400')).toEqual({
      values: [50],
      invalid: ['abc', '0', '12.5', '400'],
    });
  });
});

describe('alertDelivery', () => {
  it('distinguishes delivered, pending and failed alerts', () => {
    expect(alertDelivery({ dispatched_at: '2026-10-18T17:00:00Z', dispatch_attempts: 1 })).toEqual({
      tone: 'success',
      label: 'Delivered',
    });
    expect(alertDelivery({ dispatch_attempts: 0 })).toEqual({ tone: 'warning', label: 'Pending' });
    expect(alertDelivery({ dispatch_attempts: 3, last_error: 'timeout' })).toEqual({
      tone: 'error',
      label: 'Not delivered (3 attempts)',
    });
  });
});
