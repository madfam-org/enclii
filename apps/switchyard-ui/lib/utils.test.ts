/**
 * Unit tests for lib/utils.ts
 *
 * Tests the `cn` utility function which merges Tailwind CSS classes
 * using clsx + tailwind-merge.
 */

import { cn } from './utils';

describe('cn (class name merge)', () => {
  it('returns an empty string when called with no arguments', () => {
    expect(cn()).toBe('');
  });

  it('passes through a single class string', () => {
    expect(cn('text-red-500')).toBe('text-red-500');
  });

  it('merges multiple class strings', () => {
    const result = cn('p-4', 'text-center');
    expect(result).toContain('p-4');
    expect(result).toContain('text-center');
  });

  it('handles conditional classes via clsx syntax', () => {
    const isActive = true;
    const isDisabled = false;
    const result = cn('base', isActive && 'active', isDisabled && 'disabled');
    expect(result).toContain('base');
    expect(result).toContain('active');
    expect(result).not.toContain('disabled');
  });

  it('resolves Tailwind conflicts (last wins)', () => {
    // tailwind-merge should resolve p-4 vs p-2 to keep p-2
    const result = cn('p-4', 'p-2');
    expect(result).toBe('p-2');
    expect(result).not.toContain('p-4');
  });

  it('resolves conflicting text colors', () => {
    const result = cn('text-red-500', 'text-blue-500');
    expect(result).toBe('text-blue-500');
  });

  it('handles undefined and null gracefully', () => {
    const result = cn('base', undefined, null, 'extra');
    expect(result).toContain('base');
    expect(result).toContain('extra');
  });

  it('handles object syntax from clsx', () => {
    const result = cn('base', { 'text-bold': true, 'text-italic': false });
    expect(result).toContain('base');
    expect(result).toContain('text-bold');
    expect(result).not.toContain('text-italic');
  });

  it('handles array syntax from clsx', () => {
    const result = cn(['p-4', 'mt-2']);
    expect(result).toContain('p-4');
    expect(result).toContain('mt-2');
  });
});
