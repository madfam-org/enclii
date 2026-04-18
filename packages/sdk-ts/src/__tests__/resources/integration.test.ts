/**
 * Integration-style test: exercise the full client against a stub server
 * (not a real listening HTTP server — just a deterministic fetch handler that
 * behaves like the real API). This catches composition bugs that unit tests
 * on individual resources miss.
 */

import { describe, expect, it } from 'vitest';
import { createStubFetch, jsonResponse, newClient } from '../test-helpers';

function stubServer() {
  // Very small in-memory fixture modelling projects + services + deployments.
  const projects: Record<string, unknown> = {
    acme: { id: 'p1', slug: 'acme', name: 'Acme' },
  };
  const services: Record<string, unknown> = {
    'svc-1': { id: 'svc-1', project_id: 'p1', name: 'api' },
  };
  const versions: Record<string, unknown> = {
    '42': {
      id: 'dep-42',
      version_number: 42,
      status: 'running',
      health: 'healthy',
    },
  };

  return createStubFetch(async (call) => {
    const url = new URL(call.url);
    const p = url.pathname.replace(/^\/v1/, '');

    if (call.method === 'GET' && p === '/projects/acme') {
      return jsonResponse(projects.acme);
    }
    if (call.method === 'GET' && p === '/projects/acme/services') {
      return jsonResponse({
        services: Object.values(services),
        next_cursor: null,
      });
    }
    if (call.method === 'GET' && p === '/services/svc-1/versions/42') {
      return jsonResponse(versions['42']);
    }
    if (call.method === 'POST' && p === '/services/svc-1/canary') {
      return jsonResponse(
        {
          id: 'ro-1',
          state: 'pending',
          canary_digest: 'sha256:abc',
          canary_percentage: 20,
          total_replicas: 5,
          canary_replicas: 1,
          stable_replicas: 4,
          validation_window_seconds: 600,
          error_rate_threshold: 0.05,
          service_id: 'svc-1',
        },
        { status: 201 },
      );
    }
    if (
      call.method === 'GET' &&
      p === '/services/svc-1/canary/ro-1'
    ) {
      return jsonResponse({
        id: 'ro-1',
        state: 'succeeded',
        canary_percentage: 20,
        total_replicas: 5,
        canary_replicas: 1,
        stable_replicas: 4,
        validation_window_seconds: 600,
        error_rate_threshold: 0.05,
        service_id: 'svc-1',
      });
    }
    return new Response(
      JSON.stringify({ error: `not stubbed: ${call.method} ${p}` }),
      {
        status: 404,
        headers: { 'content-type': 'application/json' },
      },
    );
  });
}

describe('integration', () => {
  it('end-to-end: project → service → deployment by v-number', async () => {
    const { fetch } = stubServer();
    const client = newClient({ fetch });

    const project = await client.projects.get('acme');
    expect(project).toMatchObject({ slug: 'acme' });

    const services = await client.services.list('acme');
    expect(services.data).toHaveLength(1);

    const dep = await client.deployments.get('svc-1', 'v42');
    expect(dep.version_number).toBe(42);
    expect(dep.status).toBe('running');
  });

  it('end-to-end: start canary → wait for success', async () => {
    const { fetch } = stubServer();
    const client = newClient({ fetch });

    const rollout = await client.canary.start('svc-1', {
      digest: 'sha256:abc',
      percentage: 20,
    });
    expect(rollout.state).toBe('pending');

    const final = await client.canary.wait('svc-1', rollout.id, {
      intervalMs: 1,
      timeoutMs: 2_000,
    });
    expect(final.state).toBe('succeeded');
  });
});
