import { render, screen, within } from '@testing-library/react';
import { apiGet } from '@/lib/api';
import { useIsAdminScope } from '@/contexts/ScopeContext';
import type { UsageSummary } from '@/hooks/use-usage-metrics';
import { UsageOverview } from './usage-overview';
import { UsageMeters } from '@/components/usage/usage-meters';

jest.mock('@/lib/api', () => ({ apiGet: jest.fn() }));
jest.mock('@/contexts/ScopeContext', () => ({ useIsAdminScope: jest.fn() }));
jest.mock('@/hooks/use-polling', () => ({ usePolling: jest.fn() }));

const get = jest.mocked(apiGet);
const adminScope = jest.mocked(useIsAdminScope);

// Same unit/unavailable contract as GET /v1/usage; values are fixtures.
function usageFixture(): UsageSummary {
  return {
    period_start: '2026-01-01', period_end: '2026-01-31', plan_name: 'Test',
    total_cost: 0, plan_base: 20, grand_total: 20,
    metrics: [
      { type: 'compute', label: 'Compute', used: 0, included: 100, unit: 'GB-hours', cost: 0 },
      { type: 'storage', label: 'Storage', used: 12.5, included: 10, unit: 'GB', cost: 0.5 },
      { type: 'bandwidth', label: 'Bandwidth', used: 27.25, included: 100, unit: 'GB', cost: 0 },
      { type: 'build', label: 'Build Minutes', used: 0, included: 500, unit: 'minutes', cost: 0,
        unavailable: true, note: 'Build meter could not be read', source: 'test-meter' },
    ],
  };
}

function mockUsage(data = usageFixture()) {
  get.mockImplementation(async (url) => url === '/v1/usage' ? data : {
    metrics_enabled: false, total_cpu_millicores: 0, total_memory_mb: 0,
    total_pods: 0, services: [], collected_at: '2026-01-01T00:00:00Z',
  });
}

describe('usage telemetry presentation', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockUsage();
  });

  it.each(['full', 'compact'] as const)('shows API units and unread meters in admin %s tiles', async (variant) => {
    adminScope.mockReturnValue(true);
    render(<UsageOverview variant={variant} />);

    expect(await screen.findByText('12.5 GB')).toBeInTheDocument();
    expect(screen.getByText('27.25 GB')).toBeInTheDocument();
    expect(screen.getByText('0 GB-hours')).toBeInTheDocument();
    expect(screen.getByText('Unavailable')).toBeInTheDocument();
    expect(screen.getByText('Build meter could not be read')).toBeInTheDocument();
    expect(screen.queryByText('0 minutes')).not.toBeInTheDocument();
    expect(screen.queryByText('12.5 B')).not.toBeInTheDocument();
  });

  it.each(['full', 'compact'] as const)('shows API units and no false zero gauge in tenant %s view', async (variant) => {
    adminScope.mockReturnValue(false);
    render(<UsageOverview variant={variant} />);

    expect(await screen.findByText('12.5 GB')).toBeInTheDocument();
    expect(screen.getByText('27.25 GB')).toBeInTheDocument();
    expect(screen.getByText('125% — 2.5 GB over 10 GB')).toBeInTheDocument();
    expect(screen.getByText('100 GB limit')).toBeInTheDocument();
    const build = screen.getByText('Build Minutes').parentElement!;
    expect(within(build).getByText('Unavailable')).toBeInTheDocument();
    expect(build.querySelector('svg')).toBeNull();
    expect(screen.queryByText('0 minutes')).not.toBeInTheDocument();
    if (variant === 'full') {
      expect(screen.getByText('Some usage meters are unavailable.')).toBeInTheDocument();
      expect(screen.queryByText('$20.00')).not.toBeInTheDocument();
    }
  });

  it.each([true, false])('preserves a genuinely measured zero (admin=%s)', async (admin) => {
    adminScope.mockReturnValue(admin);
    const data = usageFixture();
    data.metrics[3] = { ...data.metrics[3], unavailable: false, note: undefined };
    mockUsage(data);
    render(<UsageOverview />);

    expect(await screen.findByText('0 minutes')).toBeInTheDocument();
    expect(screen.queryByText('Unavailable')).not.toBeInTheDocument();
  });

  it('renders project usage meters without an unread progress bar or false overage total', async () => {
    render(<UsageMeters projectId="test-project" />);

    expect(await screen.findByText('12.5 GB')).toBeInTheDocument();
    const build = screen.getByText('Build Minutes').closest('.space-y-2')!;
    expect(within(build as HTMLElement).getByText('Unavailable')).toBeInTheDocument();
    expect(within(build as HTMLElement).queryByRole('progressbar')).not.toBeInTheDocument();
    expect(screen.getByText('Overage unavailable')).toBeInTheDocument();
  });
});
