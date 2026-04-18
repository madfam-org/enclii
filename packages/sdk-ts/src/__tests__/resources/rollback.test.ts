import { describe, expect, it } from 'vitest';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('RollbackResource', () => {
  it('instant() posts to /services/{id}/rollback', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        message: 'rolled back',
        took_ms: 812,
        scaled_up: false,
        to_deployment_id: 'dep-prev',
        to_version: 41,
        from_version: 42,
        ready_replicas: 3,
        strategy: 'selector-flip',
        namespace: 'prod',
      }),
    );
    const client = newClient({ fetch });
    const out = await client.rollback.instant('svc-1', {
      target_deployment_id: 'dep-prev',
      reason: 'bad push',
      change_ticket_url: 'https://jira/CHG-123',
    });
    expect(calls[0]!.url).toContain('/services/svc-1/rollback');
    expect(out.to_version).toBe(41);
  });

  it('manifest() posts to /deployments/{id}/rollback', async () => {
    const { fetch, calls } = createStubFetch(
      () => new Response(null, { status: 204 }),
    );
    const client = newClient({ fetch });
    await client.rollback.manifest('dep-current', { to_release: 'rel-5' });
    expect(calls[0]!.method).toBe('POST');
    expect(calls[0]!.url).toContain('/deployments/dep-current/rollback');
    expect(JSON.parse(calls[0]!.body!)).toEqual({ to_release: 'rel-5' });
  });
});
