import { describe, expect, it } from 'vitest';
import {
  AnonymousAuth,
  StaticTokenAuth,
  TokenProviderAuth,
  resolveAuthStrategy,
} from '../auth';

describe('auth', () => {
  it('StaticTokenAuth returns the static value', async () => {
    const s = new StaticTokenAuth('abc');
    expect(await s.getToken()).toBe('abc');
  });

  it('StaticTokenAuth with null returns null', async () => {
    const s = new StaticTokenAuth(null);
    expect(await s.getToken()).toBeNull();
  });

  it('TokenProviderAuth invokes the provider each call', async () => {
    let count = 0;
    const p = new TokenProviderAuth(async () => `t-${++count}`);
    expect(await p.getToken()).toBe('t-1');
    expect(await p.getToken()).toBe('t-2');
  });

  it('AnonymousAuth returns null', async () => {
    const a = new AnonymousAuth();
    expect(await a.getToken()).toBeNull();
  });

  it('resolveAuthStrategy handles null/undefined', async () => {
    expect(await resolveAuthStrategy(null).getToken()).toBeNull();
    expect(await resolveAuthStrategy(undefined).getToken()).toBeNull();
  });

  it('resolveAuthStrategy handles strings', async () => {
    expect(await resolveAuthStrategy('token').getToken()).toBe('token');
  });

  it('resolveAuthStrategy handles provider functions', async () => {
    const s = resolveAuthStrategy(async () => 'lazy');
    expect(await s.getToken()).toBe('lazy');
  });

  it('resolveAuthStrategy passes through existing strategies', async () => {
    const strategy = new StaticTokenAuth('xyz');
    expect(resolveAuthStrategy(strategy)).toBe(strategy);
  });
});
