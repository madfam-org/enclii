import { describe, expect, it } from 'vitest';
import { isTerminal } from '../../resources/canary';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('CanaryResource', () => {
  it('isTerminal correctly identifies end states', () => {
    expect(isTerminal('succeeded')).toBe(true);
    expect(isTerminal('auto_rolled_back')).toBe(true);
    expect(isTerminal('manual_rolled_back')).toBe(true);
    expect(isTerminal('failed')).toBe(true);
    expect(isTerminal('pending')).toBe(false);
    expect(isTerminal('running')).toBe(false);
    expect(isTerminal('validating')).toBe(false);
    expect(isTerminal('promoting')).toBe(false);
  });

  it('start() posts to /services/{id}/canary with a clean payload', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ id: 'ro-1', state: 'pending' }, { status: 201 }),
    );
    const client = newClient({ fetch });
    await client.canary.start('svc-1', {
      digest: 'sha256:abc',
      percentage: 20,
      validation_window_minutes: 10,
    });
    expect(calls[0]!.url).toContain('/services/svc-1/canary');
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      digest: 'sha256:abc',
      percentage: 20,
      validation_window_minutes: 10,
    });
  });

  it('promote() posts an empty body', async () => {
    const { fetch, calls } = createStubFetch(
      () => new Response(null, { status: 202 }),
    );
    const client = newClient({ fetch });
    await client.canary.promote('svc-1', 'ro-1');
    expect(calls[0]!.method).toBe('POST');
    expect(calls[0]!.url).toContain('/services/svc-1/canary/ro-1/promote');
  });

  it('rollback() includes the reason when supplied', async () => {
    const { fetch, calls } = createStubFetch(
      () => new Response(null, { status: 202 }),
    );
    const client = newClient({ fetch });
    await client.canary.rollback('svc-1', 'ro-1', { reason: 'error rate spike' });
    expect(JSON.parse(calls[0]!.body!)).toEqual({ reason: 'error rate spike' });
  });

  it('wait() resolves at terminal state', async () => {
    let n = 0;
    const { fetch } = createStubFetch(() => {
      n++;
      if (n < 2) return jsonResponse({ id: 'ro', state: 'validating' });
      return jsonResponse({ id: 'ro', state: 'succeeded' });
    });
    const client = newClient({ fetch });
    const out = await client.canary.wait('svc-1', 'ro', {
      intervalMs: 1,
      timeoutMs: 5_000,
    });
    expect(out.state).toBe('succeeded');
  });
});
