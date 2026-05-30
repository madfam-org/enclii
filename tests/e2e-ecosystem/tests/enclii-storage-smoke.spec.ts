import { test, expect } from '@playwright/test';

/**
 * Persistent volume / service storage tests (Commercial GA bet C).
 *
 * Smoke: service settings and PATCH routes require authentication.
 * Opt-in: round-trip volumes on a service via STORAGE_E2E_* env vars.
 */

const API_BASE_URL = process.env.API_BASE_URL || 'https://api.enclii.dev';
const STORAGE_E2E_TOKEN = process.env.STORAGE_E2E_TOKEN;
const STORAGE_E2E_SERVICE_ID = process.env.STORAGE_E2E_SERVICE_ID;
const STORAGE_E2E_RELEASE_ID = process.env.STORAGE_E2E_RELEASE_ID;
const STORAGE_E2E_ENVIRONMENT_NAME = process.env.STORAGE_E2E_ENVIRONMENT_NAME || 'production';

const DEPLOY_POLL_MS = 5000;
const DEPLOY_TIMEOUT_MS = 120_000;

const fakeServiceId = '00000000-0000-4000-8000-000000000002';

function authHeaders(token: string) {
  return {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
  };
}

type ReleaseRow = { id?: string; status?: string };

async function listReadyReleases(
  request: import('@playwright/test').APIRequestContext,
  serviceId: string,
  token: string,
): Promise<ReleaseRow[]> {
  const response = await request.get(`${API_BASE_URL}/v1/services/${serviceId}/releases`, {
    headers: authHeaders(token),
    failOnStatusCode: false,
  });
  expect(response.status()).toBe(200);
  const body = await response.json();
  const releases = (body.releases ?? []) as ReleaseRow[];
  return releases.filter((r) => String(r.status ?? '') === 'ready' && r.id);
}

async function deployWithFreshRelease(
  request: import('@playwright/test').APIRequestContext,
  serviceId: string,
  token: string,
  preferredReleaseId: string,
  environmentName: string,
) {
  const headers = authHeaders(token);
  const candidates = await listReadyReleases(request, serviceId, token);
  const ordered = [
    preferredReleaseId,
    ...candidates.map((r) => r.id as string).filter((id) => id !== preferredReleaseId),
  ];

  let lastStatus = 0;
  let lastBody = '';
  for (const releaseId of ordered) {
    const deployResponse = await request.post(`${API_BASE_URL}/v1/services/${serviceId}/deploy`, {
      headers,
      data: {
        release_id: releaseId,
        environment_name: environmentName,
      },
      failOnStatusCode: false,
    });
    lastStatus = deployResponse.status();
    lastBody = await deployResponse.text();
    if ([200, 201].includes(lastStatus)) {
      return { deployResponse, releaseId, deployment: JSON.parse(lastBody) };
    }
    // UNIQUE (release_id, environment_id) — try the next ready release.
    if (lastStatus !== 422) {
      break;
    }
  }

  throw new Error(
    `Deploy failed (last status ${lastStatus}): ${lastBody.slice(0, 500)}`,
  );
}

async function pollDeploymentRunning(
  request: import('@playwright/test').APIRequestContext,
  deploymentId: string,
  token: string,
) {
  const deadline = Date.now() + DEPLOY_TIMEOUT_MS;
  while (Date.now() < deadline) {
    const response = await request.get(`${API_BASE_URL}/v1/deployments/${deploymentId}`, {
      headers: authHeaders(token),
      failOnStatusCode: false,
    });
    if (response.status() === 200) {
      const body = await response.json();
      const status = String(body.status ?? '');
      const health = String(body.health ?? '');
      if (status === 'failed') {
        throw new Error(`Deployment failed: ${JSON.stringify(body)}`);
      }
      if (status === 'running' && health === 'healthy') {
        return body;
      }
    }
    await new Promise((r) => setTimeout(r, DEPLOY_POLL_MS));
  }
  throw new Error(`Deployment ${deploymentId} did not reach running/healthy within ${DEPLOY_TIMEOUT_MS}ms`);
}

test.describe('Service storage API smoke (always-on)', () => {
  test('GET /v1/services/:id/settings requires authentication', async ({ request }) => {
    const response = await request.get(
      `${API_BASE_URL}/v1/services/${fakeServiceId}/settings`,
      { failOnStatusCode: false },
    );

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });

  test('PATCH /v1/services/:id requires authentication', async ({ request }) => {
    const response = await request.patch(`${API_BASE_URL}/v1/services/${fakeServiceId}`, {
      failOnStatusCode: false,
      data: { volumes: [] },
    });

    expect(response.status()).not.toBe(502);
    expect(response.status()).not.toBe(503);
    expect([401, 403, 404]).toContain(response.status());
  });
});

test.describe('Service volumes round-trip (staging opt-in)', () => {
  test.skip(!STORAGE_E2E_TOKEN || !STORAGE_E2E_SERVICE_ID, 'Set STORAGE_E2E_TOKEN and STORAGE_E2E_SERVICE_ID');

  test('patch volumes and read back via settings', async ({ request }) => {
    const token = STORAGE_E2E_TOKEN!;
    const serviceId = STORAGE_E2E_SERVICE_ID!;
    const headers = authHeaders(token);

    const volumes = [
      {
        name: 'e2e-data',
        mount_path: '/data/e2e',
        size: '1Gi',
        storage_class_name: 'longhorn',
        access_mode: 'ReadWriteOnce',
      },
    ];

    const patchResponse = await request.patch(`${API_BASE_URL}/v1/services/${serviceId}`, {
      headers,
      data: { volumes },
    });
    expect(patchResponse.status()).toBeLessThan(500);
    expect([200, 204]).toContain(patchResponse.status());

    const settingsResponse = await request.get(
      `${API_BASE_URL}/v1/services/${serviceId}/settings`,
      { headers },
    );
    expect(settingsResponse.status()).toBe(200);

    const body = await settingsResponse.json();
    const saved = body.settings?.volumes ?? [];
    expect(Array.isArray(saved)).toBeTruthy();
    expect(saved.some((v: { name?: string }) => v.name === 'e2e-data')).toBeTruthy();

    await request.patch(`${API_BASE_URL}/v1/services/${serviceId}`, {
      headers,
      data: { volumes: [] },
    });
  });
});

test.describe('Stateful deploy with volumes (staging opt-in)', () => {
  test.skip(
    !STORAGE_E2E_TOKEN ||
      !STORAGE_E2E_SERVICE_ID ||
      !STORAGE_E2E_RELEASE_ID,
    'Set STORAGE_E2E_TOKEN, STORAGE_E2E_SERVICE_ID, and STORAGE_E2E_RELEASE_ID',
  );

  test('patch volumes → deploy → running/healthy', async ({ request }) => {
    test.setTimeout(DEPLOY_TIMEOUT_MS + 30_000);

    const token = STORAGE_E2E_TOKEN!;
    const serviceId = STORAGE_E2E_SERVICE_ID!;
    const releaseId = STORAGE_E2E_RELEASE_ID!;
    const headers = authHeaders(token);

    const volumes = [
      {
        name: 'e2e-deploy-data',
        mount_path: '/data/e2e-deploy',
        size: '1Gi',
        storage_class_name: 'longhorn',
        access_mode: 'ReadWriteOnce',
      },
    ];

    const patchResponse = await request.patch(`${API_BASE_URL}/v1/services/${serviceId}`, {
      headers,
      data: { volumes },
    });
    expect(patchResponse.status()).toBeLessThan(500);
    expect([200, 204]).toContain(patchResponse.status());

    const { deployment } = await deployWithFreshRelease(
      request,
      serviceId,
      token,
      releaseId,
      STORAGE_E2E_ENVIRONMENT_NAME,
    );
    const deploymentId = deployment.id as string;
    expect(deploymentId).toBeTruthy();

    await pollDeploymentRunning(request, deploymentId, token);

    await request.patch(`${API_BASE_URL}/v1/services/${serviceId}`, {
      headers,
      data: { volumes: [] },
    });
  });
});
