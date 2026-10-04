import { render, screen } from '@testing-library/react';
import { UsageGauge } from './circular-gauge';

describe('UsageGauge missing readings and capacity', () => {
  it.each([null, NaN, Infinity, -1])('does not draw a healthy gauge for invalid usage %s', (used) => {
    const { container } = render(<UsageGauge used={used} limit={100} unit="GB" label="Storage" />);
    expect(screen.getByText('Unavailable')).toBeInTheDocument();
    expect(container.querySelector('svg')).toBeNull();
  });

  it.each([null, 0, NaN])('preserves usage without inventing utilization with limit %s', (limit) => {
    const { container } = render(<UsageGauge used={0} limit={limit} unit="GB" label="Storage" />);
    expect(screen.getByText('0 GB')).toBeInTheDocument();
    expect(screen.getByText('Utilization unavailable')).toBeInTheDocument();
    expect(container.querySelector('svg')).toBeNull();
  });

  it('retains explicit bytes support without interpreting GB values as bytes', () => {
    render(<UsageGauge used={1024} limit={2048} unit="bytes" label="Bytes" />);
    expect(screen.getByText('1 KB')).toBeInTheDocument();
    expect(screen.getByText('2 KB limit')).toBeInTheDocument();
  });
});
