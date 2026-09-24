import { describe, expect, it } from 'vitest';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';
import { goDeployment } from '../fixtures';

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

  // Contract: RollbackDeployment (deployment_handlers.go) reads no body and
  // answers {"message", "rolled_back_to": Deployment, "current_deployment": Deployment}.
  it('manifest() POSTs without a body and returns the rollback result', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        message: 'Deployment rolled back successfully',
        rolled_back_to: goDeployment('dep-prev'),
        current_deployment: goDeployment('dep-current'),
      }),
    );
    const client = newClient({ fetch });
    const out = await client.rollback.manifest('dep-current');
    expect(out.rolled_back_to.id).toBe('dep-prev');
    expect(out.current_deployment.id).toBe('dep-current');
    expect(out.message).toMatch(/rolled back/);
    expect(calls[0]!.method).toBe('POST');
    expect(calls[0]!.url).toBe(
      'https://api.enclii.test/v1/deployments/dep-current/rollback',
    );
    expect(calls[0]!.body).toBeNull();
  });

  it('manifest() throws instead of ignoring a target argument', async () => {
    const { fetch, calls } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    const manifest = client.rollback.manifest as unknown as (
      id: string,
      input: unknown,
    ) => Promise<unknown>;
    await expect(
      manifest.call(client.rollback, 'dep-current', { to_release: 'rel-5' }),
    ).rejects.toThrow(/rollback\.instant/);
    expect(calls).toHaveLength(0);
  });
});
