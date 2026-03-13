import * as React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Button, buttonVariants } from '../src/components/ui/button';

describe('Button', () => {
  it('renders with default variant and size', () => {
    render(<Button>Click me</Button>);
    const button = screen.getByRole('button', { name: 'Click me' });
    expect(button).toBeInTheDocument();
    expect(button.tagName).toBe('BUTTON');
  });

  it('applies custom className', () => {
    render(<Button className="my-class">Test</Button>);
    const button = screen.getByRole('button');
    expect(button).toHaveClass('my-class');
  });

  it('handles click events', () => {
    const onClick = jest.fn();
    render(<Button onClick={onClick}>Click</Button>);
    fireEvent.click(screen.getByRole('button'));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('renders as disabled', () => {
    render(<Button disabled>Disabled</Button>);
    const button = screen.getByRole('button');
    expect(button).toBeDisabled();
  });

  it('supports type attribute', () => {
    render(<Button type="submit">Submit</Button>);
    const button = screen.getByRole('button');
    expect(button).toHaveAttribute('type', 'submit');
  });

  it('has correct displayName', () => {
    expect(Button.displayName).toBe('Button');
  });
});

describe('buttonVariants', () => {
  it('returns a string for default variant', () => {
    const classes = buttonVariants();
    expect(typeof classes).toBe('string');
    expect(classes.length).toBeGreaterThan(0);
  });

  it('returns different classes for destructive variant', () => {
    const defaultClasses = buttonVariants({ variant: 'default' });
    const destructiveClasses = buttonVariants({ variant: 'destructive' });
    expect(defaultClasses).not.toBe(destructiveClasses);
  });

  it('returns different classes for size sm', () => {
    const defaultClasses = buttonVariants({ size: 'default' });
    const smClasses = buttonVariants({ size: 'sm' });
    expect(defaultClasses).not.toBe(smClasses);
  });

  it('supports all variants without error', () => {
    const variants = ['default', 'destructive', 'outline', 'secondary', 'ghost', 'link'] as const;
    for (const variant of variants) {
      const classes = buttonVariants({ variant });
      expect(typeof classes).toBe('string');
    }
  });

  it('supports all sizes without error', () => {
    const sizes = ['default', 'sm', 'lg', 'icon'] as const;
    for (const size of sizes) {
      const classes = buttonVariants({ size });
      expect(typeof classes).toBe('string');
    }
  });
});
