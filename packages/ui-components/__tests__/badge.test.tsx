import * as React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Badge, badgeVariants } from '../src/components/ui/badge';

describe('Badge', () => {
  it('renders with children', () => {
    render(<Badge>Active</Badge>);
    expect(screen.getByText('Active')).toBeInTheDocument();
  });

  it('renders as a div element', () => {
    render(<Badge data-testid="badge">Test</Badge>);
    const badge = screen.getByTestId('badge');
    expect(badge.tagName).toBe('DIV');
  });

  it('applies custom className', () => {
    render(<Badge className="extra-class" data-testid="badge">Test</Badge>);
    const badge = screen.getByTestId('badge');
    expect(badge).toHaveClass('extra-class');
  });

  it('passes through HTML attributes', () => {
    render(<Badge data-testid="badge" title="Status badge">Test</Badge>);
    const badge = screen.getByTestId('badge');
    expect(badge).toHaveAttribute('title', 'Status badge');
  });
});

describe('badgeVariants', () => {
  it('returns a string for default variant', () => {
    const classes = badgeVariants();
    expect(typeof classes).toBe('string');
    expect(classes.length).toBeGreaterThan(0);
  });

  it('supports all variants without error', () => {
    const variants = ['default', 'secondary', 'destructive', 'outline', 'success', 'warning', 'info'] as const;
    for (const variant of variants) {
      const classes = badgeVariants({ variant });
      expect(typeof classes).toBe('string');
    }
  });

  it('returns different classes for different variants', () => {
    const defaultClasses = badgeVariants({ variant: 'default' });
    const successClasses = badgeVariants({ variant: 'success' });
    expect(defaultClasses).not.toBe(successClasses);
  });
});
