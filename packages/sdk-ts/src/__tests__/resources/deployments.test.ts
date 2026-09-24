import { describe, expect, it } from 'vitest';
import { parseVersionLabel } from '../../resources/deployments';
import { goDeployment, goRelease } from '../fixtures';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('DeploymentsResource', () => {
  it('parses Heroku-style v-labels', () => {
    expect(parseVersionLabel('v42')).toBe(42);
    expect(parseVersionLabel('V7')).toBe(7);
    expect(parseVersionLabel('  v1  ')).toBe(1);
    expect(parseVersionLabel('v0')).toBeNull();
    expect(parseVersionLabel('v-5')).toBeNull();
    expect(parseVersionLabel('42')).toBeNull();
    expect(parseVersionLabel('vabc')).toBeNull();
    expect(parseVersionLabel('')).toBeNull();
  });

  it('resolves a deployment by v-label via the service/versions endpoint', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        id: 'dep-abc',
        version_number: 42,
        status: 'running',
      }),
    );
    const client = newClient({ fetch });
    await client.deployments.get('svc-1', 'v42');
    expect(calls[0]!.url).toContain('/services/svc-1/versions/42');
  });

  it('rejects bogus v-labels with a clear error', async () => {
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    await expect(
      client.deployments.get('svc-1', 'vbad'),
    ).rejects.toThrow(/invalid v-label/);
  });

  it('fetches a deployment by UUID using the legacy /deployments path', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ id: 'dep-abc', status: 'running' }),
    );
    const client = newClient({ fetch });
    await client.deployments.get('dep-abc');
    expect(calls[0]!.url).toContain('/deployments/dep-abc');
  });

  // Contract: GetLatestDeployment (deployment_handlers.go) answers
  // {"deployment": {...}, "release": {...}}, dropping "release" when the
  // release lookup fails.
  it('latest() GETs /deployments/latest and unwraps the deployment', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ deployment: goDeployment('dep-9'), release: goRelease('rel-1') }),
    );
    const client = newClient({ fetch });
    const dep = await client.deployments.latest('svc-1');
    expect(dep).toEqual(goDeployment('dep-9'));
    expect(dep.status).toBe('running');
    expect(calls[0]!.method).toBe('GET');
    expect(calls[0]!.url).toBe(
      'https://api.enclii.test/v1/services/svc-1/deployments/latest',
    );
  });

  it('latest() also parses the wrapper without a release', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({ deployment: goDeployment('dep-9') }),
    );
    const client = newClient({ fetch });
    expect((await client.deployments.latest('svc-1')).id).toBe('dep-9');
  });

  // Contract: ListServiceDeployments answers
  // {"service_id", "deployments": [...], "count"} with every row.
  it('list() reads deployments and sends no paging params', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({
        service_id: 'svc-1',
        deployments: [goDeployment('d2'), goDeployment('d1')],
        count: 2,
      }),
    );
    const client = newClient({ fetch });
    const page = await client.deployments.list('svc-1', { limit: 1 });
    expect(page.data.map((d) => d.id)).toEqual(['d2', 'd1']);
    expect(page.nextCursor).toBeNull();
    expect(calls[0]!.url).toBe('https://api.enclii.test/v1/services/svc-1/deployments');
  });

  it('calls deploy with the correct body', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse({ id: 'dep-new', status: 'pending' }, { status: 201 }),
    );
    const client = newClient({ fetch });
    await client.deployments.deploy('svc-1', {
      release_id: 'rel-1',
      environment_name: 'production',
      change_ticket_url: 'https://tickets.example.com/CHG-1',
    });
    expect(calls[0]!.method).toBe('POST');
    expect(JSON.parse(calls[0]!.body!)).toEqual({
      release_id: 'rel-1',
      environment_name: 'production',
      change_ticket_url: 'https://tickets.example.com/CHG-1',
    });
  });

  it('wait() polls until the deployment is terminal', async () => {
    let n = 0;
    const { fetch } = createStubFetch(() => {
      n++;
      if (n < 3) {
        return jsonResponse({ id: 'd', status: 'deploying', health: 'unknown' });
      }
      return jsonResponse({ id: 'd', status: 'running', health: 'healthy' });
    });
    const client = newClient({ fetch });
    const dep = await client.deployments.wait('d', {
      intervalMs: 1,
      timeoutMs: 5_000,
    });
    expect(dep.status).toBe('running');
    expect(n).toBeGreaterThanOrEqual(3);
  });

  it('wait() honors timeout', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({ id: 'd', status: 'deploying', health: 'unknown' }),
    );
    const client = newClient({ fetch });
    await expect(
      client.deployments.wait('d', { intervalMs: 1, timeoutMs: 5 }),
    ).rejects.toThrow(/timed out/);
  });
});
