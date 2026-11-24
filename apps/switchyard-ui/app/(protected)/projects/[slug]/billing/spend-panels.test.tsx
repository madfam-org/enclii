import { render, screen } from '@testing-library/react';
import type { ProjectBudget, ProjectCostResponse } from '@/types/billing';
import { compareToBudget } from '@/lib/billing-spend';
import { AlertHistory, DailySpendChart, SpendHero, TopDrivers } from './spend-panels';

const metering = (status: 'ok' | 'not_running' | 'stale') => ({
  status,
  detail: '',
  last_heartbeat_at: null,
  last_aggregated_hour: null,
  project_usage_rows: 0,
  unaggregated_events: 0,
  stale_after_seconds: 3600,
  checked_at: '2026-11-08T12:00:00Z',
});

function cost(o: Partial<ProjectCostResponse>): ProjectCostResponse {
  return {
    project_id: '00000000-0000-0000-0000-000000000001',
    period_start: '2026-11-01T00:00:00Z',
    period_end: '2026-11-08T12:00:00Z',
    total_cents: 0,
    group_by: 'service',
    ...o,
  } as ProjectCostResponse;
}

const budget: ProjectBudget = {
  id: 'b1',
  project_id: '00000000-0000-0000-0000-000000000001',
  amount_cents: 10000,
  currency: 'USD',
  period: 'monthly',
  alert_thresholds: [50, 80, 100],
  hard_throttle: true,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
};

const notRunning = cost({ spend_known: false, metering: metering('not_running') });

describe('SpendHero', () => {
  it('shows unmetered spend as unknown with no 0% bar', () => {
    render(<SpendHero monthToDate={notRunning} budget={budget} comparison={compareToBudget(budget, notRunning)} />);
    expect(screen.getByTestId('spend-unknown')).toHaveTextContent('Unknown');
    expect(screen.getByText('Metering is not running')).toBeInTheDocument();
    expect(screen.getByTestId('budget-comparison')).toHaveTextContent('Usage against budget unknown');
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
    expect(screen.getByTestId('budget-bar-unmeasured')).toBeInTheDocument();
    expect(screen.queryByText('$0.00')).not.toBeInTheDocument();
  });

  it('shows stale spend as a lower bound', () => {
    const stale = cost({ total_cents: 1234, spend_known: false, metering: metering('stale') });
    render(<SpendHero monthToDate={stale} budget={null} comparison={null} />);
    expect(screen.getByTestId('spend-unknown')).toHaveTextContent('At least $12.34');
  });

  it('shows a measured figure with a progress bar and its coverage', () => {
    const ok = cost({ total_cents: 2500, spend_known: true, metering: metering('ok') });
    render(<SpendHero monthToDate={ok} budget={budget} comparison={compareToBudget(budget, ok)} />);
    expect(screen.getByText('$25.00')).toBeInTheDocument();
    expect(screen.getByRole('progressbar', { name: 'Budget used this month' })).toHaveAttribute(
      'aria-valuenow',
      '25',
    );
    expect(screen.getByText(/^Source: Waybill/)).toBeInTheDocument();
  });

  it('formats spend in USD and does not compare it with a budget in another currency', () => {
    const mxn = { ...budget, currency: 'MXN' };
    const ok = cost({ total_cents: 2500, spend_known: true, metering: metering('ok') });
    render(<SpendHero monthToDate={ok} budget={mxn} comparison={compareToBudget(mxn, ok)} />);
    expect(screen.getByText('$25.00')).toBeInTheDocument();
    expect(screen.getByTestId('budget-comparison')).toHaveTextContent(/budget is in MXN/);
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
  });
});

describe('DailySpendChart and TopDrivers', () => {
  it('say metering is not running instead of drawing zeros or "no usage"', () => {
    render(
      <>
        <DailySpendChart series={notRunning} rangeLabel="Last 30 days" />
        <TopDrivers cost={notRunning} rangeLabel="Last 30 days" />
      </>,
    );
    expect(screen.getByText(/Daily spend can.t be shown: metering is not running/)).toBeInTheDocument();
    expect(screen.getByText(/Cost drivers can.t be shown: metering is not running/)).toBeInTheDocument();
    expect(screen.queryByText('No usage recorded in this period.')).not.toBeInTheDocument();
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
  });

  it('describe a measured series for assistive tech', () => {
    const series = cost({
      spend_known: true,
      metering: metering('ok'),
      series: [
        { bucket: '2026-11-01T00:00:00Z', cost_cents: 100 },
        { bucket: '2026-11-02T00:00:00Z', cost_cents: 300 },
      ],
    });
    render(<DailySpendChart series={series} rangeLabel="Last 14 days" />);
    expect(screen.getByRole('img').getAttribute('aria-label')).toMatch(/\$4\.00 over 2 days, highest \$3\.00/);
  });

  it('keep the empty state for a measured, empty period', () => {
    const ok = cost({ spend_known: true, metering: metering('ok'), breakdown: [] });
    render(<TopDrivers cost={ok} rangeLabel="Last 30 days" />);
    expect(screen.getByText('No usage recorded in this period.')).toBeInTheDocument();
  });
});

describe('AlertHistory', () => {
  it('labels a failed delivery instead of "Pending"', () => {
    render(
      <AlertHistory
        budgets={[budget]}
        alerts={[
          {
            id: 'a1',
            budget_id: 'b1',
            project_id: budget.project_id,
            period_start: '2026-11-01T00:00:00Z',
            period_end: '2026-12-01T00:00:00Z',
            threshold: 80,
            actual_cents: 8000,
            budget_cents: 10000,
            dispatched_at: null,
            dispatch_attempts: 2,
            last_error: 'timeout',
            created_at: '2026-11-07T00:00:00Z',
          },
        ]}
      />,
    );
    expect(screen.getByText('Not delivered (2 attempts)')).toBeInTheDocument();
  });
});
