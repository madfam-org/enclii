import * as React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { StationIdentity } from '../src/components/ui/station-identity';

describe('StationIdentity', () => {
  it.each([
    ['switchyard', 'Enclii', 'Switchyard Platform'],
    ['dispatch', 'DISPATCH', 'Infrastructure Control Tower'],
  ] as const)('preserves the %s surface name and hides decorative ASCII', (surface, title, description) => {
    const { container } = render(<StationIdentity surface={surface} />);
    expect(screen.getByRole('heading', { level: 1, name: title })).toBeInTheDocument();
    expect(screen.getByText(description)).toBeInTheDocument();
    for (const art of container.querySelectorAll('pre')) {
      expect(art.closest('[aria-hidden="true"]')).not.toBeNull();
      expect(art.textContent).toMatch(/^[\x20-\x7e\n]+$/);
    }
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('fits inside a home link without creating another heading or control', () => {
    render(<a href="/"><StationIdentity surface="switchyard" variant="compact" /></a>);
    expect(screen.getByRole('link', { name: 'Enclii Switchyard' })).toHaveAttribute('href', '/');
    expect(screen.queryByRole('heading')).not.toBeInTheDocument();
  });
});
